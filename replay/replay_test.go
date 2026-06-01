package replay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	got := Capture(request, WithClock(func() time.Time {
		return now
	}), WithHeaders("Accept", "Authorization", ohm.RequestIDHeader))

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
