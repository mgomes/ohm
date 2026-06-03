package ohm

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
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
	return RouteParam(r.r, key)
}

// RoutePattern returns the matched route pattern when available.
func (r *Request) RoutePattern() string {
	return RoutePattern(r.r)
}

// Bind decodes and validates a request body.
func (r *Request) Bind(v Binder) error {
	return bindRequest(r.r, v)
}

// Decode decodes a request body.
func (r *Request) Decode(v any) error {
	return decodeRequest(r.r, v)
}

// Render renders a structured response.
func (r *Request) Render(v Renderer) error {
	return renderResponse(r.w, r.r, v)
}

// HTML renders a templ component as HTML with status.
func (r *Request) HTML(status int, component templ.Component) error {
	return RenderHTML(r.w, r.r, status, component)
}

// JSON renders v as JSON with status.
func (r *Request) JSON(status int, v any) {
	writeJSON(r.w, status, v)
}

// PlainText renders text with status.
func (r *Request) PlainText(status int, text string) {
	renderPlainText(r.w, r.r, status, text)
}

// NoContent renders an empty 204 response.
func (r *Request) NoContent() {
	writeNoContent(r.w)
}

// Redirect redirects to url with status.
func (r *Request) Redirect(status int, url string) {
	http.Redirect(r.w, r.r, url, status)
}

func renderPlainText(w http.ResponseWriter, r *http.Request, status int, text string) {
	writePlainText(w, status, text)
}
