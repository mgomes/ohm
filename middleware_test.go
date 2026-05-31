package ohm

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgomes/ohm/scrub"
)

func TestRequestLoggerLogsExpectedFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(scrub.NewHandler(slog.NewJSONHandler(&buf, nil)))

	app := New()
	app.Use(RequestLogger(logger))
	app.Get("/posts/{id}", func(req *Request) error {
		req.PlainText(http.StatusAccepted, req.Param("id"))
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/posts/42?token=secret", nil)
	request.Header.Set(RequestIDHeader, "req-test")
	request.Header.Set("User-Agent", "ohm-test")
	request.RemoteAddr = "192.0.2.10:1234"
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.String(), res.Code, http.StatusAccepted)
	}
	if res.Header().Get(RequestIDHeader) != "req-test" {
		t.Errorf("App.ServeHTTP(%s %s) response request id = %q, want %q", request.Method, request.URL.String(), res.Header().Get(RequestIDHeader), "req-test")
	}

	output := buf.String()
	if bytes.Contains(buf.Bytes(), []byte("secret")) {
		t.Errorf("request log %q contains query secret", output)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", output, err)
	}

	want := map[string]any{
		"msg":            "request",
		"request_id":     "req-test",
		"method":         http.MethodGet,
		"path":           "/posts/42",
		"status":         float64(http.StatusAccepted),
		"remote_addr":    "192.0.2.10:1234",
		"user_agent":     "ohm-test",
		"content_length": float64(0),
		"route_pattern":  "/posts/{id}",
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("request log field %s = %v, want %v", key, got[key], wantValue)
		}
	}
	if _, ok := got["duration"]; !ok {
		t.Errorf("request log duration missing from %v", got)
	}
}

func TestRequestLoggerStoresRequestIDInContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	app := New()
	app.Use(RequestLogger(logger))
	app.Get("/request-id", func(req *Request) error {
		requestID, ok := RequestID(req.Context())
		if !ok {
			t.Errorf("RequestID(req.Context()) ok = false, want true")
		}
		req.PlainText(http.StatusOK, requestID)
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/request-id", nil)
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if res.Body.String() == "" {
		t.Fatalf("App.ServeHTTP(%s %s) body = empty, want generated request id", request.Method, request.URL.Path)
	}
	if res.Header().Get(RequestIDHeader) != res.Body.String() {
		t.Errorf("App.ServeHTTP(%s %s) response request id = %q, want %q", request.Method, request.URL.Path, res.Header().Get(RequestIDHeader), res.Body.String())
	}
}

func TestRequestLoggerPreservesOptionalResponseWriterInterfaces(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	app := New()
	app.Use(RequestLogger(logger))
	app.Get("/stream", func(req *Request) error {
		flusher, ok := req.ResponseWriter().(http.Flusher)
		if !ok {
			t.Errorf("req.ResponseWriter() does not implement http.Flusher")
			return nil
		}
		req.PlainText(http.StatusOK, "stream")
		flusher.Flush()
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/stream", nil)
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if !res.Flushed {
		t.Errorf("App.ServeHTTP(%s %s) flushed = false, want true", request.Method, request.URL.Path)
	}
}
