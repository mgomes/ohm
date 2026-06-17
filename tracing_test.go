package ohm

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
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

func TestTracingDoesNotLeakRawErrorText(t *testing.T) {
	recorder := newSpanRecorder(t)

	const secret = "dsn=postgres://user:s3cr3t@host/db"
	app := New()
	app.Use(Tracing())
	app.Get("/leak", func(*Request) error {
		// 5xx HTTPError with no public message, wrapping a sensitive error.
		return NewHTTPError(http.StatusInternalServerError, "", errors.New(secret))
	})

	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/leak", nil))

	span := recorder.Ended()[0]
	if strings.Contains(span.Status().Description, "s3cr3t") {
		t.Errorf("span status description leaked raw error: %q", span.Status().Description)
	}
	for _, event := range span.Events() {
		for _, attr := range event.Attributes {
			if strings.Contains(attr.Value.AsString(), "s3cr3t") {
				t.Errorf("span event %q leaked raw error: %q", event.Name, attr.Value.AsString())
			}
		}
	}
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want %v for a 5xx", span.Status().Code, codes.Error)
	}
}

func TestTracingExtractsPropagatorInstalledAfterConstruction(t *testing.T) {
	recorder := newSpanRecorder(t)

	// Build the middleware before the propagator is installed, mirroring app
	// construction running before otel.Setup at process startup.
	app := New()
	app.Use(Tracing())
	app.Get("/x", func(req *Request) error {
		req.PlainText(http.StatusOK, "ok")
		return nil
	})

	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })

	const upstreamTraceID = "0102030405060708090a0b0c0d0e0f10"
	request := httptest.NewRequest(http.MethodGet, "/x", nil)
	request.Header.Set("traceparent", "00-"+upstreamTraceID+"-0102030405060708-01")

	app.ServeHTTP(httptest.NewRecorder(), request)

	span := recorder.Ended()[0]
	if got := span.SpanContext().TraceID().String(); got != upstreamTraceID {
		t.Errorf("server span trace id = %q, want %q (joined upstream trace)", got, upstreamTraceID)
	}
}

func TestTracingSkipsResponseTrackingWhenSpanIsNotRecording(t *testing.T) {
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	recorder := httptest.NewRecorder()
	handler := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w != recorder {
			t.Errorf("response writer = %T, want original recorder", w)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ok", nil))
}

func BenchmarkTracingDisabled(b *testing.B) {
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(noop.NewTracerProvider())
	b.Cleanup(func() { otel.SetTracerProvider(previous) })

	handler := Tracing()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/ok", nil)

	b.ReportAllocs()
	for b.Loop() {
		handler.ServeHTTP(httptest.NewRecorder(), request)
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
