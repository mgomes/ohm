package scaffold

var projectTemplates = map[string]string{
	".gitignore": `.env
.env.*
!.env.example
!.env.*.example
/development.db
/test.db
/tmp/*
!/tmp/replays/
/tmp/replays/*
!/tmp/replays/README.md
`,
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
`}
