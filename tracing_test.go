package ohm

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return recorder
}

func TestTracingRecordsServerSpanNamedByRoute(t *testing.T) {
	recorder := newSpanRecorder(t)

	app := New()
	app.Use(Tracing())
	app.Get("/posts/{id}", func(req *Request) error {
		req.PlainText(http.StatusOK, "ok")
		return nil
	})

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/posts/42", nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name() != "GET /posts/{id}" {
		t.Errorf("span name = %q, want %q", span.Name(), "GET /posts/{id}")
	}
	if got := stringAttr(span.Attributes(), "http.route"); got != "/posts/{id}" {
		t.Errorf("http.route = %q, want %q", got, "/posts/{id}")
	}
	if got := intAttr(span.Attributes(), "http.response.status_code"); got != http.StatusOK {
		t.Errorf("http.response.status_code = %d, want %d", got, http.StatusOK)
	}
}

func TestTracingMarksServerErrors(t *testing.T) {
	recorder := newSpanRecorder(t)

	app := New()
	app.Use(Tracing())
	app.Get("/boom", func(*Request) error {
		return NewHTTPError(http.StatusInternalServerError, "boom", errors.New("boom"))
	})

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("span status code = %v, want %v", span.Status().Code, codes.Error)
	}
	if len(span.Events()) == 0 {
		t.Errorf("span events = 0, want a recorded error event")
	}
}

func TestTracingLeavesClientErrorsUnmarked(t *testing.T) {
	recorder := newSpanRecorder(t)

	app := New()
	app.Use(Tracing())
	app.Get("/missing", func(*Request) error {
		return NewHTTPError(http.StatusNotFound, "missing", errors.New("missing"))
	})

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	span := recorder.Ended()[0]
	if span.Status().Code == codes.Error {
		t.Errorf("span status code = %v, want it to remain unset for a 404", span.Status().Code)
	}
}

func stringAttr(attrs []attribute.KeyValue, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

func intAttr(attrs []attribute.KeyValue, key string) int {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return int(attr.Value.AsInt64())
		}
	}
	return 0
}
