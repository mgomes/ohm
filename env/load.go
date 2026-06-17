package env

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const defaultEnvironment = "development"

// Loader reads Ohm environment files from a directory.
type Loader struct {
	Dir         string
	Environment string
	Files       []string
	LookupEnv   func(string) (string, bool)
	SetEnv      func(string, string) error
}

// DefaultFiles returns the default environment file load order.
func DefaultFiles(environment string) []string {
	environment = normalizeEnvironment(environment)
	return []string{
		".env",
		".env." + environment,
		".env.local",
		".env." + environment + ".local",
	}
}

// Load reads configured environment files and merges them in load order.
func (l Loader) Load() (map[string]string, error) {
	dir := l.Dir
	if dir == "" {
		dir = "."
	}

	files := l.Files
	if files == nil {
		files = DefaultFiles(l.Environment)
	}

	vars := make(map[string]string)
	for _, file := range files {
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, file)
		}

		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read env file %s: %w", path, err)
		}

		parsed, err := ParseNamed(path, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		for key, value := range parsed {
			vars[key] = value
		}
	}

	return vars, nil
}

// Apply sets loaded values without overwriting existing process environment values.
func (l Loader) Apply(vars map[string]string) error {
	lookupEnv := l.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	setEnv := l.SetEnv
	if setEnv == nil {
		setEnv = os.Setenv
	}

	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		if _, ok := lookupEnv(key); ok {
			continue
		}
		if err := setEnv(key, vars[key]); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
	}

	return nil
}

// LoadAndApply loads environment files and applies them to the process environment.
func (l Loader) LoadAndApply() (map[string]string, error) {
	vars, err := l.Load()
	if err != nil {
		return nil, err
	}
	if err := l.Apply(vars); err != nil {
		return nil, err
	}
	return vars, nil
}

func normalizeEnvironment(environment string) string {
	if environment == "" {
		return defaultEnvironment
	}
	return environment
}
