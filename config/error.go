package config

import (
	"fmt"
	"strings"
)

type problem struct {
	Field   string
	Key     string
	Message string
}

// Error reports one or more configuration problems.
type Error struct {
	problems []problem
}

func newError(item problem) *Error {
	return &Error{problems: []problem{item}}
}

func (e *Error) Error() string {
	if len(e.problems) == 0 {
		return "config has no problems"
	}
	if len(e.problems) == 1 {
		problem := e.problems[0]
		return fmt.Sprintf("config %s: %s", problem.Key, problem.Message)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "config has %d problems", len(e.problems))
	for _, problem := range e.problems {
		fmt.Fprintf(&builder, "; %s: %s", problem.Key, problem.Message)
	}
	return builder.String()
}

// Problems returns configuration problems as stable, human-readable strings.
func (e *Error) Problems() []string {
	if e == nil {
		return nil
	}

	problems := make([]string, 0, len(e.problems))
	for _, problem := range e.problems {
		problems = append(problems, fmt.Sprintf("%s: %s", problem.Key, problem.Message))
	}
	return problems
}
