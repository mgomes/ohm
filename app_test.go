package ohm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
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

func TestRequestHTMLRendersTemplComponent(t *testing.T) {
	app := New()
	app.Get("/", func(req *Request) error {
		return req.HTML(http.StatusCreated, templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
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
		return req.HTML(http.StatusOK, templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
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
		return req.HTML(0, templ.ComponentFunc(func(context.Context, io.Writer) error {
			return nil
		}))
	})

	tests := []struct {
		path string
		want string
	}{
		{path: "/nil", want: "html component is required"},
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

func TestAppRoutesReturnsRegisteredRoutes(t *testing.T) {
	app := New()
	app.Get("/posts", func(req *Request) error {
		return nil
	})
	app.Post("/posts", func(req *Request) error {
		return nil
	})

	got, err := app.Routes()
	if err != nil {
		t.Fatalf("App.Routes() error = %v, want nil", err)
	}

	want := []Route{
		{Method: http.MethodGet, Pattern: "/posts"},
		{Method: http.MethodPost, Pattern: "/posts"},
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
