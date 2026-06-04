package ohm

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

// Span runs fn inside a new span named name and returns fn's result and error
// unchanged. It ends the span when fn returns and records the error, so manual
// instrumentation reads as ordinary code with no start/end/record/status
// boilerplate:
//
//	user, err := ohm.Span(ctx, "load user", func(ctx context.Context) (User, error) {
//		return store.User(ctx, id)
//	})
//
// The span is created from the globally configured OpenTelemetry tracer, which
// is a no-op until an application installs a provider, so Span is safe to use
// unconditionally. To add attributes, take the span from the context passed to
// fn with trace.SpanFromContext.
func Span[T any](ctx context.Context, name string, fn func(context.Context) (T, error)) (T, error) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, name)
	defer span.End()

	result, err := fn(ctx)
	if err != nil && span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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
