package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProgramRunDispatchesCommand(t *testing.T) {
	var stdout bytes.Buffer
	program := New("myapp", []Command{
		{
			Name:    "hello",
			Summary: "say hello",
			Run: func(_ context.Context, io IO, args []string) error {
				io.Stdout.Write([]byte(strings.Join(args, ",")))
				return nil
			},
		},
	}, WithIO(IO{Stdout: &stdout}))

	err := program.Run(context.Background(), []string{"hello", "ada", "grace"})
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", []string{"hello", "ada", "grace"}, err)
	}
	if stdout.String() != "ada,grace" {
		t.Errorf("Program.Run(%v) stdout = %q, want %q", []string{"hello", "ada", "grace"}, stdout.String(), "ada,grace")
	}
}

func TestProgramRunReportsUsageForUnknownCommand(t *testing.T) {
	var stderr bytes.Buffer
	program := New("myapp", []Command{{Name: "server", Summary: "start server"}}, WithIO(IO{Stderr: &stderr}))

	err := program.Run(context.Background(), []string{"missing"})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("Program.Run(%v) error = %v, want ErrUsage", []string{"missing"}, err)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("Program.Run(%v) stderr = %q, want unknown command", []string{"missing"}, stderr.String())
	}
	if !strings.Contains(stderr.String(), "server") {
		t.Errorf("Program.Run(%v) stderr = %q, want command list", []string{"missing"}, stderr.String())
	}
}

func TestProgramRunPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	program := New("myapp", []Command{{Name: "routes", Summary: "list routes"}}, WithIO(IO{Stdout: &stdout}))

	err := program.Run(context.Background(), []string{"help"})
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", []string{"help"}, err)
	}
	if !strings.Contains(stdout.String(), "Usage: myapp <command> [args]") {
		t.Errorf("Program.Run(%v) stdout = %q, want usage", []string{"help"}, stdout.String())
	}
	if !strings.Contains(stdout.String(), "routes") {
		t.Errorf("Program.Run(%v) stdout = %q, want routes command", []string{"help"}, stdout.String())
	}
}
