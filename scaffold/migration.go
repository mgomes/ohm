package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

	version, err := nextMigrationVersion(dir, now())
	if err != nil {
		return "", err
	}

	path = filepath.Join(dir, strconv.FormatInt(version, 10)+"_"+cfg.Name+".sql")
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

func nextMigrationVersion(dir string, now time.Time) (int64, error) {
	used, err := existingMigrationVersions(dir)
	if err != nil {
		return 0, err
	}

	now = now.UTC()
	for {
		version, err := migrationVersion(now)
		if err != nil {
			return 0, err
		}
		if _, ok := used[version]; !ok {
			return version, nil
		}
		now = now.Add(time.Second)
	}
}

func migrationVersion(t time.Time) (int64, error) {
	version, err := strconv.ParseInt(t.Format("20060102150405"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("build migration version: %w", err)
	}
	return version, nil
}

func existingMigrationVersions(dir string) (map[int64]struct{}, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory %q: %w", dir, err)
	}

	versions := make(map[int64]struct{})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			continue
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			continue
		}
		versions[version] = struct{}{}
	}
	return versions, nil
}

const migrationTemplate = `-- +goose Up
-- Write migration SQL here.

-- +goose Down
-- Write rollback SQL here.
`
