# ADR 0010: Observability with OpenTelemetry and execution-trace flight recording

## Status

Proposed

## Context

Ohm should offer first-class observability. OpenTelemetry (OTel) is the industry
standard for distributed tracing and metrics, and applications increasingly
expect to export to OTel-compatible backends without bespoke glue.

Adopting OTel naively has a well-known cost: manual instrumentation leaks into
ordinary code. The common pattern repeats four lines in every function that is
worth tracing.

```go
func loadUser(ctx context.Context, id string) (User, error) {
	ctx, span := tracer.Start(ctx, "loadUser")
	defer span.End()

	user, err := store.User(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return User{}, err
	}
	span.SetAttributes(attribute.String("user.id", id))
	return user, nil
}
```

Business logic disappears under tracer plumbing, error recording is duplicated
at every return, and the tracer must be threaded or made global. This is the
single biggest objection to adopting OTel and the primary problem this ADR sets
out to prevent.

Separately, Go 1.25 promoted `runtime/trace.FlightRecorder` to the standard
library. It keeps a low-overhead, in-memory rolling window of the Go execution
trace and lets a program snapshot the recent window on demand. This operates at
a different layer than OTel: OTel records application-level spans across
services, while the flight recorder records the Go runtime's own behavior
(goroutine scheduling, GC, syscalls, blocking). They do not interoperate and
should not be converted into one another, but they are complementary and can be
correlated by id.

## Decision

The guiding principle is: **instrument at Ohm's seams, not in application code.**

Ohm already centralizes the request lifecycle, error handling, the request
boundary, and logging. Each is a single framework-owned place to add tracing
once, so ordinary handlers and business code stay plain Go. The concrete seams:

- The request middleware wraps every request.
- `App.adapt` is the single chokepoint where handlers run and their returned
  `error` is dispatched to the `ErrorHandler`.
- `HTTPError`/`ErrorResponse` already map application errors to HTTP status.
- `Request.Context()` carries request-scoped context to all of application code.
- Framework logging already receives `ctx` on every record.

### Automatic server spans

Request middleware starts exactly one server span per request, following OTel
HTTP server semantic conventions (method, route pattern, status, and similar).
Handlers receive a live span on their context without writing any code.

### Automatic outcome recording

Because Ohm handlers return `error` and every error flows through `App.adapt`
into the `ErrorHandler`, the framework records the returned error on the span and
sets span status from the resolved HTTP status using the existing
`HTTPError`/`ErrorResponse` mapping. Application code never calls
`span.RecordError` or `span.SetStatus`.

### Spans travel in context

The active span lives on `Request.Context()`. There is no tracer to thread
through signatures and no tracer global in application code.

### One ergonomic combinator for manual spans

When an application genuinely wants a child span, it uses a single generic
combinator so the whole start/end/error-record/status dance is one expression
that reads like ordinary code.

```go
user, err := ohm.Span(ctx, "load user", func(ctx context.Context) (User, error) {
	return store.User(ctx, id)
})
```

A value-less variant covers side-effecting work.

```go
err := ohm.Do(ctx, "send welcome email", func(ctx context.Context) error {
	return mailer.Send(ctx, welcome)
})
```

The combinator starts the span, ends it on return, and records the returned
error and status automatically. It is generic over the result type so it stays
fully typed. This is the only tracing API ordinary application code is expected
to touch. Attributes, when wanted, are set through the span pulled from context,
not by passing a tracer around.

### Zero configuration and zero cost when disabled

OTel is off by default. With no provider configured, the OTel API returns a
no-op tracer, so the middleware seam and `ohm.Span`/`ohm.Do` call through the
no-op with negligible overhead and no nil checks anywhere. Enabling OTel is a
single server option that wires the resource, propagators, exporters, and
provider, and registers provider shutdown in the existing server lifecycle.

### Dependency hygiene

The Ohm core depends only on the OTel **API** modules (`go.opentelemetry.io/otel`
and its `trace`/`metric` API packages), which are designed to be a safe,
always-on, no-op-by-default dependency for libraries. The heavy OTel **SDK** and
OTLP exporters are imported only by a dedicated wiring package, so applications
that do not enable OTel do not pull the exporter and SDK graph into their
binaries.

### Automatic log and trace correlation

Framework logging already passes `ctx` to `slog`. A framework `slog.Handler`
extracts the active span context and adds `trace_id` and `span_id` to every log
record. Application logging code is unchanged, and the existing scrubbing policy
(ADR 0004) still applies.

### Flight recorder

Ohm owns a single `runtime/trace.FlightRecorder`, started and stopped with the
server and opt-in. Snapshots are triggered only by rare, high-value events:

- Recovered panics, in the `Recoverer` middleware.
- Slow requests above a configurable latency threshold, in `RequestLogger`.

Each snapshot is written to a sink (generated applications default to
`tmp/traces`), named by the correlation id, and the same id is also stamped into
the execution trace with `trace.Log` so the trace artifact joins up with logs
and spans. Snapshots are standard Go execution traces consumed by
`go tool trace`.

### Pluggable correlation key

The correlation key defaults to the Ohm request id. When OTel is enabled, the
OTel trace id is used instead, through the same mechanism, so a span in an OTel
backend, a log line, and a `.trace` artifact all point at the same incident.

## Non-goals

- Convert Go execution traces into OTLP spans. The granularities do not match.
- Capture production traffic for flight recording beyond the rare-event
  triggers.
- Require application code to import OTel packages to get tracing for ordinary
  handlers.
- Make OTel mandatory or enabled by default.
- Serialize Go's `context.Context`.

## Consequences

Observability becomes a framework concern, and application handlers stay plain
Go. The readability objection is prevented structurally — through automatic
seam instrumentation, a no-op default, and a single typed combinator — rather
than by asking developers to instrument tastefully.

Keeping the OTel SDK and exporters in a dedicated wiring package keeps the base
import graph light for applications that do not enable OTel.

The flight recorder adds memory for its rolling buffer when enabled and is
therefore opt-in. Its snapshots are binary and require `go tool trace`, so the
id-based naming and `trace.Log` stamping are what make them findable from logs
and spans.

This work depends on earlier decisions: structured logging and scrubbing
(ADR 0004), the server command and lifecycle, route introspection, and the
request and error abstractions.

## Open questions

- Should the OTel wiring live in a separate Go module
  (`github.com/mgomes/ohm/otel`) rather than a subpackage, to keep the core
  module's dependency graph minimal?
- Should the first version ship metrics and a logs bridge, or tracing plus the
  flight recorder first with metrics following?
- Should the slow-request threshold be global, or configurable per route?
- Should flight-recorder snapshots integrate with the replay snapshot format
  (ADR 0009) so a single captured incident bundles the replayable request with
  its execution trace?
