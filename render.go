package ohm

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/a-h/templ"
	"github.com/ajg/form"
)

type responseStatusKey struct{}

// SetStatus records the status code used by Render.
func SetStatus(r *http.Request, status int) {
	if r == nil {
		return
	}
	*r = *r.WithContext(context.WithValue(r.Context(), responseStatusKey{}, status))
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

	writeHTML(w, status, body.String())
	return nil
}

func bindRequest(r *http.Request, v Binder) error {
	if v == nil {
		return fmt.Errorf("binder is required")
	}
	if err := decodeRequest(r, v); err != nil {
		return err
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
		return form.NewDecoder(r.Body).Decode(v)
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
		return err
	}
	respond(w, r, v)
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		return err
	}
	if value.Kind() != reflect.Struct {
		return nil
	}

	for i := range value.NumField() {
		field := value.Field(i)
		if !field.CanInterface() || !field.Type().Implements(rendererType) || isNil(field) {
			continue
		}
		child := field.Interface().(Renderer)
		if err := renderValue(w, r, child); err != nil {
			return err
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
		return v.Bind(r)
	}

	for i := range value.NumField() {
		field := value.Field(i)
		if !field.CanInterface() || !field.Type().Implements(binderType) || isNil(field) {
			continue
		}
		child := field.Interface().(Binder)
		if err := bindValue(r, child); err != nil {
			return err
		}
	}
	return v.Bind(r)
}

func isNil(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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
	fields := strings.Split(r.Header.Get("Accept"), ",")
	if len(fields) == 0 {
		return contentTypePlainText
	}
	return parseContentType(fields[0])
}

func parseContentType(raw string) contentType {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(raw, ";")[0])
	}
	switch strings.ToLower(mediaType) {
	case "text/plain":
		return contentTypePlainText
	case "text/html", "application/xhtml+xml":
		return contentTypeHTML
	case "application/json", "text/javascript":
		return contentTypeJSON
	case "text/xml", "application/xml":
		return contentTypeXML
	case "application/x-www-form-urlencoded":
		return contentTypeForm
	default:
		return contentTypeUnknown
	}
}

var (
	rendererType = reflect.TypeOf(new(Renderer)).Elem()
	binderType   = reflect.TypeOf(new(Binder)).Elem()
)
