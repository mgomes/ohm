package ohm

import (
	"encoding"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var (
	formTextUnmarshalerType = reflect.TypeOf(new(encoding.TextUnmarshaler)).Elem()
	formTimeType            = reflect.TypeOf(time.Time{})
	formURLType             = reflect.TypeOf(url.URL{})
	formURLValuesType       = reflect.TypeOf(url.Values{})
)

func decodeForm(r *http.Request, v any) error {
	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("parse form: %w", err)
	}
	return decodeFormValues(r.PostForm, v)
}

func decodeFormValues(values url.Values, dst any) error {
	if dst == nil {
		return fmt.Errorf("form decode target is required")
	}

	value := reflect.ValueOf(dst)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return fmt.Errorf("form decode target must be a non-nil pointer")
	}

	target := value.Elem()
	if target.Type() == formURLValuesType {
		target.Set(reflect.ValueOf(cloneFormValues(values)))
		return nil
	}
	if target.Kind() == reflect.Map {
		return decodeFormMap(values, target)
	}
	if target.Kind() != reflect.Struct {
		return fmt.Errorf("form decode target must point to a struct, map, or url.Values")
	}

	used := make(map[string]struct{}, len(values))
	if err := decodeFormStruct(values, target, "", used); err != nil {
		return err
	}
	for key := range values {
		if _, ok := used[key]; !ok {
			return fmt.Errorf("unknown form field %q", key)
		}
	}
	return nil
}

func decodeFormMap(values url.Values, target reflect.Value) error {
	if target.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("form map target key type must be string")
	}
	if target.IsNil() {
		target.Set(reflect.MakeMap(target.Type()))
	}

	switch target.Type().Elem().Kind() {
	case reflect.String:
		for key, raw := range values {
			mapKey := reflect.ValueOf(key).Convert(target.Type().Key())
			mapValue := reflect.ValueOf(lastFormValue(raw)).Convert(target.Type().Elem())
			target.SetMapIndex(mapKey, mapValue)
		}
		return nil
	case reflect.Slice:
		elem := target.Type().Elem()
		if elem.Elem().Kind() != reflect.String {
			return fmt.Errorf("form map target values must be string or []string")
		}
		for key, raw := range values {
			copied := reflect.MakeSlice(elem, len(raw), len(raw))
			for i, item := range raw {
				copied.Index(i).SetString(item)
			}
			mapKey := reflect.ValueOf(key).Convert(target.Type().Key())
			target.SetMapIndex(mapKey, copied)
		}
		return nil
	default:
		return fmt.Errorf("form map target values must be string or []string")
	}
}

func decodeFormStruct(values url.Values, target reflect.Value, prefix string, used map[string]struct{}) error {
	targetType := target.Type()
	for i := range target.NumField() {
		fieldType := targetType.Field(i)
		field := target.Field(i)
		if !field.CanSet() {
			continue
		}

		name, skip := formFieldName(fieldType)
		if skip {
			continue
		}

		if name == "" {
			if canDecodeNestedFormStruct(field) {
				if err := decodeFormStruct(values, dereferenceFormField(field), prefix, used); err != nil {
					return err
				}
				continue
			}
			name = fieldType.Name
		}

		key := joinFormKey(prefix, name)
		if raw, ok := values[key]; ok {
			if err := setFormField(field, raw, key); err != nil {
				return err
			}
			used[key] = struct{}{}
			continue
		}

		if canDecodeNestedFormStruct(field) && hasFormKeyPrefix(values, key+".") {
			nested := dereferenceFormField(field)
			if err := decodeFormStruct(values, nested, key, used); err != nil {
				return err
			}
		}
	}
	return nil
}

func formFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("form")
	name, _, _ := strings.Cut(tag, ",")
	switch {
	case name == "-":
		return "", true
	case name != "":
		return name, false
	case field.Anonymous:
		return "", false
	default:
		return field.Name, false
	}
}

func joinFormKey(prefix string, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func hasFormKeyPrefix(values url.Values, prefix string) bool {
	for key := range values {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func canDecodeNestedFormStruct(field reflect.Value) bool {
	for field.Kind() == reflect.Ptr {
		if formTypeHasTextUnmarshaler(field.Type()) {
			return false
		}
		field = reflect.New(field.Type().Elem()).Elem()
	}
	return field.Kind() == reflect.Struct &&
		!formTypeHasTextUnmarshaler(field.Type()) &&
		!field.Type().ConvertibleTo(formTimeType) &&
		!field.Type().ConvertibleTo(formURLType)
}

func dereferenceFormField(field reflect.Value) reflect.Value {
	for field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}
	return field
}

func setFormField(field reflect.Value, raw []string, name string) error {
	for field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	if field.Kind() == reflect.Slice && field.Type().Elem().Kind() != reflect.Uint8 {
		values := reflect.MakeSlice(field.Type(), 0, len(raw))
		for _, item := range raw {
			elem := reflect.New(field.Type().Elem()).Elem()
			if err := setFormScalar(elem, item, name); err != nil {
				return err
			}
			values = reflect.Append(values, elem)
		}
		field.Set(values)
		return nil
	}

	if field.Kind() == reflect.Array {
		if len(raw) > field.Len() {
			return fmt.Errorf("form field %q has %d values for %d-element array", name, len(raw), field.Len())
		}
		for i, item := range raw {
			if err := setFormScalar(field.Index(i), item, name); err != nil {
				return err
			}
		}
		return nil
	}

	return setFormScalar(field, lastFormValue(raw), name)
}

func setFormScalar(field reflect.Value, raw string, name string) error {
	if field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.Uint8 {
		field.SetBytes([]byte(raw))
		return nil
	}
	if field.Type().ConvertibleTo(formTimeType) {
		return setFormTime(field, raw, name)
	}
	if field.Type().ConvertibleTo(formURLType) {
		return setFormURL(field, raw, name)
	}
	if formTypeHasTextUnmarshaler(field.Type()) {
		return setFormText(field, raw, name)
	}
	if raw == "" {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}

	switch field.Kind() {
	case reflect.Bool:
		return setFormBool(field, raw, name)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(raw, 10, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("decode form field %q as %s: %w", name, field.Type(), err)
		}
		field.SetInt(value)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw, 10, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("decode form field %q as %s: %w", name, field.Type(), err)
		}
		field.SetUint(value)
		return nil
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("decode form field %q as %s: %w", name, field.Type(), err)
		}
		field.SetFloat(value)
		return nil
	case reflect.Complex64, reflect.Complex128:
		value, err := strconv.ParseComplex(raw, field.Type().Bits())
		if err != nil {
			return fmt.Errorf("decode form field %q as %s: %w", name, field.Type(), err)
		}
		field.SetComplex(value)
		return nil
	case reflect.String:
		field.SetString(raw)
		return nil
	default:
		return fmt.Errorf("decode form field %q: unsupported type %s", name, field.Type())
	}
}

func setFormBool(field reflect.Value, raw string, name string) error {
	if strings.EqualFold(raw, "on") {
		field.SetBool(true)
		return nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("decode form field %q as %s: %w", name, field.Type(), err)
	}
	field.SetBool(value)
	return nil
}

func setFormTime(field reflect.Value, raw string, name string) error {
	if raw == "" {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}
	for _, layout := range formTimeLayouts {
		value, err := time.Parse(layout, raw)
		if err == nil {
			field.Set(reflect.ValueOf(value).Convert(field.Type()))
			return nil
		}
	}
	return fmt.Errorf("decode form field %q as %s: invalid time %q", name, field.Type(), raw)
}

func setFormURL(field reflect.Value, raw string, name string) error {
	if raw == "" {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}
	value, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("decode form field %q as %s: %w", name, field.Type(), err)
	}
	field.Set(reflect.ValueOf(*value).Convert(field.Type()))
	return nil
}

func setFormText(field reflect.Value, raw string, name string) error {
	var unmarshaler encoding.TextUnmarshaler
	if field.Type().Implements(formTextUnmarshalerType) {
		unmarshaler = field.Interface().(encoding.TextUnmarshaler)
	} else if field.CanAddr() && field.Addr().Type().Implements(formTextUnmarshalerType) {
		unmarshaler = field.Addr().Interface().(encoding.TextUnmarshaler)
	} else {
		return fmt.Errorf("decode form field %q: unsupported text target %s", name, field.Type())
	}
	if err := unmarshaler.UnmarshalText([]byte(raw)); err != nil {
		return fmt.Errorf("decode form field %q as %s: %w", name, field.Type(), err)
	}
	return nil
}

func formTypeHasTextUnmarshaler(t reflect.Type) bool {
	if t.Implements(formTextUnmarshalerType) {
		return true
	}
	if t.Kind() == reflect.Ptr {
		return false
	}
	return reflect.PointerTo(t).Implements(formTextUnmarshalerType)
}

func lastFormValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func cloneFormValues(values url.Values) url.Values {
	out := make(url.Values, len(values))
	for key, raw := range values {
		out[key] = append([]string(nil), raw...)
	}
	return out
}

var formTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02",
	"2006-01",
	"2006",
	"15:04:05",
	"15:04",
}
