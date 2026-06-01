package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const defaultMigrationsDir = "migrations"

var migrationNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Migration describes a generated goose migration file.
type Migration struct {
	Name string
	Dir  string
	Now  func() time.Time
}

// GenerateMigration writes a timestamped goose migration file and returns its path.
func GenerateMigration(cfg Migration) (path string, err error) {
	if cfg.Name == "" {
		return "", fmt.Errorf("migration name is required")
	}
	if !migrationNamePattern.MatchString(cfg.Name) {
		return "", fmt.Errorf("migration name %q must start with a lowercase letter and contain only lowercase letters, digits, or underscores", cfg.Name)
	}

	dir := cfg.Dir
	if dir == "" {
		dir = defaultMigrationsDir
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create migrations directory %q: %w", dir, err)
	}

	path = filepath.Join(dir, now().UTC().Format("20060102150405")+"_"+cfg.Name+".sql")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create migration %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close migration %q: %w", path, closeErr)
		}
	}()

	if _, err := file.WriteString(migrationTemplate); err != nil {
		return "", fmt.Errorf("write migration %q: %w", path, err)
	}
	return path, nil
}

const migrationTemplate = `-- +goose Up
-- Write migration SQL here.

-- +goose Down
-- Write rollback SQL here.
`
