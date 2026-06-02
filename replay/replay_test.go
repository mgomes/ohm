package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mgomes/ohm"
)

func TestCaptureScrubsSnapshotAndOmitsBody(t *testing.T) {
	now := time.Date(2026, 5, 31, 15, 30, 0, 0, time.UTC)
	request := httptest.NewRequest(http.MethodPost, "/login?email=ada@example.com&token=secret", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set(ohm.RequestIDHeader, "req-test")
	requestIDHeader := http.CanonicalHeaderKey(ohm.RequestIDHeader)

	got, err := Capture(request, WithClock(func() time.Time {
		return now
	}), WithHeaders("Accept", "Authorization", ohm.RequestIDHeader))
	if err != nil {
		t.Fatalf("Capture(request) error = %v, want nil", err)
	}

	if got.Version != snapshotVersion {
		t.Errorf("Capture(request) Version = %d, want %d", got.Version, snapshotVersion)
	}
	if got.Method != http.MethodPost {
		t.Errorf("Capture(request) Method = %q, want %q", got.Method, http.MethodPost)
	}
	if got.Path != "/login" {
		t.Errorf("Capture(request) Path = %q, want %q", got.Path, "/login")
	}
	if got.Query["token"][0] != "[REDACTED]" {
		t.Errorf("Capture(request) token query = %v, want [REDACTED]", got.Query["token"])
	}
	if got.Query["email"][0] != "ada@example.com" {
		t.Errorf("Capture(request) email query = %v, want ada@example.com", got.Query["email"])
	}
	if got.Headers["Authorization"][0] != "[REDACTED]" {
		t.Errorf("Capture(request) Authorization header = %v, want [REDACTED]", got.Headers["Authorization"])
	}
	if got.Headers[requestIDHeader][0] != "req-test" {
		t.Errorf("Capture(request) request id header = %v, want req-test", got.Headers[requestIDHeader])
	}
	if got.RequestID != "req-test" {
		t.Errorf("Capture(request) RequestID = %q, want %q", got.RequestID, "req-test")
	}
	if !got.CapturedAt.Equal(now) {
		t.Errorf("Capture(request) CapturedAt = %s, want %s", got.CapturedAt, now)
	}
	if !got.BodyOmitted {
		t.Errorf("Capture(request) BodyOmitted = false, want true")
	}
}

func TestCaptureIncludesScrubbedRouteParams(t *testing.T) {
	app := ohm.New()
	var got Snapshot
	var captureErr error
	app.Get("/posts/{id}/tokens/{token}", func(req *ohm.Request) error {
		got, captureErr = Capture(req.HTTPRequest())
		req.PlainText(http.StatusOK, "ok")
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/posts/42/tokens/secret", nil)
	response := httptest.NewRecorder()

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if captureErr != nil {
		t.Fatalf("Capture(request) error = %v, want nil", captureErr)
	}
	if got.RoutePattern != "/posts/{id}/tokens/{token}" {
		t.Errorf("Capture(request) RoutePattern = %q, want %q", got.RoutePattern, "/posts/{id}/tokens/{token}")
	}
	if got.RouteParams["id"] != "42" {
		t.Errorf("Capture(request) route param id = %q, want %q", got.RouteParams["id"], "42")
	}
	if got.RouteParams["token"] != "[REDACTED]" {
		t.Errorf("Capture(request) route param token = %q, want [REDACTED]", got.RouteParams["token"])
	}
}

func TestCaptureIncludesOptionalMetadata(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	featureFlags := map[string]string{
		"checkout_redesign": "enabled",
		"api_token":         "secret",
	}

	got, err := Capture(request,
		WithApplicationVersion("v1.2.3"),
		WithEnvironment("test"),
		WithFeatureFlags(featureFlags),
	)
	if err != nil {
		t.Fatalf("Capture(request, metadata options) error = %v, want nil", err)
	}
	featureFlags["checkout_redesign"] = "disabled"

	if got.ApplicationVersion != "v1.2.3" {
		t.Errorf("Capture(request, WithApplicationVersion(v1.2.3)) ApplicationVersion = %q, want %q", got.ApplicationVersion, "v1.2.3")
	}
	if got.Environment != "test" {
		t.Errorf("Capture(request, WithEnvironment(test)) Environment = %q, want %q", got.Environment, "test")
	}
	if got.FeatureFlags["checkout_redesign"] != "enabled" {
		t.Errorf("Capture(request, WithFeatureFlags(flags)) checkout_redesign = %q, want %q", got.FeatureFlags["checkout_redesign"], "enabled")
	}
	if got.FeatureFlags["api_token"] != "[REDACTED]" {
		t.Errorf("Capture(request, WithFeatureFlags(flags)) api_token = %q, want [REDACTED]", got.FeatureFlags["api_token"])
	}
}

func TestCaptureIncludesPrincipalReference(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/account", nil)

	got, err := Capture(request, WithPrincipal(PrincipalRef{Kind: "user", ID: "user_123"}))
	if err != nil {
		t.Fatalf("Capture(request, WithPrincipal(user)) error = %v, want nil", err)
	}

	if got.Principal == nil {
		t.Fatalf("Capture(request, WithPrincipal(user)) Principal = nil, want reference")
	}
	if got.Principal.Kind != "user" {
		t.Errorf("Capture(request, WithPrincipal(user)) Principal.Kind = %q, want %q", got.Principal.Kind, "user")
	}
	if got.Principal.ID != "user_123" {
		t.Errorf("Capture(request, WithPrincipal(user)) Principal.ID = %q, want %q", got.Principal.ID, "user_123")
	}
}

func TestCaptureRedactsSensitivePrincipalKinds(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/account", nil)

	got, err := Capture(request, WithPrincipal(PrincipalRef{Kind: "token", ID: "secret"}))
	if err != nil {
		t.Fatalf("Capture(request, WithPrincipal(token)) error = %v, want nil", err)
	}

	if got.Principal == nil {
		t.Fatalf("Capture(request, WithPrincipal(token)) Principal = nil, want reference")
	}
	if got.Principal.Kind != "token" {
		t.Errorf("Capture(request, WithPrincipal(token)) Principal.Kind = %q, want %q", got.Principal.Kind, "token")
	}
	if got.Principal.ID != "[REDACTED]" {
		t.Errorf("Capture(request, WithPrincipal(token)) Principal.ID = %q, want [REDACTED]", got.Principal.ID)
	}
}

func TestCaptureCapturesBodyWithinLimitAndRestoresRequestBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader("title=hello"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	got, err := Capture(request, WithBodyLimit(64))
	if err != nil {
		t.Fatalf("Capture(request, WithBodyLimit(64)) error = %v, want nil", err)
	}

	if string(got.Body) != "title=hello" {
		t.Errorf("Capture(request, WithBodyLimit(64)) Body = %q, want %q", got.Body, "title=hello")
	}
	if got.BodyOmitted {
		t.Errorf("Capture(request, WithBodyLimit(64)) BodyOmitted = true, want false")
	}

	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(restored request body) error = %v, want nil", err)
	}
	if string(restored) != "title=hello" {
		t.Errorf("restored request body = %q, want %q", restored, "title=hello")
	}
}

func TestCaptureScrubsFormBodyBeforeStoringSnapshot(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=ada&password=secret&token=abc"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	got, err := Capture(request, WithBodyLimit(128))
	if err != nil {
		t.Fatalf("Capture(request, WithBodyLimit(128)) error = %v, want nil", err)
	}

	if bytes.Contains(got.Body, []byte("secret")) || bytes.Contains(got.Body, []byte("abc")) {
		t.Fatalf("Capture(request, WithBodyLimit(128)) Body = %q, want sensitive values redacted", got.Body)
	}
	values, err := url.ParseQuery(string(got.Body))
	if err != nil {
		t.Fatalf("url.ParseQuery(%q) error = %v, want nil", got.Body, err)
	}
	if values.Get("username") != "ada" {
		t.Errorf("scrubbed form username = %q, want %q", values.Get("username"), "ada")
	}
	if values.Get("password") != "[REDACTED]" {
		t.Errorf("scrubbed form password = %q, want [REDACTED]", values.Get("password"))
	}
	if values.Get("token") != "[REDACTED]" {
		t.Errorf("scrubbed form token = %q, want [REDACTED]", values.Get("token"))
	}
}

func TestCaptureScrubsJSONBodyBeforeStoringSnapshot(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"ada","token":"secret","nested":{"password":"hidden"}}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")

	got, err := Capture(request, WithBodyLimit(128))
	if err != nil {
		t.Fatalf("Capture(request, WithBodyLimit(128)) error = %v, want nil", err)
	}

	if bytes.Contains(got.Body, []byte("secret")) || bytes.Contains(got.Body, []byte("hidden")) {
		t.Fatalf("Capture(request, WithBodyLimit(128)) Body = %q, want sensitive values redacted", got.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(got.Body, &body); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", got.Body, err)
	}
	if body["username"] != "ada" {
		t.Errorf("scrubbed JSON username = %v, want %q", body["username"], "ada")
	}
	if body["token"] != "[REDACTED]" {
		t.Errorf("scrubbed JSON token = %v, want [REDACTED]", body["token"])
	}
	nested := body["nested"].(map[string]any)
	if nested["password"] != "[REDACTED]" {
		t.Errorf("scrubbed JSON nested password = %v, want [REDACTED]", nested["password"])
	}
}

func TestCaptureOmitsBodyWhenScrubbingExceedsLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"token":"x"}`))
	request.Header.Set("Content-Type", "application/json")

	got, err := Capture(request, WithBodyLimit(13))
	if err != nil {
		t.Fatalf("Capture(request, WithBodyLimit(13)) error = %v, want nil", err)
	}

	if !got.BodyOmitted {
		t.Errorf("Capture(request, WithBodyLimit(13)) BodyOmitted = false, want true")
	}
	if len(got.Body) != 0 {
		t.Errorf("Capture(request, WithBodyLimit(13)) Body = %q, want empty", got.Body)
	}
}

func TestCaptureOmitsUnsupportedBodyContentTypes(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader("secret"))
	request.Header.Set("Content-Type", "text/plain")

	got, err := Capture(request, WithBodyLimit(64))
	if err != nil {
		t.Fatalf("Capture(request, WithBodyLimit(64)) error = %v, want nil", err)
	}

	if !got.BodyOmitted {
		t.Errorf("Capture(request, WithBodyLimit(64)) BodyOmitted = false, want true")
	}
	if len(got.Body) != 0 {
		t.Errorf("Capture(request, WithBodyLimit(64)) Body = %q, want empty", got.Body)
	}

	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(restored request body) error = %v, want nil", err)
	}
	if string(restored) != "secret" {
		t.Errorf("restored request body = %q, want %q", restored, "secret")
	}
}

func TestCaptureOmitsBodyOverLimitAndRestoresRequestBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader("abcdef"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	got, err := Capture(request, WithBodyLimit(3))
	if err != nil {
		t.Fatalf("Capture(request, WithBodyLimit(3)) error = %v, want nil", err)
	}

	if !got.BodyOmitted {
		t.Errorf("Capture(request, WithBodyLimit(3)) BodyOmitted = false, want true")
	}
	if len(got.Body) != 0 {
		t.Errorf("Capture(request, WithBodyLimit(3)) Body = %q, want empty", got.Body)
	}

	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(restored request body) error = %v, want nil", err)
	}
	if string(restored) != "abcdef" {
		t.Errorf("restored request body = %q, want %q", restored, "abcdef")
	}
}

func TestCaptureReportsBodyReadErrors(t *testing.T) {
	wantErr := errors.New("read failed")
	request := httptest.NewRequest(http.MethodPost, "/posts", nil)
	request.Body = failingReadCloser{err: wantErr}

	_, err := Capture(request, WithBodyLimit(64))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Capture(request, WithBodyLimit(64)) error = %v, want %v", err, wantErr)
	}
}

func TestRunReplaysSnapshotThroughHandler(t *testing.T) {
	app := ohm.New()
	app.Get("/posts/{id}", func(req *ohm.Request) error {
		req.JSON(http.StatusOK, map[string]string{
			"id":     req.Param("id"),
			"filter": req.HTTPRequest().URL.Query().Get("filter"),
		})
		return nil
	})

	response, err := Run(app, Snapshot{
		Version:   snapshotVersion,
		Method:    http.MethodGet,
		Path:      "/posts/42",
		Query:     map[string][]string{"filter": {"recent"}},
		RequestID: "req-replay",
	})
	if err != nil {
		t.Fatalf("Run(app, snapshot) error = %v, want nil", err)
	}

	if response.Code != http.StatusOK {
		t.Fatalf("Run(app, snapshot) status = %d, want %d", response.Code, http.StatusOK)
	}

	var got map[string]string
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got["id"] != "42" {
		t.Errorf("Run(app, snapshot) id = %q, want %q", got["id"], "42")
	}
	if got["filter"] != "recent" {
		t.Errorf("Run(app, snapshot) filter = %q, want %q", got["filter"], "recent")
	}
}

func TestExpectedResponseFromCapturesStableResponseFields(t *testing.T) {
	response := httptest.NewRecorder()
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("Location", "/posts")
	response.Header().Set("Set-Cookie", "session=secret")
	response.WriteHeader(http.StatusCreated)
	response.Body = bytes.NewBufferString("created")

	got, err := ExpectedResponseFrom(response)
	if err != nil {
		t.Fatalf("ExpectedResponseFrom(response) error = %v, want nil", err)
	}

	if got.Status != http.StatusCreated {
		t.Errorf("ExpectedResponseFrom(response) Status = %d, want %d", got.Status, http.StatusCreated)
	}
	if string(got.Body) != "created" {
		t.Errorf("ExpectedResponseFrom(response) Body = %q, want %q", got.Body, "created")
	}
	if got.Headers["Content-Type"][0] != "text/plain; charset=utf-8" {
		t.Errorf("ExpectedResponseFrom(response) Content-Type = %v, want text/plain", got.Headers["Content-Type"])
	}
	if _, ok := got.Headers["Location"]; ok {
		t.Errorf("ExpectedResponseFrom(response) Location present = true, want false")
	}
	if _, ok := got.Headers["Set-Cookie"]; ok {
		t.Errorf("ExpectedResponseFrom(response) Set-Cookie present = true, want false")
	}
}

func TestExpectedResponseFromHandlesUnrecordedBody(t *testing.T) {
	response := httptest.NewRecorder()
	response.Body = nil
	response.WriteHeader(http.StatusNoContent)

	got, err := ExpectedResponseFrom(response)
	if err != nil {
		t.Fatalf("ExpectedResponseFrom(response without body recorder) error = %v, want nil", err)
	}

	if got.Status != http.StatusNoContent {
		t.Errorf("ExpectedResponseFrom(response without body recorder) Status = %d, want %d", got.Status, http.StatusNoContent)
	}
	if !got.BodyOmitted {
		t.Errorf("ExpectedResponseFrom(response without body recorder) BodyOmitted = false, want true")
	}
}

func TestNewRequestRejectsInvalidSnapshots(t *testing.T) {
	_, err := NewRequest(Snapshot{Path: "/posts"})
	if err == nil {
		t.Fatalf("NewRequest(snapshot without method) error = nil, want non-nil")
	}

	_, err = NewRequest(Snapshot{Method: http.MethodGet})
	if err == nil {
		t.Fatalf("NewRequest(snapshot without path) error = nil, want non-nil")
	}
}

type failingReadCloser struct {
	err error
}

func (r failingReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r failingReadCloser) Close() error {
	return nil
}
