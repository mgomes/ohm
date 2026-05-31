package replay

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/scrub"
)

const snapshotVersion = 1

var defaultHeaders = []string{
	"Accept",
	"Content-Type",
	"User-Agent",
	ohm.RequestIDHeader,
}

// Snapshot captures enough request data to replay a handler request.
type Snapshot struct {
	Version      int                 `json:"version"`
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	Query        map[string][]string `json:"query,omitempty"`
	Headers      map[string][]string `json:"headers,omitempty"`
	RequestID    string              `json:"request_id,omitempty"`
	RoutePattern string              `json:"route_pattern,omitempty"`
	CapturedAt   time.Time           `json:"captured_at"`
	Body         []byte              `json:"body,omitempty"`
	BodyOmitted  bool                `json:"body_omitted"`
}

// Option configures request snapshot capture.
type Option func(*captureOptions)

type captureOptions struct {
	headers  []string
	redactor *scrub.Redactor
	now      func() time.Time
}

// WithHeaders configures the request headers captured into a snapshot.
func WithHeaders(headers ...string) Option {
	return func(opts *captureOptions) {
		opts.headers = append([]string(nil), headers...)
	}
}

// WithRedactor configures the snapshot redactor.
func WithRedactor(redactor *scrub.Redactor) Option {
	return func(opts *captureOptions) {
		if redactor != nil {
			opts.redactor = redactor
		}
	}
}

// WithClock configures the capture clock.
func WithClock(now func() time.Time) Option {
	return func(opts *captureOptions) {
		if now != nil {
			opts.now = now
		}
	}
}

// Capture creates a scrubbed request snapshot.
func Capture(r *http.Request, opts ...Option) Snapshot {
	cfg := captureOptions{
		headers:  defaultHeaders,
		redactor: scrub.New(),
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	query := scrubValues(cfg.redactor, r.URL.Query())
	headers := captureHeaders(cfg.redactor, r.Header, cfg.headers)
	requestID, _ := ohm.RequestID(r.Context())
	if requestID == "" {
		requestID = r.Header.Get(ohm.RequestIDHeader)
	}

	return Snapshot{
		Version:      snapshotVersion,
		Method:       r.Method,
		Path:         r.URL.Path,
		Query:        query,
		Headers:      headers,
		RequestID:    requestID,
		RoutePattern: routePattern(r),
		CapturedAt:   cfg.now().UTC(),
		BodyOmitted:  true,
	}
}

// NewRequest builds a test request from snapshot.
func NewRequest(snapshot Snapshot) (*http.Request, error) {
	if snapshot.Method == "" {
		return nil, fmt.Errorf("replay snapshot method is required")
	}
	if snapshot.Path == "" {
		return nil, fmt.Errorf("replay snapshot path is required")
	}

	target := snapshot.Path
	query := url.Values(snapshot.Query).Encode()
	if query != "" {
		target += "?" + query
	}

	request := httptest.NewRequest(snapshot.Method, target, bytes.NewReader(snapshot.Body))
	for key, values := range snapshot.Headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	if snapshot.RequestID != "" && request.Header.Get(ohm.RequestIDHeader) == "" {
		request.Header.Set(ohm.RequestIDHeader, snapshot.RequestID)
	}
	return request, nil
}

// Run replays snapshot through handler and returns the response.
func Run(handler http.Handler, snapshot Snapshot) (*httptest.ResponseRecorder, error) {
	if handler == nil {
		return nil, fmt.Errorf("replay handler is required")
	}

	request, err := NewRequest(snapshot)
	if err != nil {
		return nil, err
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response, nil
}

func captureHeaders(redactor *scrub.Redactor, headers http.Header, allowed []string) map[string][]string {
	out := make(map[string][]string)
	for _, header := range allowed {
		values, ok := headers[http.CanonicalHeaderKey(header)]
		if !ok {
			continue
		}
		copied := scrubValues(redactor, map[string][]string{header: values})[header]
		if len(copied) > 0 {
			out[http.CanonicalHeaderKey(header)] = copied
		}
	}
	return out
}

func scrubValues(redactor *scrub.Redactor, values map[string][]string) map[string][]string {
	out := make(map[string][]string, len(values))
	for key, rawValues := range values {
		if redactor.SensitiveKey(key) {
			out[key] = []string{fmt.Sprint(redactor.Any(key, ""))}
			continue
		}

		copied := slices.Clone(rawValues)
		out[key] = copied
	}
	return out
}

func routePattern(r *http.Request) string {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil {
		return ""
	}
	return routeContext.RoutePattern()
}
