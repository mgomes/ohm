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

func FuzzParse(f *testing.F) {
	for _, input := range []string{
		"",
		"# comment\n",
		"DATABASE_URL=postgres://localhost/ohm\n",
		"EMPTY=\nSPACED = value with spaces\n",
		"DOUBLE=\"first\\nsecond\"\n",
		"SINGLE='single quoted'\n",
		"COMMENTED=value # trailing comment\n",
		"KEY=\"unterminated\n",
		"not valid\n",
	} {
		f.Add(input)
	}

	f.Fuzz(func(t *testing.T, input string) {
		first, firstErr := Parse(strings.NewReader(input))
		second, secondErr := Parse(strings.NewReader(input))
		if got, want := firstErr == nil, secondErr == nil; got != want {
			t.Fatalf("Parse(%q) first error = %v, second error = %v, want same error presence", input, firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !maps.Equal(first, second) {
			t.Errorf("Parse(%q) first result = %v, second result = %v, want deterministic result", input, first, second)
		}
		for key := range first {
			if !validKey(key) {
				t.Errorf("Parse(%q) returned invalid key %q", input, key)
			}
		}
	})
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
