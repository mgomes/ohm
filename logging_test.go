package ohm

import (
	"context"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceLogHandlerAddsTraceContext(t *testing.T) {
	capture := &captureHandler{}
	logger := slog.New(TraceLogHandler(capture))

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("TraceIDFromHex error = %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("SpanIDFromHex error = %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	logger.InfoContext(ctx, "hello")

	if got := capture.attrs["trace_id"]; got != traceID.String() {
		t.Errorf("trace_id = %q, want %q", got, traceID.String())
	}
	if got := capture.attrs["span_id"]; got != spanID.String() {
		t.Errorf("span_id = %q, want %q", got, spanID.String())
	}
}

func TestTraceLogHandlerNoSpanContext(t *testing.T) {
	capture := &captureHandler{}
	logger := slog.New(TraceLogHandler(capture))

	logger.Info("hello")

	if _, ok := capture.attrs["trace_id"]; ok {
		t.Errorf("trace_id present without a span context, want absent")
	}
}

type captureHandler struct {
	attrs map[string]string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.attrs = map[string]string{}
	record.Attrs(func(attr slog.Attr) bool {
		h.attrs[attr.Key] = attr.Value.String()
		return true
	})
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(string) slog.Handler { return h }
