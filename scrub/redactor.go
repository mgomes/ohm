package scrub

import (
	"log/slog"
	"reflect"
	"strings"
	"unicode"
)

const defaultReplacement = "[REDACTED]"

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
	if r.SensitiveKey(key) {
		return r.replacementValue()
	}

	switch value := value.(type) {
	case nil:
		return nil
	case Sensitive:
		return r.replacementValue()
	case slog.Attr:
		return r.Attr(value)
	case []slog.Attr:
		attrs := make([]slog.Attr, 0, len(value))
		for _, attr := range value {
			attrs = append(attrs, r.Attr(attr))
		}
		return attrs
	case map[string]any:
		return r.mapAny(value)
	case map[string]string:
		out := make(map[string]any, len(value))
		for childKey, childValue := range value {
			out[childKey] = r.Any(childKey, childValue)
		}
		return out
	}

	return r.reflectAny(value)
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

func (r *Redactor) mapAny(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, childValue := range value {
		out[key] = r.Any(key, childValue)
	}
	return out
}

func (r *Redactor) reflectAny(value any) any {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return value
	}

	switch reflected.Kind() {
	case reflect.Map:
		if reflected.Type().Key().Kind() != reflect.String {
			return value
		}
		out := make(map[string]any, reflected.Len())
		iter := reflected.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			out[key] = r.Any(key, valueFromReflect(iter.Value()))
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, 0, reflected.Len())
		for i := range reflected.Len() {
			out = append(out, r.Any("", valueFromReflect(reflected.Index(i))))
		}
		return out
	default:
		return value
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
