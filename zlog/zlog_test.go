package zlog

import (
	"testing"
)

var fakeConfig = Config{
	LogLevel:   "debug",
	Compress:   true,
	MaxSize:    10,
	MaxBackups: 5,
	MaxAge:     30,
	Format:     FormatJSON,
}

func TestLog(t *testing.T) {
	Init(NewZLog(&fakeConfig))
	defer Close()
	Infof("info", "test %s", "info")
	Debugf("debug", "test %s", "debug")
	Warnf("warn", "test %s", "warn")
	Errorf("error", "test %s", "error")
}
