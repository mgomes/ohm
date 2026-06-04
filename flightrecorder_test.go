package ohm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type memSink struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newMemSink() *memSink {
	return &memSink{files: map[string][]byte{}}
}

func (s *memSink) Create(name string) (io.WriteCloser, error) {
	return &memFile{sink: s, name: name}, nil
}

func (s *memSink) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.files))
	for name := range s.files {
		names = append(names, name)
	}
	return names
}

type memFile struct {
	sink *memSink
	name string
	buf  bytes.Buffer
}

func (f *memFile) Write(p []byte) (int, error) { return f.buf.Write(p) }

func (f *memFile) Close() error {
	f.sink.mu.Lock()
	defer f.sink.mu.Unlock()
	f.sink.files[f.name] = f.buf.Bytes()
	return nil
}

func startedRecorder(t *testing.T, sink Sink, opts ...FlightRecorderOption) *FlightRecorder {
	t.Helper()
	recorder := NewFlightRecorder(append([]FlightRecorderOption{WithTraceSink(sink)}, opts...)...)
	if err := recorder.Start(); err != nil {
		t.Fatalf("FlightRecorder.Start() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = recorder.Stop(context.Background()) })
	return recorder
}

func TestFlightRecordingSnapshotsOnSlowRequest(t *testing.T) {
	sink := newMemSink()
	recorder := startedRecorder(t, sink, WithSlowRequestThreshold(time.Millisecond))

	handler := FlightRecording(recorder)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(5 * time.Millisecond)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))

	names := sink.names()
	if len(names) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(names))
	}
	if !strings.Contains(names[0], "slow") {
		t.Errorf("snapshot name = %q, want it to mention %q", names[0], "slow")
	}
	if len(sink.files[names[0]]) == 0 {
		t.Errorf("snapshot %q is empty, want trace bytes", names[0])
	}
}

func TestFlightRecordingSkipsFastRequest(t *testing.T) {
	sink := newMemSink()
	recorder := startedRecorder(t, sink, WithSlowRequestThreshold(time.Hour))

	handler := FlightRecording(recorder)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/fast", nil))

	if names := sink.names(); len(names) != 0 {
		t.Errorf("snapshots = %v, want none for a fast request", names)
	}
}

func TestFlightRecordingSnapshotsOnPanicAndRepanics(t *testing.T) {
	sink := newMemSink()
	recorder := startedRecorder(t, sink)

	handler := FlightRecording(recorder)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Errorf("panic did not propagate past FlightRecording")
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	}()

	names := sink.names()
	if len(names) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(names))
	}
	if !strings.Contains(names[0], "panic") {
		t.Errorf("snapshot name = %q, want it to mention %q", names[0], "panic")
	}
}

func TestFlightRecordingNilRecorderIsPassThrough(t *testing.T) {
	var called bool
	handler := FlightRecording(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Errorf("nil recorder middleware did not call next handler")
	}
}

func TestSnapshotNameSanitizesCorrelationID(t *testing.T) {
	name := snapshotName("panic", "../../../../pwn/etc")
	if strings.ContainsAny(name, "/\\") {
		t.Errorf("snapshot name = %q, want no path separators", name)
	}
	if strings.Contains(name, "..") {
		t.Errorf("snapshot name = %q, want no parent-directory tokens", name)
	}
}

func TestFlightRecordingTraversalRequestIDStaysInSink(t *testing.T) {
	sink := newMemSink()
	recorder := startedRecorder(t, sink, WithSlowRequestThreshold(time.Millisecond))

	handler := FlightRecording(recorder)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(5 * time.Millisecond)
	}))
	request := httptest.NewRequest(http.MethodGet, "/slow", nil)
	// Simulate a client-controlled request id seeded into the context.
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, "../../../../pwn"))

	handler.ServeHTTP(httptest.NewRecorder(), request)

	names := sink.names()
	if len(names) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(names))
	}
	if strings.ContainsAny(names[0], "/\\") || strings.Contains(names[0], "..") {
		t.Errorf("snapshot name = %q, want a path-safe token", names[0])
	}
}

func TestFlightRecorderSerializesConcurrentSnapshots(t *testing.T) {
	sink := newMemSink()
	recorder := startedRecorder(t, sink)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			ctx := context.WithValue(context.Background(), requestIDKey{}, fmt.Sprintf("req-%d", i))
			recorder.snapshot(ctx, "slow")
		})
	}
	wg.Wait()

	if got := len(sink.names()); got != 8 {
		t.Errorf("snapshots = %d, want 8 (none dropped by concurrent WriteTo)", got)
	}
}

func TestCorrelationIDPrefersTraceID(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("TraceIDFromHex error = %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("SpanIDFromHex error = %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	if got := correlationID(ctx); got != traceID.String() {
		t.Errorf("correlationID = %q, want %q", got, traceID.String())
	}
}

func TestCorrelationIDFallsBackToRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-123")
	if got := correlationID(ctx); got != "req-123" {
		t.Errorf("correlationID = %q, want %q", got, "req-123")
	}
}
