package config

import (
	"encoding"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mgomes/ohm/env"
)

var (
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	durationType        = reflect.TypeFor[time.Duration]()
)

// LookupFunc looks up a configuration value by key.
type LookupFunc func(string) (string, bool)

// Option configures Load.
type Option func(*options)

type options struct {
	dir          string
	environment  string
	files        []string
	filesSet     bool
	lookup       LookupFunc
	loadEnvFiles bool
}

// WithDir configures the directory used for environment files.
func WithDir(dir string) Option {
	return func(opts *options) {
		opts.dir = dir
	}
}

// WithEnvironment configures the environment-specific file suffix.
func WithEnvironment(environment string) Option {
	return func(opts *options) {
		opts.environment = environment
	}
}

// WithFiles configures the exact environment files Load should read.
func WithFiles(files ...string) Option {
	return func(opts *options) {
		opts.files = append([]string(nil), files...)
		opts.filesSet = true
	}
}

// WithoutEnvFiles disables environment file loading.
func WithoutEnvFiles() Option {
	return func(opts *options) {
		opts.loadEnvFiles = false
	}
}

// WithLookup configures the highest-priority lookup source.
func WithLookup(lookup LookupFunc) Option {
	return func(opts *options) {
		opts.lookup = lookup
	}
}

// FromMap returns a lookup function backed by values.
func FromMap(values map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

// Decode decodes lookup values into a typed configuration struct.
func Decode[T any](lookup LookupFunc) (T, error) {
	var cfg T
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if err := decodeStruct(reflect.ValueOf(&cfg).Elem(), lookup, ""); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Load loads environment files and decodes the merged values into a typed struct.
func Load[T any](opts ...Option) (T, error) {
	cfg := options{
		lookup:       os.LookupEnv,
		loadEnvFiles: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	lookup := cfg.lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	fileValues := map[string]string{}
	if cfg.loadEnvFiles {
		environment := cfg.environment
		if environment == "" {
			environment, _ = lookup("OHM_ENV")
		}

		loader := env.Loader{
			Dir:         cfg.dir,
			Environment: environment,
		}
		if cfg.filesSet {
			loader.Files = cfg.files
		}

		var err error
		fileValues, err = loader.Load()
		if err != nil {
			var zero T
			return zero, fmt.Errorf("load environment files: %w", err)
		}
	}

	return Decode[T](layeredLookup(lookup, FromMap(fileValues)))
}

func layeredLookup(lookups ...LookupFunc) LookupFunc {
	return func(key string) (string, bool) {
		for _, lookup := range lookups {
			if lookup == nil {
				continue
			}
			value, ok := lookup(key)
			if ok {
				return value, true
			}
		}
		return "", false
	}
}

func decodeStruct(value reflect.Value, lookup LookupFunc, prefix string) error {
	if value.Kind() != reflect.Struct {
		return newError(problem{Message: "target must be a struct"})
	}

	var problems []problem
	valueType := value.Type()
	for i := range value.NumField() {
		field := valueType.Field(i)
		fieldValue := value.Field(i)
		if field.PkgPath != "" {
			continue
		}

		tag, ok := parseTag(field.Tag.Get("env"))
		if !ok {
			continue
		}

		if shouldRecurse(fieldValue, tag) {
			childPrefix := prefix + field.Tag.Get("envPrefix")
			if err := decodeStruct(fieldValue, lookup, childPrefix); err != nil {
				var cfgErr *Error
				if errors.As(err, &cfgErr) {
					problems = append(problems, cfgErr.problems...)
					continue
				}
				return err
			}
			continue
		}

		key := tag.key
		if key == "" {
			key = prefix + fieldKey(field.Name)
		}

		raw, found := lookup(key)
		if !found {
			raw = field.Tag.Get("default")
			found = raw != ""
		}

		if !found {
			if tag.required {
				problems = append(problems, problem{
					Field:   field.Name,
					Key:     key,
					Message: "required value is missing",
				})
			}
			continue
		}

		if err := assignValue(fieldValue, raw); err != nil {
			problems = append(problems, problem{
				Field:   field.Name,
				Key:     key,
				Message: err.Error(),
			})
		}
	}

	if len(problems) > 0 {
		return &Error{problems: problems}
	}
	return nil
}

func shouldRecurse(value reflect.Value, tag fieldTag) bool {
	return tag.key == "" &&
		value.Kind() == reflect.Struct &&
		value.Type() != durationType &&
		!value.Type().Implements(textUnmarshalerType) &&
		!reflect.PointerTo(value.Type()).Implements(textUnmarshalerType)
}

func assignValue(value reflect.Value, raw string) error {
	if !value.CanSet() {
		return fmt.Errorf("field cannot be set")
	}

	if value.CanAddr() {
		addr := value.Addr()
		if addr.Type().Implements(textUnmarshalerType) {
			unmarshaler := addr.Interface().(encoding.TextUnmarshaler)
			if err := unmarshaler.UnmarshalText([]byte(raw)); err != nil {
				return fmt.Errorf("parse value: %w", err)
			}
			return nil
		}
	}

	if value.Type() == durationType {
		duration, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("parse duration %q: %w", raw, err)
		}
		value.SetInt(int64(duration))
		return nil
	}

	switch value.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("parse bool %q: %w", raw, err)
		}
		value.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(raw, 10, value.Type().Bits())
		if err != nil {
			return fmt.Errorf("parse int %q: %w", raw, err)
		}
		value.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(raw, 10, value.Type().Bits())
		if err != nil {
			return fmt.Errorf("parse uint %q: %w", raw, err)
		}
		value.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(raw, value.Type().Bits())
		if err != nil {
			return fmt.Errorf("parse float %q: %w", raw, err)
		}
		value.SetFloat(parsed)
	default:
		return fmt.Errorf("unsupported field type %s", value.Type())
	}
	return nil
}

type fieldTag struct {
	key      string
	required bool
}

func parseTag(raw string) (fieldTag, bool) {
	if raw == "-" {
		return fieldTag{}, false
	}

	tag := fieldTag{}
	if raw == "" {
		return tag, true
	}

	parts := strings.Split(raw, ",")
	tag.key = strings.TrimSpace(parts[0])
	for _, part := range parts[1:] {
		if strings.TrimSpace(part) == "required" {
			tag.required = true
		}
	}
	return tag, true
}

func fieldKey(name string) string {
	var builder strings.Builder
	runes := []rune(name)
	for i, char := range runes {
		if i > 0 && keyBoundary(runes, i) {
			builder.WriteByte('_')
		}
		builder.WriteRune(unicode.ToUpper(char))
	}
	return builder.String()
}

func keyBoundary(runes []rune, i int) bool {
	current := runes[i]
	previous := runes[i-1]
	if unicode.IsUpper(current) {
		if unicode.IsLower(previous) || unicode.IsDigit(previous) {
			return true
		}
		return i > 1 && i+1 < len(runes) && unicode.IsLower(runes[i+1])
	}
	return unicode.IsDigit(current) && unicode.IsLetter(previous)
}
