package zlogsentry

import (
	"crypto/x509"
	"io"
	"net/http"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/ast"
	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

//nolint:gochecknoglobals
var levelsMapping = map[zerolog.Level]sentry.Level{
	zerolog.DebugLevel: sentry.LevelDebug,
	zerolog.InfoLevel:  sentry.LevelInfo,
	zerolog.WarnLevel:  sentry.LevelWarning,
	zerolog.ErrorLevel: sentry.LevelError,
	zerolog.FatalLevel: sentry.LevelFatal,
	zerolog.PanicLevel: sentry.LevelFatal,
}

// GetSentryLevel returns the corresponding sentry Level - or debug if not matching.
func GetSentryLevel(level zerolog.Level) sentry.Level {
	if sLevel, ok := levelsMapping[level]; ok {
		return sLevel
	}

	return sentry.LevelDebug
}

var _ = io.WriteCloser(new(Writer))

// Writer is a sentry events writer with std io.Writer iface.
type Writer struct {
	hub *sentry.Hub

	levels          map[zerolog.Level]struct{}
	flushTimeout    time.Duration
	withBreadcrumbs bool

	// for testing
	now func() time.Time
}

// New creates writer with provided DSN and options.
func New(dsn string, opts ...WriterOption) (*Writer, error) {
	cfg := newDefaultConfig()
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		SampleRate:       cfg.sampleRate,
		Release:          cfg.release,
		Environment:      cfg.environment,
		ServerName:       cfg.serverName,
		IgnoreErrors:     cfg.ignoreErrors,
		Debug:            cfg.debug,
		EnableTracing:    cfg.tracing,
		DebugWriter:      cfg.debugWriter,
		HTTPClient:       cfg.httpClient,
		HTTPProxy:        cfg.httpProxy,
		HTTPSProxy:       cfg.httpsProxy,
		CaCerts:          cfg.caCerts,
		MaxErrorDepth:    cfg.maxErrorDepth,
		BeforeSend:       cfg.beforeSend,
		TracesSampleRate: cfg.tracesSampleRate,
		AttachStacktrace: cfg.attachStacktrace,
	})
	if err != nil {
		//nolint: wrapcheck
		return nil, err
	}

	levels := make(map[zerolog.Level]struct{}, len(cfg.levels))
	for _, lvl := range cfg.levels {
		levels[lvl] = struct{}{}
	}

	return &Writer{
		hub:             sentry.CurrentHub(),
		levels:          levels,
		flushTimeout:    cfg.flushTimeout,
		withBreadcrumbs: cfg.breadcrumbs,
		now:             cfg.now,
	}, nil
}

// NewWithHub creates a writer using an existing sentry Hub and options.
//
// constructors kept with option helpers for readability.
func NewWithHub(hub *sentry.Hub, opts ...WriterOption) (*Writer, error) {
	if hub == nil {
		return nil, ErrHubCannotBeNil
	}

	cfg := newDefaultConfig()
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	levels := make(map[zerolog.Level]struct{}, len(cfg.levels))
	for _, lvl := range cfg.levels {
		levels[lvl] = struct{}{}
	}

	return &Writer{
		hub:             hub,
		levels:          levels,
		flushTimeout:    cfg.flushTimeout,
		withBreadcrumbs: cfg.breadcrumbs,
		now:             cfg.now,
	}, nil
}

// Write handles zerolog's json and sends events to sentry.
func (w *Writer) Write(data []byte) (int, error) {
	n := len(data)

	lvl, err := w.InternalParseLogLevel(data)
	if err != nil {
		// inore event if we cannot parse it
		//nolint:nilerr
		return n, nil
	}

	event, ok := w.InternalParseLogEvent(data)
	if !ok {
		return n, nil
	}

	event.Level, ok = levelsMapping[lvl]
	if !ok {
		return n, nil
	}

	if _, enabled := w.levels[lvl]; !enabled {
		// if the level is not enabled, add event as a breadcrumb
		w.addBreadcrumb(event)
		return n, nil
	}

	w.hub.CaptureEvent(event)
	// should flush before os.Exit
	if event.Level == sentry.LevelFatal {
		w.hub.Flush(w.flushTimeout)
	}

	return n, nil
}

// WriteLevel implements zerolog.LevelWriter.
func (w *Writer) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	n := len(p)

	event, ok := w.InternalParseLogEvent(p)
	if !ok {
		// ignore unparseable event
		return n, nil
	}

	event.Level, ok = levelsMapping[level]
	if !ok {
		// ignore event with no known level
		return n, nil
	}

	if _, enabled := w.levels[level]; !enabled {
		// if the level is not enabled, add event as a breadcrumb
		w.addBreadcrumb(event)

		return n, nil
	}

	w.hub.CaptureEvent(event)
	// should flush before os.Exit
	if event.Level == sentry.LevelFatal {
		w.hub.Flush(w.flushTimeout)
	}

	return n, nil
}

// Close forces client to flush all pending events.
// Can be useful before application exits.
func (w *Writer) Close() error {
	if ok := w.hub.Flush(w.flushTimeout); !ok {
		return ErrFlushTimeout
	}

	return nil
}

// InternalParseLogLevel parses the log level from the encoded log.
func (w *Writer) InternalParseLogLevel(data []byte) (zerolog.Level, error) {
	lvlNode, err := sonic.Get(data, zerolog.LevelFieldName)
	if err != nil {
		// inore event if we cannot parse it
		//nolint:nilerr
		return zerolog.Disabled, nil
	}

	lvlStr, err := lvlNode.String()
	if err != nil {
		// inore event if we cannot parse it
		//nolint:nilerr
		return zerolog.Disabled, nil
	}

	//nolint:wrapcheck
	return zerolog.ParseLevel(lvlStr)
}

// InternalParseLogEvent parses the event except the log level - intenal use only.
func (w *Writer) InternalParseLogEvent(data []byte) (*sentry.Event, bool) {
	const logger = "zerolog"

	event := sentry.Event{
		Timestamp: w.now(),
		Logger:    logger,
		Extra:     map[string]any{},
	}

	rootNode, err := sonic.Get(data)
	if err != nil {
		return nil, false
	}

	var value ast.Pair

	for iter, err := rootNode.Properties(); iter.Next(&value); {
		if err != nil {
			return nil, false
		}

		switch value.Key {
		case zerolog.MessageFieldName:
			event.Message, err = value.Value.String()
			if err != nil {
				return nil, false
			}
		case zerolog.ErrorFieldName:
			content, err := value.Value.String()
			if err != nil {
				return nil, false
			}

			event.Exception = append(event.Exception, sentry.Exception{
				Value:      content,
				Stacktrace: newStacktrace(),
			})
		case zerolog.LevelFieldName, zerolog.TimestampFieldName:
		default:
			content, err := value.Value.String()
			if err != nil {
				// it might be an embedded json => skip this entry
				continue
			}

			event.Extra[value.Key] = content
		}
	}

	return &event, true
}

func newStacktrace() *sentry.Stacktrace {
	const (
		module       = "github.com/krombel/zerolog-sentry"
		loggerModule = "github.com/rs/zerolog"
	)

	st := sentry.NewStacktrace()

	threshold := len(st.Frames) - 1
	// drop current module frames
	for ; threshold > 0 && st.Frames[threshold].Module == module; threshold-- {
	}

outer:
	// try to drop zerolog module frames after logger call point
	for i := threshold; i > 0; i-- {
		if st.Frames[i].Module == loggerModule {
			for j := i - 1; j >= 0; j-- {
				if st.Frames[j].Module != loggerModule {
					threshold = j
					break outer
				}
			}

			break
		}
	}

	st.Frames = st.Frames[:threshold+1]

	return st
}

// WriterOption configures sentry events writer.
type WriterOption interface {
	apply(cfg *config)
}

type optionFunc func(*config)

func (fn optionFunc) apply(c *config) { fn(c) }

type EventHintCallback func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event

type config struct {
	levels           []zerolog.Level
	sampleRate       float64
	release          string
	environment      string
	serverName       string
	ignoreErrors     []string
	breadcrumbs      bool
	debug            bool
	tracing          bool
	debugWriter      io.Writer
	httpClient       *http.Client
	httpProxy        string
	httpsProxy       string
	caCerts          *x509.CertPool
	maxErrorDepth    int
	flushTimeout     time.Duration
	beforeSend       sentry.EventProcessor
	tracesSampleRate float64
	attachStacktrace bool
	now              func() time.Time
}

// WithLevels configures zerolog levels that have to be sent to Sentry.
// Default levels are: error, fatal, panic.
func WithLevels(levels ...zerolog.Level) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.levels = levels
	})
}

// WithSampleRate configures the sample rate as a percentage of events to be sent in the range of 0.0 to 1.0.
func WithSampleRate(rate float64) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.sampleRate = rate
	})
}

// WithRelease configures the release to be sent with events.
func WithRelease(release string) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.release = release
	})
}

// WithEnvironment configures the environment to be sent with events.
func WithEnvironment(environment string) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.environment = environment
	})
}

// WithServerName configures the server name field for events. Default value is OS hostname.
func WithServerName(serverName string) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.serverName = serverName
	})
}

// WithIgnoreErrors configures the list of regexp strings that will be used to match against event's message
// and if applicable, caught errors type and value. If the match is found, then a whole event will be dropped.
func WithIgnoreErrors(reList []string) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.ignoreErrors = reList
	})
}

// WithBreadcrumbs enables sentry client breadcrumbs.
func WithBreadcrumbs() WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.breadcrumbs = true
	})
}

// WithDebug enables sentry client debug logs.
func WithDebug() WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.debug = true
	})
}

// WithTracing enables sentry client tracing.
func WithTracing() WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.tracing = true
	})
}

// WithTracingSampleRate sets tracing sample rate.
func WithTracingSampleRate(tsr float64) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.tracesSampleRate = tsr
	})
}

// WithAttachStacktrace enabled AttachStacktrace.
func WithAttachStacktrace() WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.attachStacktrace = true
	})
}

// WithBeforeSend sets a callback which is called before event is sent.
func WithBeforeSend(beforeSend sentry.EventProcessor) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.beforeSend = beforeSend
	})
}

// WithDebugWriter enables sentry client tracing.
func WithDebugWriter(w io.Writer) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.debugWriter = w
	})
}

// WithHttpClient sets custom http client.
func WithHttpClient(httpClient *http.Client) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.httpClient = httpClient
	})
}

// WithHttpProxy enables sentry client tracing.
func WithHttpProxy(proxy string) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.httpProxy = proxy
	})
}

// WithHttpsProxy enables sentry client tracing.
func WithHttpsProxy(proxy string) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.httpsProxy = proxy
	})
}

// WithCaCerts enables sentry client tracing.
func WithCaCerts(caCerts *x509.CertPool) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.caCerts = caCerts
	})
}

// WithMaxErrorDepth sets the max depth of error chain.
func WithMaxErrorDepth(maxErrorDepth int) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.maxErrorDepth = maxErrorDepth
	})
}

// WithFixedTimeStamp sets the timestamp to a fixed value - for testing only.
func WithFixedTimeStamp(ts time.Time) WriterOption {
	return optionFunc(func(cfg *config) {
		cfg.now = func() time.Time { return ts }
	})
}

// addBreadcrumb adds event as a breadcrumb.
func (w *Writer) addBreadcrumb(event *sentry.Event) {
	if !w.withBreadcrumbs {
		return
	}

	// category is totally optional, but it's nice to have
	var category string

	if _, ok := event.Extra["category"]; ok {
		if v, ok := event.Extra["category"].(string); ok {
			category = v
		}
	}

	w.hub.AddBreadcrumb(&sentry.Breadcrumb{
		Category: category,
		Message:  event.Message,
		Level:    event.Level,
		Data:     event.Extra,
	}, nil)
}

func newDefaultConfig() config {
	return config{
		levels: []zerolog.Level{
			zerolog.ErrorLevel,
			zerolog.FatalLevel,
			zerolog.PanicLevel,
		},
		sampleRate:   1.0,
		flushTimeout: 3 * time.Second, //nolint:mnd
		now:          time.Now,
	}
}
