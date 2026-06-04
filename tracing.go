package ohm

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName identifies Ohm's instrumentation scope in emitted spans.
const tracerName = "github.com/mgomes/ohm"

// Tracing returns middleware that records one server span per request using the
// globally configured OpenTelemetry tracer.
//
// Until an application installs a tracer provider (see the ohm/otel package) the
// global tracer is a no-op, so this middleware is safe to keep in every stack
// and adds negligible overhead when tracing is disabled. The span is placed on
// the request context, so handlers and ohm.Span observe it without any tracer
// plumbing in application code. Span name and route attribute are set from the
// matched route once routing completes, and the response status is recorded when
// the handler returns.
func Tracing() Middleware {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(requestSpanAttrs(r)...),
			)
			defer span.End()

			tracked, state := trackResponse(w)
			r = r.WithContext(ctx)
			next.ServeHTTP(tracked, r)

			recordResponseSpan(span, r, state.status)
		})
	}
}

func requestSpanAttrs(r *http.Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(r.Method),
		semconv.URLPath(r.URL.Path),
	}
	if r.URL.Scheme != "" {
		attrs = append(attrs, semconv.URLScheme(r.URL.Scheme))
	}
	if ua := r.UserAgent(); ua != "" {
		attrs = append(attrs, semconv.UserAgentOriginal(ua))
	}
	return attrs
}

func recordResponseSpan(span trace.Span, r *http.Request, status int) {
	if pattern := RoutePattern(r); pattern != "" {
		span.SetName(r.Method + " " + pattern)
		span.SetAttributes(semconv.HTTPRoute(pattern))
	}
	span.SetAttributes(semconv.HTTPResponseStatusCode(status))
	if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, http.StatusText(status))
	}
}

// recordHandlerError records err on the active span for ctx and marks the span
// failed for server errors. Client (4xx) errors are recorded without setting
// error status, matching OpenTelemetry server-span conventions. It is a no-op
// when no span is recording, so handlers never reference tracing directly.
func recordHandlerError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.RecordError(err)
	if status, _ := ErrorResponse(err); status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, err.Error())
	}
}

// recordPanicSpan marks the active span as failed for a recovered panic. Only
// the panic's Go type is recorded, never its value, so panic payloads that may
// hold sensitive data do not reach the tracing backend. It is a no-op when no
// span is recording.
func recordPanicSpan(ctx context.Context, recovered any) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.AddEvent("panic", trace.WithAttributes(
		attribute.String("panic.type", fmt.Sprintf("%T", recovered)),
	))
	span.SetStatus(codes.Error, "panic")
}
