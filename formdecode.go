package ohm

import (
	"encoding"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
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

const (
	maxFormBodyBytes       int64 = 10 << 20
	maxFormIndexedElements       = 10_000
)

func decodeForm(r *http.Request, v any) error {
	body, err := readFormBody(r.Body, maxFormBodyBytes)
	if err != nil {
		return fmt.Errorf("read form body: %w", err)
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("parse form body: %w", err)
	}
	return decodeFormValues(values, v)
}

func readFormBody(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, NewHTTPError(http.StatusRequestEntityTooLarge, "", fmt.Errorf("form body exceeds %d byte limit", limit))
	}
	return data, nil
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

	decoder := newFormDecoder(values)
	if err := decoder.decodeStruct(target, ""); err != nil {
		return err
	}
	for key := range values {
		if _, ok := decoder.used[key]; !ok {
			return fmt.Errorf("unknown form field %q", key)
		}
	}
	return nil
}

type formDecoder struct {
	values    url.Values
	keys      []string
	used      map[string]struct{}
	keysReady bool
}

func newFormDecoder(values url.Values) formDecoder {
	return formDecoder{
		values: values,
		used:   make(map[string]struct{}, len(values)),
	}
}

func (d *formDecoder) sortedKeys() []string {
	if d.keysReady {
		return d.keys
	}
	keys := make([]string, 0, len(d.values))
	for key := range d.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	d.keys = keys
	d.keysReady = true
	return d.keys
}

func (d *formDecoder) hasKeyPrefix(prefix string) bool {
	keys := d.sortedKeys()
	start := sort.SearchStrings(keys, prefix)
	return start < len(keys) && strings.HasPrefix(keys[start], prefix)
}

func (d *formDecoder) keysWithPrefix(prefix string) []string {
	keys := d.sortedKeys()
	start := sort.SearchStrings(keys, prefix)
	end := start
	for end < len(keys) && strings.HasPrefix(keys[end], prefix) {
		end++
	}
	return keys[start:end]
}

func decodeFormMap(values url.Values, target reflect.Value) error {
	if err := validateTopLevelFormMapTarget(target.Type()); err != nil {
		return err
	}
	if target.IsNil() {
		target.Set(reflect.MakeMap(target.Type()))
	}

	switch target.Type().Elem().Kind() {
	case reflect.String:
		for key, raw := range values {
			mapKey := reflect.ValueOf(unescapeFormKeySegment(key)).Convert(target.Type().Key())
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
			mapKey := reflect.ValueOf(unescapeFormKeySegment(key)).Convert(target.Type().Key())
			target.SetMapIndex(mapKey, copied)
		}
		return nil
	default:
		return fmt.Errorf("form map target values must be string or []string")
	}
}

func validateTopLevelFormMapTarget(targetType reflect.Type) error {
	if targetType.Key().Kind() != reflect.String {
		return fmt.Errorf("form map target key type must be string")
	}
	valueType := targetType.Elem()
	if valueType.Kind() == reflect.String {
		return nil
	}
	if valueType.Kind() == reflect.Slice && valueType.Elem().Kind() == reflect.String {
		return nil
	}
	return fmt.Errorf("form map target values must be string or []string")
}

func (d *formDecoder) decodeStruct(target reflect.Value, prefix string) error {
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
				if err := d.decodeStruct(dereferenceFormField(field), prefix); err != nil {
					return err
				}
				continue
			}
			name = fieldType.Name
		}

		key := joinFormKey(prefix, name)
		if raw, ok := d.values[key]; ok {
			if err := setFormField(field, raw, key); err != nil {
				return err
			}
			d.used[key] = struct{}{}
			continue
		}

		fieldPrefix := key + "."
		if canDecodeNestedFormStruct(field) && d.hasKeyPrefix(fieldPrefix) {
			nested := dereferenceFormField(field)
			if err := d.decodeStruct(nested, key); err != nil {
				return err
			}
			continue
		}

		if canDecodeNestedFormMap(field) && d.hasKeyPrefix(fieldPrefix) {
			nested := dereferenceFormField(field)
			if err := d.decodeMapPrefix(nested, key); err != nil {
				return err
			}
			continue
		}

		if canDecodeIndexedFormField(field) && d.hasKeyPrefix(fieldPrefix) {
			indexed := dereferenceFormField(field)
			if err := d.decodeIndexedField(indexed, key); err != nil {
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
	name = escapeFormKeySegment(name)
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func splitFormKey(path string) (string, string) {
	escaped := false
	for i, r := range path {
		switch {
		case !escaped && r == '\\':
			escaped = true
		case !escaped && r == '.':
			return unescapeFormKeySegment(path[:i]), path[i+1:]
		default:
			escaped = false
		}
	}
	return unescapeFormKeySegment(path), ""
}

func escapeFormKeySegment(segment string) string {
	segment = strings.ReplaceAll(segment, `\`, `\\`)
	return strings.ReplaceAll(segment, `.`, `\.`)
}

func unescapeFormKeySegment(segment string) string {
	segment = strings.ReplaceAll(segment, `\.`, `.`)
	return strings.ReplaceAll(segment, `\\`, `\`)
}

func canDecodeNestedFormStruct(field reflect.Value) bool {
	fieldType := field.Type()
	for fieldType.Kind() == reflect.Ptr {
		if formTypeHasTextUnmarshaler(fieldType) {
			return false
		}
		fieldType = fieldType.Elem()
	}
	return fieldType.Kind() == reflect.Struct &&
		!formTypeHasTextUnmarshaler(fieldType) &&
		!fieldType.ConvertibleTo(formTimeType) &&
		!fieldType.ConvertibleTo(formURLType)
}

func canDecodeNestedFormMap(field reflect.Value) bool {
	fieldType := field.Type()
	for fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}
	return fieldType.Kind() == reflect.Map
}

func (d *formDecoder) decodeMapPrefix(target reflect.Value, prefix string) error {
	if target.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("form map field %q key type must be string", prefix)
	}
	if target.IsNil() {
		target.Set(reflect.MakeMap(target.Type()))
	}

	fieldPrefix := prefix + "."
	for _, key := range d.keysWithPrefix(fieldPrefix) {
		raw := d.values[key]
		mapKeyName := key[len(fieldPrefix):]
		if mapKeyName == "" {
			return fmt.Errorf("form map field %q has empty key", prefix)
		}

		mapValue := reflect.New(target.Type().Elem()).Elem()
		if err := setFormField(mapValue, raw, key); err != nil {
			return err
		}

		mapKey := reflect.ValueOf(unescapeFormKeySegment(mapKeyName)).Convert(target.Type().Key())
		target.SetMapIndex(mapKey, mapValue)
		d.used[key] = struct{}{}
	}
	return nil
}

func canDecodeIndexedFormField(field reflect.Value) bool {
	fieldType := field.Type()
	for fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}
	return fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array
}

func (d *formDecoder) decodeIndexedField(target reflect.Value, prefix string) error {
	if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
		return fmt.Errorf("form field %q must be a slice or array", prefix)
	}

	fieldPrefix := prefix + "."
	keys := d.keysWithPrefix(fieldPrefix)
	maxIndex := -1
	for _, key := range keys {
		suffix := key[len(fieldPrefix):]
		indexText, _ := splitFormKey(suffix)
		index, err := strconv.Atoi(indexText)
		if err != nil || index < 0 {
			return fmt.Errorf("form field %q has invalid index %q", prefix, indexText)
		}
		if index > maxIndex {
			maxIndex = index
		}
	}
	if err := prepareIndexedFormTarget(target, maxIndex, prefix); err != nil {
		return err
	}

	var nestedIndexes map[int]struct{}
	for _, key := range keys {
		raw := d.values[key]
		suffix := key[len(fieldPrefix):]

		indexText, rest := splitFormKey(suffix)
		index, _ := strconv.Atoi(indexText)
		elem := target.Index(index)
		if rest == "" {
			if err := setFormField(elem, raw, key); err != nil {
				return err
			}
			d.used[key] = struct{}{}
			continue
		}
		if nestedIndexes == nil {
			nestedIndexes = make(map[int]struct{})
		}
		nestedIndexes[index] = struct{}{}
	}

	for index := range nestedIndexes {
		elem := target.Index(index)
		childPrefix := prefix + "." + strconv.Itoa(index)
		switch {
		case canDecodeNestedFormStruct(elem):
			if err := d.decodeStruct(dereferenceFormField(elem), childPrefix); err != nil {
				return err
			}
		case canDecodeNestedFormMap(elem):
			if err := d.decodeMapPrefix(dereferenceFormField(elem), childPrefix); err != nil {
				return err
			}
		case canDecodeIndexedFormField(elem):
			if err := d.decodeIndexedField(dereferenceFormField(elem), childPrefix); err != nil {
				return err
			}
		default:
			return fmt.Errorf("form field %q does not support nested indexed values", childPrefix)
		}
	}
	return nil
}

func prepareIndexedFormTarget(target reflect.Value, index int, name string) error {
	if index < 0 {
		return nil
	}
	if target.Kind() == reflect.Array {
		if index >= target.Len() {
			return fmt.Errorf("form field %q index %d is above array size %d", name, index, target.Len())
		}
		return nil
	}

	if index >= maxFormIndexedElements {
		return fmt.Errorf("form field %q index %d exceeds max index %d", name, index, maxFormIndexedElements-1)
	}
	if index >= target.Len() {
		grown := reflect.MakeSlice(target.Type(), index+1, index+1)
		reflect.Copy(grown, target)
		target.Set(grown)
	}
	return nil
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
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02",
	"2006-01",
	"2006",
	"15:04:05.999999999",
	"15:04:05",
	"15:04",
}
