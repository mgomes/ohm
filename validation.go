package ohm

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrValidation identifies a validation failure.
var ErrValidation = errors.New("validation failed")

// Validation error codes used by Ohm's built-in validators.
const (
	ValidationCodeRequired = "required"
	ValidationCodeTooShort = "too_short"
	ValidationCodeTooLong  = "too_long"
	ValidationCodeTooSmall = "too_small"
	ValidationCodeTooLarge = "too_large"
	ValidationCodeAccepted = "accepted"
	ValidationCodeRejected = "rejected"
	ValidationCodeInvalid  = "invalid"
)

// Validatable is implemented by values that can validate themselves.
type Validatable interface {
	Validate(*Validation)
}

// Validation collects structured validation errors.
type Validation struct {
	prefix string
	state  *validationState
}

type validationState struct {
	errors []ValidationError
}

// ValidationError describes one field or form-level validation failure.
type ValidationError struct {
	Field   string            `json:"field"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Params  map[string]string `json:"params,omitempty"`
}

// ErrorParam is a named value attached to a validation error.
type ErrorParam struct {
	Name  string
	Value string
}

// Errors is a collection of validation failures.
type Errors []ValidationError

// NewValidation creates an empty validation collector.
func NewValidation() *Validation {
	return &Validation{state: &validationState{}}
}

// Validate runs v's validation rules and returns the collected errors.
func Validate(v Validatable) Errors {
	validation := NewValidation()
	if v == nil || isNil(reflect.ValueOf(v)) {
		return validation.Errors()
	}
	v.Validate(validation)
	return validation.Errors()
}

// Add records a custom validation error.
func (v *Validation) Add(field string, code string, message string, params ...ErrorParam) {
	if v == nil || v.state == nil {
		return
	}
	if code == "" {
		code = ValidationCodeInvalid
	}
	if message == "" {
		message = code
	}

	v.state.errors = append(v.state.errors, ValidationError{
		Field:   v.fieldPath(field),
		Code:    code,
		Message: message,
		Params:  validationParams(params),
	})
}

// Errors returns the collected validation errors.
func (v *Validation) Errors() Errors {
	if v == nil || v.state == nil || len(v.state.errors) == 0 {
		return Errors{}
	}
	return cloneValidationErrors(v.state.errors)
}

// Nested runs validation for v under field's prefix.
func (v *Validation) Nested(field string, value Validatable) {
	if v == nil || v.state == nil || value == nil || isNil(reflect.ValueOf(value)) {
		return
	}
	value.Validate(&Validation{
		prefix: v.fieldPath(field),
		state:  v.state,
	})
}

// String validates a string field.
func (v *Validation) String(field string, value string) StringValidation {
	return StringValidation{validation: v, field: field, value: value}
}

// Int validates an int field.
func (v *Validation) Int(field string, value int) IntValidation {
	return IntValidation{validation: v, field: field, value: value}
}

// Bool validates a bool field.
func (v *Validation) Bool(field string, value bool) BoolValidation {
	return BoolValidation{validation: v, field: field, value: value}
}

// Time validates a time field.
func (v *Validation) Time(field string, value time.Time) TimeValidation {
	return TimeValidation{validation: v, field: field, value: value}
}

// Slice validates a slice or array field.
func (v *Validation) Slice(field string, value any) SliceValidation {
	validation := SliceValidation{validation: v, field: field}
	if value == nil {
		return validation
	}

	reflected := reflect.ValueOf(value)
	for reflected.Kind() == reflect.Ptr {
		if reflected.IsNil() {
			return validation
		}
		reflected = reflected.Elem()
	}

	switch reflected.Kind() {
	case reflect.Slice, reflect.Array:
		validation.valid = true
		validation.length = reflected.Len()
	default:
		validation.invalid()
	}
	return validation
}

func (v *Validation) fieldPath(field string) string {
	if v == nil || v.prefix == "" {
		return field
	}
	if field == "" {
		return v.prefix
	}
	return v.prefix + "." + field
}

func validationParams(params []ErrorParam) map[string]string {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]string, len(params))
	for _, param := range params {
		if param.Name == "" {
			continue
		}
		out[param.Name] = param.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Any reports whether e contains at least one validation error.
func (e Errors) Any() bool {
	return len(e) > 0
}

// Len returns the number of validation errors.
func (e Errors) Len() int {
	return len(e)
}

// Error returns a stable validation failure message.
func (e Errors) Error() string {
	return ErrValidation.Error()
}

// Err returns nil when e is empty and e otherwise.
func (e Errors) Err() error {
	if !e.Any() {
		return nil
	}
	return e
}

// Is reports whether e matches ErrValidation.
func (e Errors) Is(target error) bool {
	return target == ErrValidation
}

// All returns a copy of all validation errors.
func (e Errors) All() []ValidationError {
	if len(e) == 0 {
		return []ValidationError{}
	}
	return cloneValidationErrors(e)
}

// For returns validation errors for field.
func (e Errors) For(field string) Errors {
	matches := make([]ValidationError, 0)
	for _, err := range e {
		if err.Field == field {
			matches = append(matches, cloneValidationError(err))
		}
	}
	return matches
}

// First returns the first validation error for field.
func (e Errors) First(field string) (ValidationError, bool) {
	for _, err := range e {
		if err.Field == field {
			return cloneValidationError(err), true
		}
	}
	return ValidationError{}, false
}

// Messages returns validation messages for field.
func (e Errors) Messages(field string) []string {
	messages := make([]string, 0)
	for _, err := range e {
		if err.Field == field {
			messages = append(messages, err.Message)
		}
	}
	return messages
}

// Param returns the named parameter for err.
func (e ValidationError) Param(name string) (string, bool) {
	value, ok := e.Params[name]
	return value, ok
}

func cloneValidationErrors(errs []ValidationError) []ValidationError {
	out := make([]ValidationError, len(errs))
	for i, err := range errs {
		out[i] = cloneValidationError(err)
	}
	return out
}

func cloneValidationError(err ValidationError) ValidationError {
	err.Params = cloneStringMap(err.Params)
	return err
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

// StringValidation validates a string field.
type StringValidation struct {
	validation *Validation
	field      string
	value      string
}

// Presence requires a non-blank string.
func (v StringValidation) Presence() StringValidation {
	if strings.TrimSpace(v.value) == "" {
		v.add(ValidationCodeRequired, "is required")
	}
	return v
}

// Length requires a string length between min and max.
func (v StringValidation) Length(min int, max int) StringValidation {
	return v.Min(min).Max(max)
}

// Min requires at least n characters when the string is not blank.
func (v StringValidation) Min(n int) StringValidation {
	if n <= 0 || strings.TrimSpace(v.value) == "" {
		return v
	}
	if utf8.RuneCountInString(v.value) < n {
		v.add(
			ValidationCodeTooShort,
			fmt.Sprintf("must be at least %d characters", n),
			ErrorParam{Name: "min", Value: strconv.Itoa(n)},
		)
	}
	return v
}

// Max requires at most n characters.
func (v StringValidation) Max(n int) StringValidation {
	if n < 0 {
		return v
	}
	if utf8.RuneCountInString(v.value) > n {
		v.add(
			ValidationCodeTooLong,
			fmt.Sprintf("must be at most %d characters", n),
			ErrorParam{Name: "max", Value: strconv.Itoa(n)},
		)
	}
	return v
}

func (v StringValidation) add(code string, message string, params ...ErrorParam) {
	v.validation.Add(v.field, code, message, params...)
}

// IntValidation validates an int field.
type IntValidation struct {
	validation *Validation
	field      string
	value      int
}

// Min requires a value greater than or equal to n.
func (v IntValidation) Min(n int) IntValidation {
	if v.value < n {
		v.add(
			ValidationCodeTooSmall,
			fmt.Sprintf("must be at least %d", n),
			ErrorParam{Name: "min", Value: strconv.Itoa(n)},
		)
	}
	return v
}

// Max requires a value less than or equal to n.
func (v IntValidation) Max(n int) IntValidation {
	if v.value > n {
		v.add(
			ValidationCodeTooLarge,
			fmt.Sprintf("must be at most %d", n),
			ErrorParam{Name: "max", Value: strconv.Itoa(n)},
		)
	}
	return v
}

// Between requires a value between min and max.
func (v IntValidation) Between(min int, max int) IntValidation {
	return v.Min(min).Max(max)
}

func (v IntValidation) add(code string, message string, params ...ErrorParam) {
	v.validation.Add(v.field, code, message, params...)
}

// BoolValidation validates a bool field.
type BoolValidation struct {
	validation *Validation
	field      string
	value      bool
}

// Accepted requires the value to be true.
func (v BoolValidation) Accepted() BoolValidation {
	if !v.value {
		v.add(ValidationCodeAccepted, "must be accepted")
	}
	return v
}

// Rejected requires the value to be false.
func (v BoolValidation) Rejected() BoolValidation {
	if v.value {
		v.add(ValidationCodeRejected, "must not be accepted")
	}
	return v
}

func (v BoolValidation) add(code string, message string, params ...ErrorParam) {
	v.validation.Add(v.field, code, message, params...)
}

// TimeValidation validates a time field.
type TimeValidation struct {
	validation *Validation
	field      string
	value      time.Time
}

// Presence requires a non-zero time.
func (v TimeValidation) Presence() TimeValidation {
	if v.value.IsZero() {
		v.add(ValidationCodeRequired, "is required")
	}
	return v
}

// After requires a time after boundary.
func (v TimeValidation) After(boundary time.Time) TimeValidation {
	if !v.value.IsZero() && !v.value.After(boundary) {
		formatted := formatValidationTime(boundary)
		v.add(ValidationCodeTooSmall, "must be after "+formatted, ErrorParam{
			Name:  "after",
			Value: formatted,
		})
	}
	return v
}

// Before requires a time before boundary.
func (v TimeValidation) Before(boundary time.Time) TimeValidation {
	if !v.value.IsZero() && !v.value.Before(boundary) {
		formatted := formatValidationTime(boundary)
		v.add(ValidationCodeTooLarge, "must be before "+formatted, ErrorParam{
			Name:  "before",
			Value: formatted,
		})
	}
	return v
}

func (v TimeValidation) add(code string, message string, params ...ErrorParam) {
	v.validation.Add(v.field, code, message, params...)
}

func formatValidationTime(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}

// SliceValidation validates a slice or array field.
type SliceValidation struct {
	validation *Validation
	field      string
	length     int
	valid      bool
}

// Presence requires at least one item.
func (v SliceValidation) Presence() SliceValidation {
	return v.Min(1)
}

// Length requires a length between min and max.
func (v SliceValidation) Length(min int, max int) SliceValidation {
	return v.Min(min).Max(max)
}

// Min requires at least n items.
func (v SliceValidation) Min(n int) SliceValidation {
	if !v.valid || n <= 0 {
		return v
	}
	if v.length < n {
		v.add(
			ValidationCodeTooShort,
			fmt.Sprintf("must have at least %d items", n),
			ErrorParam{Name: "min", Value: strconv.Itoa(n)},
		)
	}
	return v
}

// Max requires at most n items.
func (v SliceValidation) Max(n int) SliceValidation {
	if !v.valid || n < 0 {
		return v
	}
	if v.length > n {
		v.add(
			ValidationCodeTooLong,
			fmt.Sprintf("must have at most %d items", n),
			ErrorParam{Name: "max", Value: strconv.Itoa(n)},
		)
	}
	return v
}

func (v SliceValidation) invalid() {
	v.add(ValidationCodeInvalid, "must be a slice or array")
}

func (v SliceValidation) add(code string, message string, params ...ErrorParam) {
	v.validation.Add(v.field, code, message, params...)
}
