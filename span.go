package ohm

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
