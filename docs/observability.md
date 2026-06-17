# Observability

Ohm instruments framework seams first. Generated apps get request spans,
request logs, panic recovery, and trace-aware logging without putting tracer
plumbing in handlers or services.

The rule of thumb is:

```text
Use the request span for normal requests.
Use Observe for slow or failing helper work.
Use Span only when downstream work needs a live child span.
```

## Default request tracing

Generated apps install:

```go
application.Use(ohm.Tracing(), ohm.RequestLogger(logger), ohm.Recoverer(logger))
```

`ohm.Tracing` creates one server span per request. The span is named from the
matched route after routing completes, and Ohm records the response status.
Returned handler errors flow through the framework error boundary, so 5xx
responses mark the span as failed while 4xx client errors stay unmarked.

The raw error text is not recorded on spans. It can contain secrets. Ohm records
safe public response messages and error types, while full error detail belongs
in scrubbed logs correlated by trace id.

Generated apps also wrap `slog` with `ohm.TraceLogHandler`, so logs written with
the request context include `trace_id` and `span_id` when a span is active.

## Enable OpenTelemetry export

Tracing middleware is safe to leave installed even before telemetry is enabled.
Without a configured provider, the OpenTelemetry API uses a no-op tracer.

To export spans, call `otel.Setup` during app startup and register the returned
shutdown hook with the server command:

```go
import (
	"context"

	"github.com/mgomes/ohm/cli"
	ohmotel "github.com/mgomes/ohm/otel"

	"example.com/journal/internal/app"
)

func run(ctx context.Context, args []string) error {
	shutdownTelemetry, err := ohmotel.Setup(ctx,
		ohmotel.WithServiceName("journal"),
	)
	if err != nil {
		return err
	}

	application := app.New()
	program := cli.New("journal", []cli.Command{
		cli.ServerCommand(
			application.HTTPHandler(),
			cli.WithShutdownHook(shutdownTelemetry),
		),
	})
	return program.Run(ctx, args)
}
```

The setup package follows standard OpenTelemetry environment variables such as
`OTEL_TRACES_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`, and
`OTEL_EXPORTER_OTLP_PROTOCOL`.

If graceful HTTP shutdown exhausts its drain timeout, Ohm force-closes the
server and runs shutdown hooks with a fresh bounded context. Those hooks may
overlap abandoned handlers that are still unwinding, so cleanup code should be
tolerant of late request logging, tracing, or snapshot attempts.

## Prefer Observe for helper work

Most helper functions do not need a child span for every successful call. Use
`ohm.Observe` when you want the trace to stay quiet unless the work is slow or
fails:

```go
user, err := ohm.Observe(ctx, "load user", func(ctx context.Context) (User, error) {
	return users.Load(ctx, id)
}, ohm.SlowAfter(50*time.Millisecond))
```

Fast successes emit no span. Failures emit a span and mark it as an error. Slow
successes emit a span with the real operation start and end timestamps, so the
trace explains where time went without filling every request with routine child
spans.

`ohm.ObserveDo` is the side-effecting variant for functions that only return an
error:

```go
err := ohm.ObserveDo(ctx, "send welcome email", func(ctx context.Context) error {
	return mailer.Send(ctx, welcome)
}, ohm.SlowAfter(200*time.Millisecond))
```

`Observe` does not install a live child span in the context passed to the
function. Downstream calls keep using the existing request span. That is what
keeps trace volume low.

## Use Span for live child spans

Use `ohm.Span` when the work needs a live child span while it runs. Common cases
are outbound calls that should propagate under their own child span, or nested
work that should attach below the operation instead of the request span.

```go
receipt, err := ohm.Span(ctx, "charge card", func(ctx context.Context) (Receipt, error) {
	return payments.Charge(ctx, charge)
})
```

`ohm.Do` is the side-effecting variant:

```go
err := ohm.Do(ctx, "enqueue invoice", func(ctx context.Context) error {
	return queue.Enqueue(ctx, invoice)
})
```

Use this deliberately. A trace full of cheap helper spans is harder to read than
a trace with one request span plus a few slow or failing observations.

## Capture runtime traces for rare incidents

OpenTelemetry spans explain application-level work. Go execution traces explain
runtime behavior such as goroutine scheduling, blocking, syscalls, and garbage
collection.

Ohm's `FlightRecorder` keeps a rolling execution-trace window and snapshots it
only for high-value events such as recovered panics or slow requests. Snapshot
files are correlated with logs and spans by request id or trace id, and are read
with `go tool trace`.

If another request triggers a snapshot while one is already being written, Ohm
coalesces the trigger instead of queueing the request goroutine. The active
writer drains pending trigger batches with follow-up snapshots without
multiplying request goroutines during an incident.

Use flight recording when you need to understand why the Go process was slow,
not when you need another application span.
