package scaffold

var applicationTemplates = map[string]string{
	"internal/app/app.go": `package app

import (
	"log/slog"
	"os"

	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/scrub"

	"{{.Module}}/internal/handlers"
)

type Option func(*options)

type options struct {
	staticRoot string
}

func WithStaticRoot(root string) Option {
	return func(opts *options) {
		if root != "" {
			opts.staticRoot = root
		}
	}
}

func New(opts ...Option) *ohm.App {
	cfg := options{
		staticRoot: "static",
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	logger := slog.New(ohm.TraceLogHandler(scrub.NewHandler(slog.NewJSONHandler(os.Stderr, nil))))
	application := ohm.New(ohm.WithErrorHandler(handleError))
	application.Use(ohm.Tracing(), ohm.RequestLogger(logger), ohm.Recoverer(logger))
	application.Static("/assets/*", cfg.staticRoot)
	application.NotFound(notFound)
	application.MethodNotAllowed(methodNotAllowed)
	handlers.Register(application)
	return application
}
`,
	"internal/app/errors.go": `package app

import (
	"net/http"

	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views/pages"
)

func handleError(req *ohm.Request, err error) {
	status, message := ohm.ErrorResponse(err)
	renderError(req.ResponseWriter(), req.HTTPRequest(), status, message)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	renderError(w, r, http.StatusNotFound, http.StatusText(http.StatusNotFound))
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request, allowedMethods []string) {
	for _, method := range allowedMethods {
		w.Header().Add("Allow", method)
	}
	renderError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
}

func renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if err := ohm.RenderHTML(w, r, status, pages.Error(status, message)); err != nil {
		http.Error(w, message, status)
	}
}
`,
	"internal/app/app_test.go": `package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mgomes/ohm"
)

func TestNewRegistersHomeRoute(t *testing.T) {
	application := New()

	routes, err := application.Routes()
	if err != nil {
		t.Fatalf("New().Routes() error = %v, want nil", err)
	}
	if !hasRoute(routes, "GET", "/") {
		t.Fatalf("New().Routes() = %+v, want GET /", routes)
	}
}

func TestNewServesStaticAssets(t *testing.T) {
	staticRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticRoot, "app.css"), []byte("body { color: black; }\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(static asset) error = %v, want nil", err)
	}

	application := New(WithStaticRoot(staticRoot))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) status = %d, want %d", staticRoot, request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "body { color: black; }\n" {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) body = %q, want static asset", staticRoot, request.Method, request.URL.Path, got)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/assets/app.css", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) status = %d, want %d", staticRoot, request.Method, request.URL.Path, response.Code, http.StatusMethodNotAllowed)
	}
	if got, want := response.Header().Values("Allow"), []string{http.MethodGet, http.MethodHead}; !slices.Equal(got, want) {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) Allow header = %v, want %v", staticRoot, request.Method, request.URL.Path, got, want)
	}
	if !strings.Contains(response.Body.String(), "<h1>Method Not Allowed</h1>") {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) body = %q, want method not allowed page", staticRoot, request.Method, request.URL.Path, response.Body.String())
	}
}

func TestNewRendersHandlerErrorPage(t *testing.T) {
	application := New()
	application.Get("/posts/42", func(req *ohm.Request) error {
		return ohm.NewHTTPError(http.StatusNotFound, "Post not found", errors.New("missing post"))
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/posts/42", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Errorf("New().ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("New().ServeHTTP(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if !strings.Contains(response.Body.String(), "<h1>Post not found</h1>") {
		t.Errorf("New().ServeHTTP(%s %s) body = %q, want error page", request.Method, request.URL.Path, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "missing post") {
		t.Errorf("New().ServeHTTP(%s %s) body = %q, want no wrapped error detail", request.Method, request.URL.Path, response.Body.String())
	}
}

func TestNewRendersMissingRouteErrorPage(t *testing.T) {
	application := New()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Errorf("New().ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("New().ServeHTTP(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if !strings.Contains(response.Body.String(), "<h1>Not Found</h1>") {
		t.Errorf("New().ServeHTTP(%s %s) body = %q, want not found page", request.Method, request.URL.Path, response.Body.String())
	}
}

func hasRoute(routes []ohm.Route, method string, pattern string) bool {
	for _, route := range routes {
		if route.Method == method && route.Pattern == pattern {
			return true
		}
	}
	return false
}
`,
	"internal/apptest/apptest.go": `package apptest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"{{.Module}}/internal/app"
)

type Client struct {
	t       testing.TB
	handler http.Handler
}

func New(t testing.TB, opts ...app.Option) *Client {
	t.Helper()

	return &Client{
		t:       t,
		handler: app.New(opts...).HTTPHandler(),
	}
}

func (c *Client) Get(target string) *httptest.ResponseRecorder {
	c.t.Helper()

	return c.Request(http.MethodGet, target, nil)
}

func (c *Client) Request(method string, target string, body io.Reader) *httptest.ResponseRecorder {
	c.t.Helper()

	request := httptest.NewRequest(method, target, body)
	response := httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	return response
}
`,
	"internal/apptest/apptest_test.go": `package apptest

import (
	"net/http"
	"strings"
	"testing"
)

func TestClientGetExercisesApplicationRoute(t *testing.T) {
	client := New(t)

	response := client.Get("/")

	if response.Code != http.StatusOK {
		t.Fatalf("Client.Get(%q) status = %d, want %d", "/", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "<h1>Welcome to {{.Title}}</h1>") {
		t.Errorf("Client.Get(%q) body = %q, want home page", "/", response.Body.String())
	}
}

func TestClientRequestReportsMissingRoute(t *testing.T) {
	client := New(t)

	response := client.Request(http.MethodGet, "/missing", nil)

	if response.Code != http.StatusNotFound {
		t.Errorf("Client.Request(%q, %q, nil) status = %d, want %d", http.MethodGet, "/missing", response.Code, http.StatusNotFound)
	}
	if !strings.Contains(response.Body.String(), "<h1>Not Found</h1>") {
		t.Errorf("Client.Request(%q, %q, nil) body = %q, want not found page", http.MethodGet, "/missing", response.Body.String())
	}
}
`,
	"internal/handlers/home.go": `package handlers

import (
	"net/http"

	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views/pages"
)

func Home(req *ohm.Request) error {
	return req.HTML(http.StatusOK, pages.Home("{{.Title}}"))
}
`,
	"internal/handlers/routes.go": `package handlers

import "github.com/mgomes/ohm"

func Register(application *ohm.App) {
	application.Get("/", Home)
}
`,
	"internal/handlers/home_test.go": `package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgomes/ohm"
)

func TestHome(t *testing.T) {
	application := ohm.New()
	Register(application)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("Home(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Home(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if !strings.Contains(response.Body.String(), "<title>{{.Title}}</title>") {
		t.Errorf("Home(%s %s) body = %q, want page title", request.Method, request.URL.Path, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "<h1>Welcome to {{.Title}}</h1>") {
		t.Errorf("Home(%s %s) body = %q, want welcome heading", request.Method, request.URL.Path, response.Body.String())
	}
}
`,
	"internal/services/README.md": `# Services

Put business workflows here. Services should own multi-step application work,
including transaction boundaries that coordinate multiple queries.
`,
}
