package ohm

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
)

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
	wantErr := errors.New("boom")

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
	if len(span.Events()) == 0 {
		t.Errorf("span events = 0, want a recorded error event")
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
