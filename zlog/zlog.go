package zlog

import (
	"fmt"
	"go-flow/utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"os"
)

var defaultLogger *ZLog

func Init(logger *ZLog) {
	defaultLogger = logger
	zap.ReplaceGlobals(defaultLogger.provider)
}

func L() *ZLog {
	return defaultLogger
}

func getZapLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zap.DebugLevel
	case "info":
		return zap.InfoLevel
	case "warn":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	case "panic":
		return zap.PanicLevel
	case "fatal":
		return zap.FatalLevel
	default:
		return zap.InfoLevel
	}
}

func newLogWriter(logPath string, maxSize, maxBackups, maxAge int, compress bool) io.Writer {
	if logPath == "" || logPath == "-" {
		return os.Stdout
	}
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   compress,
	}
}

func newZapEncoder() zapcore.EncoderConfig {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "line",
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout(utils.TimeLayout),
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeName:     zapcore.FullNameEncoder,
	}
	return encoderConfig
}

func newZapCore(cfg *Config) zapcore.Core {
	hook := newLogWriter(cfg.LogPath, cfg.MaxSize, cfg.MaxBackups, cfg.MaxAge, cfg.Compress)

	encoderConfig := newZapEncoder()

	atomLevel := zap.NewAtomicLevelAt(getZapLevel(cfg.LogLevel))

	var encoder zapcore.Encoder
	if cfg.Format == FormatJSON {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.NewMultiWriteSyncer(zapcore.AddSync(hook)),
		atomLevel,
	)
	return core
}

type ZLog struct {
	provider *zap.Logger
	level    zapcore.Level
	prefix   string
}

func Close() {
	if defaultLogger == nil {
		return
	}
	_ = defaultLogger.provider.Sync()
}

func newZapOptions() []zap.Option {
	options := []zap.Option{
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.Development(),
		zap.Fields(zap.String("app", "go-flow")),
	}
	return options
}

func NewZLog(cfg *Config) *ZLog {
	cfg.fillWithDefault()
	return &ZLog{
		provider: zap.New(newZapCore(cfg), newZapOptions()...),
		level:    getZapLevel(cfg.LogLevel),
	}
}

func (l *ZLog) SetPrefix(prefix string) {
	l.prefix = prefix
}

func (l *ZLog) Print(v ...interface{}) {
	l.provider.Info(fmt.Sprintf("[%s] %v", l.prefix, v))
}

func (l *ZLog) Printf(format string, v ...interface{}) {
	l.provider.Info(fmt.Sprintf("[%s] %s", l.prefix, fmt.Sprintf(format, v...)))
}

func (l *ZLog) Println(v ...interface{}) {
	l.provider.Info(fmt.Sprintf("[%s] %v", l.prefix, v))
}

func Fatalf(prefix string, format string, v ...interface{}) {
	if defaultLogger == nil {
		fmt.Printf(format+"\n", v...)
		return
	}
	if defaultLogger.level <= zap.FatalLevel {
		defaultLogger.provider.Fatal(fmt.Sprintf("[%s] %s", prefix, fmt.Sprintf(format, v...)))
	}
}

func Errorf(prefix string, format string, v ...interface{}) {
	if defaultLogger == nil {
		fmt.Printf(format+"\n", v...)
		return
	}
	if defaultLogger.level <= zap.ErrorLevel {
		defaultLogger.provider.Error(fmt.Sprintf("[%s] %s", prefix, fmt.Sprintf(format, v...)))
	}
}

func Warnf(prefix string, format string, v ...interface{}) {
	if defaultLogger == nil {
		fmt.Printf(format+"\n", v...)
		return
	}
	if defaultLogger.level <= zap.WarnLevel {
		defaultLogger.provider.Warn(fmt.Sprintf("[%s] %s", prefix, fmt.Sprintf(format, v...)))
	}
}

func Infof(prefix string, format string, v ...interface{}) {
	if defaultLogger == nil {
		fmt.Printf(format+"\n", v...)
		return
	}
	if defaultLogger.level <= zap.InfoLevel {
		defaultLogger.provider.Info(fmt.Sprintf("[%s] %s", prefix, fmt.Sprintf(format, v...)))
	}
}

func Debugf(prefix string, format string, v ...interface{}) {
	if defaultLogger == nil {
		fmt.Printf(format+"\n", v...)
		return
	}
	if defaultLogger.level <= zap.DebugLevel {
		defaultLogger.provider.Debug(fmt.Sprintf("[%s] %s", prefix, fmt.Sprintf(format, v...)))
	}
}
