package scrub

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSensitiveKeyMatchesCommonStyles(t *testing.T) {
	redactor := New()
	tests := []struct {
		key  string
		want bool
	}{
		{key: "password", want: true},
		{key: "password_confirmation", want: true},
		{key: "apiKey", want: true},
		{key: "X-CSRF-Token", want: true},
		{key: "Set-Cookie", want: true},
		{key: "display_name", want: false},
	}

	for _, tt := range tests {
		got := redactor.SensitiveKey(tt.key)
		if got != tt.want {
			t.Errorf("Redactor.SensitiveKey(%q) = %t, want %t", tt.key, got, tt.want)
		}
	}
}

func TestZeroValueRedactorUsesDefaultScrubbing(t *testing.T) {
	var redactor Redactor

	got := redactor.Any("password", "secret")
	if got != defaultReplacement {
		t.Errorf("Redactor.Any(%q, %q) = %v, want %v", "password", "secret", got, defaultReplacement)
	}
}

func TestAnyRedactsNestedMapsAndMarkedValues(t *testing.T) {
	redactor := New()
	input := map[string]any{
		"name": "Ada",
		"credentials": map[string]any{
			"password": "secret",
			"apiKey":   "key",
		},
		"headers": http.Header{
			"Accept":        []string{"text/html"},
			"Authorization": []string{"Bearer token"},
			"Cookie":        []string{"session=abc"},
		},
		"explicit": Mark("hide me"),
	}

	got := redactor.Any("", input).(map[string]any)

	if got["name"] != "Ada" {
		t.Errorf("Redactor.Any(%q, input)[name] = %v, want %v", "", got["name"], "Ada")
	}
	credentials := got["credentials"].(map[string]any)
	if credentials["password"] != defaultReplacement {
		t.Errorf("Redactor.Any(%q, input)[credentials][password] = %v, want %v", "", credentials["password"], defaultReplacement)
	}
	if credentials["apiKey"] != defaultReplacement {
		t.Errorf("Redactor.Any(%q, input)[credentials][apiKey] = %v, want %v", "", credentials["apiKey"], defaultReplacement)
	}
	headers := got["headers"].(map[string]any)
	if headers["Accept"] == defaultReplacement {
		t.Errorf("Redactor.Any(%q, input)[headers][Accept] = %v, want unsanitized value", "", headers["Accept"])
	}
	if headers["Authorization"] != defaultReplacement {
		t.Errorf("Redactor.Any(%q, input)[headers][Authorization] = %v, want %v", "", headers["Authorization"], defaultReplacement)
	}
	if headers["Cookie"] != defaultReplacement {
		t.Errorf("Redactor.Any(%q, input)[headers][Cookie] = %v, want %v", "", headers["Cookie"], defaultReplacement)
	}
	if got["explicit"] != defaultReplacement {
		t.Errorf("Redactor.Any(%q, input)[explicit] = %v, want %v", "", got["explicit"], defaultReplacement)
	}
}

func TestAnyRedactsNestedStructFields(t *testing.T) {
	type databaseConfig struct {
		DSN      string
		Password string
	}
	type appConfig struct {
		Name      string
		Database  *databaseConfig `json:"database"`
		CreatedAt time.Time       `json:"created_at"`
		ignored   string
	}

	redactor := New()
	input := appConfig{
		Name: "admin",
		Database: &databaseConfig{
			DSN:      "postgres://localhost/app",
			Password: "secret",
		},
		CreatedAt: time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
		ignored:   "private field",
	}

	got := redactor.Any("", input).(map[string]any)

	if got["Name"] != "admin" {
		t.Errorf("Redactor.Any(%q, input)[Name] = %v, want %v", "", got["Name"], "admin")
	}
	database := got["database"].(map[string]any)
	if database["DSN"] != "postgres://localhost/app" {
		t.Errorf("Redactor.Any(%q, input)[database][DSN] = %v, want %v", "", database["DSN"], "postgres://localhost/app")
	}
	if database["Password"] != defaultReplacement {
		t.Errorf("Redactor.Any(%q, input)[database][Password] = %v, want %v", "", database["Password"], defaultReplacement)
	}
	if got["created_at"] != input.CreatedAt {
		t.Errorf("Redactor.Any(%q, input)[created_at] = %v, want %v", "", got["created_at"], input.CreatedAt)
	}
	if _, ok := got["ignored"]; ok {
		t.Errorf("Redactor.Any(%q, input)[ignored] present = %t, want false", "", ok)
	}
}

func TestHandlerRedactsSlogOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info(
		"request",
		slog.String("password", "secret"),
		slog.Group("request",
			slog.String("method", "POST"),
			slog.String("authorization", "Bearer token"),
		),
		slog.Any("headers", map[string][]string{
			"Accept": {"application/json"},
			"Cookie": {"session=abc"},
		}),
	)

	output := buf.String()
	for _, leaked := range []string{"secret", "Bearer token", "session=abc"} {
		if strings.Contains(output, leaked) {
			t.Errorf("logged output %q contains sensitive value %q", output, leaked)
		}
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", output, err)
	}

	if got["password"] != defaultReplacement {
		t.Errorf("logged password = %v, want %v", got["password"], defaultReplacement)
	}
	request := got["request"].(map[string]any)
	if request["method"] != "POST" {
		t.Errorf("logged request.method = %v, want %v", request["method"], "POST")
	}
	if request["authorization"] != defaultReplacement {
		t.Errorf("logged request.authorization = %v, want %v", request["authorization"], defaultReplacement)
	}
	headers := got["headers"].(map[string]any)
	if headers["Cookie"] != defaultReplacement {
		t.Errorf("logged headers.Cookie = %v, want %v", headers["Cookie"], defaultReplacement)
	}
}

func TestHandlerRedactsStructFields(t *testing.T) {
	type credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Token    string
	}

	var buf bytes.Buffer
	logger := slog.New(NewHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("request", slog.Any("credentials", credentials{
		Username: "ada",
		Password: "secret",
		Token:    "token",
	}))

	output := buf.String()
	for _, leaked := range []string{"secret", "token"} {
		if strings.Contains(output, leaked) {
			t.Errorf("logged output %q contains sensitive value %q", output, leaked)
		}
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", output, err)
	}

	credentialsLog := got["credentials"].(map[string]any)
	if credentialsLog["username"] != "ada" {
		t.Errorf("logged credentials.username = %v, want %v", credentialsLog["username"], "ada")
	}
	if credentialsLog["password"] != defaultReplacement {
		t.Errorf("logged credentials.password = %v, want %v", credentialsLog["password"], defaultReplacement)
	}
	if credentialsLog["Token"] != defaultReplacement {
		t.Errorf("logged credentials.Token = %v, want %v", credentialsLog["Token"], defaultReplacement)
	}
}

func TestHandlerOmitsJSONSkippedStructFields(t *testing.T) {
	type credentials struct {
		Username string
		Password string `json:"-"`
	}

	var buf bytes.Buffer
	logger := slog.New(NewHandler(slog.NewTextHandler(&buf, nil)))

	logger.Info("request", slog.Any("credentials", credentials{
		Username: "ada",
		Password: "secret",
	}))

	output := buf.String()
	if strings.Contains(output, "secret") {
		t.Errorf("logged output %q contains sensitive value %q", output, "secret")
	}
	if !strings.Contains(output, "Username:ada") {
		t.Errorf("logged output %q contains Username:ada, want true", output)
	}
}

func TestHandlerRedactsWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(slog.NewJSONHandler(&buf, nil))).With("session_id", "abc123")

	logger.Info("request")

	output := buf.String()
	if strings.Contains(output, "abc123") {
		t.Errorf("logged output %q contains sensitive session value", output)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", output, err)
	}
	if got["session_id"] != defaultReplacement {
		t.Errorf("logged session_id = %v, want %v", got["session_id"], defaultReplacement)
	}
}

func TestHandlerRedactsSensitiveGroups(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewHandler(slog.NewTextHandler(&buf, nil))).WithGroup("password")

	logger.Info("request", slog.String("value", "secret"))

	output := buf.String()
	if strings.Contains(output, "secret") {
		t.Errorf("logged output %q contains sensitive value %q", output, "secret")
	}
	if !strings.Contains(output, "password.value=[REDACTED]") {
		t.Errorf("logged output %q contains password.value=[REDACTED], want true", output)
	}
}
