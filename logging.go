package ohm

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceLogHandler wraps next so every log record carries the active trace and
// span ids when a valid span context is present on the record context.
//
// It composes with any other handler, including the scrubbing handler, and is a
// no-op when no span is active. Because Ohm logging already passes the request
// context to slog, this correlates logs with traces without any change to
// application logging code.
func TraceLogHandler(next slog.Handler) slog.Handler {
	if next == nil {
		return nil
	}
	return &traceLogHandler{next: next}
}

type traceLogHandler struct {
	next slog.Handler
}

func (h *traceLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *traceLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record = record.Clone()
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h *traceLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceLogHandler{next: h.next.WithAttrs(attrs)}
}

func (h *traceLogHandler) WithGroup(name string) slog.Handler {
	return &traceLogHandler{next: h.next.WithGroup(name)}
}
