package config

import (
	"fmt"
	"strings"
)

// Problem describes one invalid or missing configuration value.
type Problem struct {
	// Field is the exported struct field name, when the problem is tied to a field.
	Field string
	// Key is the environment lookup key used for the field.
	Key string
	// Message explains the validation or parsing failure.
	Message string
}

// Error reports one or more configuration problems.
type Error struct {
	problems []Problem
}

func newError(item Problem) *Error {
	return &Error{problems: []Problem{item}}
}

// Error returns a human-readable summary of the configuration problems.
func (e *Error) Error() string {
	if len(e.problems) == 0 {
		return "config has no problems"
	}
	if len(e.problems) == 1 {
		problem := e.problems[0]
		if problem.Key == "" {
			return fmt.Sprintf("config: %s", problem.Message)
		}
		return fmt.Sprintf("config %s: %s", problem.Key, problem.Message)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "config has %d problems", len(e.problems))
	for _, problem := range e.problems {
		fmt.Fprintf(&builder, "; %s", problem)
	}
	return builder.String()
}

// Problems returns a copy of the configuration problems.
func (e *Error) Problems() []Problem {
	if e == nil {
		return nil
	}

	problems := make([]Problem, len(e.problems))
	copy(problems, e.problems)
	return problems
}

// String returns a stable, human-readable problem description.
func (p Problem) String() string {
	if p.Key != "" {
		return fmt.Sprintf("%s: %s", p.Key, p.Message)
	}
	if p.Field != "" {
		return fmt.Sprintf("%s: %s", p.Field, p.Message)
	}
	return p.Message
}
