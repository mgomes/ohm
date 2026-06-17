package ohm

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
)

type responseStatusKey struct{}

// SetStatus records the status code used by Render.
func SetStatus(r *http.Request, status int) {
	if r == nil {
		return
	}
	*r = *r.WithContext(context.WithValue(r.Context(), responseStatusKey{}, status))
}

// RenderHTML renders content as an HTML response with status.
func RenderHTML(w http.ResponseWriter, r *http.Request, status int, html HTML) error {
	if w == nil {
		return fmt.Errorf("html response writer is required")
	}
	if r == nil {
		return fmt.Errorf("html request is required")
	}
	if html == nil {
		return fmt.Errorf("html renderer is required")
	}
	if status < 100 || status > 999 {
		return fmt.Errorf("html status code %d is invalid", status)
	}

	var body bytes.Buffer
	if err := html.RenderHTML(r.Context(), &body); err != nil {
		return fmt.Errorf("render html: %w", err)
	}

	writeHTML(w, status, body.Bytes())
	return nil
}

func bindRequest(r *http.Request, v Binder) error {
	if v == nil {
		return fmt.Errorf("binder is required")
	}
	if err := decodeRequest(r, v); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	return bindValue(r, v)
}

func decodeRequest(r *http.Request, v any) error {
	if r == nil {
		return fmt.Errorf("decode request is required")
	}
	if v == nil {
		return fmt.Errorf("decode target is required")
	}
	if r.Body == nil {
		return io.EOF
	}

	switch requestContentType(r) {
	case contentTypeJSON:
		defer drainBody(r.Body)
		return json.NewDecoder(r.Body).Decode(v)
	case contentTypeXML:
		defer drainBody(r.Body)
		return xml.NewDecoder(r.Body).Decode(v)
	case contentTypeForm:
		return decodeForm(r, v)
	default:
		return errors.New("ohm: unable to automatically decode the request content type")
	}
}

func renderResponse(w http.ResponseWriter, r *http.Request, v Renderer) error {
	if w == nil {
		return fmt.Errorf("response writer is required")
	}
	if r == nil {
		return fmt.Errorf("render request is required")
	}
	if v == nil {
		return fmt.Errorf("renderer is required")
	}
	if err := renderValue(w, r, v); err != nil {
		return fmt.Errorf("render response: %w", err)
	}
	respond(w, r, v)
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(v); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func writePlainText(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text))
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func writeHTML(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func respond(w http.ResponseWriter, r *http.Request, v any) {
	switch acceptedContentType(r) {
	case contentTypeXML:
		writeXML(w, responseStatus(r, http.StatusOK), v)
	default:
		writeJSON(w, responseStatus(r, http.StatusOK), v)
	}
}

func writeXML(w http.ResponseWriter, status int, v any) {
	body, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)

	searchLen := len(body)
	if searchLen > 100 {
		searchLen = 100
	}
	if !bytes.Contains(body[:searchLen], []byte("<?xml")) {
		_, _ = w.Write([]byte(xml.Header))
	}
	_, _ = w.Write(body)
}

func responseStatus(r *http.Request, fallback int) int {
	if status, ok := r.Context().Value(responseStatusKey{}).(int); ok {
		return status
	}
	return fallback
}

func drainBody(r io.Reader) {
	_, _ = io.Copy(io.Discard, r)
}

func renderValue(w http.ResponseWriter, r *http.Request, v Renderer) error {
	value := reflect.ValueOf(v)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}

	if err := v.Render(w, r); err != nil {
		return fmt.Errorf("render %T: %w", v, err)
	}
	if value.Kind() != reflect.Struct {
		return nil
	}

	for _, i := range cachedImplementingFieldIndexes(&rendererChildFieldIndexes, value.Type(), rendererType) {
		field := value.Field(i)
		if isNil(field) {
			continue
		}
		child := field.Interface().(Renderer)
		if err := renderValue(w, r, child); err != nil {
			return fmt.Errorf("render child %T: %w", child, err)
		}
	}
	return nil
}

func bindValue(r *http.Request, v Binder) error {
	value := reflect.ValueOf(v)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		if err := v.Bind(r); err != nil {
			return fmt.Errorf("bind %T: %w", v, err)
		}
		return nil
	}

	for _, i := range cachedImplementingFieldIndexes(&binderChildFieldIndexes, value.Type(), binderType) {
		field := value.Field(i)
		if isNil(field) {
			continue
		}
		child := field.Interface().(Binder)
		if err := bindValue(r, child); err != nil {
			return fmt.Errorf("bind child %T: %w", child, err)
		}
	}
	if err := v.Bind(r); err != nil {
		return fmt.Errorf("bind %T: %w", v, err)
	}
	return nil
}

func isNil(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func cachedImplementingFieldIndexes(cache *sync.Map, typ reflect.Type, iface reflect.Type) []int {
	key := implementingFieldIndexCacheKey{typ: typ, iface: iface}
	if indexes, ok := cache.Load(key); ok {
		return indexes.([]int)
	}

	indexes := implementingFieldIndexes(typ, iface)
	actual, _ := cache.LoadOrStore(key, indexes)
	return actual.([]int)
}

type implementingFieldIndexCacheKey struct {
	typ   reflect.Type
	iface reflect.Type
}

func implementingFieldIndexes(typ reflect.Type, iface reflect.Type) []int {
	var indexes []int
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() || !field.Type.Implements(iface) {
			continue
		}
		indexes = append(indexes, i)
	}
	return indexes
}

type contentType int

const (
	contentTypeUnknown contentType = iota
	contentTypePlainText
	contentTypeHTML
	contentTypeJSON
	contentTypeXML
	contentTypeForm
)

func requestContentType(r *http.Request) contentType {
	return parseContentType(r.Header.Get("Content-Type"))
}

func acceptedContentType(r *http.Request) contentType {
	field, _, _ := strings.Cut(r.Header.Get("Accept"), ",")
	return parseContentType(field)
}

func parseContentType(raw string) contentType {
	mediaType, _, _ := strings.Cut(raw, ";")
	mediaType = strings.TrimSpace(mediaType)
	switch {
	case asciiEqualFold(mediaType, "text/plain"):
		return contentTypePlainText
	case asciiEqualFold(mediaType, "text/html"), asciiEqualFold(mediaType, "application/xhtml+xml"):
		return contentTypeHTML
	case asciiEqualFold(mediaType, "application/json"), asciiEqualFold(mediaType, "text/javascript"):
		return contentTypeJSON
	case asciiEqualFold(mediaType, "text/xml"), asciiEqualFold(mediaType, "application/xml"):
		return contentTypeXML
	case asciiEqualFold(mediaType, "application/x-www-form-urlencoded"):
		return contentTypeForm
	default:
		return contentTypeUnknown
	}
}

func asciiEqualFold(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := range len(s) {
		sb := s[i]
		if 'A' <= sb && sb <= 'Z' {
			sb += 'a' - 'A'
		}
		tb := t[i]
		if 'A' <= tb && tb <= 'Z' {
			tb += 'a' - 'A'
		}
		if sb != tb {
			return false
		}
	}
	return true
}

var (
	rendererType              = reflect.TypeOf(new(Renderer)).Elem()
	binderType                = reflect.TypeOf(new(Binder)).Elem()
	rendererChildFieldIndexes sync.Map
	binderChildFieldIndexes   sync.Map
)
