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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type responseStatusKey struct{}

type responseStatusState struct {
	code           atomic.Int32
	handlerStarted atomic.Bool
	seedable       atomic.Bool
}

type pendingResponseStatus struct {
	code      atomic.Int32
	request   *http.Request
	sharedKey pendingResponseStatusSharedKey
	cloneKeys []any

	cleanupMu      sync.Mutex
	stopCleanup    func() bool
	cleanupStopped bool
}

var (
	pendingResponseStatusByRequest sync.Map
	// Preserve pre-handler SetStatus calls across Request.WithContext shallow copies with background contexts.
	pendingResponseStatusBySharedKey sync.Map
	pendingResponseStatusByCloneKey  sync.Map
	pendingResponseStatusTTL         = time.Minute
)

type pendingResponseStatusSharedKey struct {
	method     string
	url        uintptr
	header     uintptr
	body       uintptr
	host       string
	requestURI string
}

type pendingResponseStatusRewrittenCopyKey struct {
	method     string
	header     uintptr
	body       any
	host       string
	requestURI string
}

type pendingResponseStatusCloneRequestKey struct {
	method        string
	url           string
	host          string
	requestURI    string
	body          any
	contentLength int64
}

type pendingResponseStatusContextCloneKey struct {
	context any
	request pendingResponseStatusCloneRequestKey
}

type pendingResponseStatusDoneCloneKey struct {
	done    <-chan struct{}
	request pendingResponseStatusCloneRequestKey
}

type pendingResponseStatusBodyCloneKey struct {
	request pendingResponseStatusCloneRequestKey
}

type pendingResponseStatusGroup struct {
	mu       sync.Mutex
	pendings []*pendingResponseStatus
}

func withResponseStatus(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	if _, ok := responseStatusStateFromRequest(r); ok {
		return r
	}
	state := &responseStatusState{}
	if status, ok := takePendingResponseStatus(r); ok {
		state.code.Store(status)
		state.seedable.Store(true)
	}
	return r.WithContext(context.WithValue(r.Context(), responseStatusKey{}, state))
}

func withNewResponseStatus(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	state := &responseStatusState{}
	if existing, ok := responseStatusStateFromRequest(r); ok {
		if existing.seedable.Load() {
			state.code.Store(existing.code.Load())
			state.seedable.Store(true)
		}
	}
	if status, ok := takePendingResponseStatus(r); ok {
		state.code.Store(status)
		state.seedable.Store(true)
	}
	return r.WithContext(context.WithValue(r.Context(), responseStatusKey{}, state))
}

// SetStatus records the status code used by Render.
func SetStatus(r *http.Request, status int) {
	state, ok := responseStatusStateFromRequest(r)
	if !ok {
		rememberPendingResponseStatus(r, int32(status))
		return
	}
	state.code.Store(int32(status))
	state.seedable.Store(!state.handlerStarted.Load())
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
	if err := validateDecodeTarget(v); err != nil {
		return err
	}
	if r.Body == nil {
		return decodeClientInputError(io.EOF)
	}

	switch requestContentType(r) {
	case contentTypeJSON:
		body, err := readRequestBody(r.Body, requestBodyLimit(r), "JSON")
		if err != nil {
			return decodeClientInputError(err)
		}
		if err := decodeJSONBody(bytes.NewReader(body), v); err != nil {
			return decodeClientInputError(err)
		}
		return nil
	case contentTypeXML:
		body, err := readRequestBody(r.Body, requestBodyLimit(r), "XML")
		if err != nil {
			return decodeClientInputError(err)
		}
		if err := decodeXMLBody(bytes.NewReader(body), v); err != nil {
			return decodeClientInputError(err)
		}
		return nil
	case contentTypeForm:
		if err := validateFormDecodeTarget(v); err != nil {
			return err
		}
		if err := decodeForm(r, v); err != nil {
			return decodeClientInputError(err)
		}
		return nil
	default:
		return decodeClientInputError(errors.New("ohm: unable to automatically decode the request content type"))
	}
}

func decodeJSONBody(body io.Reader, v any) error {
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("ohm: JSON request body must contain exactly one value; found trailing token %v", token)
	}
	return nil
}

func decodeXMLBody(body io.Reader, v any) error {
	decoder := xml.NewDecoder(body)
	if err := decoder.Decode(v); err != nil {
		return err
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if isXMLDocumentEpilogToken(token) {
			continue
		}
		return fmt.Errorf("ohm: XML request body must contain exactly one value; found trailing token %T", token)
	}
}

func isXMLDocumentEpilogToken(token xml.Token) bool {
	switch token := token.(type) {
	case xml.CharData:
		return strings.TrimSpace(string(token)) == ""
	case xml.Comment, xml.ProcInst:
		return true
	default:
		return false
	}
}

func validateDecodeTarget(v any) error {
	if v == nil {
		return fmt.Errorf("decode target is required")
	}
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return fmt.Errorf("decode target must be a non-nil pointer")
	}
	return nil
}

func validateFormDecodeTarget(v any) error {
	target := reflect.ValueOf(v).Elem()
	if target.Type() == formURLValuesType || target.Kind() == reflect.Struct {
		return nil
	}
	if target.Kind() == reflect.Map {
		return validateTopLevelFormMapTarget(target.Type())
	}
	return fmt.Errorf("form decode target must point to a struct, map, or url.Values")
}

func decodeClientInputError(err error) error {
	if err == nil {
		return nil
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return err
	}
	var targetErr *formTargetError
	if errors.As(err, &targetErr) {
		return err
	}
	return &DecodeError{
		Status: http.StatusBadRequest,
		Err:    err,
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
	state, ok := responseStatusStateFromRequest(r)
	if !ok {
		status, ok := takePendingResponseStatus(r)
		return responseStatusOrFallback(status, ok, fallback)
	}
	if status := int(state.code.Load()); status != 0 {
		return status
	}
	return fallback
}

func responseStatusStateFromRequest(r *http.Request) (*responseStatusState, bool) {
	if r == nil {
		return nil, false
	}
	state, ok := r.Context().Value(responseStatusKey{}).(*responseStatusState)
	return state, ok
}

func markResponseStatusHandlerStarted(r *http.Request) {
	state, ok := responseStatusStateFromRequest(r)
	if ok {
		state.handlerStarted.Store(true)
		state.seedable.Store(false)
	}
}

func rememberPendingResponseStatus(r *http.Request, status int32) {
	if r == nil {
		return
	}
	if pending, ok := lookupPendingResponseStatusForWrite(r); ok {
		pending.code.Store(status)
		return
	}

	pending := &pendingResponseStatus{}
	pending.request = r
	pending.sharedKey = pendingResponseStatusSharedKeyFor(r)
	pending.cloneKeys = pendingResponseStatusCloneKeysFor(r)
	pending.code.Store(status)

	actual, loaded := pendingResponseStatusByRequest.LoadOrStore(r, pending)
	if loaded {
		stopPendingResponseStatusCleanup(pending)
		actual.(*pendingResponseStatus).code.Store(status)
		return
	}
	if !storePendingResponseStatusBySharedKey(pending, status) {
		return
	}
	storePendingResponseStatusByCloneKeys(pending)
	startPendingResponseStatusCleanup(pending, r.Context())
}

func takePendingResponseStatus(r *http.Request) (int32, bool) {
	if pending, ok := lookupPendingResponseStatusForWrite(r); ok {
		deletePendingResponseStatus(pending)
		return pending.code.Load(), true
	}

	pending, ambiguous := lookupPendingResponseStatusByCloneKeys(r)
	if pending != nil {
		deletePendingResponseStatus(pending)
		return pending.code.Load(), true
	}
	// Identical cloned requests cannot be attributed to their originals without mutating the request.
	for _, pending := range ambiguous {
		deletePendingResponseStatus(pending)
	}
	return 0, false
}

func lookupPendingResponseStatusForWrite(r *http.Request) (*pendingResponseStatus, bool) {
	if r == nil {
		return nil, false
	}
	if value, ok := pendingResponseStatusByRequest.Load(r); ok {
		return value.(*pendingResponseStatus), true
	}
	sharedKey := pendingResponseStatusSharedKeyFor(r)
	if sharedKey.isZero() {
		return nil, false
	}
	if value, ok := pendingResponseStatusBySharedKey.Load(sharedKey); ok {
		return value.(*pendingResponseStatus), true
	}
	return nil, false
}

func responseStatusOrFallback(status int32, ok bool, fallback int) int {
	if ok && status != 0 {
		return int(status)
	}
	return fallback
}

func stopPendingResponseStatusCleanup(pending *pendingResponseStatus) {
	if pending == nil {
		return
	}
	pending.cleanupMu.Lock()
	pending.cleanupStopped = true
	stopCleanup := pending.stopCleanup
	pending.stopCleanup = nil
	pending.cleanupMu.Unlock()
	if stopCleanup != nil {
		stopCleanup()
	}
}

func startPendingResponseStatusCleanup(pending *pendingResponseStatus, ctx context.Context) {
	if ctx.Done() == nil {
		startPendingResponseStatusTimer(pending)
		return
	}
	stopCleanup := context.AfterFunc(ctx, func() {
		deletePendingResponseStatus(pending)
	})
	publishPendingResponseStatusCleanup(pending, stopCleanup)
}

func startPendingResponseStatusTimer(pending *pendingResponseStatus) {
	if pendingResponseStatusTTL <= 0 {
		return
	}
	timer := time.AfterFunc(pendingResponseStatusTTL, func() {
		deletePendingResponseStatus(pending)
	})
	publishPendingResponseStatusCleanup(pending, timer.Stop)
}

func publishPendingResponseStatusCleanup(pending *pendingResponseStatus, stopCleanup func() bool) {
	pending.cleanupMu.Lock()
	if pending.cleanupStopped {
		pending.cleanupMu.Unlock()
		stopCleanup()
		return
	}
	pending.stopCleanup = stopCleanup
	pending.cleanupMu.Unlock()
}

func deletePendingResponseStatus(pending *pendingResponseStatus) {
	if pending == nil {
		return
	}
	if pending.request != nil {
		pendingResponseStatusByRequest.CompareAndDelete(pending.request, pending)
	}
	if !pending.sharedKey.isZero() {
		pendingResponseStatusBySharedKey.CompareAndDelete(pending.sharedKey, pending)
	}
	deletePendingResponseStatusFromCloneGroups(pending)
	stopPendingResponseStatusCleanup(pending)
}

func storePendingResponseStatusBySharedKey(pending *pendingResponseStatus, status int32) bool {
	if pending.sharedKey.isZero() {
		return true
	}
	actual, loaded := pendingResponseStatusBySharedKey.LoadOrStore(pending.sharedKey, pending)
	if loaded {
		deletePendingResponseStatus(pending)
		actual.(*pendingResponseStatus).code.Store(status)
		return false
	}
	return true
}

func storePendingResponseStatusByCloneKeys(pending *pendingResponseStatus) {
	for _, key := range pending.cloneKeys {
		storePendingResponseStatusByCloneKey(key, pending)
	}
}

func storePendingResponseStatusByCloneKey(key any, pending *pendingResponseStatus) {
	for {
		value, _ := pendingResponseStatusByCloneKey.LoadOrStore(key, &pendingResponseStatusGroup{})
		group := value.(*pendingResponseStatusGroup)
		group.mu.Lock()
		current, ok := pendingResponseStatusByCloneKey.Load(key)
		if ok && current == group {
			group.pendings = append(group.pendings, pending)
			group.mu.Unlock()
			return
		}
		group.mu.Unlock()
	}
}

func lookupPendingResponseStatusByCloneKeys(r *http.Request) (*pendingResponseStatus, []*pendingResponseStatus) {
	var ambiguous []*pendingResponseStatus
	for _, key := range pendingResponseStatusCloneKeysFor(r) {
		pending, matches := lookupPendingResponseStatusByCloneKey(key)
		if pending != nil {
			return pending, nil
		}
		ambiguous = append(ambiguous, matches...)
	}
	return nil, ambiguous
}

func lookupPendingResponseStatusByCloneKey(key any) (*pendingResponseStatus, []*pendingResponseStatus) {
	for {
		value, ok := pendingResponseStatusByCloneKey.Load(key)
		if !ok {
			return nil, nil
		}
		group := value.(*pendingResponseStatusGroup)
		group.mu.Lock()
		current, ok := pendingResponseStatusByCloneKey.Load(key)
		if !ok || current != group {
			group.mu.Unlock()
			continue
		}
		switch len(group.pendings) {
		case 0:
			group.mu.Unlock()
			return nil, nil
		case 1:
			pending := group.pendings[0]
			group.mu.Unlock()
			return pending, nil
		default:
			matches := slices.Clone(group.pendings)
			group.mu.Unlock()
			return nil, matches
		}
	}
}

func deletePendingResponseStatusFromCloneGroups(pending *pendingResponseStatus) {
	for _, key := range pending.cloneKeys {
		deletePendingResponseStatusFromCloneGroup(key, pending)
	}
}

func deletePendingResponseStatusFromCloneGroup(key any, pending *pendingResponseStatus) {
	value, ok := pendingResponseStatusByCloneKey.Load(key)
	if !ok {
		return
	}
	group := value.(*pendingResponseStatusGroup)
	group.mu.Lock()
	for i, candidate := range group.pendings {
		if candidate == pending {
			group.pendings = slices.Delete(group.pendings, i, i+1)
			break
		}
	}
	if len(group.pendings) == 0 {
		pendingResponseStatusByCloneKey.CompareAndDelete(key, group)
	}
	group.mu.Unlock()
}

func pendingResponseStatusSharedKeyFor(r *http.Request) pendingResponseStatusSharedKey {
	if r == nil {
		return pendingResponseStatusSharedKey{}
	}
	return pendingResponseStatusSharedKey{
		method:     r.Method,
		url:        pointerIdentity(r.URL),
		header:     pointerIdentity(r.Header),
		body:       pointerIdentity(r.Body),
		host:       r.Host,
		requestURI: r.RequestURI,
	}
}

func (key pendingResponseStatusSharedKey) isZero() bool {
	return key.url == 0 && key.header == 0 && key.body == 0
}

func pendingResponseStatusCloneKeysFor(r *http.Request) []any {
	if r == nil {
		return nil
	}

	var keys []any
	done := r.Context().Done()
	if key := pendingResponseStatusRewrittenCopyKeyFor(r); !key.isZero() {
		keys = append(keys, key)
	}
	for _, requestKey := range pendingResponseStatusCloneRequestKeysFor(r) {
		// Deep no-body clones cannot be matched safely: they are indistinguishable
		// from unrelated requests that reuse the same context, method, URL, and host.
		if requestKey.body == nil {
			continue
		}
		if contextKey, ok := comparableContextKey(r.Context()); ok {
			keys = append(keys, pendingResponseStatusContextCloneKey{
				context: contextKey,
				request: requestKey,
			})
		}
		if done != nil {
			keys = append(keys, pendingResponseStatusDoneCloneKey{
				done:    done,
				request: requestKey,
			})
		}
		keys = append(keys, pendingResponseStatusBodyCloneKey{request: requestKey})
	}
	return keys
}

func pendingResponseStatusRewrittenCopyKeyFor(r *http.Request) pendingResponseStatusRewrittenCopyKey {
	if r == nil {
		return pendingResponseStatusRewrittenCopyKey{}
	}
	return pendingResponseStatusRewrittenCopyKey{
		method:     r.Method,
		header:     pointerIdentity(r.Header),
		body:       bodyIdentity(r.Body),
		host:       r.Host,
		requestURI: r.RequestURI,
	}
}

func (key pendingResponseStatusRewrittenCopyKey) isZero() bool {
	return key.requestURI == "" || (key.header == 0 && key.body == nil)
}

func pendingResponseStatusCloneRequestKeysFor(r *http.Request) []pendingResponseStatusCloneRequestKey {
	key := pendingResponseStatusCloneRequestKeyFor(r)
	if key.isZero() {
		return nil
	}
	keys := []pendingResponseStatusCloneRequestKey{key}
	if key.requestURI != "" && key.url != "" {
		key.url = ""
		keys = append(keys, key)
	}
	return keys
}

func pendingResponseStatusCloneRequestKeyFor(r *http.Request) pendingResponseStatusCloneRequestKey {
	if r == nil {
		return pendingResponseStatusCloneRequestKey{}
	}
	url := ""
	if r.URL != nil {
		url = r.URL.String()
	}
	return pendingResponseStatusCloneRequestKey{
		method:        r.Method,
		url:           url,
		host:          r.Host,
		requestURI:    r.RequestURI,
		body:          bodyIdentity(r.Body),
		contentLength: r.ContentLength,
	}
}

func (key pendingResponseStatusCloneRequestKey) isZero() bool {
	return key.url == "" && key.requestURI == "" && key.body == nil
}

func comparableContextKey(ctx context.Context) (any, bool) {
	if ctx == nil {
		return nil, false
	}
	value := reflect.ValueOf(ctx)
	if !value.IsValid() || !value.Comparable() {
		return nil, false
	}
	return ctx, true
}

func pointerIdentity(v any) uintptr {
	if v == nil {
		return 0
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.String, reflect.UnsafePointer:
		return value.Pointer()
	default:
		return 0
	}
}

func bodyIdentity(v any) any {
	if v == nil {
		return nil
	}
	if reflect.TypeOf(v) == reflect.TypeOf(http.NoBody) {
		return nil
	}
	if pointer := pointerIdentity(v); pointer != 0 {
		return pointer
	}
	value := reflect.ValueOf(v)
	if !value.IsValid() || !value.Comparable() {
		return nil
	}
	return v
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
