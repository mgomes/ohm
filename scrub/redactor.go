package scrub

import (
	"encoding"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"time"
	"unicode"
)

const defaultReplacement = "[REDACTED]"

const maxErrorUnwrapDepth = 32

var defaultKeys = []string{
	"password",
	"passwd",
	"pwd",
	"token",
	"secret",
	"api_key",
	"apikey",
	"authorization",
	"cookie",
	"set-cookie",
	"csrf",
	"session",
}

var normalizedDefaultKeys = normalizedKeys(defaultKeys...)

var timeType = reflect.TypeOf(time.Time{})
var errorType = reflect.TypeOf((*error)(nil)).Elem()

type unwrapOne interface {
	Unwrap() error
}

type unwrapMany interface {
	Unwrap() []error
}

// Option configures a Redactor.
type Option func(*Redactor)

// Redactor redacts sensitive values from structured data.
type Redactor struct {
	keys        map[string]struct{}
	replacement string
}

// Sensitive marks a value that must always be redacted.
type Sensitive struct {
	Value any
}

// New creates a redactor with Ohm's default sensitive keys.
func New(opts ...Option) *Redactor {
	redactor := &Redactor{
		keys:        make(map[string]struct{}, len(normalizedDefaultKeys)),
		replacement: defaultReplacement,
	}
	for key := range normalizedDefaultKeys {
		redactor.keys[key] = struct{}{}
	}
	for _, opt := range opts {
		opt(redactor)
	}
	return redactor
}

// WithKeys adds sensitive keys to a redactor.
func WithKeys(keys ...string) Option {
	return func(redactor *Redactor) {
		for _, key := range keys {
			redactor.addKey(key)
		}
	}
}

// WithReplacement changes the replacement value used for redacted data.
func WithReplacement(replacement string) Option {
	return func(redactor *Redactor) {
		if replacement != "" {
			redactor.replacement = replacement
		}
	}
}

// Mark returns a value wrapper that is always redacted.
func Mark(value any) Sensitive {
	return Sensitive{Value: value}
}

// SensitiveKey reports whether key should be redacted.
func (r *Redactor) SensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	if normalized == "" {
		return false
	}
	for sensitiveKey := range r.keySet() {
		if strings.Contains(normalized, sensitiveKey) {
			return true
		}
	}
	return false
}

// Attr returns a scrubbed slog attribute.
func (r *Redactor) Attr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if r.SensitiveKey(attr.Key) {
		return slog.String(attr.Key, r.replacementValue())
	}

	switch attr.Value.Kind() {
	case slog.KindGroup:
		attrs := attr.Value.Group()
		args := make([]any, 0, len(attrs))
		for _, child := range attrs {
			args = append(args, r.Attr(child))
		}
		return slog.Group(attr.Key, args...)
	case slog.KindAny:
		return slog.Any(attr.Key, r.Any(attr.Key, attr.Value.Any()))
	default:
		return attr
	}
}

// Any returns a scrubbed copy of value when the value is a supported structured type.
func (r *Redactor) Any(key string, value any) any {
	redacted, _ := r.any(key, value)
	return redacted
}

func (r *Redactor) any(key string, value any) (any, bool) {
	if r.SensitiveKey(key) {
		return r.replacementValue(), true
	}

	switch value := value.(type) {
	case nil:
		return nil, false
	case Sensitive:
		return r.replacementValue(), true
	case error:
		if isNilError(value) {
			return nil, true
		}
		if r.preservesErrorEncoding(value) {
			return value, false
		}
	case slog.Attr:
		return r.Attr(value), true
	case []slog.Attr:
		attrs := make([]slog.Attr, 0, len(value))
		for _, attr := range value {
			attrs = append(attrs, r.Attr(attr))
		}
		return attrs, true
	case map[string]any:
		return r.mapAny(value)
	case map[string]string:
		out := make(map[string]any, len(value))
		changed := false
		for childKey, childValue := range value {
			redacted, childChanged := r.any(childKey, childValue)
			out[childKey] = redacted
			changed = changed || childChanged
		}
		if !changed {
			return value, false
		}
		return out, true
	}

	return r.reflectAny(value)
}

func (r *Redactor) preservesErrorEncoding(value error) bool {
	return r.preservesErrorEncodingAtDepth(value, 0)
}

func (r *Redactor) preservesErrorEncodingAtDepth(value error, depth int) bool {
	if value == nil {
		return true
	}
	if depth > maxErrorUnwrapDepth {
		return false
	}
	if isNilError(value) {
		return false
	}
	if implementsEncoding(value) {
		return false
	}

	unwrapsOne := false
	unwrapsMany := false
	switch value := value.(type) {
	case unwrapMany:
		unwrapsMany = true
		for _, child := range value.Unwrap() {
			if !r.preservesErrorEncodingAtDepth(child, depth+1) {
				return false
			}
		}
	case unwrapOne:
		unwrapsOne = true
		if !r.preservesErrorEncodingAtDepth(value.Unwrap(), depth+1) {
			return false
		}
	}

	reflected := reflect.ValueOf(value)
	for reflected.Kind() == reflect.Interface || reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return true
		}
		reflected = reflected.Elem()
	}

	switch reflected.Kind() {
	case reflect.Struct:
		return !hasExportedFields(reflected.Type()) &&
			!r.hasSensitiveFieldName(reflected.Type(), make(map[reflect.Type]struct{})) &&
			!hasPrivateStructuredFields(reflected.Type(), make(map[reflect.Type]struct{}), unwrapsOne, unwrapsMany) &&
			!r.hasUnsafePrivateFieldValues(reflected, unwrapsOne, unwrapsMany, make(map[uintptr]struct{}), 0)
	case reflect.Map, reflect.Slice, reflect.Array:
		return false
	}
	return true
}

func isNilError(value error) bool {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func hasExportedFields(t reflect.Type) bool {
	for i := range t.NumField() {
		if t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

func hasPrivateStructuredFields(
	t reflect.Type,
	seen map[reflect.Type]struct{},
	allowErrorInterfaces bool,
	allowErrorCollections bool,
) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if _, ok := seen[t]; ok {
		return false
	}
	seen[t] = struct{}{}

	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			if field.IsExported() {
				continue
			}
			if hasStructuredFieldType(field.Type, seen, allowErrorInterfaces, allowErrorCollections) {
				return true
			}
		}
	}
	return false
}

func hasStructuredFieldType(
	t reflect.Type,
	seen map[reflect.Type]struct{},
	allowErrorInterfaces bool,
	allowErrorCollections bool,
) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Interface:
		if allowErrorInterfaces && t.Implements(errorType) {
			return false
		}
		return true
	case reflect.Map, reflect.Slice, reflect.Array:
		if allowErrorCollections && isErrorCollection(t) {
			return false
		}
		return true
	case reflect.Struct:
		return hasPrivateStructuredFields(t, seen, allowErrorInterfaces, allowErrorCollections)
	default:
		return false
	}
}

func isErrorCollection(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return t.Elem().Implements(errorType)
	default:
		return false
	}
}

func (r *Redactor) hasUnsafePrivateFieldValues(
	value reflect.Value,
	allowErrorInterfaces bool,
	allowErrorCollections bool,
	seenPointers map[uintptr]struct{},
	depth int,
) bool {
	if depth > maxErrorUnwrapDepth {
		return true
	}

	value, ok := dereferenceValue(value, seenPointers)
	if !ok {
		return true
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return false
	}

	t := value.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if field.IsExported() {
			continue
		}
		if r.hasUnsafePrivateFieldValue(value.Field(i), allowErrorInterfaces, allowErrorCollections, seenPointers, depth+1) {
			return true
		}
	}
	return false
}

func (r *Redactor) hasUnsafePrivateFieldValue(
	value reflect.Value,
	allowErrorInterfaces bool,
	allowErrorCollections bool,
	seenPointers map[uintptr]struct{},
	depth int,
) bool {
	if !value.IsValid() {
		return false
	}
	if depth > maxErrorUnwrapDepth {
		return true
	}

	switch value.Kind() {
	case reflect.Interface:
		if allowErrorInterfaces && value.Type().Implements(errorType) {
			if value.IsNil() {
				return false
			}
			return r.hasUnsafeErrorFieldValue(value.Elem(), seenPointers, depth+1)
		}
	case reflect.Pointer:
		if value.IsNil() {
			return false
		}
		return r.hasUnsafePrivateFieldValue(value.Elem(), allowErrorInterfaces, allowErrorCollections, seenPointers, depth+1)
	case reflect.Slice, reflect.Array:
		if allowErrorCollections && isErrorCollection(value.Type()) {
			for i := range value.Len() {
				if r.hasUnsafeErrorFieldValue(value.Index(i), seenPointers, depth+1) {
					return true
				}
			}
		}
	case reflect.Struct:
		return r.hasUnsafePrivateFieldValues(value, allowErrorInterfaces, allowErrorCollections, seenPointers, depth+1)
	}
	return false
}

func (r *Redactor) hasUnsafeErrorFieldValue(value reflect.Value, seenPointers map[uintptr]struct{}, depth int) bool {
	if depth > maxErrorUnwrapDepth {
		return true
	}

	value, ok := dereferenceValue(value, seenPointers)
	if !ok {
		return true
	}
	if !value.IsValid() {
		return false
	}

	switch value.Kind() {
	case reflect.Struct:
		return hasExportedFields(value.Type()) ||
			r.hasSensitiveFieldName(value.Type(), make(map[reflect.Type]struct{})) ||
			hasPrivateStructuredFields(value.Type(), make(map[reflect.Type]struct{}), true, true) ||
			r.hasUnsafePrivateFieldValues(value, true, true, seenPointers, depth+1)
	case reflect.Map:
		return true
	case reflect.Slice, reflect.Array:
		if isErrorCollection(value.Type()) {
			for i := range value.Len() {
				if r.hasUnsafeErrorFieldValue(value.Index(i), seenPointers, depth+1) {
					return true
				}
			}
			return false
		}
		return true
	}
	return false
}

func dereferenceValue(value reflect.Value, seenPointers map[uintptr]struct{}) (reflect.Value, bool) {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return reflect.Value{}, true
		}
		if value.Kind() == reflect.Pointer {
			pointer := value.Pointer()
			if _, ok := seenPointers[pointer]; ok {
				return reflect.Value{}, false
			}
			seenPointers[pointer] = struct{}{}
		}
		value = value.Elem()
	}
	return value, true
}

func (r *Redactor) hasSensitiveFieldName(t reflect.Type, seen map[reflect.Type]struct{}) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if _, ok := seen[t]; ok {
		return false
	}
	seen[t] = struct{}{}

	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			if r.SensitiveKey(field.Name) {
				return true
			}
			if key, ok := fieldKey(field); ok && r.SensitiveKey(key) {
				return true
			}
			if r.hasSensitiveFieldName(field.Type, seen) {
				return true
			}
		}
	case reflect.Map, reflect.Slice, reflect.Array:
		return r.hasSensitiveFieldName(t.Elem(), seen)
	}
	return false
}

func (r *Redactor) addKey(key string) {
	if r.keys == nil {
		r.keys = make(map[string]struct{}, len(normalizedDefaultKeys)+1)
		for key := range normalizedDefaultKeys {
			r.keys[key] = struct{}{}
		}
	}

	normalized := normalizeKey(key)
	if normalized != "" {
		r.keys[normalized] = struct{}{}
	}
}

func (r *Redactor) keySet() map[string]struct{} {
	if r == nil || r.keys == nil {
		return normalizedDefaultKeys
	}
	return r.keys
}

func (r *Redactor) replacementValue() string {
	if r == nil || r.replacement == "" {
		return defaultReplacement
	}
	return r.replacement
}

func (r *Redactor) mapAny(value map[string]any) (any, bool) {
	out := make(map[string]any, len(value))
	changed := false
	for key, childValue := range value {
		redacted, childChanged := r.any(key, childValue)
		out[key] = redacted
		changed = changed || childChanged
	}
	if !changed {
		return value, false
	}
	return out, true
}

func (r *Redactor) reflectAny(value any) (any, bool) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return value, false
	}
	return r.reflectValue(reflected)
}

func (r *Redactor) reflectValue(reflected reflect.Value) (any, bool) {
	for reflected.Kind() == reflect.Interface || reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return valueFromReflect(reflected), false
		}
		redacted, changed := r.reflectValue(reflected.Elem())
		if changed {
			return redacted, true
		}
		return valueFromReflect(reflected), false
	}

	switch reflected.Kind() {
	case reflect.Map:
		if reflected.Type().Key().Kind() != reflect.String {
			return valueFromReflect(reflected), false
		}
		out := make(map[string]any, reflected.Len())
		changed := false
		iter := reflected.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			redacted, childChanged := r.any(key, valueFromReflect(iter.Value()))
			out[key] = redacted
			changed = changed || childChanged
		}
		if !changed {
			return valueFromReflect(reflected), false
		}
		return out, true
	case reflect.Slice, reflect.Array:
		out := make([]any, 0, reflected.Len())
		changed := false
		for i := range reflected.Len() {
			redacted, childChanged := r.any("", valueFromReflect(reflected.Index(i)))
			out = append(out, redacted)
			changed = changed || childChanged
		}
		if !changed {
			return valueFromReflect(reflected), false
		}
		return out, true
	case reflect.Struct:
		if usesSafeEncoding(reflected) {
			return valueFromReflect(reflected), false
		}
		return r.structAny(reflected, usesOwnEncoding(reflected))
	default:
		return valueFromReflect(reflected), false
	}
}

func (r *Redactor) structAny(reflected reflect.Value, forceMap bool) (any, bool) {
	reflectedType := reflected.Type()
	out := make(map[string]any, reflected.NumField())
	changed := false

	for i := range reflected.NumField() {
		field := reflectedType.Field(i)
		if !field.IsExported() {
			changed = true
			continue
		}

		key, ok := fieldKey(field)
		if !ok {
			changed = true
			continue
		}

		redacted, fieldChanged := r.any(key, valueFromReflect(reflected.Field(i)))
		out[key] = redacted
		changed = changed || fieldChanged
	}

	if !changed && !forceMap {
		return valueFromReflect(reflected), false
	}
	return out, true
}

func usesSafeEncoding(value reflect.Value) bool {
	return value.Type() == timeType
}

func usesOwnEncoding(value reflect.Value) bool {
	if value.CanInterface() && implementsEncoding(value.Interface()) {
		return true
	}
	if value.CanAddr() && value.Addr().CanInterface() {
		return implementsEncoding(value.Addr().Interface())
	}
	return false
}

func implementsEncoding(value any) bool {
	_, jsonMarshaler := value.(json.Marshaler)
	_, textMarshaler := value.(encoding.TextMarshaler)
	return jsonMarshaler || textMarshaler
}

func fieldKey(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	name, _, _ := strings.Cut(tag, ",")
	switch name {
	case "-":
		return "", false
	case "":
		return field.Name, true
	default:
		return name, true
	}
}

func valueFromReflect(value reflect.Value) any {
	if !value.IsValid() || !value.CanInterface() {
		return nil
	}
	return value.Interface()
}

func normalizeKey(key string) string {
	var builder strings.Builder
	for _, char := range key {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			builder.WriteRune(unicode.ToLower(char))
		}
	}
	return builder.String()
}

func normalizedKeys(keys ...string) map[string]struct{} {
	normalized := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = normalizeKey(key)
		if key != "" {
			normalized[key] = struct{}{}
		}
	}
	return normalized
}
