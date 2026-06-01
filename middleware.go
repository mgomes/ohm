package ohm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
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
			start := time.Now()

			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				attrs := requestLogAttrs(r, requestID, http.StatusInternalServerError, time.Since(start))
				attrs = append(attrs,
					slog.String("panic_type", fmt.Sprintf("%T", recovered)),
					slog.Any("panic", redactedPanic(recovered)),
					slog.String("stack", string(debug.Stack())),
				)
				logger.LogAttrs(r.Context(), slog.LevelError, "panic", attrs...)

				render.Status(r, http.StatusInternalServerError)
				render.PlainText(w, r, http.StatusText(http.StatusInternalServerError))
			}()

			next.ServeHTTP(w, r)
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

	if routePattern := routePattern(r); routePattern != "" {
		attrs = append(attrs, slog.String("route_pattern", routePattern))
	}
	return attrs
}

func redactedPanic(value any) any {
	return scrub.New().Any("", scrub.Mark(value))
}

func routePattern(r *http.Request) string {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil {
		return ""
	}
	return routeContext.RoutePattern()
}

func newRequestID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err == nil {
		return hex.EncodeToString(data[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
