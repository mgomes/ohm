package ohm

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
