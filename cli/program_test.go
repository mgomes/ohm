package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestProgramRunPrintsCommandHelpWithFlagPackageHelpArg(t *testing.T) {
	var stdout bytes.Buffer
	program := New("myapp", []Command{{
		Name:    "server",
		Summary: "start server",
		Usage:   "server [-addr 127.0.0.1:3000]",
		Run: func(context.Context, IO, []string) error {
			t.Fatalf("command runner called for help argument")
			return nil
		},
	}}, WithIO(IO{Stdout: &stdout}))

	err := program.Run(context.Background(), []string{"server", "-help"})
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", []string{"server", "-help"}, err)
	}
	if !strings.Contains(stdout.String(), "Usage: myapp server [-addr 127.0.0.1:3000]") {
		t.Errorf("Program.Run(%v) stdout = %q, want command usage", []string{"server", "-help"}, stdout.String())
	}
}

func TestProgramRunPrintsCommandHelpAfterCommandFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	program := New("myapp", []Command{
		ServerCommand(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			WithAddr(":8080"),
			WithServerRunner(func(context.Context, *http.Server, time.Duration, []ShutdownHook) error {
				t.Fatalf("server runner called for help argument")
				return nil
			}),
		),
	}, WithIO(IO{Stdout: &stdout, Stderr: &stderr}))

	args := []string{"server", "-addr", ":4000", "-h"}
	err := program.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", args, err)
	}
	if !strings.Contains(stdout.String(), "Usage: myapp server [-addr :8080]") {
		t.Errorf("Program.Run(%v) stdout = %q, want command usage", args, stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("Program.Run(%v) stderr = %q, want empty", args, stderr.String())
	}
}
