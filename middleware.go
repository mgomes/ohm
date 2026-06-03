package ohm

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/mgomes/ohm/scrub"
)

// RequestIDHeader is the default HTTP header for request ids.
const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

// RequestID returns the request id stored in ctx.
func RequestID(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestIDKey{}).(string)
	return requestID, ok
}

// RequestLogger logs one structured event for each completed request.
func RequestLogger(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, requestID := ensureRequestID(w, r)

			metrics := httpsnoop.CaptureMetrics(next, w, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request", requestLogAttrs(r, requestID, metrics.Code, metrics.Duration)...)
		})
	}
}

// Recoverer recovers panics, logs a redacted panic report, and renders a 500 response.
func Recoverer(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, requestID := ensureRequestID(w, r)
			tracked, state := trackResponse(w)
			start := time.Now()

			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}

				committed := state.committed()
				status := http.StatusInternalServerError
				if committed {
					status = state.status
				}

				attrs := requestLogAttrs(r, requestID, status, time.Since(start))
				attrs = append(attrs,
					slog.Bool("response_committed", committed),
					slog.String("panic_type", fmt.Sprintf("%T", recovered)),
					slog.Any("panic", redactedPanic(recovered)),
					slog.String("stack", string(debug.Stack())),
				)
				logger.LogAttrs(r.Context(), slog.LevelError, "panic", attrs...)

				if committed {
					return
				}
				renderPlainText(tracked, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
			}()

			next.ServeHTTP(tracked, r)
		})
	}
}

func ensureRequestID(w http.ResponseWriter, r *http.Request) (*http.Request, string) {
	requestID, _ := RequestID(r.Context())
	if requestID == "" {
		requestID = r.Header.Get(RequestIDHeader)
	}
	if requestID == "" {
		requestID = newRequestID()
	}

	ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
	r = r.WithContext(ctx)
	w.Header().Set(RequestIDHeader, requestID)
	return r, requestID
}

func requestLogAttrs(r *http.Request, requestID string, status int, duration time.Duration) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("request_id", requestID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.Duration("duration", duration),
		slog.String("remote_addr", r.RemoteAddr),
		slog.String("user_agent", r.UserAgent()),
		slog.Int64("content_length", r.ContentLength),
	}

	if routePattern := RoutePattern(r); routePattern != "" {
		attrs = append(attrs, slog.String("route_pattern", routePattern))
	}
	return attrs
}

func redactedPanic(value any) any {
	return scrub.New().Any("", scrub.Mark(value))
}

type responseState struct {
	status  int
	written bool
}

func trackResponse(w http.ResponseWriter) (http.ResponseWriter, *responseState) {
	state := &responseState{status: http.StatusOK}
	wrapped := httpsnoop.Wrap(w, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(code int) {
				next(code)
				if finalStatus(code) {
					state.mark(code)
				}
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(body []byte) (int, error) {
				n, err := next(body)
				state.mark(http.StatusOK)
				return n, err
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(src io.Reader) (int64, error) {
				n, err := next(src)
				state.mark(http.StatusOK)
				return n, err
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() {
				next()
				state.mark(http.StatusOK)
			}
		},
		Hijack: func(next httpsnoop.HijackFunc) httpsnoop.HijackFunc {
			return func() (net.Conn, *bufio.ReadWriter, error) {
				conn, rw, err := next()
				if err == nil && !state.committed() {
					state.mark(http.StatusSwitchingProtocols)
				}
				return conn, rw, err
			}
		},
	})
	return wrapped, state
}

func (s *responseState) mark(status int) {
	if s.written {
		return
	}
	s.status = status
	s.written = true
}

func (s *responseState) committed() bool {
	return s != nil && s.written
}

func finalStatus(code int) bool {
	return code == http.StatusSwitchingProtocols || code >= 200
}

func newRequestID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err == nil {
		return hex.EncodeToString(data[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
