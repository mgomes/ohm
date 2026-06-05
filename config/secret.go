package config

import (
	"encoding/json"
	"log/slog"
)

const secretReplacement = "[REDACTED]"

// Secret stores a sensitive configuration value and redacts itself in logs.
type Secret string

// Reveal returns the underlying sensitive value.
func (s Secret) Reveal() string {
	return string(s)
}

// String returns a redacted display value.
func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return secretReplacement
}

// LogValue returns a redacted slog value.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(s.String())
}

// MarshalJSON returns a redacted JSON string.
func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalText stores text as a secret value.
func (s *Secret) UnmarshalText(text []byte) error {
	*s = Secret(text)
	return nil
}
