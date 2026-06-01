package env

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	input := `
# comment
DATABASE_URL=postgres://localhost/ohm
EMPTY=
SPACED = value with spaces
SINGLE='single quoted'
DOUBLE="double quoted"
MULTILINE="first\nsecond"
HASH=abc#123
COMMENTED=value # trailing comment
NO_EXPANSION=$DATABASE_URL
`

	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v, want nil", input, err)
	}

	want := map[string]string{
		"DATABASE_URL": "postgres://localhost/ohm",
		"EMPTY":        "",
		"SPACED":       "value with spaces",
		"SINGLE":       "single quoted",
		"DOUBLE":       "double quoted",
		"MULTILINE":    "first\nsecond",
		"HASH":         "abc#123",
		"COMMENTED":    "value",
		"NO_EXPANSION": "$DATABASE_URL",
	}
	if !maps.Equal(got, want) {
		t.Errorf("Parse(%q) = %v, want %v", input, got, want)
	}
}

func TestParseReportsLineNumber(t *testing.T) {
	input := "VALID=true\nnot valid\n"

	_, err := ParseNamed(".env.test", strings.NewReader(input))
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("ParseNamed(%q) error = %v, want *ParseError", input, err)
	}
	if parseErr.Name != ".env.test" {
		t.Errorf("ParseNamed(%q) error name = %q, want %q", input, parseErr.Name, ".env.test")
	}
	if parseErr.Line != 2 {
		t.Errorf("ParseNamed(%q) error line = %d, want 2", input, parseErr.Line)
	}
}

func TestParseRejectsDataAfterQuotedValue(t *testing.T) {
	input := "KEY=\"value\" trailing\n"

	_, err := Parse(strings.NewReader(input))
	if err == nil {
		t.Fatalf("Parse(%q) error = nil, want non-nil", input)
	}
}
