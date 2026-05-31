package scrub

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
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
