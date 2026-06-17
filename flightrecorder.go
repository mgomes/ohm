package ohm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/trace"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
)

const defaultTraceDir = "tmp/traces"

// FlightRecorder captures the recent Go execution-trace window on demand.
//
// It wraps runtime/trace.FlightRecorder, which keeps a low-overhead rolling
// window of execution-trace data in memory. Ohm snapshots that window on rare,
// high-value events — recovered panics and slow requests — so the runtime
// behavior leading up to an incident is available for go tool trace without
// always-on tracing. Snapshots are correlated with logs and OpenTelemetry spans
// by id, never converted into spans.
//
// At most one flight recorder may be active in a process. Construct one, Start
// it during startup, add FlightRecording to the middleware stack, and register
// Stop as a server shutdown hook:
//
//	recorder := ohm.NewFlightRecorder(ohm.WithSlowRequestThreshold(time.Second))
//	if err := recorder.Start(); err != nil {
//		return err
//	}
//	application.Use(ohm.Tracing(), ohm.RequestLogger(logger), ohm.Recoverer(logger), ohm.FlightRecording(recorder))
//	// cli.ServerCommand(handler, cli.WithShutdownHook(recorder.Stop))
type FlightRecorder struct {
	recorder  *trace.FlightRecorder
	sink      Sink
	threshold time.Duration
	logger    *slog.Logger

	// writeMu serializes snapshots: runtime/trace.FlightRecorder.WriteTo fails
	// when another WriteTo is in progress, so concurrent triggers must queue
	// rather than drop a high-value snapshot.
	writeMu sync.Mutex

	// recording tracks lifecycle separately from the runtime recorder so late
	// snapshots from abandoned handlers can be skipped or downgraded after Stop.
	recording atomic.Bool

	// seq makes snapshot filenames unique so simultaneous triggers sharing a
	// reason and correlation id cannot overwrite each other's artifact.
	seq atomic.Uint64
}

// Sink stores captured execution-trace snapshots.
type Sink interface {
	// Create opens a writer for a snapshot identified by name. The caller closes
	// the returned writer.
	Create(name string) (io.WriteCloser, error)
}

type flightRecorderConfig struct {
	minAge    time.Duration
	maxBytes  uint64
	threshold time.Duration
	sink      Sink
	logger    *slog.Logger
}

// FlightRecorderOption configures a FlightRecorder.
type FlightRecorderOption func(*flightRecorderConfig)

// WithSlowRequestThreshold enables slow-request snapshots for requests whose
// duration meets or exceeds d. A non-positive d disables the slow-request
// trigger, leaving only panic snapshots.
func WithSlowRequestThreshold(d time.Duration) FlightRecorderOption {
	return func(c *flightRecorderConfig) {
		c.threshold = d
	}
}

// WithTraceSink sets where snapshots are written. The default writes files under
// tmp/traces.
func WithTraceSink(sink Sink) FlightRecorderOption {
	return func(c *flightRecorderConfig) {
		if sink != nil {
			c.sink = sink
		}
	}
}

// WithTraceWindow sets the recorder's window bounds. minAge is the lower bound on
// how far back the window reaches; maxBytes caps its size. Zero values use the
// runtime defaults.
func WithTraceWindow(minAge time.Duration, maxBytes uint64) FlightRecorderOption {
	return func(c *flightRecorderConfig) {
		c.minAge = minAge
		c.maxBytes = maxBytes
	}
}

// WithFlightRecorderLogger sets the logger used to report snapshot activity.
func WithFlightRecorderLogger(logger *slog.Logger) FlightRecorderOption {
	return func(c *flightRecorderConfig) {
		if logger != nil {
			c.logger = logger
		}
	}
}

// NewFlightRecorder creates a flight recorder. It does not begin recording until
// Start is called.
func NewFlightRecorder(opts ...FlightRecorderOption) *FlightRecorder {
	cfg := flightRecorderConfig{
		sink:   DirSink{Dir: defaultTraceDir},
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &FlightRecorder{
		recorder: trace.NewFlightRecorder(trace.FlightRecorderConfig{
			MinAge:   cfg.minAge,
			MaxBytes: cfg.maxBytes,
		}),
		sink:      cfg.sink,
		threshold: cfg.threshold,
		logger:    cfg.logger,
	}
}

// Start begins recording the execution-trace window.
func (f *FlightRecorder) Start() error {
	if err := f.recorder.Start(); err != nil {
		return err
	}
	f.recording.Store(true)
	return nil
}

// Stop stops recording. It can be passed directly to cli.WithShutdownHook.
func (f *FlightRecorder) Stop(context.Context) error {
	f.recording.Store(false)
	f.recorder.Stop()
	return nil
}

// snapshot writes the current window to the sink, named and stamped with the
// correlation id for ctx. It is a no-op when recording is not active.
func (f *FlightRecorder) snapshot(ctx context.Context, reason string) {
	if f == nil || !f.recording.Load() || !f.recorder.Enabled() {
		return
	}

	id := correlationID(ctx)
	// Stamp the id into the trace itself so the snapshot joins up with the log
	// line and OpenTelemetry span for the same incident.
	trace.Log(ctx, "ohm.flight", reason+" "+id)

	f.writeMu.Lock()
	defer f.writeMu.Unlock()

	name := snapshotName(reason, id, f.seq.Add(1))
	writer, err := f.sink.Create(name)
	if err != nil {
		f.logger.LogAttrs(ctx, slog.LevelError, "flight snapshot failed",
			slog.String("reason", reason), slog.Any("error", err))
		return
	}
	defer func() { _ = writer.Close() }()

	if _, err := f.recorder.WriteTo(writer); err != nil {
		level := slog.LevelError
		if !f.recording.Load() {
			level = slog.LevelDebug
		}
		f.logger.LogAttrs(ctx, level, "flight snapshot failed",
			slog.String("reason", reason), slog.Any("error", err))
		return
	}

	f.logger.LogAttrs(ctx, slog.LevelWarn, "flight snapshot captured",
		slog.String("reason", reason), slog.String("snapshot", name))
}

// FlightRecording returns middleware that snapshots the execution-trace window
// on a recovered panic or a slow request. Place it innermost, below Recoverer,
// so a panic snapshot is taken before Recoverer renders the response. A nil
// recorder yields a pass-through middleware.
func FlightRecording(recorder *FlightRecorder) Middleware {
	return func(next http.Handler) http.Handler {
		if recorder == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			defer func() {
				if recovered := recover(); recovered != nil {
					// Mirror Recoverer: http.ErrAbortHandler is an intentional
					// abort, not a fault, so do not snapshot it.
					if recovered != http.ErrAbortHandler {
						recorder.snapshot(r.Context(), "panic")
					}
					panic(recovered)
				}
				if recorder.threshold > 0 && time.Since(start) >= recorder.threshold {
					recorder.snapshot(r.Context(), "slow")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// correlationID returns the OpenTelemetry trace id when a valid span context is
// present, otherwise the Ohm request id, otherwise an empty string.
func correlationID(ctx context.Context) string {
	if spanContext := oteltrace.SpanContextFromContext(ctx); spanContext.IsValid() {
		return spanContext.TraceID().String()
	}
	if requestID, ok := RequestID(ctx); ok {
		return requestID
	}
	return ""
}

func snapshotName(reason string, id string, seq uint64) string {
	stamp := time.Now().UTC().Format("20060102T150405.000")
	id = sanitizeID(id)
	if id == "" {
		return fmt.Sprintf("%s-%s-%d.trace", stamp, reason, seq)
	}
	return fmt.Sprintf("%s-%s-%s-%d.trace", stamp, reason, id, seq)
}

const maxSanitizedIDLen = 64

// sanitizeID reduces id to a path-safe token. The correlation id can originate
// from a client-controlled X-Request-ID header, so it must never reach a file
// path verbatim; non-alphanumeric characters are replaced and the result is
// length-capped.
func sanitizeID(id string) string {
	if len(id) > maxSanitizedIDLen {
		id = id[:maxSanitizedIDLen]
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
}

// DirSink writes snapshots as files under Dir, creating it as needed.
type DirSink struct {
	Dir string
}

// Create opens a snapshot file named name under the sink directory.
func (s DirSink) Create(name string) (io.WriteCloser, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}
	return os.Create(filepath.Join(s.Dir, name))
}
