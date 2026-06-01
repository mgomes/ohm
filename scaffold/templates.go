package scaffold

var appTemplates = map[string]string{
	"go.mod": `module {{.Module}}

go 1.25.0

require (
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
		db.MigrateCommand(),
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
	logger := slog.New(scrub.NewHandler(slog.NewJSONHandler(os.Stdout, nil)))
	application := ohm.New()
	application.Use(ohm.RequestLogger(logger))
	handlers.Register(application)
	return application
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

func MigrateCommand() cli.Command {
	return cli.Command{
		Name:    "migrate",
		Summary: "run database migrations",
		Usage:   "migrate <up|down|status>",
		Run:     runMigrations,
	}
}

func runMigrations(ctx context.Context, io cli.IO, args []string) (err error) {
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

	runner, err := migrate.NewFromDir(db, {{.MigrateDialect}}, migrationsDir)
	if err != nil {
		return err
	}
	return migrate.Command(runner).Run(ctx, io, args)
}
`,
	"internal/handlers/home.go": `package handlers

import (
	"net/http"

	"github.com/mgomes/ohm"
)

func Register(application *ohm.App) {
	application.Get("/", Home)
}

func Home(req *ohm.Request) error {
	req.PlainText(http.StatusOK, "Welcome to {{.Title}}")
	return nil
}
`,
	"internal/handlers/home_test.go": `package handlers

import (
	"net/http"
	"net/http/httptest"
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
	if response.Body.String() != "Welcome to {{.Title}}" {
		t.Errorf("Home(%s %s) body = %q, want %q", request.Method, request.URL.Path, response.Body.String(), "Welcome to {{.Title}}")
	}
}
`,
	"migrations/README.md": `# Migrations

This app uses goose migrations against {{.DatabaseSummary}}.

Create migration files with:

` + "```text" + `
ohm generate migration create_posts
` + "```" + `
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

test:
    go test ./...

test-unit:
    go test ./...

test-integration:
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

check: fmt-check tidy-check vet test
`,
}
