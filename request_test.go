package ohm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(strings.Join([]string{
		"title=hello",
		"published=on",
		"count=12",
		"tags=go",
		"tags=html",
		"page=3",
		"author.name=ada",
		"prefs.theme=dark",
		"prefs.locale=en-US",
		"published_at=2026-06-07T12:30",
		"time_only=12:30:15.123",
	}, "&")))
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
	if !got.Published {
		t.Errorf("Request.Decode(form).Published = false, want true")
	}
	if got.Count != 12 {
		t.Errorf("Request.Decode(form).Count = %d, want %d", got.Count, 12)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" || got.Tags[1] != "html" {
		t.Errorf("Request.Decode(form).Tags = %#v, want %#v", got.Tags, []string{"go", "html"})
	}
	if got.Page == nil {
		t.Fatalf("Request.Decode(form).Page = nil, want pointer")
	}
	if *got.Page != 3 {
		t.Errorf("Request.Decode(form).Page = %d, want %d", *got.Page, 3)
	}
	if got.Author.Name != "ada" {
		t.Errorf("Request.Decode(form).Author.Name = %q, want %q", got.Author.Name, "ada")
	}
	if got.Preferences["theme"] != "dark" {
		t.Errorf("Request.Decode(form).Preferences[theme] = %q, want %q", got.Preferences["theme"], "dark")
	}
	if got.Preferences["locale"] != "en-US" {
		t.Errorf("Request.Decode(form).Preferences[locale] = %q, want %q", got.Preferences["locale"], "en-US")
	}
	wantPublishedAt := time.Date(2026, 6, 7, 12, 30, 0, 0, time.UTC)
	if !got.PublishedAt.Equal(wantPublishedAt) {
		t.Errorf("Request.Decode(form).PublishedAt = %v, want %v", got.PublishedAt, wantPublishedAt)
	}
	wantTimeOnly := time.Date(0, 1, 1, 12, 30, 15, 123000000, time.UTC)
	if !got.TimeOnly.Equal(wantTimeOnly) {
		t.Errorf("Request.Decode(form).TimeOnly = %v, want %v", got.TimeOnly, wantTimeOnly)
	}
	if got.Ignored != "" {
		t.Errorf("Request.Decode(form).Ignored = %q, want empty", got.Ignored)
	}
}

func TestRequestDecodeReadsFormBodyForAnyMethod(t *testing.T) {
	app := New()
	app.Delete("/posts/1", func(req *Request) error {
		var payload formPayload
		if err := req.Decode(&payload); err != nil {
			return err
		}
		req.JSON(http.StatusOK, payload)
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/posts/1", strings.NewReader("title=removed"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}

	var got formPayload
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("json.Decode(response body) error = %v, want nil", err)
	}
	if got.Title != "removed" {
		t.Errorf("Request.Decode(form).Title = %q, want %q", got.Title, "removed")
	}
}

func TestRequestDecodeRejectsOversizedFormBodyAsClientError(t *testing.T) {
	app := New()
	app.Post("/posts", func(req *Request) error {
		var payload formPayload
		return req.Decode(&payload)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(strings.Repeat("a", int(maxFormBodyBytes)+1)))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusRequestEntityTooLarge)
	}
	if got := response.Body.String(); got != http.StatusText(http.StatusRequestEntityTooLarge) {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want %q", request.Method, request.URL.Path, got, http.StatusText(http.StatusRequestEntityTooLarge))
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

func TestRequestJSONDoesNotExposeMarshalErrors(t *testing.T) {
	app := New()
	app.Get("/bad-json", func(req *Request) error {
		req.JSON(http.StatusOK, map[string]any{"bad": func() {}})
		return nil
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/bad-json", nil)

	app.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusInternalServerError)
	}
	if got := response.Body.String(); got != http.StatusText(http.StatusInternalServerError)+"\n" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want safe internal server error", request.Method, request.URL.Path, got)
	}
	if strings.Contains(response.Body.String(), "unsupported type") {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want no marshal internals", request.Method, request.URL.Path, response.Body.String())
	}
}

func TestRequestRenderXMLDoesNotExposeMarshalErrors(t *testing.T) {
	app := New()
	app.Get("/bad-xml", func(req *Request) error {
		return req.Render(xmlMarshalErrorPayload{Bad: func() {}})
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/bad-xml", nil)
	request.Header.Set("Accept", "application/xml")

	app.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("App.ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusInternalServerError)
	}
	if got := response.Body.String(); got != http.StatusText(http.StatusInternalServerError)+"\n" {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want safe internal server error", request.Method, request.URL.Path, got)
	}
	if strings.Contains(response.Body.String(), "unsupported type") {
		t.Errorf("App.ServeHTTP(%s %s) body = %q, want no marshal internals", request.Method, request.URL.Path, response.Body.String())
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
	Title       string            `form:"title" json:"title"`
	Published   bool              `form:"published" json:"published"`
	Count       int               `form:"count" json:"count"`
	Tags        []string          `form:"tags" json:"tags"`
	Page        *int              `form:"page" json:"page"`
	Author      author            `form:"author" json:"author"`
	Preferences map[string]string `form:"prefs" json:"prefs"`
	PublishedAt time.Time         `form:"published_at" json:"published_at"`
	TimeOnly    time.Time         `form:"time_only" json:"time_only"`
	Ignored     string            `form:"-" json:"ignored"`
}

type author struct {
	Name string `form:"name" json:"name"`
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

type xmlMarshalErrorPayload struct {
	Bad func()
}

func (p xmlMarshalErrorPayload) Render(http.ResponseWriter, *http.Request) error {
	return nil
}
