package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
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

var defaultResponseHeaders = []string{
	"Content-Type",
}

// Snapshot captures enough request data to replay a handler request.
type Snapshot struct {
	Version            int                 `json:"version"`
	Method             string              `json:"method"`
	Path               string              `json:"path"`
	Query              map[string][]string `json:"query,omitempty"`
	Headers            map[string][]string `json:"headers,omitempty"`
	RequestID          string              `json:"request_id,omitempty"`
	RoutePattern       string              `json:"route_pattern,omitempty"`
	RouteParams        map[string]string   `json:"route_params,omitempty"`
	ApplicationVersion string              `json:"application_version,omitempty"`
	Environment        string              `json:"environment,omitempty"`
	FeatureFlags       map[string]string   `json:"feature_flags,omitempty"`
	CapturedAt         time.Time           `json:"captured_at"`
	Body               []byte              `json:"body,omitempty"`
	BodyOmitted        bool                `json:"body_omitted"`
	ExpectedResponse   *ExpectedResponse   `json:"expected_response,omitempty"`
}

// ExpectedResponse captures the response assertions used by generated replay tests.
type ExpectedResponse struct {
	Status      int                 `json:"status"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Body        []byte              `json:"body,omitempty"`
	BodyOmitted bool                `json:"body_omitted,omitempty"`
}

// Option configures request snapshot capture.
type Option func(*captureOptions)

type captureOptions struct {
	headers            []string
	redactor           *scrub.Redactor
	now                func() time.Time
	bodyLimit          int64
	applicationVersion string
	environment        string
	featureFlags       map[string]string
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

// WithBodyLimit captures the request body when it is no larger than limit bytes.
func WithBodyLimit(limit int64) Option {
	return func(opts *captureOptions) {
		if limit >= 0 {
			opts.bodyLimit = limit
		}
	}
}

// WithApplicationVersion includes the application version in captured snapshots.
func WithApplicationVersion(version string) Option {
	return func(opts *captureOptions) {
		opts.applicationVersion = version
	}
}

// WithEnvironment includes the application environment in captured snapshots.
func WithEnvironment(environment string) Option {
	return func(opts *captureOptions) {
		opts.environment = environment
	}
}

// WithFeatureFlags includes scrubbed feature flag values in captured snapshots.
func WithFeatureFlags(flags map[string]string) Option {
	return func(opts *captureOptions) {
		opts.featureFlags = cloneStringMap(flags)
	}
}

// Capture creates a scrubbed request snapshot.
func Capture(r *http.Request, opts ...Option) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, fmt.Errorf("request is required")
	}

	cfg := captureOptions{
		headers:   defaultHeaders,
		redactor:  scrub.New(),
		now:       time.Now,
		bodyLimit: -1,
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

	snapshot := Snapshot{
		Version:            snapshotVersion,
		Method:             r.Method,
		Path:               r.URL.Path,
		Query:              query,
		Headers:            headers,
		RequestID:          requestID,
		RoutePattern:       routePattern(r),
		RouteParams:        routeParams(cfg.redactor, r),
		ApplicationVersion: cfg.applicationVersion,
		Environment:        cfg.environment,
		FeatureFlags:       scrubFeatureFlags(cfg.redactor, cfg.featureFlags),
		CapturedAt:         cfg.now().UTC(),
		BodyOmitted:        true,
	}
	if cfg.bodyLimit >= 0 {
		body, omitted, err := captureBody(r, cfg.bodyLimit)
		if err != nil {
			return Snapshot{}, fmt.Errorf("capture request body: %w", err)
		}
		if !omitted {
			body, omitted, err = scrubBody(cfg.redactor, r.Header.Get("Content-Type"), body)
			if err != nil {
				return Snapshot{}, fmt.Errorf("scrub request body: %w", err)
			}
			if !omitted && int64(len(body)) > cfg.bodyLimit {
				body = nil
				omitted = true
			}
		}
		snapshot.Body = body
		snapshot.BodyOmitted = omitted
	}
	return snapshot, nil
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
	return run(context.Background(), handler, snapshot)
}

// ExpectedResponseFrom captures stable response fields from a replay result.
func ExpectedResponseFrom(response *httptest.ResponseRecorder) (ExpectedResponse, error) {
	if response == nil {
		return ExpectedResponse{}, fmt.Errorf("response is required")
	}

	result := response.Result()
	expected := ExpectedResponse{
		Status:  result.StatusCode,
		Headers: captureHeaders(scrub.New(), result.Header, defaultResponseHeaders),
	}
	if response.Body == nil {
		expected.BodyOmitted = true
		return expected, nil
	}
	expected.Body = slices.Clone(response.Body.Bytes())
	return expected, nil
}

func run(ctx context.Context, handler http.Handler, snapshot Snapshot) (*httptest.ResponseRecorder, error) {
	if handler == nil {
		return nil, fmt.Errorf("replay handler is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	request, err := NewRequest(snapshot)
	if err != nil {
		return nil, err
	}
	request = request.WithContext(ctx)

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

func captureBody(r *http.Request, limit int64) ([]byte, bool, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, false, nil
	}

	readLimit := limit + 1
	if limit == maxInt64 {
		readLimit = limit
	}
	body := r.Body
	captured, err := io.ReadAll(io.LimitReader(body, readLimit))
	r.Body = bodyWithPrefix(body, captured)
	if err != nil {
		return nil, true, err
	}
	if int64(len(captured)) > limit {
		return nil, true, nil
	}
	return captured, false, nil
}

func bodyWithPrefix(body io.ReadCloser, prefix []byte) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(prefix), body),
		Closer: body,
	}
}

func scrubBody(redactor *scrub.Redactor, contentType string, body []byte) ([]byte, bool, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, true, nil
	}

	switch {
	case mediaType == "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, true, nil
		}
		return []byte(url.Values(scrubValues(redactor, values)).Encode()), false, nil
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		var value any
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, true, nil
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, true, nil
		}
		switch value.(type) {
		case map[string]any, []any:
		default:
			return nil, true, nil
		}
		scrubbed, err := json.Marshal(redactor.Any("", value))
		if err != nil {
			return nil, false, err
		}
		return scrubbed, false, nil
	default:
		return nil, true, nil
	}
}

func routePattern(r *http.Request) string {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil {
		return ""
	}
	return routeContext.RoutePattern()
}

func routeParams(redactor *scrub.Redactor, r *http.Request) map[string]string {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil || len(routeContext.URLParams.Keys) == 0 {
		return nil
	}

	params := make(map[string]string, len(routeContext.URLParams.Keys))
	for i, key := range routeContext.URLParams.Keys {
		if key == "" {
			continue
		}
		value := ""
		if i < len(routeContext.URLParams.Values) {
			value = routeContext.URLParams.Values[i]
		}
		if redactor.SensitiveKey(key) {
			value = fmt.Sprint(redactor.Any(key, value))
		}
		params[key] = value
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func scrubFeatureFlags(redactor *scrub.Redactor, flags map[string]string) map[string]string {
	if len(flags) == 0 {
		return nil
	}

	out := make(map[string]string, len(flags))
	for key, value := range flags {
		if redactor.SensitiveKey(key) {
			value = fmt.Sprint(redactor.Any(key, value))
		}
		out[key] = value
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

const maxInt64 = int64(^uint64(0) >> 1)
