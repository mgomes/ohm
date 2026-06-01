package scaffold

var appTemplates = map[string]string{
	".env.example": `# Shared values loaded for every OHM_ENV.
# Put environment-specific database settings in .env.development or .env.test.
`,
	".env.development.example": `DATABASE_URL={{.ExampleDatabaseURL}}
`,
	".env.test.example": `DATABASE_URL={{.TestDatabaseURL}}
`,
	"go.mod": `module {{.Module}}

go 1.25.0

require (
	{{.TemplModule}} {{.TemplVersion}}
	github.com/mgomes/ohm {{.OhmVersion}}
	{{.DriverModule}} {{.DriverVersion}}
)
`,
	"cmd/{{.Name}}/main.go": `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mgomes/ohm/cli"
	"github.com/mgomes/ohm/replay"

	"{{.Module}}/internal/app"
	"{{.Module}}/internal/db"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	application := app.New()
	program := cli.New("{{.Name}}", []cli.Command{
		cli.ServerCommand(application.HTTPHandler()),
		cli.RoutesCommand(application),
		db.Command(),
		db.MigrateCommand(),
		replay.Command(application.HTTPHandler()),
	})
	return program.Run(ctx, args)
}
`,
	"internal/app/app.go": `package app

import (
	"log/slog"
	"os"

	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/scrub"

	"{{.Module}}/internal/handlers"
)

func New() *ohm.App {
	logger := slog.New(scrub.NewHandler(slog.NewJSONHandler(os.Stderr, nil)))
	application := ohm.New()
	application.Use(ohm.RequestLogger(logger), ohm.Recoverer(logger))
	handlers.Register(application)
	return application
}
`,
	"internal/app/app_test.go": `package app

import (
	"testing"

	"github.com/mgomes/ohm"
)

func TestNewRegistersHomeRoute(t *testing.T) {
	application := New()

	routes, err := application.Routes()
	if err != nil {
		t.Fatalf("New().Routes() error = %v, want nil", err)
	}
	if !hasRoute(routes, "GET", "/") {
		t.Fatalf("New().Routes() = %+v, want GET /", routes)
	}
}

func hasRoute(routes []ohm.Route, method string, pattern string) bool {
	for _, route := range routes {
		if route.Method == method && route.Pattern == pattern {
			return true
		}
	}
	return false
}
`,
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
	if got := stdout.String(); got != "No pending migrations.\n" {
		t.Errorf("MigrateCommand().Run(ctx, io, %v) stdout = %q, want no pending migrations", []string{"up"}, got)
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
	"internal/handlers/home.go": `package handlers

import (
	"net/http"

	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views/pages"
)

func Register(application *ohm.App) {
	application.Get("/", Home)
}

func Home(req *ohm.Request) error {
	return req.HTML(http.StatusOK, pages.Home("{{.Title}}"))
}
`,
	"internal/handlers/home_test.go": `package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mgomes/ohm"
)

func TestHome(t *testing.T) {
	application := ohm.New()
	Register(application)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("Home(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Home(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if !strings.Contains(response.Body.String(), "<title>{{.Title}}</title>") {
		t.Errorf("Home(%s %s) body = %q, want page title", request.Method, request.URL.Path, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "<h1>Welcome to {{.Title}}</h1>") {
		t.Errorf("Home(%s %s) body = %q, want welcome heading", request.Method, request.URL.Path, response.Body.String())
	}
}
`,
	"internal/views/layouts/application.templ": `package layouts

templ Application(title string) {
	<!doctype html>
	<html lang="en">
		<head>
			<meta charset="utf-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1"/>
			<title>{ title }</title>
		</head>
		<body>
			<main>
				{ children... }
			</main>
		</body>
	</html>
}
`,
	"internal/views/layouts/application_templ.go": `// Code generated by templ - DO NOT EDIT.

// templ: version: {{.TemplVersion}}
package layouts

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

func Application(title string) templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var1 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var1 == nil {
			templ_7745c5c3_Var1 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 1, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		var templ_7745c5c3_Var2 string
		templ_7745c5c3_Var2, templ_7745c5c3_Err = templ.JoinStringErrs(title)
		if templ_7745c5c3_Err != nil {
			return templ.Error{Err: templ_7745c5c3_Err, FileName: ` + "`internal/views/layouts/application.templ`" + `, Line: 9, Col: 17}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var2))
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 2, "</title></head><body><main>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templ_7745c5c3_Var1.Render(ctx, templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 3, "</main></body></html>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return nil
	})
}

var _ = templruntime.GeneratedTemplate
`,
	"internal/views/pages/home.templ": `package pages

import "{{.Module}}/internal/views/layouts"

templ Home(title string) {
	@layouts.Application(title) {
		<h1>{ "Welcome to " + title }</h1>
	}
}
`,
	"internal/views/pages/home_test.go": `package pages

import (
	"context"
	"strings"
	"testing"
)

func TestHomeRendersApplicationLayout(t *testing.T) {
	var body strings.Builder
	if err := Home("{{.Title}}").Render(context.Background(), &body); err != nil {
		t.Fatalf("Home(title).Render(ctx, w) error = %v, want nil", err)
	}
	if !strings.Contains(body.String(), "<title>{{.Title}}</title>") {
		t.Errorf("Home(title) body = %q, want page title", body.String())
	}
	if !strings.Contains(body.String(), "<h1>Welcome to {{.Title}}</h1>") {
		t.Errorf("Home(title) body = %q, want heading", body.String())
	}
}
`,
	"internal/views/pages/home_templ.go": `// Code generated by templ - DO NOT EDIT.

// templ: version: {{.TemplVersion}}
package pages

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

import "{{.Module}}/internal/views/layouts"

func Home(title string) templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var1 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var1 == nil {
			templ_7745c5c3_Var1 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)
		templ_7745c5c3_Var2 := templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
			templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
			templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
			if !templ_7745c5c3_IsBuffer {
				defer func() {
					templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
					if templ_7745c5c3_Err == nil {
						templ_7745c5c3_Err = templ_7745c5c3_BufErr
					}
				}()
			}
			ctx = templ.InitializeContext(ctx)
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 1, "<h1>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			var templ_7745c5c3_Var3 string
			templ_7745c5c3_Var3, templ_7745c5c3_Err = templ.JoinStringErrs("Welcome to " + title)
			if templ_7745c5c3_Err != nil {
				return templ.Error{Err: templ_7745c5c3_Err, FileName: ` + "`internal/views/pages/home.templ`" + `, Line: 7, Col: 29}
			}
			_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var3))
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 2, "</h1>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			return nil
		})
		templ_7745c5c3_Err = layouts.Application(title).Render(templ.WithChildren(ctx, templ_7745c5c3_Var2), templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return nil
	})
}

var _ = templruntime.GeneratedTemplate
`,
	"internal/views/components/README.md": `# Components

Place reusable templ components here.
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
	"static/README.md": `# Static Assets

Place application static assets here.
`,
	"justfile": `set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

default:
    just --list

server:
    go run ./cmd/{{.Name}} server

routes:
    go run ./cmd/{{.Name}} routes

migrate-up:
    go run ./cmd/{{.Name}} migrate up

migrate-down:
    go run ./cmd/{{.Name}} migrate down

migrate-status:
    go run ./cmd/{{.Name}} migrate status

migrate-reset:
    go run ./cmd/{{.Name}} migrate reset

db-seed:
    go run ./cmd/{{.Name}} db seed

db-reset: migrate-reset migrate-up

test-db-setup:
    OHM_ENV=test go run ./cmd/{{.Name}} migrate up

sqlc:
    go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate

templ:
    go run github.com/a-h/templ/cmd/templ@{{.TemplVersion}} generate

generate: templ sqlc

test: generate
    go test ./...

test-unit: generate
    go test ./...

test-integration: generate
    go test ./...

vet:
    go vet ./...

fmt:
    gofmt -w $(git ls-files '*.go')

fmt-check:
    files="$(gofmt -l .)"; \
    test -z "$files" || { printf '%s\n' "$files"; exit 1; }

tidy:
    go mod tidy

tidy-check:
    go mod tidy -diff

check: generate fmt-check tidy vet
    go test ./...
`,
}
