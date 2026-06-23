package ohm

import (
	"io"
	"net/http"
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
		a.router.Method(http.MethodHead, pattern, discardResponseBody(handler))
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
	a.router.ServeHTTP(w, withNewResponseStatus(r))
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
	a.explicitHeadRoutes[pattern] = struct{}{}
}

func (a *App) hasExplicitHeadRoute(pattern string) bool {
	if a.explicitHeadRoutes == nil {
		return false
	}
	_, ok := a.explicitHeadRoutes[pattern]
	return ok
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
		tracked, state := trackResponse(w)
		r = withResponseStatus(r)
		markResponseStatusHandlerStarted(r)
		req := newRequestWithRawResponseWriter(tracked, w, r)
		if err := handler(req); err != nil {
			recordHandlerError(r.Context(), err)
			if state.committed() {
				return
			}
			a.errorHandler(req, err)
		}
	})
}

func discardResponseBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer, state := newHeadResponseWriter(w)
		next.ServeHTTP(writer, r)
		state.finish()
	})
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
	var total int64
	var buf [32 * 1024]byte
	for {
		n, readErr := src.Read(buf[:])
		if n > 0 {
			written, writeErr := s.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
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
