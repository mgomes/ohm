package ohm

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
)

// Handler handles one Ohm request.
type Handler func(*Request) error

// Middleware wraps an HTTP handler.
type Middleware func(http.Handler) http.Handler

// ErrorHandler handles errors returned by Ohm handlers.
type ErrorHandler func(*Request, error)

// App is an Ohm HTTP application.
type App struct {
	router       chi.Router
	errorHandler ErrorHandler
}

// Option configures an App.
type Option func(*App)

// WithRouter configures the chi router used by an app.
func WithRouter(router chi.Router) Option {
	return func(app *App) {
		if router != nil {
			app.router = router
		}
	}
}

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
	a.router.Method(method, pattern, a.adapt(handler))
}

// Get registers a GET route.
func (a *App) Get(pattern string, handler Handler) {
	a.Handle(http.MethodGet, pattern, handler)
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

// ServeHTTP serves HTTP requests.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

// HTTPHandler returns the underlying HTTP handler.
func (a *App) HTTPHandler() http.Handler {
	return a.router
}

// ChiRouter returns the underlying chi router escape hatch.
func (a *App) ChiRouter() chi.Router {
	return a.router
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

// AllowedMethods returns HTTP methods that match path in routes.
func AllowedMethods(routes chi.Routes, path string) []string {
	if routes == nil {
		return nil
	}

	seen := map[string]struct{}{}
	var methods []string
	if err := chi.Walk(routes, func(method string, _ string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, ok := seen[method]; ok {
			return nil
		}
		seen[method] = struct{}{}
		methods = append(methods, method)
		return nil
	}); err != nil {
		return nil
	}
	slices.Sort(methods)

	var allowed []string
	for _, method := range methods {
		if routes.Match(chi.NewRouteContext(), method, path) {
			allowed = append(allowed, method)
		}
	}
	return allowed
}

func (a *App) adapt(handler Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := newRequest(w, r)
		if err := handler(req); err != nil {
			a.errorHandler(req, err)
		}
	})
}

// Route describes one registered route.
type Route struct {
	Method  string
	Pattern string
}
