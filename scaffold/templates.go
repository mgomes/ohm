package scaffold

var appTemplates = map[string]string{
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
	"internal/app/app.go": `package app

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/scrub"

	"{{.Module}}/internal/handlers"
)

type Option func(*options)

type options struct {
	staticRoot string
}

func WithStaticRoot(root string) Option {
	return func(opts *options) {
		if root != "" {
			opts.staticRoot = root
		}
	}
}

func New(opts ...Option) *ohm.App {
	cfg := options{
		staticRoot: "static",
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	logger := slog.New(scrub.NewHandler(slog.NewJSONHandler(os.Stderr, nil)))
	application := ohm.New(ohm.WithErrorHandler(handleError))
	application.Use(ohm.RequestLogger(logger), ohm.Recoverer(logger))
	assets := http.StripPrefix("/assets/", http.FileServer(http.Dir(cfg.staticRoot)))
	router := application.ChiRouter()
	router.Get("/assets/*", assets.ServeHTTP)
	router.Head("/assets/*", assets.ServeHTTP)
	router.NotFound(notFound)
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r, ohm.AllowedMethods(router, r.URL.Path))
	})
	handlers.Register(application)
	return application
}
`,
	"internal/app/errors.go": `package app

import (
	"net/http"

	"github.com/mgomes/ohm"

	"{{.Module}}/internal/views/pages"
)

func handleError(req *ohm.Request, err error) {
	status, message := ohm.ErrorResponse(err)
	renderError(req.ResponseWriter(), req.HTTPRequest(), status, message)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	renderError(w, r, http.StatusNotFound, http.StatusText(http.StatusNotFound))
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request, allowedMethods []string) {
	for _, method := range allowedMethods {
		w.Header().Add("Allow", method)
	}
	renderError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
}

func renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if err := ohm.RenderHTML(w, r, status, pages.Error(status, message)); err != nil {
		http.Error(w, message, status)
	}
}
`,
	"internal/app/app_test.go": `package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestNewServesStaticAssets(t *testing.T) {
	staticRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticRoot, "app.css"), []byte("body { color: black; }\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(static asset) error = %v, want nil", err)
	}

	application := New(WithStaticRoot(staticRoot))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) status = %d, want %d", staticRoot, request.Method, request.URL.Path, response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != "body { color: black; }\n" {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) body = %q, want static asset", staticRoot, request.Method, request.URL.Path, got)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/assets/app.css", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) status = %d, want %d", staticRoot, request.Method, request.URL.Path, response.Code, http.StatusMethodNotAllowed)
	}
	if got, want := response.Header().Values("Allow"), []string{http.MethodGet, http.MethodHead}; !slices.Equal(got, want) {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) Allow header = %v, want %v", staticRoot, request.Method, request.URL.Path, got, want)
	}
	if !strings.Contains(response.Body.String(), "<h1>Method Not Allowed</h1>") {
		t.Errorf("New(WithStaticRoot(%q)).ServeHTTP(%s %s) body = %q, want method not allowed page", staticRoot, request.Method, request.URL.Path, response.Body.String())
	}
}

func TestNewRendersHandlerErrorPage(t *testing.T) {
	application := New()
	application.Get("/posts/42", func(req *ohm.Request) error {
		return ohm.NewHTTPError(http.StatusNotFound, "Post not found", errors.New("missing post"))
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/posts/42", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Errorf("New().ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("New().ServeHTTP(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if !strings.Contains(response.Body.String(), "<h1>Post not found</h1>") {
		t.Errorf("New().ServeHTTP(%s %s) body = %q, want error page", request.Method, request.URL.Path, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "missing post") {
		t.Errorf("New().ServeHTTP(%s %s) body = %q, want no wrapped error detail", request.Method, request.URL.Path, response.Body.String())
	}
}

func TestNewRendersMissingRouteErrorPage(t *testing.T) {
	application := New()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)

	application.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Errorf("New().ServeHTTP(%s %s) status = %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusNotFound)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("New().ServeHTTP(%s %s) Content-Type = %q, want %q", request.Method, request.URL.Path, got, "text/html; charset=utf-8")
	}
	if !strings.Contains(response.Body.String(), "<h1>Not Found</h1>") {
		t.Errorf("New().ServeHTTP(%s %s) body = %q, want not found page", request.Method, request.URL.Path, response.Body.String())
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
	"internal/apptest/apptest.go": `package apptest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"{{.Module}}/internal/app"
)

type Client struct {
	t       testing.TB
	handler http.Handler
}

func New(t testing.TB, opts ...app.Option) *Client {
	t.Helper()

	return &Client{
		t:       t,
		handler: app.New(opts...).HTTPHandler(),
	}
}

func (c *Client) Get(target string) *httptest.ResponseRecorder {
	c.t.Helper()

	return c.Request(http.MethodGet, target, nil)
}

func (c *Client) Request(method string, target string, body io.Reader) *httptest.ResponseRecorder {
	c.t.Helper()

	request := httptest.NewRequest(method, target, body)
	response := httptest.NewRecorder()
	c.handler.ServeHTTP(response, request)
	return response
}
`,
	"internal/apptest/apptest_test.go": `package apptest

import (
	"net/http"
	"strings"
	"testing"
)

func TestClientGetExercisesApplicationRoute(t *testing.T) {
	client := New(t)

	response := client.Get("/")

	if response.Code != http.StatusOK {
		t.Fatalf("Client.Get(%q) status = %d, want %d", "/", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "<h1>Welcome to {{.Title}}</h1>") {
		t.Errorf("Client.Get(%q) body = %q, want home page", "/", response.Body.String())
	}
}

func TestClientRequestReportsMissingRoute(t *testing.T) {
	client := New(t)

	response := client.Request(http.MethodGet, "/missing", nil)

	if response.Code != http.StatusNotFound {
		t.Errorf("Client.Request(%q, %q, nil) status = %d, want %d", http.MethodGet, "/missing", response.Code, http.StatusNotFound)
	}
	if !strings.Contains(response.Body.String(), "<h1>Not Found</h1>") {
		t.Errorf("Client.Request(%q, %q, nil) body = %q, want not found page", http.MethodGet, "/missing", response.Body.String())
	}
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
	"internal/views/pages/error.templ": `package pages

import (
	"strconv"

	"{{.Module}}/internal/views/layouts"
)

templ Error(status int, message string) {
	@layouts.Application(message) {
		<h1>{ message }</h1>
		<p>{ "HTTP " + strconv.Itoa(status) }</p>
	}
}
`,
	"internal/views/pages/error_test.go": `package pages

import (
	"context"
	"strings"
	"testing"
)

func TestErrorRendersApplicationLayout(t *testing.T) {
	var body strings.Builder
	if err := Error(404, "Not Found").Render(context.Background(), &body); err != nil {
		t.Fatalf("Error(status, message).Render(ctx, w) error = %v, want nil", err)
	}
	if !strings.Contains(body.String(), "<title>Not Found</title>") {
		t.Errorf("Error(status, message) body = %q, want page title", body.String())
	}
	if !strings.Contains(body.String(), "<h1>Not Found</h1>") {
		t.Errorf("Error(status, message) body = %q, want heading", body.String())
	}
	if !strings.Contains(body.String(), "<p>HTTP 404</p>") {
		t.Errorf("Error(status, message) body = %q, want status code", body.String())
	}
}
`,
	"internal/views/pages/error_templ.go": `// Code generated by templ - DO NOT EDIT.

// templ: version: {{.TemplVersion}}
package pages

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

import (
	"strconv"

	"{{.Module}}/internal/views/layouts"
)

func Error(status int, message string) templ.Component {
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
			templ_7745c5c3_Var3, templ_7745c5c3_Err = templ.JoinStringErrs(message)
			if templ_7745c5c3_Err != nil {
				return templ.Error{Err: templ_7745c5c3_Err, FileName: ` + "`internal/views/pages/error.templ`" + `, Line: 11, Col: 15}
			}
			_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var3))
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 2, "</h1><p>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			var templ_7745c5c3_Var4 string
			templ_7745c5c3_Var4, templ_7745c5c3_Err = templ.JoinStringErrs("HTTP " + strconv.Itoa(status))
			if templ_7745c5c3_Err != nil {
				return templ.Error{Err: templ_7745c5c3_Err, FileName: ` + "`internal/views/pages/error.templ`" + `, Line: 12, Col: 37}
			}
			_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var4))
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 3, "</p>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			return nil
		})
		templ_7745c5c3_Err = layouts.Application(message).Render(templ.WithChildren(ctx, templ_7745c5c3_Var2), templ_7745c5c3_Buffer)
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
	"internal/views/components/flash.go": `package components

type FlashLevel string

const (
	FlashInfo    FlashLevel = "info"
	FlashSuccess FlashLevel = "success"
	FlashWarning FlashLevel = "warning"
	FlashError   FlashLevel = "error"
)

type FlashMessage struct {
	Level FlashLevel
	Text  string
}

func NewFlashMessage(level FlashLevel, text string) FlashMessage {
	if level == "" {
		level = FlashInfo
	}
	return FlashMessage{Level: level, Text: text}
}

func (m FlashMessage) CSSClass() string {
	switch m.Level {
	case FlashSuccess:
		return "flash flash-success"
	case FlashWarning:
		return "flash flash-warning"
	case FlashError:
		return "flash flash-error"
	default:
		return "flash flash-info"
	}
}

func (m FlashMessage) Role() string {
	if m.Level == FlashError {
		return "alert"
	}
	return "status"
}
`,
	"internal/views/components/flash.templ": `package components

templ Flash(messages []FlashMessage) {
	if len(messages) > 0 {
		<section aria-label="Notifications" class="flash-messages">
			for _, message := range messages {
				<p class={ message.CSSClass() } role={ message.Role() }>{ message.Text }</p>
			}
		</section>
	}
}
`,
	"internal/views/components/flash_test.go": `package components

import (
	"context"
	"strings"
	"testing"
)

func TestFlashRendersMessages(t *testing.T) {
	messages := []FlashMessage{
		NewFlashMessage(FlashSuccess, "Saved"),
		NewFlashMessage(FlashError, "<failed>"),
	}

	var body strings.Builder
	if err := Flash(messages).Render(context.Background(), &body); err != nil {
		t.Fatalf("Flash(messages).Render(ctx, w) error = %v, want nil", err)
	}
	if !strings.Contains(body.String(), "<section aria-label=\"Notifications\" class=\"flash-messages\">") {
		t.Errorf("Flash(messages) body = %q, want notifications region", body.String())
	}
	if !strings.Contains(body.String(), "<p class=\"flash flash-success\" role=\"status\">Saved</p>") {
		t.Errorf("Flash(messages) body = %q, want success message", body.String())
	}
	if !strings.Contains(body.String(), "<p class=\"flash flash-error\" role=\"alert\">&lt;failed&gt;</p>") {
		t.Errorf("Flash(messages) body = %q, want escaped error alert", body.String())
	}
}

func TestFlashOmitsEmptyMessages(t *testing.T) {
	var body strings.Builder
	if err := Flash(nil).Render(context.Background(), &body); err != nil {
		t.Fatalf("Flash(nil).Render(ctx, w) error = %v, want nil", err)
	}
	if body.String() != "" {
		t.Errorf("Flash(nil) body = %q, want empty", body.String())
	}
}

func TestNewFlashMessageDefaultsLevel(t *testing.T) {
	message := NewFlashMessage("", "Saved")

	if message.Level != FlashInfo {
		t.Errorf("NewFlashMessage(%q, %q).Level = %q, want %q", "", "Saved", message.Level, FlashInfo)
	}
	if message.CSSClass() != "flash flash-info" {
		t.Errorf("NewFlashMessage(%q, %q).CSSClass() = %q, want %q", "", "Saved", message.CSSClass(), "flash flash-info")
	}
	if message.Role() != "status" {
		t.Errorf("NewFlashMessage(%q, %q).Role() = %q, want %q", "", "Saved", message.Role(), "status")
	}
}
`,
	"internal/views/components/flash_templ.go": `// Code generated by templ - DO NOT EDIT.

// templ: version: {{.TemplVersion}}
package components

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

func Flash(messages []FlashMessage) templ.Component {
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
		if len(messages) > 0 {
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 1, "<section aria-label=\"Notifications\" class=\"flash-messages\">")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			for _, message := range messages {
				var templ_7745c5c3_Var2 = []any{message.CSSClass()}
				templ_7745c5c3_Err = templ.RenderCSSItems(ctx, templ_7745c5c3_Buffer, templ_7745c5c3_Var2...)
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
				templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 2, "<p class=\"")
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
				var templ_7745c5c3_Var3 string
				templ_7745c5c3_Var3, templ_7745c5c3_Err = templ.ResolveAttributeValue(templ.CSSClasses(templ_7745c5c3_Var2).String())
				if templ_7745c5c3_Err != nil {
					return templ.Error{Err: templ_7745c5c3_Err, FileName: ` + "`internal/views/components/flash.templ`" + `, Line: 1, Col: 0}
				}
				_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ_7745c5c3_Var3)
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
				templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 3, "\" role=\"")
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
				var templ_7745c5c3_Var4 string
				templ_7745c5c3_Var4, templ_7745c5c3_Err = templ.ResolveAttributeValue(message.Role())
				if templ_7745c5c3_Err != nil {
					return templ.Error{Err: templ_7745c5c3_Err, FileName: ` + "`internal/views/components/flash.templ`" + `, Line: 7, Col: 57}
				}
				_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ_7745c5c3_Var4)
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
				templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 4, "\">")
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
				var templ_7745c5c3_Var5 string
				templ_7745c5c3_Var5, templ_7745c5c3_Err = templ.JoinStringErrs(message.Text)
				if templ_7745c5c3_Err != nil {
					return templ.Error{Err: templ_7745c5c3_Err, FileName: ` + "`internal/views/components/flash.templ`" + `, Line: 7, Col: 74}
				}
				_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var5))
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
				templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 5, "</p>")
				if templ_7745c5c3_Err != nil {
					return templ_7745c5c3_Err
				}
			}
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 6, "</section>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
		}
		return nil
	})
}

var _ = templruntime.GeneratedTemplate
`,
	"internal/views/forms/forms.go": `package forms

import (
	"slices"
	"strings"
	"unicode"
)

type Values map[string]string

type Errors map[string][]string

type Field struct {
	Name   string
	ID     string
	Label  string
	Value  string
	Errors []string
}

func NewField(name string, label string, values Values, errors Errors) Field {
	if label == "" {
		label = Label(name)
	}
	return Field{
		Name:   name,
		ID:     FieldID(name),
		Label:  label,
		Value:  values.Get(name),
		Errors: errors.Get(name),
	}
}

func (v Values) Get(name string) string {
	if v == nil {
		return ""
	}
	return v[name]
}

func (e Errors) Get(name string) []string {
	if e == nil {
		return nil
	}
	return slices.Clone(e[name])
}

func FieldID(name string) string {
	id := normalizedFieldID(name)
	if id == "" {
		return "field"
	}
	return id
}

func Label(name string) string {
	id := normalizedFieldID(name)
	if id == "" {
		return ""
	}

	parts := strings.Split(id, "-")
	for i, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func normalizedFieldID(name string) string {
	var builder strings.Builder
	lastSeparator := false
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
			lastSeparator = false
			continue
		}
		if builder.Len() > 0 && !lastSeparator {
			builder.WriteByte('-')
			lastSeparator = true
		}
	}

	return strings.Trim(builder.String(), "-")
}
`,
	"internal/views/forms/forms_test.go": `package forms

import (
	"slices"
	"testing"
)

func TestNewFieldBuildsViewData(t *testing.T) {
	values := Values{"post[title]": "Hello"}
	errors := Errors{"post[title]": []string{"is required"}}

	field := NewField("post[title]", "", values, errors)

	if field.Name != "post[title]" {
		t.Errorf("NewField(%q, label, values, errors).Name = %q, want %q", "post[title]", field.Name, "post[title]")
	}
	if field.ID != "post-title" {
		t.Errorf("NewField(%q, label, values, errors).ID = %q, want %q", "post[title]", field.ID, "post-title")
	}
	if field.Label != "Post Title" {
		t.Errorf("NewField(%q, label, values, errors).Label = %q, want %q", "post[title]", field.Label, "Post Title")
	}
	if field.Value != "Hello" {
		t.Errorf("NewField(%q, label, values, errors).Value = %q, want %q", "post[title]", field.Value, "Hello")
	}
	if !slices.Equal(field.Errors, []string{"is required"}) {
		t.Errorf("NewField(%q, label, values, errors).Errors = %v, want %v", "post[title]", field.Errors, []string{"is required"})
	}

	errors["post[title]"][0] = "changed"
	if !slices.Equal(field.Errors, []string{"is required"}) {
		t.Errorf("NewField(%q, label, values, errors).Errors after source mutation = %v, want %v", "post[title]", field.Errors, []string{"is required"})
	}
}

func TestNewFieldUsesExplicitLabel(t *testing.T) {
	field := NewField("email", "Email address", nil, nil)

	if field.Label != "Email address" {
		t.Errorf("NewField(%q, %q, nil, nil).Label = %q, want %q", "email", "Email address", field.Label, "Email address")
	}
}

func TestLabel(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "field", want: "Field"},
		{name: "Field", want: "Field"},
		{name: "post[title]", want: "Post Title"},
		{name: "!!!", want: ""},
	}

	for _, tt := range tests {
		got := Label(tt.name)
		if got != tt.want {
			t.Errorf("Label(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFieldID(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "email", want: "email"},
		{name: "post[title]", want: "post-title"},
		{name: " user email ", want: "user-email"},
		{name: "profile.avatar_url", want: "profile-avatar-url"},
		{name: "!!!", want: "field"},
	}

	for _, tt := range tests {
		got := FieldID(tt.name)
		if got != tt.want {
			t.Errorf("FieldID(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
`,
	"internal/views/assets/assets.go": `package assets

import (
	"net/url"
	"path"
	"strings"
)

const basePath = "/assets/"

func Path(name string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(name, "/"))
	if cleaned == "/" {
		return basePath
	}

	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return basePath + strings.Join(parts, "/")
}
`,
	"internal/views/assets/assets_test.go": `package assets

import "testing"

func TestPathBuildsAssetURL(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "", want: "/assets/"},
		{name: "app.css", want: "/assets/app.css"},
		{name: "/icons/logo.svg", want: "/assets/icons/logo.svg"},
		{name: "icons/../app.css", want: "/assets/app.css"},
		{name: "avatars/Jane Doe.png", want: "/assets/avatars/Jane%20Doe.png"},
	}

	for _, tt := range tests {
		got := Path(tt.name)
		if got != tt.want {
			t.Errorf("Path(%q) = %q, want %q", tt.name, got, tt.want)
		}
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
	"static/README.md": `# Static Assets

Place application static assets here.
`,
	"tmp/replays/README.md": `# Replay Snapshots

Store local replay snapshot JSON files here while debugging requests.

Replay snapshots are local debugging artifacts. Review them before committing
because they may include scrubbed request and response details.
`,
	"static/app.css": `body {
	margin: 2rem;
	font-family: system-ui, sans-serif;
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
`,
}
