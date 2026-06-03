package scaffold

var databaseTemplates = map[string]string{
	"internal/db/db.go": `package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "{{.DriverImport}}"

	"github.com/mgomes/ohm/cli"
	"github.com/mgomes/ohm/config"
	"github.com/mgomes/ohm/migrate"
)

const (
	driverName    = "{{.DriverName}}"
	migrationsDir = "migrations"
)

type Config struct {
	URL config.Secret ` + "`{{.DatabaseTags}}`" + `
}

func Open(ctx context.Context, cfg Config) (*sql.DB, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open(driverName, cfg.URL.Reveal())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("ping database: %w", errors.Join(err, closeErr))
		}
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func withConfiguredDB(ctx context.Context, fn func(*sql.DB) error) (err error) {
	if fn == nil {
		return fmt.Errorf("database function is required")
	}

	cfg, err := config.Load[Config]()
	if err != nil {
		return fmt.Errorf("load database config: %w", err)
	}

	db, err := Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close database: %w", closeErr)
		}
	}()

	return fn(db)
}

func MigrateCommand() cli.Command {
	return cli.Command{
		Name:    "migrate",
		Summary: "run database migrations",
		Usage:   "migrate <up|down|reset|status>",
		Run:     runMigrations,
	}
}

func runMigrations(ctx context.Context, io cli.IO, args []string) error {
	return withConfiguredDB(ctx, func(db *sql.DB) error {
		runner, err := migrate.NewFromDir(db, {{.MigrateDialect}}, migrationsDir)
		if err != nil {
			return err
		}
		return migrate.Command(runner).Run(ctx, io, args)
	})
}
`,
	"internal/db/command.go": `package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	"github.com/mgomes/ohm/cli"
)

func Command() cli.Command {
	return cli.Command{
		Name:    "db",
		Summary: "run database tasks",
		Usage:   "db <seed>",
		Run:     runDBCommand,
	}
}

func runDBCommand(ctx context.Context, commandIO cli.IO, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: db requires one subcommand", cli.ErrUsage)
	}

	switch args[0] {
	case "seed":
		return runSeed(ctx, commandIO)
	default:
		return fmt.Errorf("%w: unknown db subcommand %q", cli.ErrUsage, args[0])
	}
}

func runSeed(ctx context.Context, commandIO cli.IO) error {
	if err := withConfiguredDB(ctx, func(db *sql.DB) error {
		return Seed(ctx, db)
	}); err != nil {
		return err
	}
	fmt.Fprintln(output(commandIO.Stdout), "Seeded database.")
	return nil
}

func output(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
`,
	"internal/db/command_test.go": `package db

import (
{{- if .IsSQLite }}
	"bytes"
{{- end }}
	"context"
	"errors"
{{- if .IsSQLite }}
	"path/filepath"
{{- end }}
	"strings"
	"testing"

	"github.com/mgomes/ohm/cli"
)

func TestCommandRunsSeed(t *testing.T) {
{{- if .IsSQLite }}
	databaseURL := "file:" + filepath.Join(t.TempDir(), "seed.db")
	t.Setenv("DATABASE_URL", databaseURL)

	var stdout bytes.Buffer
	command := Command()
	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"seed"})
	if err != nil {
		t.Fatalf("Command().Run(ctx, io, %v) error = %v, want nil", []string{"seed"}, err)
	}
	if got := stdout.String(); got != "Seeded database.\n" {
		t.Errorf("Command().Run(ctx, io, %v) stdout = %q, want %q", []string{"seed"}, got, "Seeded database.\n")
	}
{{- else }}
	t.Skip("db seed integration test requires a configured Postgres test database")
{{- end }}
}

func TestCommandRejectsInvalidSubcommand(t *testing.T) {
	command := Command()
	err := command.Run(context.Background(), cli.IO{}, []string{"drop"})
	if !errors.Is(err, cli.ErrUsage) {
		t.Fatalf("Command().Run(ctx, io, %v) error = %v, want ErrUsage", []string{"drop"}, err)
	}
	if !strings.Contains(err.Error(), "unknown db subcommand") {
		t.Errorf("Command().Run(ctx, io, %v) error = %v, want unknown subcommand context", []string{"drop"}, err)
	}
}
`,
	"internal/db/db_test.go": `package db

import (
	"testing"

	"github.com/mgomes/ohm/config"
)

func TestConfigLoadsDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "{{.TestDatabaseURL}}")

	cfg, err := config.Load[Config](config.WithoutEnvFiles())
	if err != nil {
		t.Fatalf("config.Load[Config](WithoutEnvFiles()) error = %v, want nil", err)
	}
	if got := cfg.URL.Reveal(); got != "{{.TestDatabaseURL}}" {
		t.Errorf("config.Load[Config](WithoutEnvFiles()) DATABASE_URL = %q, want %q", got, "{{.TestDatabaseURL}}")
	}
}
`,
	"internal/db/migrate_test.go": `package db

import (
{{- if .IsSQLite }}
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
{{- end }}
	"testing"
{{- if .IsSQLite }}

	"github.com/mgomes/ohm/cli"
{{- end }}
)

func TestMigrateCommandRunsAgainstTestDatabase(t *testing.T) {
{{- if .IsSQLite }}
	t.Chdir(projectRoot(t))

	databaseURL := "file:" + filepath.Join(t.TempDir(), "migrate.db")
	t.Setenv("DATABASE_URL", databaseURL)

	command := MigrateCommand()
	var stdout bytes.Buffer
	if err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"up"}); err != nil {
		t.Fatalf("MigrateCommand().Run(ctx, io, %v) error = %v, want nil", []string{"up"}, err)
	}
	if got := stdout.String(); got != "No pending migrations.\n" && !strings.Contains(got, "Applied ") {
		t.Errorf("MigrateCommand().Run(ctx, io, %v) stdout = %q, want migration result", []string{"up"}, got)
	}

	stdout.Reset()
	if err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"status"}); err != nil {
		t.Fatalf("MigrateCommand().Run(ctx, io, %v) error = %v, want nil", []string{"status"}, err)
	}
	if got := stdout.String(); !strings.Contains(got, "VERSION") || !strings.Contains(got, "STATE") || !strings.Contains(got, "SOURCE") {
		t.Errorf("MigrateCommand().Run(ctx, io, %v) stdout = %q, want status header", []string{"status"}, got)
	}
{{- else }}
	t.Skip("migration integration test requires a configured Postgres test database")
{{- end }}
}
{{- if .IsSQLite }}

func projectRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v, want nil", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		} else if !os.IsNotExist(err) {
			t.Fatalf("os.Stat(go.mod) in %q error = %v, want nil or not exist", dir, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("project root with go.mod not found from %q", dir)
		}
		dir = parent
	}
}
{{- end }}
`,
	"internal/db/seeds.go": `package db

import (
	"context"
	"database/sql"
	"fmt"
)

func Seed(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
`,
	"migrations/README.md": `# Migrations

This app uses goose migrations against {{.DatabaseSummary}}.

Create migration files with:

` + "```text" + `
ohm generate migration create_posts
` + "```" + `
`,
	"queries/health.sql": `-- name: HealthCheck :one
SELECT 1;
`,
	"internal/db/dbgen/README.md": `# Generated Queries

sqlc writes generated query code into this package.
`,
	"internal/db/dbtest/dbtest.go": `package dbtest

import (
	"context"
	"database/sql"
{{- if .IsSQLite }}
	"path/filepath"
{{- else }}
	"os"
{{- end }}
	"testing"

	"github.com/mgomes/ohm/config"

	"{{.Module}}/internal/db"
)

func Open(t testing.TB) *sql.DB {
	t.Helper()

{{- if .IsSQLite }}
	databaseURL := "file:" + filepath.Join(t.TempDir(), "test.db")
{{- else }}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for Postgres database tests")
	}
{{- end }}
	database, err := db.Open(context.Background(), db.Config{URL: config.Secret(databaseURL)})
	if err != nil {
		t.Fatalf("db.Open(ctx, cfg) error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("db.Close() error = %v, want nil", err)
		}
	})
	return database
}
`,
	"internal/db/dbtest/dbtest_test.go": `package dbtest

import (
	"context"
	"testing"
)

func TestOpenReturnsUsableDatabase(t *testing.T) {
	database := Open(t)
	if err := database.PingContext(context.Background()); err != nil {
		t.Fatalf("Open(t).PingContext(ctx) error = %v, want nil", err)
	}
}
`,
	"sqlc.yaml": `version: "2"
sql:
  - engine: "{{.SQLCEngine}}"
    schema: "migrations"
    queries: "queries"
    gen:
      go:
        package: "dbgen"
        out: "internal/db/dbgen"
        sql_package: "database/sql"
`,
}
