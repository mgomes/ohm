package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAppWritesSQLiteApplication(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")

	err := GenerateApp(App{
		Destination: destination,
		Module:      "example.com/journal",
		Database:    DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	})
	if err != nil {
		t.Fatalf("GenerateApp(sqlite app) error = %v, want nil", err)
	}

	wantFiles := []string{
		"go.mod",
		"cmd/journal/main.go",
		"internal/app/app.go",
		"internal/db/db.go",
		"internal/handlers/home.go",
		"internal/handlers/home_test.go",
		"migrations/README.md",
		"static/README.md",
		"justfile",
	}
	for _, file := range wantFiles {
		if _, err := os.Stat(filepath.Join(destination, filepath.FromSlash(file))); err != nil {
			t.Errorf("GenerateApp(sqlite app) file %s stat error = %v, want nil", file, err)
		}
	}

	goMod := readFile(t, filepath.Join(destination, "go.mod"))
	if !strings.Contains(goMod, "module example.com/journal") {
		t.Errorf("GenerateApp(sqlite app) go.mod = %q, want module path", goMod)
	}
	if !strings.Contains(goMod, "modernc.org/sqlite v1.51.0") {
		t.Errorf("GenerateApp(sqlite app) go.mod = %q, want sqlite driver dependency", goMod)
	}

	dbFile := readFile(t, filepath.Join(destination, "internal", "db", "db.go"))
	if !strings.Contains(dbFile, `default:"file:development.db"`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/db.go = %q, want sqlite default database URL", dbFile)
	}
}

func TestGenerateAppWritesPostgresApplicationByDefault(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "ledger")

	err := GenerateApp(App{Destination: destination, OhmVersion: "v0.0.0"})
	if err != nil {
		t.Fatalf("GenerateApp(default app) error = %v, want nil", err)
	}

	goMod := readFile(t, filepath.Join(destination, "go.mod"))
	if !strings.Contains(goMod, "github.com/jackc/pgx/v5 v5.9.2") {
		t.Errorf("GenerateApp(default app) go.mod = %q, want pgx dependency", goMod)
	}

	dbFile := readFile(t, filepath.Join(destination, "internal", "db", "db.go"))
	if !strings.Contains(dbFile, `env:"DATABASE_URL,required"`) {
		t.Errorf("GenerateApp(default app) internal/db/db.go = %q, want required database URL", dbFile)
	}
	if !strings.Contains(dbFile, "migrate.DialectPostgres") {
		t.Errorf("GenerateApp(default app) internal/db/db.go = %q, want Postgres migration dialect", dbFile)
	}
}

func TestGenerateAppRejectsUnsupportedDatabase(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "bad-db")
	err := GenerateApp(App{
		Destination: destination,
		Database:    Database("mysql"),
		OhmVersion:  "v0.0.0",
	})
	if err == nil {
		t.Fatalf("GenerateApp(unsupported database) error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), ErrUnsupportedDatabase.Error()) {
		t.Errorf("GenerateApp(unsupported database) error = %v, want %v", err, ErrUnsupportedDatabase)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Errorf("GenerateApp(unsupported database) destination stat error = %v, want not exist", statErr)
	}
}

func TestGenerateAppDoesNotOverwriteNonEmptyDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", destination, err)
	}
	if err := os.WriteFile(filepath.Join(destination, "README.md"), []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(existing README) error = %v, want nil", err)
	}

	err := GenerateApp(App{Destination: destination, OhmVersion: "v0.0.0"})
	if err == nil {
		t.Fatalf("GenerateApp(non-empty destination) error = nil, want non-nil")
	}
}

func TestGeneratedSQLiteApplicationBuilds(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "smoke")
	err := GenerateApp(App{
		Destination: destination,
		Module:      "example.com/smoke",
		Database:    DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	})
	if err != nil {
		t.Fatalf("GenerateApp(smoke app) error = %v, want nil", err)
	}

	root := repoRoot(t)
	runGo(t, destination, "mod", "edit", "-replace", "github.com/mgomes/ohm="+root)
	runGo(t, destination, "mod", "tidy")
	runGo(t, destination, "test", "./...")
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	return string(data)
}

func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("filepath.Abs(%q) error = %v, want nil", "..", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %q go.mod stat error = %v, want nil", root, err)
	}
	return root
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s in %s error = %v\n%s", strings.Join(args, " "), dir, err, output)
	}
}
