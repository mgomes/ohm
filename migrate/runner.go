package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/pressly/goose/v3"
)

// Dialect identifies a database migration dialect.
type Dialect string

const (
	// DialectPostgres runs migrations against Postgres.
	DialectPostgres Dialect = "postgres"
	// DialectSQLite runs migrations against SQLite.
	DialectSQLite Dialect = "sqlite"
)

// ErrUnsupportedDialect reports that a migration dialect is not supported.
var ErrUnsupportedDialect = errors.New("unsupported migration dialect")

// Result describes one applied migration.
type Result struct {
	Version   int64
	Source    string
	Direction string
	Duration  time.Duration
	Empty     bool
	Skipped   bool
}

// Status describes one migration status row.
type Status struct {
	Version   int64
	Source    string
	State     string
	AppliedAt time.Time
}

// Runner applies and inspects database migrations.
type Runner interface {
	Up(context.Context) ([]Result, error)
	Down(context.Context) (Result, error)
	Status(context.Context) ([]Status, error)
}

// GooseRunner applies migrations with goose.
type GooseRunner struct {
	provider *goose.Provider
}

// Option configures a GooseRunner.
type Option func(*gooseOptions)

type gooseOptions struct {
	tableName string
}

// WithTableName configures the migration version table name.
func WithTableName(tableName string) Option {
	return func(opts *gooseOptions) {
		opts.tableName = tableName
	}
}

// New creates a goose-backed migration runner. The caller owns db and remains
// responsible for closing it.
func New(db *sql.DB, dialect Dialect, migrations fs.FS, opts ...Option) (*GooseRunner, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	if migrations == nil {
		return nil, fmt.Errorf("migration filesystem is required")
	}

	cfg := gooseOptions{}
	for _, opt := range opts {
		opt(&cfg)
	}

	dialectName, err := gooseDialect(dialect)
	if err != nil {
		return nil, err
	}

	providerOpts := []goose.ProviderOption{
		goose.WithDisableGlobalRegistry(true),
	}
	if cfg.tableName != "" {
		providerOpts = append(providerOpts, goose.WithTableName(cfg.tableName))
	}

	provider, err := goose.NewProvider(dialectName, db, migrations, providerOpts...)
	if err != nil {
		if errors.Is(err, goose.ErrNoMigrations) {
			return &GooseRunner{}, nil
		}
		return nil, fmt.Errorf("create migration provider: %w", err)
	}

	return &GooseRunner{provider: provider}, nil
}

// NewFromDir creates a goose-backed migration runner from a local directory.
func NewFromDir(db *sql.DB, dialect Dialect, dir string, opts ...Option) (*GooseRunner, error) {
	if dir == "" {
		return nil, fmt.Errorf("migration directory is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat migration directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("migration directory %q is not a directory", dir)
	}
	return New(db, dialect, os.DirFS(dir), opts...)
}

// Up applies all pending migrations.
func (r *GooseRunner) Up(ctx context.Context) ([]Result, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if r.provider == nil {
		return nil, nil
	}

	results, err := r.provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate up: %w", err)
	}
	return migrationResults(results), nil
}

// Down rolls back one migration.
func (r *GooseRunner) Down(ctx context.Context) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	if r.provider == nil {
		return Result{Skipped: true}, nil
	}

	result, err := r.provider.Down(ctx)
	if err != nil {
		if errors.Is(err, goose.ErrNoNextVersion) {
			return Result{Skipped: true}, nil
		}
		return Result{}, fmt.Errorf("migrate down: %w", err)
	}
	return migrationResult(result), nil
}

// Status returns migration state.
func (r *GooseRunner) Status(ctx context.Context) ([]Status, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if r.provider == nil {
		return nil, nil
	}

	statuses, err := r.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration status: %w", err)
	}
	return migrationStatuses(statuses), nil
}

func (r *GooseRunner) validate() error {
	if r == nil {
		return fmt.Errorf("migration runner is required")
	}
	return nil
}

func gooseDialect(dialect Dialect) (goose.Dialect, error) {
	switch dialect {
	case DialectPostgres:
		return goose.DialectPostgres, nil
	case DialectSQLite:
		return goose.DialectSQLite3, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
}

func migrationResults(results []*goose.MigrationResult) []Result {
	out := make([]Result, 0, len(results))
	for _, result := range results {
		out = append(out, migrationResult(result))
	}
	return out
}

func migrationResult(result *goose.MigrationResult) Result {
	if result == nil {
		return Result{}
	}

	out := Result{
		Direction: result.Direction,
		Duration:  result.Duration,
		Empty:     result.Empty,
	}
	if result.Source != nil {
		out.Version = result.Source.Version
		out.Source = result.Source.Path
	}
	return out
}

func migrationStatuses(statuses []*goose.MigrationStatus) []Status {
	out := make([]Status, 0, len(statuses))
	for _, status := range statuses {
		item := Status{
			State:     string(status.State),
			AppliedAt: status.AppliedAt,
		}
		if status.Source != nil {
			item.Version = status.Source.Version
			item.Source = status.Source.Path
		}
		out = append(out, item)
	}
	return out
}
