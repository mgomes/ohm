package ohm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAppRoutesRequestsThroughOhmHandler(t *testing.T) {
	app := New()
	app.Get("/posts/{id}", func(req *Request) error {
		req.JSON(http.StatusOK, map[string]string{
			"id":      req.Param("id"),
			"pattern": req.RoutePattern(),
		})
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/posts/42", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}

	var got map[string]string
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}

	want := map[string]string{
		"id":      "42",
		"pattern": "/posts/{id}",
	}
	if got["id"] != want["id"] {
		t.Errorf("App.ServeHTTP(%s %s) response id = %q, want %q", request.Method, request.URL.Path, got["id"], want["id"])
	}
	if got["pattern"] != want["pattern"] {
		t.Errorf("App.ServeHTTP(%s %s) response pattern = %q, want %q", request.Method, request.URL.Path, got["pattern"], want["pattern"])
	}
}

func TestAppAnyRoutesRequestsThroughOhmHandler(t *testing.T) {
	app := New()
	app.Any("/proxy/{id}/*", func(req *Request) error {
		req.PlainText(http.StatusOK, req.HTTPRequest().Method+" "+req.Param("id")+" "+req.RoutePattern())
		return nil
	})

	tests := []struct {
		method string
	}{
		{method: http.MethodGet},
		{method: http.MethodPost},
		{method: "PROPFIND"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			res := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/proxy/tun/webhooks/stripe", nil)

			app.ServeHTTP(res, request)

			if res.Code != http.StatusOK {
				t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
			}
			want := tt.method + " tun /proxy/{id}/*"
			if res.Body.String() != want {
				t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), want)
			}
		})
	}
}

func TestAppAnyHTTPPreservesRequestMethod(t *testing.T) {
	app := New()
	app.AnyHTTP("/raw", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(r.Method))
	}))

	res := httptest.NewRecorder()
	request := httptest.NewRequest("PROPFIND", "/raw", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusAccepted)
	}
	if res.Body.String() != "PROPFIND" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "PROPFIND")
	}
}

func TestAppAnyRoutesPreserveMountedRoutePath(t *testing.T) {
	chi.RegisterMethod("PROPFIND")

	app := New()
	app.Any("/proxy/*", func(req *Request) error {
		req.PlainText(http.StatusOK, req.HTTPRequest().Method+" "+req.RoutePattern())
		return nil
	})

	parent := chi.NewRouter()
	parent.Mount("/api", app.HTTPHandler())

	res := httptest.NewRecorder()
	request := httptest.NewRequest("PROPFIND", "/api/proxy/webhooks/stripe", nil)

	parent.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("parent.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if res.Body.String() != "PROPFIND /api/proxy/*" {
		t.Errorf("parent.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "PROPFIND /api/proxy/*")
	}
}

func TestAppAnyDoesNotShadowMatchingMethodRoute(t *testing.T) {
	app := New()
	app.Get("/health", func(req *Request) error {
		req.PlainText(http.StatusOK, "health")
		return nil
	})
	app.Any("/*", func(req *Request) error {
		req.PlainText(http.StatusOK, "proxy")
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if res.Body.String() != "health" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "health")
	}
}

func TestAppAnyDoesNotReplaceExistingMethodRoute(t *testing.T) {
	app := New()
	app.Get("/health", func(req *Request) error {
		req.PlainText(http.StatusOK, "health")
		return nil
	})
	app.Any("/health", func(req *Request) error {
		req.PlainText(http.StatusOK, "proxy")
		return nil
	})

	getResponse := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/health", nil)

	app.ServeHTTP(getResponse, getRequest)

	if getResponse.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", getRequest.Method, getRequest.URL.Path, getResponse.Code, http.StatusOK)
	}
	if getResponse.Body.String() != "health" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", getRequest.Method, getRequest.URL.Path, getResponse.Body.String(), "health")
	}

	postResponse := httptest.NewRecorder()
	postRequest := httptest.NewRequest(http.MethodPost, "/health", nil)

	app.ServeHTTP(postResponse, postRequest)

	if postResponse.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", postRequest.Method, postRequest.URL.Path, postResponse.Code, http.StatusOK)
	}
	if postResponse.Body.String() != "proxy" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", postRequest.Method, postRequest.URL.Path, postResponse.Body.String(), "proxy")
	}
}

func TestAppAnyRoutesAfterMiddlewareRewritesPath(t *testing.T) {
	app := New()
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
			next.ServeHTTP(w, r)
		})
	})
	app.Any("/hello", func(req *Request) error {
		req.PlainText(http.StatusOK, req.HTTPRequest().Method)
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest("PROPFIND", "/hello/", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if res.Body.String() != "PROPFIND" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "PROPFIND")
	}
}

func TestAppGetHandlesHeadWithoutBody(t *testing.T) {
	app := New()
	app.Get("/hello", func(req *Request) error {
		if req.HTTPRequest().Method != http.MethodHead {
			t.Errorf("Request.HTTPRequest().Method = %q, want %q", req.HTTPRequest().Method, http.MethodHead)
		}
		req.PlainText(http.StatusOK, "hello")
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/hello", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/plain; charset=utf-8")
	}
	if got := res.Header().Get("Content-Length"); got != "5" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", request.Method, request.URL.Path, got, "5")
	}
	if res.Body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, res.Body.Len())
	}
}

func TestAppAnyHandlesHeadWithoutBody(t *testing.T) {
	app := New()
	app.Any("/hello", func(req *Request) error {
		if req.HTTPRequest().Method != http.MethodHead {
			t.Errorf("Request.HTTPRequest().Method = %q, want %q", req.HTTPRequest().Method, http.MethodHead)
		}
		req.PlainText(http.StatusOK, "hello")
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/hello", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Length"); got != "5" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", request.Method, request.URL.Path, got, "5")
	}
	if res.Body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, res.Body.Len())
	}
}

func TestAppGetHeadFallbackPreservesImplicitWriteHeaders(t *testing.T) {
	app := New()
	app.GetHTTP("/raw", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<main>hello</main>"))
	}))

	getResponse := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/raw", nil)
	app.ServeHTTP(getResponse, getRequest)

	if getResponse.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", getRequest.Method, getRequest.URL.Path, getResponse.Code, http.StatusOK)
	}
	wantContentType := getResponse.Header().Get("Content-Type")
	if wantContentType == "" {
		t.Fatalf("App.ServeHTTP(%s %s) Content-Type is empty, want inferred type", getRequest.Method, getRequest.URL.Path)
	}

	headResponse := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, "/raw", nil)
	app.ServeHTTP(headResponse, headRequest)

	if headResponse.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", headRequest.Method, headRequest.URL.Path, headResponse.Code, http.StatusOK)
	}
	if got := headResponse.Header().Get("Content-Type"); got != wantContentType {
		t.Errorf("App.ServeHTTP(%s %s) Content-Type = %q, want %q", headRequest.Method, headRequest.URL.Path, got, wantContentType)
	}
	if got := headResponse.Header().Get("Content-Length"); got != "18" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", headRequest.Method, headRequest.URL.Path, got, "18")
	}
	if headResponse.Body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", headRequest.Method, headRequest.URL.Path, headResponse.Body.Len())
	}
}

func TestAppGetHeadFallbackInfersBodyHeadersAfterWriteHeader(t *testing.T) {
	app := New()
	app.GetHTTP("/raw", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/raw", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusCreated)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if got := response.Header().Get("Content-Length"); got != "31" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", request.Method, request.URL.Path, got, "31")
	}
	if response.Body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, response.Body.Len())
	}
}

func TestAppGetHeadFallbackPreservesOptionalWriterInterfaces(t *testing.T) {
	app := New()
	app.Get("/copy", func(req *Request) error {
		if _, ok := req.ResponseWriter().(http.Flusher); !ok {
			return errors.New("response writer missing http.Flusher")
		}
		if _, ok := req.ResponseWriter().(io.ReaderFrom); !ok {
			return errors.New("response writer missing io.ReaderFrom")
		}
		_, err := io.Copy(req.ResponseWriter(), strings.NewReader("hello"))
		return err
	})

	res := &optionalInterfaceRecorder{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest(http.MethodHead, "/copy", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Length"); got != "5" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", request.Method, request.URL.Path, got, "5")
	}
	if res.Body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, res.Body.Len())
	}
	if res.readFromCalled {
		t.Errorf("App.ServeHTTP(%s %s) called underlying ReadFrom, want body suppressed by HEAD wrapper", request.Method, request.URL.Path)
	}
}

func TestAppGetHeadFallbackFreezesHeadersAtWriteHeader(t *testing.T) {
	app := New()
	app.GetHTTP("/late-header", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Early", "yes")
		w.WriteHeader(http.StatusAccepted)
		w.Header().Set("X-Late", "no")
	}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/late-header", nil)

	app.ServeHTTP(response, request)

	result := response.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, result.StatusCode, http.StatusAccepted)
	}
	if got := result.Header.Get("X-Early"); got != "yes" {
		t.Errorf("App.ServeHTTP(%s %s) X-Early = %q, want %q", request.Method, request.URL.Path, got, "yes")
	}
	if got := result.Header.Get("X-Late"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) X-Late = %q, want empty", request.Method, request.URL.Path, got)
	}
}

func TestAppGetHeadFallbackLetsMiddlewareObserveWrites(t *testing.T) {
	app := New()
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&transformingMiddlewareWriter{ResponseWriter: w}, r)
		})
	})
	app.Get("/hello", func(req *Request) error {
		req.PlainText(http.StatusOK, "hello")
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/hello", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Header().Get("X-Middleware-Saw-Body"); got != "yes" {
		t.Errorf("App.ServeHTTP(%s %s) X-Middleware-Saw-Body = %q, want %q", request.Method, request.URL.Path, got, "yes")
	}
	if got := response.Header().Get("Content-Encoding"); got != "test" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Encoding = %q, want %q", request.Method, request.URL.Path, got, "test")
	}
	if response.Body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, response.Body.Len())
	}
}

func TestAppHeadOverridesGetHeadFallback(t *testing.T) {
	app := New()
	app.Get("/hello", func(req *Request) error {
		req.ResponseWriter().Header().Set("X-Handler", "get")
		req.PlainText(http.StatusOK, "hello")
		return nil
	})
	app.Head("/hello", func(req *Request) error {
		req.ResponseWriter().Header().Set("X-Handler", "head")
		req.ResponseWriter().WriteHeader(http.StatusNoContent)
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/hello", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusNoContent {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("X-Handler"); got != "head" {
		t.Errorf("App.ServeHTTP(%s %s) X-Handler = %q, want %q", request.Method, request.URL.Path, got, "head")
	}
}

func TestAppGetDoesNotOverrideExistingHeadHandler(t *testing.T) {
	app := New()
	app.Head("/hello", func(req *Request) error {
		req.ResponseWriter().Header().Set("X-Handler", "head")
		req.ResponseWriter().WriteHeader(http.StatusNoContent)
		return nil
	})
	app.Get("/hello", func(req *Request) error {
		req.ResponseWriter().Header().Set("X-Handler", "get")
		req.PlainText(http.StatusOK, "hello")
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/hello", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusNoContent {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("X-Handler"); got != "head" {
		t.Errorf("App.ServeHTTP(%s %s) X-Handler = %q, want %q", request.Method, request.URL.Path, got, "head")
	}
}

func TestAppGetDoesNotOverrideExistingHeadHandlerWithRenamedParam(t *testing.T) {
	app := New()
	app.Head("/posts/{postID}", func(req *Request) error {
		req.ResponseWriter().Header().Set("X-Handler", "head")
		req.ResponseWriter().Header().Set("X-Post-ID", req.Param("postID"))
		req.ResponseWriter().WriteHeader(http.StatusNoContent)
		return nil
	})
	app.Get("/posts/{id}", func(req *Request) error {
		req.ResponseWriter().Header().Set("X-Handler", "get")
		req.ResponseWriter().Header().Set("X-Post-ID", req.Param("id"))
		req.PlainText(http.StatusOK, "post")
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/posts/42", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusNoContent {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("X-Handler"); got != "head" {
		t.Errorf("App.ServeHTTP(%s %s) X-Handler = %q, want %q", request.Method, request.URL.Path, got, "head")
	}
	if got := res.Header().Get("X-Post-ID"); got != "42" {
		t.Errorf("App.ServeHTTP(%s %s) X-Post-ID = %q, want %q", request.Method, request.URL.Path, got, "42")
	}
}

func TestAppHeadFallbackCommitsBufferedHeadersDuringPanic(t *testing.T) {
	app := New()
	app.GetHTTP("/panic", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Before-Panic", "yes")
		w.WriteHeader(http.StatusAccepted)
		panic("boom")
	}))

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/panic", nil)

	recoverUncommittedResponses(app.HTTPHandler()).ServeHTTP(res, request)

	if res.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusAccepted)
	}
	if got := res.Header().Get("X-Before-Panic"); got != "yes" {
		t.Errorf("App.ServeHTTP(%s %s) X-Before-Panic = %q, want %q", request.Method, request.URL.Path, got, "yes")
	}
	if got := res.Header().Get("X-Recovered"); got != "" {
		t.Errorf("App.ServeHTTP(%s %s) X-Recovered = %q, want empty", request.Method, request.URL.Path, got)
	}
	if res.Body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, res.Body.Len())
	}
}

func TestAppDefaultErrorHandlerRendersHTTPError(t *testing.T) {
	app := New()
	app.Get("/missing", func(req *Request) error {
		return NewHTTPError(http.StatusNotFound, "post not found", errors.New("missing post"))
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusNotFound {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusNotFound)
	}
	if res.Body.String() != "post not found" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "post not found")
	}
}

func TestAppDefaultErrorHandlerDoesNotExposeWrappedInternalError(t *testing.T) {
	app := New()
	app.Get("/error", func(req *Request) error {
		return NewHTTPError(http.StatusInternalServerError, "", errors.New("database password leaked"))
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/error", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusInternalServerError {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusInternalServerError)
	}
	if res.Body.String() != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), http.StatusText(http.StatusInternalServerError))
	}
}

func TestAppDefaultErrorHandlerDoesNotWriteAfterCommittedResponse(t *testing.T) {
	app := New()
	app.Get("/partial", func(req *Request) error {
		req.PlainText(http.StatusOK, "partial")
		return NewHTTPError(http.StatusBadRequest, "boom", errors.New("bad request"))
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/partial", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusOK {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusOK)
	}
	if res.Body.String() != "partial" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "partial")
	}
}

func TestErrorResponseUsesHTTPErrorPublicMessage(t *testing.T) {
	err := NewHTTPError(http.StatusNotFound, "post not found", errors.New("missing post"))

	status, message := ErrorResponse(err)

	if status != http.StatusNotFound {
		t.Errorf("ErrorResponse(%v) status = %d, want %d", err, status, http.StatusNotFound)
	}
	if message != "post not found" {
		t.Errorf("ErrorResponse(%v) message = %q, want %q", err, message, "post not found")
	}
}

func TestErrorResponseDoesNotExposeUnexpectedErrors(t *testing.T) {
	err := errors.New("database password leaked")

	status, message := ErrorResponse(err)

	if status != http.StatusInternalServerError {
		t.Errorf("ErrorResponse(%v) status = %d, want %d", err, status, http.StatusInternalServerError)
	}
	if message != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("ErrorResponse(%v) message = %q, want %q", err, message, http.StatusText(http.StatusInternalServerError))
	}
}

func TestRequestNoContentRendersNoContent(t *testing.T) {
	app := New()
	app.Delete("/posts/{id}", func(req *Request) error {
		req.NoContent()
		return nil
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/posts/42", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusNoContent {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusNoContent)
	}
	if res.Body.String() != "" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want empty", request.Method, request.URL.Path, res.Body.String())
	}
}

func TestRequestHTMLRendersHTML(t *testing.T) {
	app := New()
	app.Get("/", func(req *Request) error {
		return req.HTML(http.StatusCreated, HTMLFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, "<h1>Welcome</h1>")
			return err
		}))
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusCreated {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusCreated)
	}
	if got := res.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if res.Body.String() != "<h1>Welcome</h1>" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "<h1>Welcome</h1>")
	}
}

func TestRequestHTMLReturnsComponentErrorBeforeWritingResponse(t *testing.T) {
	componentErr := errors.New("template exploded")
	var gotErr error
	app := New(WithErrorHandler(func(req *Request, err error) {
		gotErr = err
		req.PlainText(http.StatusInternalServerError, "handled")
	}))
	app.Get("/", func(req *Request) error {
		return req.HTML(http.StatusOK, HTMLFunc(func(_ context.Context, w io.Writer) error {
			if _, err := io.WriteString(w, "partial"); err != nil {
				return err
			}
			return componentErr
		}))
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	app.ServeHTTP(res, request)

	if !errors.Is(gotErr, componentErr) {
		t.Errorf("App.ServeHTTP(%s %s) error = %v, want wrapped %v", request.Method, request.URL.Path, gotErr, componentErr)
	}
	if res.Code != http.StatusInternalServerError {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusInternalServerError)
	}
	if res.Body.String() != "handled" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "handled")
	}
}

func TestRequestHTMLRejectsInvalidResponse(t *testing.T) {
	var gotErr error
	app := New(WithErrorHandler(func(req *Request, err error) {
		gotErr = err
		req.PlainText(http.StatusInternalServerError, "handled")
	}))
	app.Get("/nil", func(req *Request) error {
		return req.HTML(http.StatusOK, nil)
	})
	app.Get("/status", func(req *Request) error {
		return req.HTML(0, HTMLFunc(func(context.Context, io.Writer) error {
			return nil
		}))
	})

	tests := []struct {
		path string
		want string
	}{
		{path: "/nil", want: "html renderer is required"},
		{path: "/status", want: "html status code 0 is invalid"},
	}
	for _, tt := range tests {
		gotErr = nil
		res := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, tt.path, nil)

		app.ServeHTTP(res, request)

		if gotErr == nil || !strings.Contains(gotErr.Error(), tt.want) {
			t.Errorf("App.ServeHTTP(%s %s) error = %v, want containing %q", request.Method, request.URL.Path, gotErr, tt.want)
		}
		if res.Code != http.StatusInternalServerError {
			t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusInternalServerError)
		}
		if res.Body.String() != "handled" {
			t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "handled")
		}
	}
}

func BenchmarkRenderHTMLLargeBody(b *testing.B) {
	const body = "<section><h1>Welcome</h1><p>Rendered content</p></section>"
	htmlBody := strings.Repeat(body, 2048)
	html := HTMLFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, htmlBody)
		return err
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	var writer benchmarkResponseWriter

	b.ReportAllocs()
	for b.Loop() {
		writer.reset()
		if err := RenderHTML(&writer, request, http.StatusOK, html); err != nil {
			b.Fatalf("RenderHTML(w, r, status, html) error = %v, want nil", err)
		}
		if writer.bytes != len(htmlBody) {
			b.Fatalf("RenderHTML(w, r, status, html) wrote %d bytes, want %d", writer.bytes, len(htmlBody))
		}
	}
}

type benchmarkResponseWriter struct {
	header http.Header
	status int
	bytes  int
}

func (w *benchmarkResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *benchmarkResponseWriter) Write(body []byte) (int, error) {
	w.bytes += len(body)
	return len(body), nil
}

func (w *benchmarkResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *benchmarkResponseWriter) reset() {
	clear(w.Header())
	w.status = 0
	w.bytes = 0
}

func TestAppErrorHandlerRunsAfterZeroByteReadFromError(t *testing.T) {
	readErr := errors.New("source failed before writing")
	app := New()
	app.Get("/stream", func(req *Request) error {
		readerFrom, ok := req.ResponseWriter().(io.ReaderFrom)
		if !ok {
			t.Errorf("req.ResponseWriter() implements io.ReaderFrom = false, want true")
			return nil
		}

		_, err := readerFrom.ReadFrom(errorReader{err: readErr})
		return err
	})

	res := &readerFromRecorder{}
	request := httptest.NewRequest(http.MethodGet, "/stream", nil)

	app.ServeHTTP(res, request)

	if res.readFromCalls != 1 {
		t.Errorf("App.ServeHTTP(%s %s) ReadFrom calls = %d, want %d", request.Method, request.URL.Path, res.readFromCalls, 1)
	}
	if res.status != http.StatusInternalServerError {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.status, http.StatusInternalServerError)
	}
	if res.body.String() != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.body.String(), http.StatusText(http.StatusInternalServerError))
	}
}

type readerFromRecorder struct {
	header        http.Header
	body          bytes.Buffer
	status        int
	readFromCalls int
}

func (w *readerFromRecorder) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *readerFromRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (w *readerFromRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *readerFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	w.readFromCalls++
	return io.Copy(readerFromBodyWriter{recorder: w}, src)
}

type readerFromBodyWriter struct {
	recorder *readerFromRecorder
}

func (w readerFromBodyWriter) Write(body []byte) (int, error) {
	return w.recorder.Write(body)
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestAppResponseControllerFlushPreservesFlushError(t *testing.T) {
	flushErr := errors.New("flush failed")
	var gotErr error
	app := New()
	app.Get("/stream", func(req *Request) error {
		gotErr = http.NewResponseController(req.ResponseWriter()).Flush()
		return nil
	})

	res := &flushErrorRecorder{flushErr: flushErr}
	request := httptest.NewRequest(http.MethodGet, "/stream", nil)

	app.ServeHTTP(res, request)

	if !errors.Is(gotErr, flushErr) {
		t.Errorf("ResponseController.Flush() error = %v, want %v", gotErr, flushErr)
	}
	if res.flushErrorCalls != 1 {
		t.Errorf("App.ServeHTTP(%s %s) FlushError calls = %d, want %d", request.Method, request.URL.Path, res.flushErrorCalls, 1)
	}
	if res.flushCalls != 0 {
		t.Errorf("App.ServeHTTP(%s %s) Flush calls = %d, want %d", request.Method, request.URL.Path, res.flushCalls, 0)
	}
	if res.status != http.StatusOK {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.status, http.StatusOK)
	}
}

type flushErrorRecorder struct {
	header          http.Header
	status          int
	flushErr        error
	flushCalls      int
	flushErrorCalls int
}

func (w *flushErrorRecorder) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *flushErrorRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(body), nil
}

func (w *flushErrorRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *flushErrorRecorder) Flush() {
	w.flushCalls++
	if w.status == 0 {
		w.status = http.StatusOK
	}
}

func (w *flushErrorRecorder) FlushError() error {
	w.flushErrorCalls++
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.flushErr
}

func TestAppRawResponseWriterPreservesCustomExtensions(t *testing.T) {
	var gotCustom bool
	app := New()
	app.Get("/custom", func(req *Request) error {
		custom, ok := req.RawResponseWriter().(customResponseWriterExtension)
		gotCustom = ok
		if !ok {
			return errors.New("custom response writer extension missing")
		}

		req.PlainText(http.StatusOK, custom.CustomResponseWriterValue())
		return nil
	})

	res := &customExtensionRecorder{value: "custom"}
	request := httptest.NewRequest(http.MethodGet, "/custom", nil)

	app.ServeHTTP(res, request)

	if !gotCustom {
		t.Errorf("Request.RawResponseWriter() custom extension = false, want true")
	}
	if res.status != http.StatusOK {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.status, http.StatusOK)
	}
	if res.body.String() != "custom" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.body.String(), "custom")
	}
}

func TestAppHeadRawResponseWriterPreservesCustomExtensions(t *testing.T) {
	var gotCustom bool
	app := New()
	app.Get("/custom", func(req *Request) error {
		custom, ok := req.RawResponseWriter().(customResponseWriterExtension)
		gotCustom = ok
		if !ok {
			return errors.New("custom response writer extension missing")
		}

		req.PlainText(http.StatusOK, custom.CustomResponseWriterValue())
		return nil
	})

	res := &customExtensionRecorder{value: "custom"}
	request := httptest.NewRequest(http.MethodHead, "/custom", nil)

	app.ServeHTTP(res, request)

	if !gotCustom {
		t.Errorf("Request.RawResponseWriter() custom extension = false, want true")
	}
	if res.status != http.StatusOK {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.status, http.StatusOK)
	}
	if got := res.header.Get("Content-Length"); got != "6" {
		t.Errorf("App.ServeHTTP(%s %s) Content-Length = %q, want %q", request.Method, request.URL.Path, got, "6")
	}
	if res.body.Len() != 0 {
		t.Errorf("App.ServeHTTP(%s %s) body length = %d, want 0", request.Method, request.URL.Path, res.body.Len())
	}
}

type customResponseWriterExtension interface {
	CustomResponseWriterValue() string
}

type customExtensionRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
	value  string
}

func (w *customExtensionRecorder) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *customExtensionRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (w *customExtensionRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *customExtensionRecorder) CustomResponseWriterValue() string {
	return w.value
}

func TestAppUsesCustomErrorHandler(t *testing.T) {
	app := New(WithErrorHandler(func(req *Request, err error) {
		req.PlainText(http.StatusTeapot, err.Error())
	}))
	app.Get("/error", func(req *Request) error {
		return errors.New("short and stout")
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/error", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusTeapot {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusTeapot)
	}
	if res.Body.String() != "short and stout" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "short and stout")
	}
}

func TestAppCustomErrorHandlerDoesNotRunAfterCommittedResponse(t *testing.T) {
	var handled bool
	app := New(WithErrorHandler(func(req *Request, err error) {
		handled = true
		req.PlainText(http.StatusInternalServerError, err.Error())
	}))
	app.Get("/partial", func(req *Request) error {
		req.PlainText(http.StatusAccepted, "partial")
		return errors.New("handler failed after commit")
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/partial", nil)

	app.ServeHTTP(res, request)

	if handled {
		t.Errorf("App.ServeHTTP(%s %s) custom error handler ran after committed response, want skipped", request.Method, request.URL.Path)
	}
	if res.Code != http.StatusAccepted {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusAccepted)
	}
	if res.Body.String() != "partial" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, res.Body.String(), "partial")
	}
}

func TestAppRoutesReturnsRegisteredRoutes(t *testing.T) {
	app := New()
	app.Get("/posts", func(req *Request) error {
		return nil
	})
	app.Post("/posts", func(req *Request) error {
		return nil
	})
	app.Any("/proxy/*", func(req *Request) error {
		return nil
	})

	got, err := app.Routes()
	if err != nil {
		t.Fatalf("App.Routes() error = %v, want nil", err)
	}

	want := []Route{
		{Method: http.MethodGet, Pattern: "/posts"},
		{Method: http.MethodHead, Pattern: "/posts"},
		{Method: http.MethodPost, Pattern: "/posts"},
		{Method: MethodAny, Pattern: "/proxy/*"},
	}
	if len(got) != len(want) {
		t.Fatalf("App.Routes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("App.Routes()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAppRoutesReturnsExplicitMethodsAlongsideAnyRoutes(t *testing.T) {
	app := New()
	app.Any("/proxy", func(req *Request) error {
		return nil
	})
	app.Get("/proxy", func(req *Request) error {
		return nil
	})

	got, err := app.Routes()
	if err != nil {
		t.Fatalf("App.Routes() error = %v, want nil", err)
	}

	want := []Route{
		{Method: MethodAny, Pattern: "/proxy"},
		{Method: http.MethodGet, Pattern: "/proxy"},
		{Method: http.MethodHead, Pattern: "/proxy"},
	}
	if len(got) != len(want) {
		t.Fatalf("App.Routes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("App.Routes()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAppStaticServesGetAndHead(t *testing.T) {
	staticRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticRoot, "app.css"), []byte("body { color: black; }\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(static asset) error = %v, want nil", err)
	}
	app := New()
	app.Static("/assets/*", staticRoot)

	getResponse := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)

	app.ServeHTTP(getResponse, getRequest)

	if getResponse.Code != http.StatusOK {
		t.Fatalf("App.Static(%q, %q) GET status = %d, want %d", "/assets/*", staticRoot, getResponse.Code, http.StatusOK)
	}
	if !strings.Contains(getResponse.Body.String(), "body { color: black; }") {
		t.Errorf("App.Static(%q, %q) GET body = %q, want static asset", "/assets/*", staticRoot, getResponse.Body.String())
	}

	headResponse := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, "/assets/app.css", nil)

	app.ServeHTTP(headResponse, headRequest)

	if headResponse.Code != http.StatusOK {
		t.Errorf("App.Static(%q, %q) HEAD status = %d, want %d", "/assets/*", staticRoot, headResponse.Code, http.StatusOK)
	}
	if headResponse.Body.Len() != 0 {
		t.Errorf("App.Static(%q, %q) HEAD body length = %d, want 0", "/assets/*", staticRoot, headResponse.Body.Len())
	}
}

func TestAppUsesNotFoundHandler(t *testing.T) {
	app := New()
	app.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusNotFound {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusNotFound)
	}
	if !strings.Contains(res.Body.String(), "missing") {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want missing message", request.Method, request.URL.Path, res.Body.String())
	}
}

func TestAppUsesMethodNotAllowedHandler(t *testing.T) {
	app := New()
	app.Get("/posts", func(req *Request) error {
		return nil
	})
	app.Head("/posts", func(req *Request) error {
		return nil
	})
	app.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request, allowedMethods []string) {
		for _, method := range allowedMethods {
			w.Header().Add("Allow", method)
		}
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	})

	res := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/posts", nil)

	app.ServeHTTP(res, request)

	if res.Code != http.StatusMethodNotAllowed {
		t.Errorf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, res.Code, http.StatusMethodNotAllowed)
	}
	want := []string{http.MethodGet, http.MethodHead}
	got := res.Header().Values("Allow")
	if !slices.Equal(got, want) {
		t.Errorf("App.ServeHTTP(%s %s) Allow header = %v, want %v", request.Method, request.URL.Path, got, want)
	}
}

func TestAllowedMethodsReturnsMatchingRouteMethods(t *testing.T) {
	app := New()
	app.Get("/assets/*", func(req *Request) error {
		return nil
	})
	app.Head("/assets/*", func(req *Request) error {
		return nil
	})

	got := app.AllowedMethods("/assets/app.css")
	want := []string{http.MethodGet, http.MethodHead}

	if !slices.Equal(got, want) {
		t.Errorf("App.AllowedMethods(%q) = %v, want %v", "/assets/app.css", got, want)
	}
}

func TestAllowedMethodsReturnsAnyForMethodAgnosticRoutes(t *testing.T) {
	app := New()
	app.Any("/proxy/*", func(req *Request) error {
		return nil
	})

	got := app.AllowedMethods("/proxy/webhooks/stripe")
	want := []string{MethodAny}

	if !slices.Equal(got, want) {
		t.Errorf("App.AllowedMethods(%q) = %v, want %v", "/proxy/webhooks/stripe", got, want)
	}
}

func TestAllowedMethodsNormalizesGenericHandleMethod(t *testing.T) {
	app := New()
	app.Handle("get", "/posts", func(req *Request) error {
		return nil
	})

	got := app.AllowedMethods("/posts")
	want := []string{http.MethodGet, http.MethodHead}

	if !slices.Equal(got, want) {
		t.Errorf("App.AllowedMethods(%q) = %v, want %v", "/posts", got, want)
	}
}

func TestAllowedMethodsUsesProvidedMethodSetAndReusesRouteContext(t *testing.T) {
	routes := &allowedMethodRoutes{
		matches: map[string]bool{
			http.MethodPost: true,
		},
	}

	got := allowedMethods(routes, []string{http.MethodGet, http.MethodPost}, "/posts")
	want := []string{http.MethodPost}

	if !slices.Equal(got, want) {
		t.Errorf("allowedMethods(routes, methods, %q) = %v, want %v", "/posts", got, want)
	}
	if routes.routesCalls != 0 {
		t.Errorf("allowedMethods(routes, methods, path) Routes calls = %d, want 0", routes.routesCalls)
	}
	if routes.matchCalls != 2 {
		t.Errorf("allowedMethods(routes, methods, path) Match calls = %d, want 2", routes.matchCalls)
	}
	if routes.changedContext {
		t.Errorf("allowedMethods(routes, methods, path) changed route context between matches")
	}
	if routes.dirtyContext {
		t.Errorf("allowedMethods(routes, methods, path) reused route context without reset")
	}
}

type allowedMethodRoutes struct {
	matches        map[string]bool
	firstContext   *chi.Context
	routesCalls    int
	matchCalls     int
	changedContext bool
	dirtyContext   bool
}

type optionalInterfaceRecorder struct {
	*httptest.ResponseRecorder
	readFromCalled bool
}

func (r *optionalInterfaceRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalled = true
	return r.Body.ReadFrom(src)
}

type transformingMiddlewareWriter struct {
	http.ResponseWriter
	status int
}

func (w *transformingMiddlewareWriter) WriteHeader(status int) {
	w.status = status
}

func (w *transformingMiddlewareWriter) Write(body []byte) (int, error) {
	w.Header().Set("X-Middleware-Saw-Body", "yes")
	w.Header().Set("Content-Encoding", "test")
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(w.status)
	n, err := w.ResponseWriter.Write(body)
	if err != nil {
		return n, err
	}
	return len(body), nil
}

func recoverUncommittedResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracked := &commitTrackingWriter{ResponseWriter: w}
		defer func() {
			if recover() == nil || tracked.committed {
				return
			}
			tracked.Header().Set("X-Recovered", "yes")
			http.Error(tracked, "recovered", http.StatusInternalServerError)
		}()
		next.ServeHTTP(tracked, r)
	})
}

type commitTrackingWriter struct {
	http.ResponseWriter
	committed bool
}

func (w *commitTrackingWriter) WriteHeader(status int) {
	w.committed = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *commitTrackingWriter) Write(body []byte) (int, error) {
	w.committed = true
	return w.ResponseWriter.Write(body)
}

func (r *allowedMethodRoutes) Routes() []chi.Route {
	r.routesCalls++
	return nil
}

func (r *allowedMethodRoutes) Middlewares() chi.Middlewares {
	return nil
}

func (r *allowedMethodRoutes) Match(ctx *chi.Context, method string, _ string) bool {
	if r.firstContext == nil {
		r.firstContext = ctx
	} else if r.firstContext != ctx {
		r.changedContext = true
	}
	if ctx.RoutePath != "" {
		r.dirtyContext = true
	}
	ctx.RoutePath = "dirty"
	r.matchCalls++
	return r.matches[method]
}

func (r *allowedMethodRoutes) Find(*chi.Context, string, string) string {
	return ""
}
