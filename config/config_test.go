package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strconv"
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
	want := map[Problem]bool{
		{Field: "Name", Key: "APP_NAME", Message: "required value is missing"}: true,
		{
			Field:   "Debug",
			Key:     "DEBUG",
			Message: "parse bool \"not-a-bool\": strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		}: true,
		{Field: "DatabaseURL", Key: "DATABASE_URL", Message: "required value is missing"}: true,
	}
	if !maps.Equal(got, want) {
		t.Errorf("Decode[appConfig](%v) problems = %v, want %v", values, got, want)
	}
}

func TestDecodeReturnsZeroValueOnError(t *testing.T) {
	values := map[string]string{
		"APP_NAME":     "ohm-test",
		"DEBUG":        "not-a-bool",
		"DATABASE_URL": "postgres://localhost/ohm",
	}

	got, err := Decode[appConfig](FromMap(values))
	if err == nil {
		t.Fatalf("Decode[appConfig](%v) error = nil, want non-nil", values)
	}
	if got != (appConfig{}) {
		t.Errorf("Decode[appConfig](%v) = %+v, want zero value on error", values, got)
	}
}

func TestDecodeUsesExplicitEmptyDefault(t *testing.T) {
	type cfg struct {
		Name string `env:"APP_NAME,required" default:""`
	}

	got, err := Decode[cfg](FromMap(map[string]string{}))
	if err != nil {
		t.Fatalf("Decode[cfg](empty lookup) error = %v, want nil", err)
	}
	if got.Name != "" {
		t.Errorf("Decode[cfg](empty lookup) Name = %q, want empty", got.Name)
	}
}

type fuzzScalarConfig struct {
	Name        string `env:"APP_NAME,required"`
	Debug       bool
	Port        int16
	Count       uint8
	Ratio       float32
	Timeout     time.Duration `env:"REQUEST_TIMEOUT"`
	DatabaseURL Secret        `env:"DATABASE_URL,required"`
}

func FuzzDecodeScalars(f *testing.F) {
	seeds := []struct {
		name     string
		debug    string
		port     string
		count    string
		ratio    string
		timeout  string
		database string
	}{
		{name: "ohm", debug: "true", port: "3000", count: "2", ratio: "1.5", timeout: "5s", database: "postgres://localhost/ohm"},
		{name: "", debug: "false", port: "-1", count: "0", ratio: "0", timeout: "0s", database: ""},
		{name: "bad", debug: "not-bool", port: "abc", count: "-1", ratio: "nan", timeout: "soon", database: "db"},
	}
	for _, seed := range seeds {
		f.Add(seed.name, seed.debug, seed.port, seed.count, seed.ratio, seed.timeout, seed.database)
	}

	f.Fuzz(func(t *testing.T, name string, debugRaw string, portRaw string, countRaw string, ratioRaw string, timeoutRaw string, databaseRaw string) {
		values := map[string]string{
			"APP_NAME":        name,
			"DEBUG":           debugRaw,
			"PORT":            portRaw,
			"COUNT":           countRaw,
			"RATIO":           ratioRaw,
			"REQUEST_TIMEOUT": timeoutRaw,
			"DATABASE_URL":    databaseRaw,
		}

		got, err := Decode[fuzzScalarConfig](FromMap(values))
		if err != nil {
			return
		}

		if got.Name != name {
			t.Errorf("Decode[fuzzScalarConfig](%v) Name = %q, want %q", values, got.Name, name)
		}
		debug, err := strconv.ParseBool(debugRaw)
		if err != nil {
			t.Fatalf("Decode[fuzzScalarConfig](%v) error = nil, but strconv.ParseBool(%q) error = %v", values, debugRaw, err)
		}
		if got.Debug != debug {
			t.Errorf("Decode[fuzzScalarConfig](%v) Debug = %t, want %t", values, got.Debug, debug)
		}
		port, err := strconv.ParseInt(portRaw, 10, 16)
		if err != nil {
			t.Fatalf("Decode[fuzzScalarConfig](%v) error = nil, but strconv.ParseInt(%q, 10, 16) error = %v", values, portRaw, err)
		}
		if got.Port != int16(port) {
			t.Errorf("Decode[fuzzScalarConfig](%v) Port = %d, want %d", values, got.Port, int16(port))
		}
		count, err := strconv.ParseUint(countRaw, 10, 8)
		if err != nil {
			t.Fatalf("Decode[fuzzScalarConfig](%v) error = nil, but strconv.ParseUint(%q, 10, 8) error = %v", values, countRaw, err)
		}
		if got.Count != uint8(count) {
			t.Errorf("Decode[fuzzScalarConfig](%v) Count = %d, want %d", values, got.Count, uint8(count))
		}
		ratio, err := strconv.ParseFloat(ratioRaw, 32)
		if err != nil {
			t.Fatalf("Decode[fuzzScalarConfig](%v) error = nil, but strconv.ParseFloat(%q, 32) error = %v", values, ratioRaw, err)
		}
		if gotRatio := float64(got.Ratio); gotRatio != float64(float32(ratio)) && !(math.IsNaN(gotRatio) && math.IsNaN(ratio)) {
			t.Errorf("Decode[fuzzScalarConfig](%v) Ratio = %v, want %v", values, got.Ratio, float32(ratio))
		}
		timeout, err := time.ParseDuration(timeoutRaw)
		if err != nil {
			t.Fatalf("Decode[fuzzScalarConfig](%v) error = nil, but time.ParseDuration(%q) error = %v", values, timeoutRaw, err)
		}
		if got.Timeout != timeout {
			t.Errorf("Decode[fuzzScalarConfig](%v) Timeout = %s, want %s", values, got.Timeout, timeout)
		}
		if got.DatabaseURL.Reveal() != databaseRaw {
			t.Errorf("Decode[fuzzScalarConfig](%v) DatabaseURL = %q, want %q", values, got.DatabaseURL.Reveal(), databaseRaw)
		}
	})
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

func problemSet(problems []Problem) map[Problem]bool {
	set := make(map[Problem]bool, len(problems))
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
