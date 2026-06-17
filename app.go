package ohm

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Handler handles one Ohm request.
type Handler func(*Request) error

// Middleware wraps an HTTP handler.
type Middleware func(http.Handler) http.Handler

// ErrorHandler handles errors returned by Ohm handlers.
type ErrorHandler func(*Request, error)

// MethodNotAllowedHandler handles requests whose path matches other methods.
type MethodNotAllowedHandler func(http.ResponseWriter, *http.Request, []string)

// App is an Ohm HTTP application.
type App struct {
	router       chi.Router
	errorHandler ErrorHandler
	routeMethods []string
}

// Option configures an App.
type Option func(*App)

// WithErrorHandler configures how handler errors are rendered.
func WithErrorHandler(handler ErrorHandler) Option {
	return func(app *App) {
		if handler != nil {
			app.errorHandler = handler
		}
	}
}

// New creates an Ohm application.
func New(opts ...Option) *App {
	app := &App{
		router:       chi.NewRouter(),
		errorHandler: DefaultErrorHandler,
	}
	for _, opt := range opts {
		opt(app)
	}
	return app
}

// Use appends middleware to the app router.
func (a *App) Use(middlewares ...Middleware) {
	for _, middleware := range middlewares {
		a.router.Use(middleware)
	}
}

// Handle registers handler for method and pattern.
func (a *App) Handle(method string, pattern string, handler Handler) {
	a.HandleHTTP(method, pattern, a.adapt(handler))
}

// HandleHTTP registers handler for method and pattern.
func (a *App) HandleHTTP(method string, pattern string, handler http.Handler) {
	a.router.Method(method, pattern, handler)
	a.addRouteMethod(method)
}

// Get registers a GET route.
func (a *App) Get(pattern string, handler Handler) {
	a.Handle(http.MethodGet, pattern, handler)
}

// GetHTTP registers an HTTP handler for a GET route.
func (a *App) GetHTTP(pattern string, handler http.Handler) {
	a.HandleHTTP(http.MethodGet, pattern, handler)
}

// Head registers a HEAD route.
func (a *App) Head(pattern string, handler Handler) {
	a.Handle(http.MethodHead, pattern, handler)
}

// HeadHTTP registers an HTTP handler for a HEAD route.
func (a *App) HeadHTTP(pattern string, handler http.Handler) {
	a.HandleHTTP(http.MethodHead, pattern, handler)
}

// Post registers a POST route.
func (a *App) Post(pattern string, handler Handler) {
	a.Handle(http.MethodPost, pattern, handler)
}

// Put registers a PUT route.
func (a *App) Put(pattern string, handler Handler) {
	a.Handle(http.MethodPut, pattern, handler)
}

// Patch registers a PATCH route.
func (a *App) Patch(pattern string, handler Handler) {
	a.Handle(http.MethodPatch, pattern, handler)
}

// Delete registers a DELETE route.
func (a *App) Delete(pattern string, handler Handler) {
	a.Handle(http.MethodDelete, pattern, handler)
}

// Static registers GET and HEAD routes for files under root.
func (a *App) Static(pattern string, root string) {
	prefix := staticPrefix(pattern)
	files := http.StripPrefix(prefix, http.FileServer(http.Dir(root)))
	a.GetHTTP(pattern, files)
	a.HeadHTTP(pattern, files)
}

// NotFound configures the handler used when no route matches.
func (a *App) NotFound(handler http.HandlerFunc) {
	if handler != nil {
		a.router.NotFound(handler)
	}
}

// MethodNotAllowed configures the handler used when only the method is missing.
func (a *App) MethodNotAllowed(handler MethodNotAllowedHandler) {
	if handler == nil {
		return
	}
	a.router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, a.AllowedMethods(r.URL.Path))
	})
}

// ServeHTTP serves HTTP requests.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, withResponseStatus(r))
}

// HTTPHandler returns the underlying HTTP handler.
func (a *App) HTTPHandler() http.Handler {
	return http.HandlerFunc(a.ServeHTTP)
}

// Routes returns registered routes sorted by method and pattern.
func (a *App) Routes() ([]Route, error) {
	var routes []Route
	if err := chi.Walk(a.router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, Route{
			Method:  method,
			Pattern: route,
		})
		return nil
	}); err != nil {
		return nil, err
	}

	slices.SortFunc(routes, func(a Route, b Route) int {
		if a.Pattern < b.Pattern {
			return -1
		}
		if a.Pattern > b.Pattern {
			return 1
		}
		if a.Method < b.Method {
			return -1
		}
		if a.Method > b.Method {
			return 1
		}
		return 0
	})
	return routes, nil
}

// AllowedMethods returns HTTP methods that match path.
func (a *App) AllowedMethods(path string) []string {
	return allowedMethods(a.router, a.routeMethods, path)
}

func allowedMethods(routes chi.Routes, methods []string, path string) []string {
	if routes == nil || len(methods) == 0 {
		return nil
	}

	var allowed []string
	ctx := chi.NewRouteContext()
	for _, method := range methods {
		ctx.Reset()
		if routes.Match(ctx, method, path) {
			allowed = append(allowed, method)
		}
	}
	return allowed
}

func (a *App) addRouteMethod(method string) {
	method = strings.ToUpper(method)
	index, found := slices.BinarySearch(a.routeMethods, method)
	if found {
		return
	}
	a.routeMethods = slices.Insert(a.routeMethods, index, method)
}

func staticPrefix(pattern string) string {
	prefix := strings.TrimSuffix(pattern, "*")
	if prefix == "" {
		return "/"
	}
	return prefix
}

func (a *App) adapt(handler Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = withResponseStatus(r)
		req := newRequest(w, r)
		if err := handler(req); err != nil {
			recordHandlerError(r.Context(), err)
			a.errorHandler(req, err)
		}
	})
}

// Route describes one registered route.
type Route struct {
	Method  string
	Pattern string
}
