package ohm

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/felixge/httpsnoop"
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
	router             chi.Router
	errorHandler       ErrorHandler
	routeMethods       []string
	explicitHeadRoutes map[string]struct{}
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
		router:             chi.NewRouter(),
		errorHandler:       DefaultErrorHandler,
		explicitHeadRoutes: make(map[string]struct{}),
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
	method = strings.ToUpper(method)
	a.router.Method(method, pattern, handler)
	a.addRouteMethod(method)
	if method == http.MethodHead {
		a.addExplicitHeadRoute(pattern)
		return
	}
	if method == http.MethodGet {
		if a.hasExplicitHeadRoute(pattern) {
			return
		}
		a.router.Method(http.MethodHead, pattern, handler)
		a.addRouteMethod(http.MethodHead)
	}
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
	r = withNewResponseStatus(r)
	if r.Method == http.MethodHead {
		writer, state := newHeadResponseWriter(w)
		defer state.finish()
		r = withHeadResponseWriter(r, writer, w)
		a.router.ServeHTTP(writer, r)
		return
	}
	a.router.ServeHTTP(w, r)
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

func (a *App) addExplicitHeadRoute(pattern string) {
	if a.explicitHeadRoutes == nil {
		a.explicitHeadRoutes = make(map[string]struct{})
	}
	a.explicitHeadRoutes[routePatternShape(pattern)] = struct{}{}
}

func (a *App) hasExplicitHeadRoute(pattern string) bool {
	if a.explicitHeadRoutes == nil {
		return false
	}
	_, ok := a.explicitHeadRoutes[routePatternShape(pattern)]
	return ok
}

func routePatternShape(pattern string) string {
	var shape strings.Builder
	for len(pattern) > 0 {
		paramStart := strings.Index(pattern, "{")
		wildcardStart := strings.Index(pattern, "*")
		if paramStart < 0 && wildcardStart < 0 {
			shape.WriteString(pattern)
			return shape.String()
		}
		if wildcardStart >= 0 && (paramStart < 0 || wildcardStart < paramStart) {
			shape.WriteString(pattern[:wildcardStart+1])
			return shape.String()
		}

		shape.WriteString(pattern[:paramStart])
		paramEnd := routeParamEnd(pattern[paramStart:])
		if paramEnd < 0 {
			shape.WriteString(pattern[paramStart:])
			return shape.String()
		}

		param := pattern[paramStart+1 : paramStart+paramEnd]
		_, rexpat, hasRegexp := strings.Cut(param, ":")
		if !hasRegexp {
			shape.WriteString("{}")
		} else {
			shape.WriteString("{:")
			shape.WriteString(normalizeRouteRegexp(rexpat))
			shape.WriteString("}")
		}
		pattern = pattern[paramStart+paramEnd+1:]
	}
	return shape.String()
}

func routeParamEnd(pattern string) int {
	depth := 0
	for i, r := range pattern {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func normalizeRouteRegexp(rexpat string) string {
	if rexpat == "" {
		return rexpat
	}
	if rexpat[0] != '^' {
		rexpat = "^" + rexpat
	}
	if rexpat[len(rexpat)-1] != '$' {
		rexpat += "$"
	}
	return rexpat
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
		rawW := rawResponseWriter(w, r)
		tracked, state := trackResponse(w)
		r = withResponseStatus(r)
		markResponseStatusHandlerStarted(r)
		req := newRequestWithRawResponseWriter(tracked, rawW, r)
		if err := handler(req); err != nil {
			recordHandlerError(r.Context(), err)
			if state.committed() {
				return
			}
			a.errorHandler(req, err)
		}
	})
}

type headResponseWriterContextKey struct{}

type headResponseWriterContext struct {
	writer http.ResponseWriter
	raw    http.ResponseWriter
}

func withHeadResponseWriter(r *http.Request, writer http.ResponseWriter, raw http.ResponseWriter) *http.Request {
	ctx := context.WithValue(r.Context(), headResponseWriterContextKey{}, headResponseWriterContext{
		writer: writer,
		raw:    raw,
	})
	return r.WithContext(ctx)
}

func rawResponseWriter(w http.ResponseWriter, r *http.Request) http.ResponseWriter {
	head, ok := r.Context().Value(headResponseWriterContextKey{}).(headResponseWriterContext)
	if !ok || !sameResponseWriter(w, head.writer) {
		return w
	}
	return head.raw
}

func sameResponseWriter(a http.ResponseWriter, b http.ResponseWriter) bool {
	if a == nil || b == nil {
		return a == b
	}
	aValue := reflect.ValueOf(a)
	bValue := reflect.ValueOf(b)
	if !aValue.Type().Comparable() || !bValue.Type().Comparable() {
		return false
	}
	return a == b
}

type headResponseState struct {
	writer      http.ResponseWriter
	writeHeader func(int)

	status         int
	wroteHeader    bool
	headerSnapshot http.Header
	bodyBytes      int64
	flushed        bool
	committed      bool
}

func newHeadResponseWriter(w http.ResponseWriter) (http.ResponseWriter, *headResponseState) {
	state := &headResponseState{
		writer:      w,
		writeHeader: w.WriteHeader,
	}
	return httpsnoop.Wrap(w, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			state.writeHeader = next
			return state.WriteHeader
		},
		Write: func(httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return state.Write
		},
		WriteString: func(httpsnoop.WriteStringFunc) httpsnoop.WriteStringFunc {
			return state.WriteString
		},
		ReadFrom: func(httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return state.ReadFrom
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() {
				state.Flush(next)
			}
		},
		FlushError: func(next httpsnoop.FlushErrorFunc) httpsnoop.FlushErrorFunc {
			return func() error {
				return state.FlushError(next)
			}
		},
	}), state
}

func (s *headResponseState) Header() http.Header {
	if s.headerSnapshot != nil {
		return s.headerSnapshot
	}
	return s.writer.Header()
}

func (s *headResponseState) WriteHeader(status int) {
	if !finalStatus(status) {
		s.writeHeader(status)
		return
	}
	s.beginFinalResponse(status)
}

func (s *headResponseState) Write(body []byte) (int, error) {
	if !s.wroteHeader {
		s.beginFinalResponse(http.StatusOK)
	}
	if !statusAllowsResponseBody(s.status) {
		return 0, http.ErrBodyNotAllowed
	}
	s.recordBody(body)
	return len(body), nil
}

func (s *headResponseState) WriteString(body string) (int, error) {
	return s.Write([]byte(body))
}

func (s *headResponseState) ReadFrom(src io.Reader) (int64, error) {
	return io.Copy(headResponseBodyWriter{state: s}, src)
}

type headResponseBodyWriter struct {
	state *headResponseState
}

func (w headResponseBodyWriter) Write(body []byte) (int, error) {
	return w.state.Write(body)
}

func (s *headResponseState) Flush(next httpsnoop.FlushFunc) {
	if !s.wroteHeader {
		s.beginFinalResponse(http.StatusOK)
	}
	s.flushed = true
	s.commit()
	next()
}

func (s *headResponseState) FlushError(next httpsnoop.FlushErrorFunc) error {
	if !s.wroteHeader {
		s.beginFinalResponse(http.StatusOK)
	}
	s.flushed = true
	s.commit()
	return next()
}

func (s *headResponseState) finish() {
	s.commit()
}

func (s *headResponseState) beginFinalResponse(status int) {
	if s.wroteHeader {
		return
	}
	s.status = status
	s.wroteHeader = true
	s.headerSnapshot = s.writer.Header().Clone()
}

func (s *headResponseState) recordBody(body []byte) {
	s.bodyBytes += int64(len(body))
	s.sniffContentType(body)
}

func (s *headResponseState) sniffContentType(body []byte) {
	if len(body) == 0 {
		return
	}
	header := s.Header()
	if _, ok := header["Content-Type"]; ok {
		return
	}
	if header.Get("Content-Encoding") != "" || header.Get("Transfer-Encoding") != "" {
		return
	}
	header.Set("Content-Type", http.DetectContentType(body))
}

func (s *headResponseState) commit() {
	if s.committed || !s.wroteHeader {
		return
	}

	header := s.Header()
	s.applyBodyHeaders(header)
	s.restoreHeader(header)
	s.writeHeader(s.status)
	s.committed = true
}

func (s *headResponseState) applyBodyHeaders(header http.Header) {
	if !statusAllowsResponseBody(s.status) {
		header.Del("Content-Type")
		header.Del("Content-Length")
		header.Del("Transfer-Encoding")
		return
	}
	if s.flushed || s.bodyBytes == 0 {
		return
	}
	if _, ok := header["Content-Length"]; ok {
		return
	}
	if header.Get("Transfer-Encoding") != "" {
		return
	}
	header.Set("Content-Length", strconv.FormatInt(s.bodyBytes, 10))
}

func (s *headResponseState) restoreHeader(header http.Header) {
	live := s.writer.Header()
	clear(live)
	for name, values := range header {
		live[name] = slices.Clone(values)
	}
}

func statusAllowsResponseBody(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == http.StatusNoContent:
		return false
	case status == http.StatusNotModified:
		return false
	default:
		return true
	}
}

// Route describes one registered route.
type Route struct {
	Method  string
	Pattern string
}
