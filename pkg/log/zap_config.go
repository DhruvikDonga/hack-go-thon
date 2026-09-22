package log

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates a configured zap.SugaredLogger based on the provided log level and environment.
func NewLogger(level, env string) *zap.SugaredLogger {
	logLevel := parseLogLevel(level)

	var cfg zap.Config
	if env == "production" {
		cfg = zap.Config{
			Encoding:         "json",
			Level:            zap.NewAtomicLevelAt(logLevel),
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
			EncoderConfig: zapcore.EncoderConfig{
				MessageKey:     "message",
				LevelKey:       "level",
				TimeKey:        "timestamp",
				NameKey:        "logger",
				CallerKey:      "caller",
				StacktraceKey:  "stacktrace",
				EncodeTime:     zapcore.ISO8601TimeEncoder,
				EncodeLevel:    zapcore.LowercaseLevelEncoder,
				EncodeCaller:   zapcore.ShortCallerEncoder,
				EncodeDuration: zapcore.MillisDurationEncoder,
			},
		}
	} else {
		// Development configuration: colored, human-readable console output
		cfg = zap.Config{
			Encoding:         "console",
			Level:            zap.NewAtomicLevelAt(logLevel),
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
			EncoderConfig: zapcore.EncoderConfig{
				MessageKey:       "message",
				LevelKey:         "level",
				TimeKey:          "timestamp",
				NameKey:          "logger",
				CallerKey:        "caller",
				StacktraceKey:    "stacktrace",
				EncodeTime:       zapcore.RFC3339NanoTimeEncoder,
				EncodeLevel:      zapcore.CapitalColorLevelEncoder,
				EncodeCaller:     zapcore.ShortCallerEncoder,
				ConsoleSeparator: "\t",
			},
		}
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	return logger.Sugar()
}

func parseLogLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "info":
		return zapcore.InfoLevel
	default:
		return zapcore.InfoLevel
	}
}
