package ohm

import (
	"bytes"
	"context"
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
	name := snapshotName("panic", "../../../../pwn/etc", 1)
	if strings.ContainsAny(name, "/\\") {
		t.Errorf("snapshot name = %q, want no path separators", name)
	}
	if strings.Contains(name, "..") {
		t.Errorf("snapshot name = %q, want no parent-directory tokens", name)
	}
}

func TestSnapshotNameUniquePerSequence(t *testing.T) {
	first := snapshotName("slow", "req-1", 1)
	second := snapshotName("slow", "req-1", 2)
	if first == second {
		t.Errorf("snapshot names collided: %q == %q", first, second)
	}
}

func TestFlightRecordingSkipsAbortHandlerPanic(t *testing.T) {
	sink := newMemSink()
	recorder := startedRecorder(t, sink)

	handler := FlightRecording(recorder)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	func() {
		defer func() {
			if recovered := recover(); recovered != http.ErrAbortHandler {
				t.Errorf("recovered = %v, want http.ErrAbortHandler re-panicked", recovered)
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
	}()

	if names := sink.names(); len(names) != 0 {
		t.Errorf("snapshots = %v, want none for an intentional abort", names)
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

func TestFlightRecorderCoalescesConcurrentSnapshots(t *testing.T) {
	sink := newMemSink()
	blocking := &blockingSink{
		sink:    sink,
		created: make(chan struct{}),
		release: make(chan struct{}),
	}
	recorder := startedRecorder(t, blocking)

	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-shared")
	firstDone := make(chan struct{})
	go func() {
		recorder.snapshot(ctx, "slow")
		close(firstDone)
	}()

	select {
	case <-blocking.created:
	case <-time.After(time.Second):
		t.Fatalf("first snapshot did not reach sink Create")
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			recorder.snapshot(ctx, "slow")
		})
	}
	contendersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(contendersDone)
	}()

	select {
	case <-contendersDone:
	case <-time.After(time.Second):
		t.Fatalf("concurrent snapshots queued behind in-progress snapshot, want coalesced")
	}

	close(blocking.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatalf("first snapshot did not finish after sink release")
	}

	if got := len(sink.names()); got != 2 {
		t.Errorf("snapshots = %d, want active snapshot plus one coalesced follow-up", got)
	}
}

type blockingSink struct {
	sink    Sink
	once    sync.Once
	created chan struct{}
	release chan struct{}
}

func (s *blockingSink) Create(name string) (io.WriteCloser, error) {
	s.once.Do(func() {
		close(s.created)
		<-s.release
	})
	return s.sink.Create(name)
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
