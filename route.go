package ohm

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// RouteParam returns a route parameter by name.
func RouteParam(r *http.Request, key string) string {
	if r == nil {
		return ""
	}
	return decodeRouteParam(r, chi.URLParam(r, key))
}

// RoutePattern returns the matched route pattern when available.
func RoutePattern(r *http.Request) string {
	routeContext := routeContext(r)
	if routeContext == nil {
		return ""
	}
	return routeContext.RoutePattern()
}

// RouteParams returns the matched route parameters.
func RouteParams(r *http.Request) map[string]string {
	routeContext := routeContext(r)
	if routeContext == nil || len(routeContext.URLParams.Keys) == 0 {
		return nil
	}

	params := make(map[string]string, len(routeContext.URLParams.Keys))
	for i, key := range routeContext.URLParams.Keys {
		if key == "" {
			continue
		}
		value := ""
		if i < len(routeContext.URLParams.Values) {
			value = decodeRouteParam(r, routeContext.URLParams.Values[i])
		}
		params[key] = value
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func routeContext(r *http.Request) *chi.Context {
	if r == nil {
		return nil
	}
	return chi.RouteContext(r.Context())
}

func decodeRouteParam(r *http.Request, value string) string {
	if r == nil || r.URL == nil || r.URL.RawPath == "" {
		return value
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}
