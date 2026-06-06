package ohm

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Span runs fn inside a new span named name and returns fn's result and error
// unchanged. It ends the span when fn returns and, on error, marks the span
// failed, so manual instrumentation reads as ordinary code with no
// start/end/record/status boilerplate:
//
//	user, err := ohm.Span(ctx, "load user", func(ctx context.Context) (User, error) {
//		return store.User(ctx, id)
//	})
//
// On error the span status is set to error and the error's Go type is recorded
// as the error.type attribute. The raw error text is deliberately not sent to
// the tracing backend, since it may hold sensitive data; the full error is
// available through correlated logs, consistent with the framework's scrubbing
// policy. To attach additional attributes, take the span from the context passed
// to fn with trace.SpanFromContext.
//
// The span is created from the globally configured OpenTelemetry tracer, which
// is a no-op until an application installs a provider, so Span is safe to use
// unconditionally.
func Span[T any](ctx context.Context, name string, fn func(context.Context) (T, error)) (T, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, name)
	defer span.End()

	result, err := fn(ctx)
	if err != nil && span.IsRecording() {
		span.SetStatus(codes.Error, "")
		span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", err)))
	}
	return result, err
}

// Do runs fn inside a new span named name for side-effecting work that returns
// only an error. It is the result-less companion to Span:
//
//	err := ohm.Do(ctx, "send welcome email", func(ctx context.Context) error {
//		return mailer.Send(ctx, welcome)
//	})
func Do(ctx context.Context, name string, fn func(context.Context) error) error {
	_, err := Span(ctx, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

// Observe runs fn and only emits a retrospective span when the operation fails
// or matches a configured observation policy, such as SlowAfter. Fast successful
// operations are left out of the trace so high-volume helper calls do not drown
// out the request span.
//
// Unlike Span, Observe does not place a live child span on the context passed to
// fn. Downstream work therefore continues to see ctx's existing span. Use Span
// instead when nested work must propagate under a child span while it runs.
//
// When Observe emits a span, it records the operation's real start and end times
// with OpenTelemetry timestamps. Returned errors mark the span failed and record
// only the error's Go type, never the raw error text.
func Observe[T any](ctx context.Context, name string, fn func(context.Context) (T, error), opts ...ObserveOption) (T, error) {
	cfg := observeConfig{
		now: time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	started := cfg.now()
	result, err := fn(ctx)
	ended := cfg.now()

	if reason := cfg.reason(err, ended.Sub(started)); reason != "" {
		recordObservedSpan(ctx, name, started, ended, err, reason, cfg.slowAfter)
	}
	return result, err
}

// ObserveDo runs fn through Observe for side-effecting work that returns only an
// error.
func ObserveDo(ctx context.Context, name string, fn func(context.Context) error, opts ...ObserveOption) error {
	_, err := Observe(ctx, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	}, opts...)
	return err
}

// ObserveOption configures Observe.
type ObserveOption func(*observeConfig)

// SlowAfter emits an observed span when fn takes at least d. Non-positive
// durations disable slow-success observation; returned errors are still
// observed.
func SlowAfter(d time.Duration) ObserveOption {
	return func(cfg *observeConfig) {
		cfg.slowAfter = d
	}
}

type observeConfig struct {
	slowAfter time.Duration
	now       func() time.Time
}

func (cfg observeConfig) reason(err error, duration time.Duration) string {
	if err != nil {
		return "error"
	}
	if cfg.slowAfter > 0 && duration >= cfg.slowAfter {
		return "slow"
	}
	return ""
}

func recordObservedSpan(
	ctx context.Context,
	name string,
	started time.Time,
	ended time.Time,
	err error,
	reason string,
	slowAfter time.Duration,
) {
	attrs := []attribute.KeyValue{
		attribute.String("ohm.observe.reason", reason),
	}
	if slowAfter > 0 {
		attrs = append(attrs, attribute.Int64("ohm.observe.slow_after_ns", slowAfter.Nanoseconds()))
	}

	_, span := otel.Tracer(tracerName).Start(ctx, name,
		trace.WithTimestamp(started),
		trace.WithAttributes(attrs...),
	)
	if err != nil && span.IsRecording() {
		span.SetStatus(codes.Error, "")
		span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", err)))
	}
	span.End(trace.WithTimestamp(ended))
}
