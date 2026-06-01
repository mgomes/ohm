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

func TestGenerateMigrationCommandCreatesMigrationWithFlagsAfterName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", dir, err)
	}
	t.Chdir(dir)

	var stdout bytes.Buffer
	program := New(WithIO(cli.IO{Stdout: &stdout}))

	args := []string{"generate", "migration", "create_posts", "--dir", "db/migrations"}
	err := program.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", args, err)
	}

	files, err := filepath.Glob(filepath.Join("db", "migrations", "*_create_posts.sql"))
	if err != nil {
		t.Fatalf("filepath.Glob(generated migration) error = %v, want nil", err)
	}
	if len(files) != 1 {
		t.Fatalf("Program.Run(%v) generated migrations = %v, want one file", args, files)
	}
	if !strings.Contains(stdout.String(), "Created "+files[0]) {
		t.Errorf("Program.Run(%v) stdout = %q, want generated migration path", args, stdout.String())
	}
}

func TestGenerateHandlerCommandCreatesHandlerWithFlagsAfterName(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	if err := scaffold.GenerateApp(scaffold.App{
		Destination: destination,
		Module:      "example.com/journal",
		Database:    scaffold.DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	}); err != nil {
		t.Fatalf("GenerateApp(journal) error = %v, want nil", err)
	}
	t.Chdir(destination)

	var stdout bytes.Buffer
	program := New(WithIO(cli.IO{Stdout: &stdout}))

	args := []string{"generate", "handler", "Posts", "--dir", "internal/handlers"}
	err := program.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Program.Run(%v) error = %v, want nil", args, err)
	}

	postsPath := filepath.Join("internal", "handlers", "posts.go")
	if _, err := os.Stat(postsPath); err != nil {
		t.Fatalf("Program.Run(%v) generated %s stat error = %v, want nil", args, postsPath, err)
	}
	home, err := os.ReadFile(filepath.Join("internal", "handlers", "home.go"))
	if err != nil {
		t.Fatalf("os.ReadFile(generated home.go) error = %v, want nil", err)
	}
	if !strings.Contains(string(home), `application.Get("/posts", PostsIndex)`) {
		t.Errorf("Program.Run(%v) generated home.go = %q, want posts route registration", args, home)
	}
	if !strings.Contains(stdout.String(), "Created "+postsPath) {
		t.Errorf("Program.Run(%v) stdout = %q, want generated handler path", args, stdout.String())
	}
	if !strings.Contains(stdout.String(), "Updated "+filepath.Join("internal", "handlers", "home.go")) {
		t.Errorf("Program.Run(%v) stdout = %q, want updated register path", args, stdout.String())
	}
}

func TestGenerateMigrationCommandRejectsUnknownGenerator(t *testing.T) {
	program := New()

	args := []string{"generate", "model", "Post"}
	err := program.Run(context.Background(), args)
	if !errors.Is(err, cli.ErrUsage) {
		t.Fatalf("Program.Run(%v) error = %v, want ErrUsage", args, err)
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
