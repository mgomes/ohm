package ohm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
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

func TestEnsureRequestIDReusesRequestWithExistingContextID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/request-id", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, "req-existing"))
	res := httptest.NewRecorder()

	got, requestID := ensureRequestID(res, request)

	if got != request {
		t.Errorf("ensureRequestID(response, request) request = %p, want %p", got, request)
	}
	if requestID != "req-existing" {
		t.Errorf("ensureRequestID(response, request) request id = %q, want %q", requestID, "req-existing")
	}
	if res.Header().Get(RequestIDHeader) != "" {
		t.Errorf("ensureRequestID(response, request) response request id = %q, want empty", res.Header().Get(RequestIDHeader))
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

func TestRecovererLogsRedactedPanicAndRendersInternalServerError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	app := New()
	app.Use(Recoverer(logger))
	app.Get("/panic/{id}", func(*Request) error {
		panic(map[string]string{"token": "secret"})
	})

	request := httptest.NewRequest(http.MethodGet, "/panic/42?token=secret", nil)
	request.Header.Set(RequestIDHeader, "req-panic")
	request.Header.Set("User-Agent", "panic-test")
	request.RemoteAddr = "192.0.2.20:4321"
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.String(), res.Code, http.StatusInternalServerError)
	}
	if res.Body.String() != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.String(), res.Body.String(), http.StatusText(http.StatusInternalServerError))
	}
	if res.Header().Get(RequestIDHeader) != "req-panic" {
		t.Errorf("App.ServeHTTP(%s %s) response request id = %q, want %q", request.Method, request.URL.String(), res.Header().Get(RequestIDHeader), "req-panic")
	}

	output := buf.String()
	if bytes.Contains(buf.Bytes(), []byte("secret")) {
		t.Errorf("panic log %q contains sensitive value", output)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", output, err)
	}

	want := map[string]any{
		"level":              "ERROR",
		"msg":                "panic",
		"request_id":         "req-panic",
		"method":             http.MethodGet,
		"path":               "/panic/42",
		"status":             float64(http.StatusInternalServerError),
		"remote_addr":        "192.0.2.20:4321",
		"user_agent":         "panic-test",
		"content_length":     float64(0),
		"route_pattern":      "/panic/{id}",
		"response_committed": false,
		"panic_type":         "map[string]string",
		"panic":              "[REDACTED]",
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("panic log field %s = %v, want %v", key, got[key], wantValue)
		}
	}
	if got["stack"] == "" {
		t.Errorf("panic log stack missing from %v", got)
	}
	if _, ok := got["duration"]; !ok {
		t.Errorf("panic log duration missing from %v", got)
	}
}

func TestRecovererLetsRequestLoggerRecordRecoveredStatus(t *testing.T) {
	var requestBuf bytes.Buffer
	var panicBuf bytes.Buffer

	app := New()
	app.Use(
		RequestLogger(slog.New(slog.NewJSONHandler(&requestBuf, nil))),
		Recoverer(slog.New(slog.NewJSONHandler(&panicBuf, nil))),
	)
	app.Get("/panic", func(*Request) error {
		panic("boom")
	})

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusInternalServerError)
	}

	var got map[string]any
	if err := json.Unmarshal(requestBuf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", requestBuf.String(), err)
	}
	if got["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("request log status = %v, want %d", got["status"], http.StatusInternalServerError)
	}
	if got["request_id"] == "" {
		t.Errorf("request log request_id = empty, want generated request id")
	}
}

func TestRecovererDoesNotWriteAfterCommittedResponse(t *testing.T) {
	tests := []struct {
		name       string
		write      func(http.ResponseWriter)
		wantStatus int
		wantBody   string
	}{
		{
			name: "explicit status",
			write: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("partial"))
			},
			wantStatus: http.StatusAccepted,
			wantBody:   "partial",
		},
		{
			name: "implicit status",
			write: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte("partial"))
			},
			wantStatus: http.StatusOK,
			wantBody:   "partial",
		},
		{
			name: "flushed headers",
			write: func(w http.ResponseWriter) {
				w.(http.Flusher).Flush()
			},
			wantStatus: http.StatusOK,
			wantBody:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))

			app := New()
			app.Use(Recoverer(logger))
			app.Get("/partial", func(req *Request) error {
				tt.write(req.ResponseWriter())
				panic("boom")
			})

			request := httptest.NewRequest(http.MethodGet, "/partial", nil)
			res := httptest.NewRecorder()

			app.ServeHTTP(res, request)

			if res.Code != tt.wantStatus {
				t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, tt.wantStatus)
			}
			if res.Body.String() != tt.wantBody {
				t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), tt.wantBody)
			}

			var got map[string]any
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("json.Unmarshal(%q) error = %v, want nil", buf.String(), err)
			}
			if got["status"] != float64(tt.wantStatus) {
				t.Errorf("panic log status = %v, want %d", got["status"], tt.wantStatus)
			}
			if got["response_committed"] != true {
				t.Errorf("panic log response_committed = %v, want true", got["response_committed"])
			}
		})
	}
}

func TestRecovererRendersWhenWriteHeaderPanicsBeforeCommit(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	app := New()
	app.Use(Recoverer(logger))
	app.Get("/bad-status", func(req *Request) error {
		req.ResponseWriter().WriteHeader(0)
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/bad-status", nil)
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusInternalServerError)
	}
	if res.Body.String() != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), http.StatusText(http.StatusInternalServerError))
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", buf.String(), err)
	}
	if got["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("panic log status = %v, want %d", got["status"], http.StatusInternalServerError)
	}
	if got["response_committed"] != false {
		t.Errorf("panic log response_committed = %v, want false", got["response_committed"])
	}
}

func TestRecovererDoesNotWriteAfterHijack(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	app := New()
	app.Use(Recoverer(logger))
	app.Get("/hijack", func(req *Request) error {
		hijacker, ok := req.ResponseWriter().(http.Hijacker)
		if !ok {
			panic("response writer cannot hijack")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			panic(err)
		}
		_ = conn.Close()
		panic("boom")
	})

	request := httptest.NewRequest(http.MethodGet, "/hijack", nil)
	res := newHijackRecorder(t)

	app.ServeHTTP(res, request)

	if !res.hijacked {
		t.Fatalf("App.ServeHTTP(%s %s) hijacked = false, want true", request.Method, request.URL.Path)
	}
	if res.body.String() != "" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want empty", request.Method, request.URL.Path, res.body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", buf.String(), err)
	}
	if got["status"] != float64(http.StatusSwitchingProtocols) {
		t.Errorf("panic log status = %v, want %d", got["status"], http.StatusSwitchingProtocols)
	}
	if got["response_committed"] != true {
		t.Errorf("panic log response_committed = %v, want true", got["response_committed"])
	}
}

func TestRecovererRendersAfterInformationalResponse(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	app := New()
	app.Use(Recoverer(logger))
	app.Get("/early", func(req *Request) error {
		req.ResponseWriter().WriteHeader(http.StatusEarlyHints)
		panic("boom")
	})

	request := httptest.NewRequest(http.MethodGet, "/early", nil)
	res := newInformationalRecorder()

	app.ServeHTTP(res, request)

	if len(res.informational) != 1 || res.informational[0] != http.StatusEarlyHints {
		t.Fatalf("App.ServeHTTP(%s %s) informational statuses = %v, want [%d]", request.Method, request.URL.Path, res.informational, http.StatusEarlyHints)
	}
	if res.status != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) final status = %d, want %d", request.Method, request.URL.Path, res.status, http.StatusInternalServerError)
	}
	if res.body.String() != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.body.String(), http.StatusText(http.StatusInternalServerError))
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", buf.String(), err)
	}
	if got["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("panic log status = %v, want %d", got["status"], http.StatusInternalServerError)
	}
	if got["response_committed"] != false {
		t.Errorf("panic log response_committed = %v, want false", got["response_committed"])
	}
}

func TestRecovererPreservesHTTPAbortHandlerPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	app := New()
	app.Use(Recoverer(logger))
	app.Get("/abort", func(*Request) error {
		panic(http.ErrAbortHandler)
	})

	request := httptest.NewRequest(http.MethodGet, "/abort", nil)
	res := httptest.NewRecorder()

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		app.ServeHTTP(res, request)
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("App.ServeHTTP(%s %s) panic = %v, want %v", request.Method, request.URL.Path, recovered, http.ErrAbortHandler)
	}
	if buf.Len() != 0 {
		t.Errorf("Recoverer(logger) log = %q, want empty for http.ErrAbortHandler", buf.String())
	}
}

type informationalRecorder struct {
	header        http.Header
	body          bytes.Buffer
	status        int
	informational []int
}

func newInformationalRecorder() *informationalRecorder {
	return &informationalRecorder{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (r *informationalRecorder) Header() http.Header {
	return r.header
}

func (r *informationalRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(body)
}

func (r *informationalRecorder) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		r.informational = append(r.informational, status)
		return
	}
	r.status = status
}

type hijackRecorder struct {
	*informationalRecorder
	hijacked bool
	peer     net.Conn
}

func newHijackRecorder(t *testing.T) *hijackRecorder {
	t.Helper()

	recorder := &hijackRecorder{
		informationalRecorder: newInformationalRecorder(),
	}
	t.Cleanup(func() {
		if recorder.peer != nil {
			_ = recorder.peer.Close()
		}
	})
	return recorder
}

func (r *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	server, client := net.Pipe()
	r.peer = server
	r.hijacked = true
	rw := bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client))
	return client, rw, nil
}
