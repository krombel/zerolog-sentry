package zlogsentry

import (
	"errors"
)

var (
	ErrHubCannotBeNil = errors.New("zlogsentry hub cannot be nil")
	ErrDialTimeout    = errors.New("dial timeout")
	ErrFlushTimeout   = errors.New("zlogsentry flush timeout")
)
