package zlogsentry_test

import (
	"io"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	zlogsentry "github.com/krombel/zerolog-sentry"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:lll,gochecknoglobals
var logEventJSON = []byte(`{"level":"error","requestId":"bee07485-2485-4f64-99e1-d10165884ca7","error":"dial timeout","time":"2020-06-25T17:19:00+03:00","message":"test message"}`)

func TestInternalParseLogEvent(t *testing.T) {
	ts := time.Now()

	w, err := zlogsentry.New("", zlogsentry.WithFixedTimeStamp(ts))
	require.NoError(t, err)

	ev, ok := w.InternalParseLogEvent(logEventJSON)
	require.True(t, ok)
	zLevel, err := w.InternalParseLogLevel(logEventJSON)
	require.NoError(t, err)
	ev.Level = zlogsentry.GetSentryLevel(zLevel)
	assert.NotEqual(t, sentry.LevelDebug, ev.Level)

	assert.Equal(t, ts, ev.Timestamp)
	assert.Equal(t, sentry.LevelError, ev.Level)
	assert.Equal(t, "zerolog", ev.Logger)
	assert.Equal(t, "test message", ev.Message)

	require.Len(t, ev.Exception, 1)
	assert.Equal(t, "dial timeout", ev.Exception[0].Value)

	require.Len(t, ev.Extra, 1)
	assert.Equal(t, "bee07485-2485-4f64-99e1-d10165884ca7", ev.Extra["requestId"])
}

func TestInternalParseLogLevel(t *testing.T) {
	w, err := zlogsentry.New("")
	require.NoError(t, err)

	level, err := w.InternalParseLogLevel(logEventJSON)
	require.NoError(t, err)
	assert.Equal(t, zerolog.ErrorLevel, level)
}

func TestWrite(t *testing.T) {
	beforeSendCalled := false
	writer, err := zlogsentry.New("",
		zlogsentry.WithBeforeSend(func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			assert.Equal(t, sentry.LevelError, event.Level)
			assert.Equal(t, "test message", event.Message)
			require.Len(t, event.Exception, 1)
			assert.Equal(t, "dial timeout", event.Exception[0].Value)
			assert.Less(t, time.Since(event.Timestamp).Minutes(), float64(1))
			assert.Equal(t, "bee07485-2485-4f64-99e1-d10165884ca7", event.Extra["requestId"])
			beforeSendCalled = true
			return event
		}))
	require.NoError(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) { //nolint:reassign
		zerologError = err
	}

	// use io.MultiWriter to enforce using the Write() method
	log := zerolog.New(io.MultiWriter(writer)).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Err(zlogsentry.ErrDialTimeout).
		Msg("test message")

	require.NoError(t, zerologError)
	require.True(t, beforeSendCalled)
}

func TestWrite_TraceDoesNotPanic(t *testing.T) {
	beforeSendCalled := false
	writer, err := zlogsentry.New("",
		zlogsentry.WithBeforeSend(func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			beforeSendCalled = true
			return event
		}))
	require.NoError(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) { //nolint:reassign
		zerologError = err
	}

	// use io.MultiWriter to enforce using the Write() method
	log := zerolog.New(io.MultiWriter(writer)).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Trace().Msg("test message")

	require.NoError(t, zerologError)
	require.False(t, beforeSendCalled)
}

func TestWriteLevel(t *testing.T) {
	beforeSendCalled := false
	writer, err := zlogsentry.New("",
		zlogsentry.WithBeforeSend(func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			assert.Equal(t, sentry.LevelError, event.Level)
			assert.Equal(t, "test message", event.Message)
			require.Len(t, event.Exception, 1)
			assert.Equal(t, "dial timeout", event.Exception[0].Value)
			assert.Less(t, time.Since(event.Timestamp).Minutes(), float64(1))
			assert.Equal(t, "bee07485-2485-4f64-99e1-d10165884ca7", event.Extra["requestId"])
			beforeSendCalled = true
			return event
		}))
	require.NoError(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) { //nolint:reassign
		zerologError = err
	}

	log := zerolog.New(writer).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Err(zlogsentry.ErrDialTimeout).
		Msg("test message")

	require.NoError(t, zerologError)
	require.True(t, beforeSendCalled)
}

func TestWrite_Disabled(t *testing.T) {
	beforeSendCalled := false
	writer, err := zlogsentry.New("",
		zlogsentry.WithLevels(zerolog.FatalLevel),
		zlogsentry.WithBeforeSend(func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			beforeSendCalled = true
			return event
		}))
	require.NoError(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) { //nolint:reassign
		zerologError = err
	}

	// use io.MultiWriter to enforce using the Write() method
	log := zerolog.New(io.MultiWriter(writer)).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Err(zlogsentry.ErrDialTimeout).
		Msg("test message")

	require.NoError(t, zerologError)
	require.False(t, beforeSendCalled)
}

func TestWriteLevel_Disabled(t *testing.T) {
	beforeSendCalled := false
	writer, err := zlogsentry.New("",
		zlogsentry.WithLevels(zerolog.FatalLevel),
		zlogsentry.WithBeforeSend(func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			beforeSendCalled = true
			return event
		}))
	require.NoError(t, err)

	var zerologError error
	zerolog.ErrorHandler = func(err error) { //nolint:reassign
		zerologError = err
	}

	log := zerolog.New(writer).With().Timestamp().
		Str("requestId", "bee07485-2485-4f64-99e1-d10165884ca7").
		Logger()
	log.Err(zlogsentry.ErrDialTimeout).
		Msg("test message")

	require.NoError(t, zerologError)
	require.False(t, beforeSendCalled)
}

func BenchmarkInternalParseLogEvent(b *testing.B) {
	w, err := zlogsentry.New("")
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		w.InternalParseLogEvent(logEventJSON)
	}
}

func BenchmarkInternalParseLogEvent_Disabled(b *testing.B) {
	w, err := zlogsentry.New("", zlogsentry.WithLevels(zerolog.FatalLevel))
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		w.InternalParseLogEvent(logEventJSON)
	}
}

func BenchmarkWriteLogEvent(b *testing.B) {
	w, err := zlogsentry.New("")
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		_, _ = w.Write(logEventJSON)
	}
}

func BenchmarkWriteLogEvent_Disabled(b *testing.B) {
	w, err := zlogsentry.New("", zlogsentry.WithLevels(zerolog.FatalLevel))
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		_, _ = w.Write(logEventJSON)
	}
}

func BenchmarkWriteLogLevelEvent(b *testing.B) {
	w, err := zlogsentry.New("")
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		_, _ = w.WriteLevel(zerolog.ErrorLevel, logEventJSON)
	}
}

func BenchmarkWriteLogLevelEvent_Disabled(b *testing.B) {
	w, err := zlogsentry.New("", zlogsentry.WithLevels(zerolog.FatalLevel))
	if err != nil {
		b.Errorf("failed to create writer: %v", err)
	}

	for i := 0; i < b.N; i++ {
		_, _ = w.WriteLevel(zerolog.ErrorLevel, logEventJSON)
	}
}
