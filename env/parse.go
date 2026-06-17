package env

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const scannerMaxTokenSize = 1024 * 1024

// ParseError describes an invalid line in an environment file.
type ParseError struct {
	Name    string
	Line    int
	Message string
}

// Error returns the formatted parse location and message.
func (e *ParseError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("%s:%d: %s", e.Name, e.Line, e.Message)
	}
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

// Parse reads environment variables from r.
func Parse(r io.Reader) (map[string]string, error) {
	return ParseNamed("", r)
}

// ParseNamed reads environment variables from r and uses name in parse errors.
func ParseNamed(name string, r io.Reader) (map[string]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), scannerMaxTokenSize)

	vars := make(map[string]string)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, ok, err := parseLine(scanner.Text())
		if err != nil {
			return nil, &ParseError{Name: name, Line: lineNo, Message: err.Error()}
		}
		if !ok {
			continue
		}
		vars[key] = value
	}
	if err := scanner.Err(); err != nil {
		if name != "" {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		return nil, fmt.Errorf("read env data: %w", err)
	}

	return vars, nil
}

func parseLine(line string) (string, string, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}

	eq := strings.IndexRune(line, '=')
	if eq < 0 {
		return "", "", false, fmt.Errorf("missing =")
	}

	key := strings.TrimSpace(line[:eq])
	if !validKey(key) {
		return "", "", false, fmt.Errorf("invalid key %q", key)
	}

	rawValue := strings.TrimLeftFunc(line[eq+1:], unicode.IsSpace)
	if rawValue == "" {
		return key, "", true, nil
	}

	if rawValue[0] == '\'' || rawValue[0] == '"' {
		var value string
		var rest string
		var err error
		switch rawValue[0] {
		case '\'':
			value, rest, err = parseSingleQuotedValue(rawValue)
		case '"':
			value, rest, err = parseDoubleQuotedValue(rawValue)
		}
		if err != nil {
			return "", "", false, err
		}
		rest = strings.TrimSpace(rest)
		if rest != "" && !strings.HasPrefix(rest, "#") {
			return "", "", false, fmt.Errorf("unexpected data after quoted value")
		}
		return key, value, true, nil
	}

	return key, trimUnquotedValue(rawValue), true, nil
}

func parseSingleQuotedValue(rawValue string) (string, string, error) {
	for i := range len(rawValue) {
		if i == 0 {
			continue
		}
		if rawValue[i] == '\'' {
			return rawValue[1:i], rawValue[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("unterminated quoted value")
}

func parseDoubleQuotedValue(rawValue string) (string, string, error) {
	var builder strings.Builder
	escaped := false

	for i := range len(rawValue) {
		if i == 0 {
			continue
		}
		char := rawValue[i]
		if escaped {
			switch char {
			case 'n':
				builder.WriteByte('\n')
			case 'r':
				builder.WriteByte('\r')
			case 't':
				builder.WriteByte('\t')
			case '\\', '\'', '"':
				builder.WriteByte(char)
			default:
				builder.WriteByte(char)
			}
			escaped = false
			continue
		}

		switch char {
		case '\\':
			escaped = true
		case '"':
			return builder.String(), rawValue[i+1:], nil
		default:
			builder.WriteByte(char)
		}
	}

	if escaped {
		return "", "", fmt.Errorf("unfinished escape sequence")
	}
	return "", "", fmt.Errorf("unterminated quoted value")
}

func trimUnquotedValue(value string) string {
	previousWasSpace := false
	for i, char := range value {
		if char == '#' && (i == 0 || previousWasSpace) {
			return strings.TrimSpace(value[:i])
		}
		previousWasSpace = unicode.IsSpace(char)
	}
	return strings.TrimSpace(value)
}

func validKey(key string) bool {
	if key == "" {
		return false
	}

	for i, char := range key {
		if i == 0 {
			if !isKeyStart(char) {
				return false
			}
			continue
		}
		if !isKeyChar(char) {
			return false
		}
	}

	return true
}

func isKeyStart(char rune) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func isKeyChar(char rune) bool {
	return isKeyStart(char) || char >= '0' && char <= '9'
}
