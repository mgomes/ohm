package ohm

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCompressGzipsAcceptedResponse(t *testing.T) {
	body := strings.Repeat("hello ", 32)

	app := New()
	app.Use(Compress(5))
	app.Get("/hello", func(req *Request) error {
		req.ResponseWriter().Header().Set("Accept-Ranges", "bytes")
		req.ResponseWriter().Header().Set("Content-Length", strconv.Itoa(len(body)))
		req.PlainText(http.StatusOK, body)
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/hello", nil)
	request.Header.Set("Accept-Encoding", "br, gzip;q=1")
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want %q", request.Method, request.URL.Path, got, "gzip")
	}
	if got := res.Header().Get("Content-Length"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want empty", request.Method, request.URL.Path, got)
	}
	if got := res.Header().Get("Accept-Ranges"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) Accept-Ranges = %q, want empty", request.Method, request.URL.Path, got)
	}
	if !hasVary(res.Header(), "Accept-Encoding") {
		t.Errorf("App.ServeHTTP(%s %s) Vary = %v, want Accept-Encoding", request.Method, request.URL.Path, res.Header().Values("Vary"))
	}
	if got := readGzipBody(t, res.Body.Bytes()); got != body {
		t.Errorf("App.ServeHTTP(%s %s) decompressed body = %q, want %q", request.Method, request.URL.Path, got, body)
	}
}

func TestCompressSkipsWhenGzipIsNotAccepted(t *testing.T) {
	body := "hello"

	app := New()
	app.Use(Compress(5))
	app.Get("/hello", func(req *Request) error {
		req.PlainText(http.StatusOK, body)
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/hello", nil)
	request.Header.Set("Accept-Encoding", "br, gzip;q=0")
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want empty", request.Method, request.URL.Path, got)
	}
	if !hasVary(res.Header(), "Accept-Encoding") {
		t.Errorf("App.ServeHTTP(%s %s) Vary = %v, want Accept-Encoding", request.Method, request.URL.Path, res.Header().Values("Vary"))
	}
	if got := res.Body.String(); got != body {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, got, body)
	}
}

func TestCompressAcceptsWildcardEncoding(t *testing.T) {
	body := "wildcard response"

	app := New()
	app.Use(Compress(5))
	app.Get("/wildcard", func(req *Request) error {
		req.PlainText(http.StatusOK, body)
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/wildcard", nil)
	request.Header.Set("Accept-Encoding", "*")
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want %q", request.Method, request.URL.Path, got, "gzip")
	}
	if got := readGzipBody(t, res.Body.Bytes()); got != body {
		t.Errorf("App.ServeHTTP(%s %s) decompressed body = %q, want %q", request.Method, request.URL.Path, got, body)
	}
}

func TestCompressSkipsRangeResponses(t *testing.T) {
	body := "part"

	app := New()
	app.Use(Compress(5))
	app.Get("/range", func(req *Request) error {
		req.ResponseWriter().Header().Set("Accept-Ranges", "bytes")
		req.ResponseWriter().Header().Set("Content-Length", strconv.Itoa(len(body)))
		req.ResponseWriter().Header().Set("Content-Type", "text/plain; charset=utf-8")
		req.ResponseWriter().WriteHeader(http.StatusPartialContent)
		_, _ = req.ResponseWriter().Write([]byte(body))
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/range", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Range", "bytes=0-3")
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusPartialContent {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusPartialContent)
	}
	if got := res.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want empty", request.Method, request.URL.Path, got)
	}
	if got := res.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("App.ServeHTTP(%s %s) Accept-Ranges = %q, want %q", request.Method, request.URL.Path, got, "bytes")
	}
	if got := res.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", request.Method, request.URL.Path, got, strconv.Itoa(len(body)))
	}
	if got := res.Body.String(); got != body {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, got, body)
	}
}

func TestCompressStaticFileRangeRequest(t *testing.T) {
	body := strings.Repeat("body { color: black; }\n", 16)
	staticRoot := t.TempDir()
	staticPath := filepath.Join(staticRoot, "app.css")
	if err := os.WriteFile(staticPath, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", staticPath, err)
	}

	app := New()
	app.Use(Compress(5))
	app.Static("/assets/*", staticRoot)

	fullRequest := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)
	fullRequest.Header.Set("Accept-Encoding", "gzip")
	fullResponse := httptest.NewRecorder()

	app.ServeHTTP(fullResponse, fullRequest)

	if fullResponse.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", fullRequest.Method, fullRequest.URL.Path, fullResponse.Code, http.StatusOK)
	}
	if got := fullResponse.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want %q", fullRequest.Method, fullRequest.URL.Path, got, "gzip")
	}
	if got := fullResponse.Header().Get("Accept-Ranges"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) Accept-Ranges = %q, want empty", fullRequest.Method, fullRequest.URL.Path, got)
	}
	if got := readGzipBody(t, fullResponse.Body.Bytes()); got != body {
		t.Errorf("App.ServeHTTP(%s %s) decompressed body = %q, want %q", fullRequest.Method, fullRequest.URL.Path, got, body)
	}

	rangeRequest := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)
	rangeRequest.Header.Set("Accept-Encoding", "gzip")
	rangeRequest.Header.Set("Range", "bytes=0-3")
	rangeResponse := httptest.NewRecorder()

	app.ServeHTTP(rangeResponse, rangeRequest)

	if rangeResponse.Code != http.StatusPartialContent {
		t.Fatalf("App.ServeHTTP(%s %s) range status = %d, want %d", rangeRequest.Method, rangeRequest.URL.Path, rangeResponse.Code, http.StatusPartialContent)
	}
	if got := rangeResponse.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) range Content-Encoding = %q, want empty", rangeRequest.Method, rangeRequest.URL.Path, got)
	}
	if got := rangeResponse.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("App.ServeHTTP(%s %s) range Accept-Ranges = %q, want %q", rangeRequest.Method, rangeRequest.URL.Path, got, "bytes")
	}
	if got := rangeResponse.Body.String(); got != "body" {
		t.Errorf("App.ServeHTTP(%s %s) range body = %q, want %q", rangeRequest.Method, rangeRequest.URL.Path, got, "body")
	}
}

func TestCompressSkipsStatusWithoutResponseBody(t *testing.T) {
	app := New()
	app.Use(Compress(5))
	app.Get("/empty", func(req *Request) error {
		req.ResponseWriter().Header().Set("Content-Type", "text/plain; charset=utf-8")
		req.NoContent()
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/empty", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusNoContent {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want empty", request.Method, request.URL.Path, got)
	}
	if got := res.Body.Len(); got != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, got)
	}
}

func TestCompressSkipsAlreadyEncodedResponses(t *testing.T) {
	body := "already encoded"

	app := New()
	app.Use(Compress(5))
	app.Get("/encoded", func(req *Request) error {
		req.ResponseWriter().Header().Set("Content-Encoding", "br")
		req.ResponseWriter().Header().Set("Content-Length", strconv.Itoa(len(body)))
		req.PlainText(http.StatusOK, body)
		return nil
	})

	request := httptest.NewRequest(http.MethodGet, "/encoded", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Encoding"); got != "br" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want %q", request.Method, request.URL.Path, got, "br")
	}
	if got := res.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", request.Method, request.URL.Path, got, strconv.Itoa(len(body)))
	}
	if hasVary(res.Header(), "Accept-Encoding") {
		t.Errorf("App.ServeHTTP(%s %s) Vary = %v, want no Accept-Encoding", request.Method, request.URL.Path, res.Header().Values("Vary"))
	}
	if got := res.Body.String(); got != body {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, got, body)
	}
}

func readGzipBody(t testing.TB, body []byte) string {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip.NewReader(response body) error = %v, want nil", err)
	}
	defer reader.Close()

	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll(gzip response body) error = %v, want nil", err)
	}
	return string(decoded)
}

func hasVary(header http.Header, want string) bool {
	want = http.CanonicalHeaderKey(want)
	for _, value := range header.Values("Vary") {
		for _, part := range strings.Split(value, ",") {
			if http.CanonicalHeaderKey(strings.TrimSpace(part)) == want {
				return true
			}
		}
	}
	return false
}
