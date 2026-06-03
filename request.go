package ohm

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// Request is the Ohm request and response boundary passed to handlers.
type Request struct {
	w http.ResponseWriter
	r *http.Request
}

// Binder decodes and validates a request body.
type Binder interface {
	Bind(*http.Request) error
}

// Renderer writes a structured response.
type Renderer interface {
	Render(http.ResponseWriter, *http.Request) error
}

func newRequest(w http.ResponseWriter, r *http.Request) *Request {
	return &Request{w: w, r: r}
}

// Context returns the standard Go request context.
func (r *Request) Context() context.Context {
	return r.r.Context()
}

// HTTPRequest returns the underlying HTTP request escape hatch.
func (r *Request) HTTPRequest() *http.Request {
	return r.r
}

// ResponseWriter returns the underlying HTTP response writer escape hatch.
func (r *Request) ResponseWriter() http.ResponseWriter {
	return r.w
}

// Param returns a route parameter by name.
func (r *Request) Param(key string) string {
	return chi.URLParam(r.r, key)
}

// RoutePattern returns the matched route pattern when available.
func (r *Request) RoutePattern() string {
	routeContext := chi.RouteContext(r.r.Context())
	if routeContext == nil {
		return ""
	}
	return routeContext.RoutePattern()
}

// Bind decodes and validates a request body.
func (r *Request) Bind(v Binder) error {
	return render.Bind(r.r, v)
}

// Decode decodes a request body.
func (r *Request) Decode(v any) error {
	return render.Decode(r.r, v)
}

// Render renders a structured response.
func (r *Request) Render(v Renderer) error {
	return render.Render(r.w, r.r, v)
}

// HTML renders a templ component as HTML with status.
func (r *Request) HTML(status int, component templ.Component) error {
	return RenderHTML(r.w, r.r, status, component)
}

// RenderHTML renders a templ component as an HTML response with status.
func RenderHTML(w http.ResponseWriter, r *http.Request, status int, component templ.Component) error {
	if w == nil {
		return fmt.Errorf("html response writer is required")
	}
	if r == nil {
		return fmt.Errorf("html request is required")
	}
	if component == nil {
		return fmt.Errorf("html component is required")
	}
	if status < 100 || status > 999 {
		return fmt.Errorf("html status code %d is invalid", status)
	}

	var body bytes.Buffer
	if err := component.Render(r.Context(), &body); err != nil {
		return fmt.Errorf("render html component: %w", err)
	}

	render.Status(r, status)
	render.HTML(w, r, body.String())
	return nil
}

// JSON renders v as JSON with status.
func (r *Request) JSON(status int, v any) {
	render.Status(r.r, status)
	render.JSON(r.w, r.r, v)
}

// PlainText renders text with status.
func (r *Request) PlainText(status int, text string) {
	renderPlainText(r.w, r.r, status, text)
}

// NoContent renders an empty 204 response.
func (r *Request) NoContent() {
	render.NoContent(r.w, r.r)
}

// Redirect redirects to url with status.
func (r *Request) Redirect(status int, url string) {
	http.Redirect(r.w, r.r, url, status)
}

func renderPlainText(w http.ResponseWriter, r *http.Request, status int, text string) {
	render.Status(r, status)
	render.PlainText(w, r, text)
}
