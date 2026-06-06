package ohm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type countingError struct {
	calls int
}

func (e *countingError) Error() string {
	e.calls++
	return "boom"
}

func TestSpanReturnsResultAndRecordsSpan(t *testing.T) {
	recorder := newSpanRecorder(t)

	got, err := Span(context.Background(), "load user", func(context.Context) (string, error) {
		return "ada", nil
	})
	if err != nil {
		t.Fatalf("Span error = %v, want nil", err)
	}
	if got != "ada" {
		t.Errorf("Span result = %q, want %q", got, "ada")
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded spans = %d, want 1", len(spans))
	}
	if spans[0].Name() != "load user" {
		t.Errorf("span name = %q, want %q", spans[0].Name(), "load user")
	}
	if spans[0].Status().Code != codes.Unset {
		t.Errorf("span status = %v, want unset on success", spans[0].Status().Code)
	}
}

func TestSpanRecordsError(t *testing.T) {
	recorder := newSpanRecorder(t)
	wantErr := errors.New("connection string s3cr3t")

	_, err := Span(context.Background(), "charge", func(context.Context) (int, error) {
		return 0, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Span error = %v, want wantErr", err)
	}

	span := recorder.Ended()[0]
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want %v", span.Status().Code, codes.Error)
	}
	if got := stringAttr(span.Attributes(), "error.type"); got != "*errors.errorString" {
		t.Errorf("error.type = %q, want %q", got, "*errors.errorString")
	}

	// The raw error text must never reach the tracing backend.
	if strings.Contains(span.Status().Description, "s3cr3t") {
		t.Errorf("span status description leaked raw error: %q", span.Status().Description)
	}
	for _, attr := range span.Attributes() {
		if strings.Contains(attr.Value.AsString(), "s3cr3t") {
			t.Errorf("span attribute %q leaked raw error", attr.Key)
		}
	}
	for _, event := range span.Events() {
		for _, attr := range event.Attributes {
			if strings.Contains(attr.Value.AsString(), "s3cr3t") {
				t.Errorf("span event %q leaked raw error", event.Name)
			}
		}
	}
}

func TestSpanPropagatesContext(t *testing.T) {
	newSpanRecorder(t)

	var childHadParent bool
	_, _ = Span(context.Background(), "parent", func(ctx context.Context) (struct{}, error) {
		_, _ = Span(ctx, "child", func(ctx context.Context) (struct{}, error) {
			childHadParent = true
			return struct{}{}, nil
		})
		return struct{}{}, nil
	})
	if !childHadParent {
		t.Errorf("nested span did not run")
	}
}

func TestSpanSkipsErrorFormattingWhenNotRecording(t *testing.T) {
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	failure := &countingError{}
	_, err := Span(context.Background(), "op", func(context.Context) (int, error) {
		return 0, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("Span error = %v, want failure", err)
	}
	if failure.calls != 0 {
		t.Errorf("Error() called %d times, want 0 when the span is not recording", failure.calls)
	}
}

func TestDoRecordsError(t *testing.T) {
	recorder := newSpanRecorder(t)
	wantErr := errors.New("send failed")

	err := Do(context.Background(), "send email", func(context.Context) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Do error = %v, want wantErr", err)
	}

	span := recorder.Ended()[0]
	if span.Name() != "send email" {
		t.Errorf("span name = %q, want %q", span.Name(), "send email")
	}
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want %v", span.Status().Code, codes.Error)
	}
}

func TestObserveSkipsFastSuccess(t *testing.T) {
	recorder := newSpanRecorder(t)
	started := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)

	got, err := Observe(context.Background(), "load user", func(context.Context) (string, error) {
		return "ada", nil
	}, SlowAfter(50*time.Millisecond), withObserveClock(sequenceClock(t, started, started.Add(10*time.Millisecond))))
	if err != nil {
		t.Fatalf("Observe error = %v, want nil", err)
	}
	if got != "ada" {
		t.Errorf("Observe result = %q, want %q", got, "ada")
	}
	if spans := recorder.Ended(); len(spans) != 0 {
		t.Errorf("recorded spans = %d, want 0 for a fast success", len(spans))
	}
}

func TestObserveRecordsSlowSuccessWithTimestamps(t *testing.T) {
	recorder := newSpanRecorder(t)
	started := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	ended := started.Add(120 * time.Millisecond)

	got, err := Observe(context.Background(), "load user", func(context.Context) (string, error) {
		return "ada", nil
	}, SlowAfter(50*time.Millisecond), withObserveClock(sequenceClock(t, started, ended)))
	if err != nil {
		t.Fatalf("Observe error = %v, want nil", err)
	}
	if got != "ada" {
		t.Errorf("Observe result = %q, want %q", got, "ada")
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded spans = %d, want 1 for a slow success", len(spans))
	}
	span := spans[0]
	if span.Name() != "load user" {
		t.Errorf("span name = %q, want %q", span.Name(), "load user")
	}
	if !span.StartTime().Equal(started) {
		t.Errorf("span start time = %s, want %s", span.StartTime(), started)
	}
	if !span.EndTime().Equal(ended) {
		t.Errorf("span end time = %s, want %s", span.EndTime(), ended)
	}
	if span.Status().Code != codes.Unset {
		t.Errorf("span status = %v, want unset on slow success", span.Status().Code)
	}
	if got := stringAttr(span.Attributes(), "ohm.observe.reason"); got != "slow" {
		t.Errorf("ohm.observe.reason = %q, want %q", got, "slow")
	}
	if got := intAttr(span.Attributes(), "ohm.observe.slow_after_ns"); got != int(50*time.Millisecond) {
		t.Errorf("ohm.observe.slow_after_ns = %d, want %d", got, 50*time.Millisecond)
	}
}

func TestObserveRecordsError(t *testing.T) {
	recorder := newSpanRecorder(t)
	wantErr := errors.New("connection string s3cr3t")
	started := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	ended := started.Add(time.Millisecond)

	_, err := Observe(context.Background(), "charge", func(context.Context) (int, error) {
		return 0, wantErr
	}, withObserveClock(sequenceClock(t, started, ended)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Observe error = %v, want wantErr", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded spans = %d, want 1 for an error", len(spans))
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want %v", span.Status().Code, codes.Error)
	}
	if got := stringAttr(span.Attributes(), "ohm.observe.reason"); got != "error" {
		t.Errorf("ohm.observe.reason = %q, want %q", got, "error")
	}
	if got := stringAttr(span.Attributes(), "error.type"); got != "*errors.errorString" {
		t.Errorf("error.type = %q, want %q", got, "*errors.errorString")
	}
	if strings.Contains(span.Status().Description, "s3cr3t") {
		t.Errorf("span status description leaked raw error: %q", span.Status().Description)
	}
	for _, attr := range span.Attributes() {
		if strings.Contains(attr.Value.AsString(), "s3cr3t") {
			t.Errorf("span attribute %q leaked raw error", attr.Key)
		}
	}
}

func TestObserveDoesNotInstallLiveChildSpan(t *testing.T) {
	newSpanRecorder(t)
	started := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)

	_, err := Span(context.Background(), "parent", func(ctx context.Context) (struct{}, error) {
		parentSpan := trace.SpanContextFromContext(ctx)
		if !parentSpan.IsValid() {
			t.Fatalf("Span(parent) context span is invalid, want valid parent span")
		}

		_, err := Observe(ctx, "observed", func(ctx context.Context) (struct{}, error) {
			got := trace.SpanContextFromContext(ctx)
			if got.SpanID() != parentSpan.SpanID() {
				t.Errorf("Observe fn span id = %s, want parent span id %s", got.SpanID(), parentSpan.SpanID())
			}
			return struct{}{}, nil
		}, SlowAfter(50*time.Millisecond), withObserveClock(sequenceClock(t, started, started.Add(10*time.Millisecond))))
		return struct{}{}, err
	})
	if err != nil {
		t.Fatalf("Span(parent) error = %v, want nil", err)
	}
}

func TestObserveDoRecordsError(t *testing.T) {
	recorder := newSpanRecorder(t)
	wantErr := errors.New("send failed")
	started := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)

	err := ObserveDo(context.Background(), "send email", func(context.Context) error {
		return wantErr
	}, withObserveClock(sequenceClock(t, started, started.Add(time.Millisecond))))
	if !errors.Is(err, wantErr) {
		t.Fatalf("ObserveDo error = %v, want wantErr", err)
	}

	span := recorder.Ended()[0]
	if span.Name() != "send email" {
		t.Errorf("span name = %q, want %q", span.Name(), "send email")
	}
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want %v", span.Status().Code, codes.Error)
	}
}

func sequenceClock(t testing.TB, times ...time.Time) func() time.Time {
	t.Helper()
	return func() time.Time {
		if len(times) == 0 {
			t.Fatalf("sequenceClock exhausted, want no additional clock reads")
		}
		next := times[0]
		times = times[1:]
		return next
	}
}

func withObserveClock(now func() time.Time) ObserveOption {
	return func(cfg *observeConfig) {
		if now != nil {
			cfg.now = now
		}
	}
}
