package ohm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestBindDecodesJSONAndRunsBinders(t *testing.T) {
	app := New()
	app.Post("/login", func(req *Request) error {
		var payload bindPayload
		if err := req.Bind(&payload); err != nil {
			return err
		}
		req.JSON(http.StatusOK, payload)
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"name":"ada","child":{"value":"nested"}}`))
	request.Header.Set("Content-Type", "application/json")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got bindPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Name != "ada" {
		t.Errorf("Request.Bind(payload).Name = %q, want %q", got.Name, "ada")
	}
	if !got.Bound {
		t.Errorf("Request.Bind(payload).Bound = false, want true")
	}
	if got.Child == nil {
		t.Fatalf("Request.Bind(payload).Child = nil, want child")
	}
	if got.Child.Value != "nested" {
		t.Errorf("Request.Bind(payload).Child.Value = %q, want %q", got.Child.Value, "nested")
	}
	if !got.Child.Bound {
		t.Errorf("Request.Bind(payload).Child.Bound = false, want true")
	}
}

func TestRequestDecodeDecodesForm(t *testing.T) {
	app := New()
	app.Post("/posts", func(req *Request) error {
		var payload formPayload
		if err := req.Decode(&payload); err != nil {
			return err
		}
		req.JSON(http.StatusOK, payload)
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader("title=hello"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got formPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Title != "hello" {
		t.Errorf("Request.Decode(form).Title = %q, want %q", got.Title, "hello")
	}
}

func TestRequestRenderRunsNestedRenderersAndUsesStatus(t *testing.T) {
	app := New()
	app.Get("/render", func(req *Request) error {
		return req.Render(&renderPayload{Child: &renderChild{}})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/render", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusAccepted)
	}

	var got renderPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Message != "parent" {
		t.Errorf("Request.Render(payload).Message = %q, want %q", got.Message, "parent")
	}
	if got.Child == nil {
		t.Fatalf("Request.Render(payload).Child = nil, want child")
	}
	if got.Child.Message != "child" {
		t.Errorf("Request.Render(payload).Child.Message = %q, want %q", got.Child.Message, "child")
	}
}

func TestRouteHelpersExposeMatchedRoute(t *testing.T) {
	app := New()
	app.Get("/posts/{id}", func(req *Request) error {
		req.JSON(http.StatusOK, map[string]any{
			"param":   RouteParam(req.HTTPRequest(), "id"),
			"pattern": RoutePattern(req.HTTPRequest()),
			"params":  RouteParams(req.HTTPRequest()),
		})
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/posts/42", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got struct {
		Param   string            `json:"param"`
		Pattern string            `json:"pattern"`
		Params  map[string]string `json:"params"`
	}
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Param != "42" {
		t.Errorf("RouteParam(request, id) = %q, want %q", got.Param, "42")
	}
	if got.Pattern != "/posts/{id}" {
		t.Errorf("RoutePattern(request) = %q, want %q", got.Pattern, "/posts/{id}")
	}
	if got.Params["id"] != "42" {
		t.Errorf("RouteParams(request)[id] = %q, want %q", got.Params["id"], "42")
	}
}

type bindPayload struct {
	Name  string     `json:"name"`
	Bound bool       `json:"bound"`
	Child *bindChild `json:"child,omitempty"`
}

func (p *bindPayload) Bind(*http.Request) error {
	p.Bound = true
	return nil
}

type bindChild struct {
	Value string `json:"value"`
	Bound bool   `json:"bound"`
}

func (c *bindChild) Bind(*http.Request) error {
	c.Bound = true
	return nil
}

type formPayload struct {
	Title string `form:"title" json:"title"`
}

type renderPayload struct {
	Message string       `json:"message"`
	Child   *renderChild `json:"child,omitempty"`
}

func (p *renderPayload) Render(_ http.ResponseWriter, r *http.Request) error {
	SetStatus(r, http.StatusAccepted)
	p.Message = "parent"
	return nil
}

type renderChild struct {
	Message string `json:"message"`
}

func (c *renderChild) Render(http.ResponseWriter, *http.Request) error {
	c.Message = "child"
	return nil
}
