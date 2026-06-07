package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"
)

const (
	postgresDriverModule  = "github.com/jackc/pgx/v5"
	postgresDriverImport  = "github.com/jackc/pgx/v5/stdlib"
	postgresDriverName    = "pgx"
	postgresDriverVersion = "v5.9.2"
	sqliteDriverModule    = "modernc.org/sqlite"
	sqliteDriverImport    = "modernc.org/sqlite"
	sqliteDriverName      = "sqlite"
	sqliteDriverVersion   = "v1.51.0"
	sqliteDefaultURL      = "file:development.db"
)

var appNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Database identifies the generated application's database default.
type Database string

const (
	// DatabasePostgres generates a Postgres application using pgx.
	DatabasePostgres Database = "postgres"
	// DatabaseSQLite generates a SQLite application using modernc.org/sqlite.
	DatabaseSQLite Database = "sqlite"
)

// ErrUnsupportedDatabase reports an unknown generated database target.
var ErrUnsupportedDatabase = errors.New("unsupported database")

// App describes a generated Ohm application.
type App struct {
	Name        string
	Module      string
	Destination string
	Database    Database
	OhmVersion  string
}

// GenerateApp writes a new Ohm application skeleton to cfg.Destination.
func GenerateApp(cfg App) error {
	normalized, err := normalizeApp(cfg)
	if err != nil {
		return err
	}

	data, err := newAppData(normalized)
	if err != nil {
		return err
	}

	paths := make([]string, 0, len(appTemplates))
	for path := range appTemplates {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	files := make([]generatedFile, 0, len(paths))
	for _, path := range paths {
		renderedPath, err := renderString(path, data)
		if err != nil {
			return fmt.Errorf("render path %q: %w", path, err)
		}

		body, err := renderFile(renderedPath, appTemplates[path], data)
		if err != nil {
			return fmt.Errorf("render %s: %w", renderedPath, err)
		}

		files = append(files, generatedFile{
			path: renderedPath,
			body: body,
		})
	}

	if err := ensureEmptyDestination(normalized.Destination); err != nil {
		return err
	}

	for _, file := range files {
		fullPath := filepath.Join(normalized.Destination, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", file.path, err)
		}
		if err := os.WriteFile(fullPath, file.body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file.path, err)
		}
	}

	return nil
}

type generatedFile struct {
	path string
	body []byte
}

func normalizeApp(cfg App) (App, error) {
	if cfg.Destination == "" {
		return App{}, fmt.Errorf("destination is required")
	}

	cfg.Destination = filepath.Clean(cfg.Destination)
	if cfg.Name == "" {
		cfg.Name = filepath.Base(cfg.Destination)
	}
	if !appNamePattern.MatchString(cfg.Name) {
		return App{}, fmt.Errorf("app name %q must start with a lowercase letter and contain only lowercase letters, digits, or hyphens", cfg.Name)
	}
	if cfg.Module == "" {
		cfg.Module = cfg.Name
	}
	if strings.TrimSpace(cfg.Module) != cfg.Module || cfg.Module == "" || strings.ContainsAny(cfg.Module, " \t\r\n") {
		return App{}, fmt.Errorf("module path %q is invalid", cfg.Module)
	}
	if cfg.Database == "" {
		cfg.Database = DatabasePostgres
	}
	if cfg.OhmVersion == "" {
		return App{}, fmt.Errorf("ohm version is required")
	}
	return cfg, nil
}

func ensureEmptyDestination(destination string) error {
	entries, err := os.ReadDir(destination)
	if err == nil {
		if len(entries) > 0 {
			return fmt.Errorf("destination %q already exists and is not empty", destination)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect destination %q: %w", destination, err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create destination %q: %w", destination, err)
	}
	return nil
}

type appData struct {
	Name               string
	Title              string
	Module             string
	OhmVersion         string
	DriverModule       string
	DriverImport       string
	DriverName         string
	DriverVersion      string
	MigrateDialect     string
	SQLCEngine         string
	DatabaseTags       string
	ExampleDatabaseURL string
	IsSQLite           bool
	TestDatabaseURL    string
	DatabaseSummary    string
}

func newAppData(cfg App) (appData, error) {
	data := appData{
		Name:       cfg.Name,
		Title:      titleName(cfg.Name),
		Module:     cfg.Module,
		OhmVersion: cfg.OhmVersion,
	}

	switch cfg.Database {
	case DatabasePostgres:
		data.DriverModule = postgresDriverModule
		data.DriverImport = postgresDriverImport
		data.DriverName = postgresDriverName
		data.DriverVersion = postgresDriverVersion
		data.MigrateDialect = "migrate.DialectPostgres"
		data.SQLCEngine = "postgresql"
		data.DatabaseTags = `env:"DATABASE_URL,required"`
		data.ExampleDatabaseURL = fmt.Sprintf("postgres://localhost/%s_development?sslmode=disable", databaseName(cfg.Name))
		data.TestDatabaseURL = "postgres://localhost/test?sslmode=disable"
		data.DatabaseSummary = "Postgres via pgx"
	case DatabaseSQLite:
		data.DriverModule = sqliteDriverModule
		data.DriverImport = sqliteDriverImport
		data.DriverName = sqliteDriverName
		data.DriverVersion = sqliteDriverVersion
		data.MigrateDialect = "migrate.DialectSQLite"
		data.SQLCEngine = "sqlite"
		data.DatabaseTags = fmt.Sprintf(`env:"DATABASE_URL" default:%q`, sqliteDefaultURL)
		data.ExampleDatabaseURL = sqliteDefaultURL
		data.IsSQLite = true
		data.TestDatabaseURL = "file:test.db"
		data.DatabaseSummary = "SQLite via modernc.org/sqlite"
	default:
		return appData{}, fmt.Errorf("%w: %q", ErrUnsupportedDatabase, cfg.Database)
	}

	return data, nil
}

func titleName(name string) string {
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func databaseName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

func renderFile(path string, raw string, data appData) ([]byte, error) {
	rendered, err := renderString(raw, data)
	if err != nil {
		return nil, err
	}

	body := []byte(rendered)
	if strings.HasSuffix(path, ".go") {
		formatted, err := format.Source(body)
		if err != nil {
			return nil, err
		}
		body = formatted
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}
	return body, nil
}

func renderString(raw string, data appData) (string, error) {
	tmpl, err := template.New("app").Parse(raw)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
