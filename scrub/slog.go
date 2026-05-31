package scrub

import (
	"context"
	"io"
	"log/slog"
)

// Handler is a slog handler that redacts sensitive attributes before logging.
type Handler struct {
	next     slog.Handler
	redactor *Redactor
}

// NewHandler wraps next with a redacting slog handler.
func NewHandler(next slog.Handler, opts ...Option) *Handler {
	if next == nil {
		next = slog.NewTextHandler(io.Discard, nil)
	}
	return &Handler{
		next:     next,
		redactor: New(opts...),
	}
}

// Enabled reports whether records at level should be logged.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle redacts record attributes before delegating to the wrapped handler.
func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	redacted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		redacted.AddAttrs(h.redactor.Attr(attr))
		return true
	})
	return h.next.Handle(ctx, redacted)
}

// WithAttrs returns a handler with redacted attributes attached.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, h.redactor.Attr(attr))
	}
	return &Handler{
		next:     h.next.WithAttrs(redacted),
		redactor: h.redactor,
	}
}

// WithGroup returns a handler with name added to subsequent attribute keys.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		next:     h.next.WithGroup(name),
		redactor: h.redactor,
	}
}
