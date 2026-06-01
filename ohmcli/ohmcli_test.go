package ohmcli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgomes/ohm/cli"
	"github.com/mgomes/ohm/scaffold"
)

func TestNewCommandCreatesApplicationWithFlagsAfterName(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	program := New(
		WithIO(cli.IO{
			Stdout: &stdout,
			Stderr: &stderr,
		}),
		WithOhmVersion("v0.0.0"),
	)

	args := []string{"new", destination, "--db", "sqlite", "--module", "example.com/journal"}
	err := program.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", args, err)
	}
	if stderr.String() != "" {
		t.Errorf("Program.Run(%v) stderr = %q, want empty", args, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Created "+destination) {
		t.Errorf("Program.Run(%v) stdout = %q, want creation message", args, stdout.String())
	}

	goMod, err := os.ReadFile(filepath.Join(destination, "go.mod"))
	if err != nil {
		t.Fatalf("os.ReadFile(generated go.mod) error = %v, want nil", err)
	}
	if !strings.Contains(string(goMod), "module example.com/journal") {
		t.Errorf("Program.Run(%v) generated go.mod = %q, want module path", args, goMod)
	}
}

func TestNewCommandCreatesPostgresApplicationByDefault(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "ledger")
	var stdout bytes.Buffer
	program := New(WithIO(cli.IO{Stdout: &stdout}), WithOhmVersion("v0.0.0"))

	args := []string{"new", destination}
	err := program.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", args, err)
	}

	dbFile, err := os.ReadFile(filepath.Join(destination, "internal", "db", "db.go"))
	if err != nil {
		t.Fatalf("os.ReadFile(generated db.go) error = %v, want nil", err)
	}
	if !strings.Contains(string(dbFile), "migrate.DialectPostgres") {
		t.Errorf("Program.Run(%v) generated db.go = %q, want Postgres dialect", args, dbFile)
	}
}

func TestNewCommandRejectsUnknownDatabase(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bad-db")
	program := New(WithOhmVersion("v0.0.0"))

	args := []string{"new", destination, "--db", "mysql"}
	err := program.Run(context.Background(), args)
	if !errors.Is(err, scaffold.ErrUnsupportedDatabase) {
		t.Fatalf("Program.Run(%v) error = %v, want ErrUnsupportedDatabase", args, err)
	}
}

func TestNewCommandPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	program := New(WithIO(cli.IO{Stdout: &stdout}), WithOhmVersion("v0.0.0"))

	args := []string{"new", "--help"}
	err := program.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", args, err)
	}
	if !strings.Contains(stdout.String(), "Usage: ohm new <name> [-db postgres|sqlite] [-module module/path] [-ohm-version version]") {
		t.Errorf("Program.Run(%v) stdout = %q, want new command usage", args, stdout.String())
	}
}

func TestNewCommandAcceptsExplicitOhmVersion(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	var stdout bytes.Buffer
	program := New(WithIO(cli.IO{Stdout: &stdout}))

	args := []string{"new", destination, "--ohm-version", "v0.1.2"}
	err := program.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", args, err)
	}

	goMod, err := os.ReadFile(filepath.Join(destination, "go.mod"))
	if err != nil {
		t.Fatalf("os.ReadFile(generated go.mod) error = %v, want nil", err)
	}
	if !strings.Contains(string(goMod), "github.com/mgomes/ohm v0.1.2") {
		t.Errorf("Program.Run(%v) generated go.mod = %q, want explicit Ohm version", args, goMod)
	}
}
