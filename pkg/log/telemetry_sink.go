package log

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogEntry represents an intercepted log record.
type LogEntry struct {
	Level     string         `json:"level"`
	Timestamp time.Time      `json:"timestamp"`
	Message   string         `json:"message"`
	Caller    string         `json:"caller,omitempty"`
	Stack     string         `json:"stack,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// TelemetryCore filters log entries and non-blockingly forwards warnings and errors to a sink.
type TelemetryCore struct {
	sink   chan<- LogEntry
	fields []zapcore.Field
}

// NewTelemetryCore creates a zapcore.Core that intercepts WarnLevel and above (excluding Debug and Info).
func NewTelemetryCore(sink chan<- LogEntry) *TelemetryCore {
	return &TelemetryCore{
		sink: sink,
	}
}

// Enabled returns true for any log level except Debug and Info.
func (c *TelemetryCore) Enabled(lvl zapcore.Level) bool {
	// DebugLevel = -1, InfoLevel = 0. WarnLevel = 1, ErrorLevel = 2, DPanicLevel = 3, PanicLevel = 4, FatalLevel = 5
	return lvl != zapcore.DebugLevel && lvl != zapcore.InfoLevel
}

// With returns a clone of the core with accumulated structured fields.
func (c *TelemetryCore) With(fields []zapcore.Field) zapcore.Core {
	cloned := &TelemetryCore{
		sink:   c.sink,
		fields: append(append([]zapcore.Field(nil), c.fields...), fields...),
	}
	return cloned
}

// Check decides whether the given log entry should be handled by this core.
func (c *TelemetryCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Write non-blockingly dispatches the intercepted entry to the sink channel.
func (c *TelemetryCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	if c.sink == nil {
		return nil
	}

	allFields := append(append([]zapcore.Field(nil), c.fields...), fields...)
	var fieldsMap map[string]any
	if len(allFields) > 0 {
		enc := zapcore.NewMapObjectEncoder()
		for _, f := range allFields {
			f.AddTo(enc)
		}
		fieldsMap = enc.Fields
	}

	entry := LogEntry{
		Level:     ent.Level.String(),
		Timestamp: ent.Time,
		Message:   ent.Message,
		Caller:    ent.Caller.String(),
		Stack:     ent.Stack,
		Fields:    fieldsMap,
	}

	// Non-blocking write: never block or slow down caller application execution
	select {
	case c.sink <- entry:
	default:
	}

	return nil
}

// Sync is a no-op for the memory sink.
func (c *TelemetryCore) Sync() error {
	return nil
}

// RegisterLogSink registers a channel to non-blockingly receive all logs except Debug and Info.
func RegisterLogSink(sink chan<- LogEntry) {
	current := L()
	if current == nil {
		return
	}
	baseLogger := current.Desugar()
	telemetryCore := NewTelemetryCore(sink)
	newCore := zapcore.NewTee(baseLogger.Core(), telemetryCore)
	newLogger := zap.New(newCore, zap.AddCaller())
	logAtomic.Store(newLogger.Sugar())
}
