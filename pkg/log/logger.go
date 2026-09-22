package log

import (
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

var (
	logAtomic atomic.Pointer[zap.SugaredLogger]
	once      sync.Once
)

func init() {
	logAtomic.Store(zap.NewNop().Sugar())
}

// Init initializes the global logger singleton.
func Init(level, env string) {
	once.Do(func() {
		logAtomic.Store(NewLogger(level, env))
	})
}

// L returns the active global sugared logger or a no-op fallback.
func L() *zap.SugaredLogger {
	return logAtomic.Load()
}

// Debug logs at debug level with key-value pairs.
func Debug(msg string, kv ...any) {
	L().Debugw(msg, normalizeKV(kv)...)
}

// Info logs at info level with key-value pairs.
func Info(msg string, kv ...any) {
	L().Infow(msg, normalizeKV(kv)...)
}

// Warn logs at warn level with key-value pairs.
func Warn(msg string, kv ...any) {
	L().Warnw(msg, normalizeKV(kv)...)
}

// Error logs at error level with key-value pairs.
func Error(msg string, kv ...any) {
	L().Errorw(msg, normalizeKV(kv)...)
}

// Fatal logs at fatal level with key-value pairs and exits.
func Fatal(msg string, kv ...any) {
	L().Fatalw(msg, normalizeKV(kv)...)
}

// Panic logs at panic level with key-value pairs and panics.
func Panic(msg string, kv ...any) {
	L().Panicw(msg, normalizeKV(kv)...)
}

// With creates a child logger with preset key-value pairs.
func With(kv ...any) *zap.SugaredLogger {
	return L().With(normalizeKV(kv)...)
}

// Sync flushes any buffered log entries.
func Sync() error {
	return L().Sync()
}

func normalizeKV(kv []any) []any {
	if len(kv)%2 != 0 {
		kv = append(kv, "<missing>")
	}

	for i := 0; i < len(kv); i += 2 {
		if _, ok := kv[i].(string); !ok {
			kv[i] = "<invalid_key>"
		}
	}
	return kv
}
