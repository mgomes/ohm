package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type appConfig struct {
	Name        string `env:"APP_NAME,required"`
	Port        int    `default:"3000"`
	Debug       bool
	Timeout     time.Duration `env:"REQUEST_TIMEOUT" default:"5s"`
	DatabaseURL Secret        `env:"DATABASE_URL,required"`
	Server      serverConfig  `envPrefix:"SERVER_"`
	Ignored     string        `env:"-"`
}

type serverConfig struct {
	Host string `default:"127.0.0.1"`
	Port int
}

func TestDecodePopulatesTypedConfig(t *testing.T) {
	values := map[string]string{
		"APP_NAME":        "ohm-test",
		"DEBUG":           "true",
		"REQUEST_TIMEOUT": "10s",
		"DATABASE_URL":    "postgres://localhost/ohm",
		"SERVER_PORT":     "8080",
	}

	got, err := Decode[appConfig](FromMap(values))
	if err != nil {
		t.Fatalf("Decode[appConfig](%v) error = %v, want nil", values, err)
	}

	want := appConfig{
		Name:        "ohm-test",
		Port:        3000,
		Debug:       true,
		Timeout:     10 * time.Second,
		DatabaseURL: Secret("postgres://localhost/ohm"),
		Server: serverConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
	}
	if got != want {
		t.Errorf("Decode[appConfig](%v) = %+v, want %+v", values, got, want)
	}
	if got.DatabaseURL.String() != secretReplacement {
		t.Errorf("Secret.String() = %q, want %q", got.DatabaseURL.String(), secretReplacement)
	}
	if got.DatabaseURL.Reveal() != "postgres://localhost/ohm" {
		t.Errorf("Secret.Reveal() = %q, want %q", got.DatabaseURL.Reveal(), "postgres://localhost/ohm")
	}
}

func TestDecodeReportsMissingAndInvalidValues(t *testing.T) {
	values := map[string]string{
		"DEBUG": "not-a-bool",
	}

	_, err := Decode[appConfig](FromMap(values))
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Decode[appConfig](%v) error = %v, want *Error", values, err)
	}

	got := problemSet(cfgErr.Problems())
	want := map[string]bool{
		"APP_NAME: required value is missing": true,
		"DEBUG: parse bool \"not-a-bool\": strconv.ParseBool: parsing \"not-a-bool\": invalid syntax": true,
		"DATABASE_URL: required value is missing":                                                     true,
	}
	if !maps.Equal(got, want) {
		t.Errorf("Decode[appConfig](%v) problems = %v, want %v", values, got, want)
	}
}

func TestLoadUsesProcessLookupBeforeEnvFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "APP_NAME=file\nDATABASE_URL=file-db\n")

	got, err := Load[appConfig](
		WithDir(dir),
		WithEnvironment("development"),
		WithLookup(FromMap(map[string]string{
			"APP_NAME":     "process",
			"DATABASE_URL": "process-db",
		})),
	)
	if err != nil {
		t.Fatalf("Load[appConfig]() error = %v, want nil", err)
	}

	if got.Name != "process" {
		t.Errorf("Load[appConfig]() Name = %q, want %q", got.Name, "process")
	}
	if got.DatabaseURL.Reveal() != "process-db" {
		t.Errorf("Load[appConfig]() DatabaseURL = %q, want %q", got.DatabaseURL.Reveal(), "process-db")
	}
}

func TestLoadUsesOHMEnvForDefaultFileOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "APP_NAME=base\nDATABASE_URL=base-db\n")
	writeFile(t, filepath.Join(dir, ".env.test"), "APP_NAME=test\n")

	got, err := Load[appConfig](
		WithDir(dir),
		WithLookup(FromMap(map[string]string{
			"OHM_ENV": "test",
		})),
	)
	if err != nil {
		t.Fatalf("Load[appConfig]() error = %v, want nil", err)
	}

	if got.Name != "test" {
		t.Errorf("Load[appConfig]() Name = %q, want %q", got.Name, "test")
	}
}

func TestSecretMarshalJSONRedactsNestedValues(t *testing.T) {
	input := struct {
		DatabaseURL Secret
	}{
		DatabaseURL: Secret("postgres://localhost/ohm"),
	}

	got, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal(%+v) error = %v, want nil", input, err)
	}

	want := `{"DatabaseURL":"[REDACTED]"}`
	if string(got) != want {
		t.Errorf("json.Marshal(%+v) = %s, want %s", input, got, want)
	}
}

func TestFieldKey(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "Port", want: "PORT"},
		{name: "DatabaseURL", want: "DATABASE_URL"},
		{name: "HTTPPort", want: "HTTP_PORT"},
		{name: "OAuthClientID", want: "OAUTH_CLIENT_ID"},
		{name: "Version2Name", want: "VERSION_2_NAME"},
	}

	for _, tt := range tests {
		got := fieldKey(tt.name)
		if got != tt.want {
			t.Errorf("fieldKey(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func problemSet(problems []string) map[string]bool {
	set := make(map[string]bool, len(problems))
	for _, problem := range problems {
		set[problem] = true
	}
	return set
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}

func ExampleSecret() {
	secret := Secret("postgres://localhost/ohm")

	fmt.Println(secret)
	fmt.Println(secret.Reveal())

	// Output:
	// [REDACTED]
	// postgres://localhost/ohm
}
