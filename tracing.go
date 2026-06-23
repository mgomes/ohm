package ohm

import (
	"context"
	"errors"
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Fetch the propagator per request rather than caching it: the
			// middleware is built during app construction, before the app
			// installs a propagator, so a cached value could miss it.
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(requestSpanAttrs(r)...),
			)
			defer span.End()

			r = r.WithContext(ctx)
			if !span.IsRecording() {
				next.ServeHTTP(w, r)
				return
			}

			tracked, state := trackResponse(w)
			next.ServeHTTP(tracked, r)

			recordResponseSpan(span, r, state.status)
		})
	}
}

func requestSpanAttrs(r *http.Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(r.Method),
	}
	if r.URL.Scheme != "" {
		attrs = append(attrs, semconv.URLScheme(r.URL.Scheme))
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

// recordHandlerError marks the active span failed for server (5xx) errors.
// Client (4xx) errors are left unmarked, matching OpenTelemetry server-span
// conventions. Only the public response message is recorded, never the raw
// error: the raw text may hold sensitive data and is handled by scrubbed
// logging, not sent to the tracing backend (consistent with ADR 0004). It is a
// no-op when no span is recording, so handlers never reference tracing directly.
func recordHandlerError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	status, message := ErrorResponse(err)
	if status < http.StatusInternalServerError {
		return
	}
	span.RecordError(errors.New(message), trace.WithAttributes(
		attribute.String("error.type", fmt.Sprintf("%T", err)),
	))
	span.SetStatus(codes.Error, message)
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
