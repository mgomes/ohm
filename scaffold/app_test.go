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
		".env.development.example",
		".env.example",
		".env.test.example",
		"go.mod",
		"cmd/journal/main.go",
		"internal/app/app.go",
		"internal/app/app_test.go",
		"internal/db/command.go",
		"internal/db/command_test.go",
		"internal/db/db.go",
		"internal/db/db_test.go",
		"internal/db/dbgen/README.md",
		"internal/db/dbtest/dbtest.go",
		"internal/db/dbtest/dbtest_test.go",
		"internal/db/migrate_test.go",
		"internal/db/seeds.go",
		"internal/handlers/home.go",
		"internal/handlers/home_test.go",
		"internal/views/components/README.md",
		"internal/views/layouts/application.templ",
		"internal/views/layouts/application_templ.go",
		"internal/views/pages/home.templ",
		"internal/views/pages/home_test.go",
		"internal/views/pages/home_templ.go",
		"migrations/README.md",
		"queries/health.sql",
		"sqlc.yaml",
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
	if !strings.Contains(goMod, "github.com/a-h/templ v0.3.1020") {
		t.Errorf("GenerateApp(sqlite app) go.mod = %q, want templ dependency", goMod)
	}

	envExample := readFile(t, filepath.Join(destination, ".env.example"))
	if strings.Contains(envExample, "DATABASE_URL=") {
		t.Errorf("GenerateApp(sqlite app) .env.example = %q, want no shared database URL", envExample)
	}

	developmentEnvExample := readFile(t, filepath.Join(destination, ".env.development.example"))
	if !strings.Contains(developmentEnvExample, "DATABASE_URL=file:development.db") {
		t.Errorf("GenerateApp(sqlite app) .env.development.example = %q, want sqlite database URL", developmentEnvExample)
	}

	testEnvExample := readFile(t, filepath.Join(destination, ".env.test.example"))
	if !strings.Contains(testEnvExample, "DATABASE_URL=file:test.db") {
		t.Errorf("GenerateApp(sqlite app) .env.test.example = %q, want sqlite test database URL", testEnvExample)
	}

	dbFile := readFile(t, filepath.Join(destination, "internal", "db", "db.go"))
	if !strings.Contains(dbFile, `default:"file:development.db"`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/db.go = %q, want sqlite default database URL", dbFile)
	}
	if !strings.Contains(dbFile, `func withConfiguredDB(ctx context.Context, fn func(*sql.DB) error)`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/db.go = %q, want configured database helper", dbFile)
	}

	dbCommand := readFile(t, filepath.Join(destination, "internal", "db", "command.go"))
	if !strings.Contains(dbCommand, `Name:    "db"`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/command.go = %q, want db command", dbCommand)
	}
	if !strings.Contains(dbCommand, `case "seed":`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/command.go = %q, want seed subcommand", dbCommand)
	}
	dbCommandTest := readFile(t, filepath.Join(destination, "internal", "db", "command_test.go"))
	if !strings.Contains(dbCommandTest, `databaseURL := "file:" + filepath.Join(t.TempDir(), "seed.db")`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/command_test.go = %q, want local SQLite seed test", dbCommandTest)
	}

	dbTest := readFile(t, filepath.Join(destination, "internal", "db", "db_test.go"))
	if !strings.Contains(dbTest, `t.Setenv("DATABASE_URL", "file:test.db")`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/db_test.go = %q, want test database URL", dbTest)
	}

	dbTestHelper := readFile(t, filepath.Join(destination, "internal", "db", "dbtest", "dbtest.go"))
	if !strings.Contains(dbTestHelper, `databaseURL := "file:" + filepath.Join(t.TempDir(), "test.db")`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/dbtest/dbtest.go = %q, want isolated SQLite database helper", dbTestHelper)
	}
	if !strings.Contains(dbTestHelper, `"example.com/journal/internal/db"`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/dbtest/dbtest.go = %q, want generated db package import", dbTestHelper)
	}

	migrateTest := readFile(t, filepath.Join(destination, "internal", "db", "migrate_test.go"))
	if !strings.Contains(migrateTest, `databaseURL := "file:" + filepath.Join(t.TempDir(), "migrate.db")`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/migrate_test.go = %q, want local SQLite migration test", migrateTest)
	}
	if !strings.Contains(migrateTest, `command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"up"})`) {
		t.Errorf("GenerateApp(sqlite app) internal/db/migrate_test.go = %q, want migrate up command assertion", migrateTest)
	}

	justfile := readFile(t, filepath.Join(destination, "justfile"))
	if !strings.Contains(justfile, "test-unit:") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want test-unit task", justfile)
	}
	if !strings.Contains(justfile, "test-integration:") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want test-integration task", justfile)
	}
	if !strings.Contains(justfile, "generate: templ sqlc") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want generate task", justfile)
	}
	if !strings.Contains(justfile, "test: generate") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want test task to regenerate code", justfile)
	}
	if !strings.Contains(justfile, "check: generate fmt-check tidy vet") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want check task to run closed-loop checks", justfile)
	}
	if !strings.Contains(justfile, "migrate-reset:") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want migrate-reset task", justfile)
	}
	if !strings.Contains(justfile, "db-seed:") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want db seed task", justfile)
	}
	if !strings.Contains(justfile, "db-reset: migrate-reset migrate-up") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want db-reset task", justfile)
	}
	if !strings.Contains(justfile, "test-db-setup:") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want test database setup task", justfile)
	}
	if !strings.Contains(justfile, "github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want pinned sqlc generation task", justfile)
	}
	if !strings.Contains(justfile, "github.com/a-h/templ/cmd/templ@v0.3.1020 generate") {
		t.Errorf("GenerateApp(sqlite app) justfile = %q, want pinned templ generation task", justfile)
	}

	sqlcConfig := readFile(t, filepath.Join(destination, "sqlc.yaml"))
	if !strings.Contains(sqlcConfig, `engine: "sqlite"`) {
		t.Errorf("GenerateApp(sqlite app) sqlc.yaml = %q, want SQLite engine", sqlcConfig)
	}
	if !strings.Contains(sqlcConfig, `out: "internal/db/dbgen"`) {
		t.Errorf("GenerateApp(sqlite app) sqlc.yaml = %q, want generated query output package", sqlcConfig)
	}

	appFile := readFile(t, filepath.Join(destination, "internal", "app", "app.go"))
	mainFile := readFile(t, filepath.Join(destination, "cmd", "journal", "main.go"))
	if !strings.Contains(mainFile, "db.Command()") {
		t.Errorf("GenerateApp(sqlite app) cmd/journal/main.go = %q, want db command registered", mainFile)
	}

	if !strings.Contains(appFile, "slog.NewJSONHandler(os.Stderr, nil)") {
		t.Errorf("GenerateApp(sqlite app) internal/app/app.go = %q, want request logs on stderr", appFile)
	}
	if !strings.Contains(appFile, "ohm.RequestLogger(logger), ohm.Recoverer(logger)") {
		t.Errorf("GenerateApp(sqlite app) internal/app/app.go = %q, want recovery middleware after request logger", appFile)
	}

	homeFile := readFile(t, filepath.Join(destination, "internal", "handlers", "home.go"))
	if !strings.Contains(homeFile, `"example.com/journal/internal/views/pages"`) {
		t.Errorf("GenerateApp(sqlite app) internal/handlers/home.go = %q, want page view import", homeFile)
	}
	if !strings.Contains(homeFile, `return req.HTML(http.StatusOK, pages.Home("Journal"))`) {
		t.Errorf("GenerateApp(sqlite app) internal/handlers/home.go = %q, want HTML view rendering", homeFile)
	}

	homeView := readFile(t, filepath.Join(destination, "internal", "views", "pages", "home.templ"))
	if !strings.Contains(homeView, `@layouts.Application(title)`) {
		t.Errorf("GenerateApp(sqlite app) internal/views/pages/home.templ = %q, want application layout", homeView)
	}

	appTest := readFile(t, filepath.Join(destination, "internal", "app", "app_test.go"))
	if !strings.Contains(appTest, `hasRoute(routes, "GET", "/")`) {
		t.Errorf("GenerateApp(sqlite app) internal/app/app_test.go = %q, want home route smoke test", appTest)
	}

	viewTest := readFile(t, filepath.Join(destination, "internal", "views", "pages", "home_test.go"))
	if !strings.Contains(viewTest, `<h1>Welcome to Journal</h1>`) {
		t.Errorf("GenerateApp(sqlite app) internal/views/pages/home_test.go = %q, want rendered view assertion", viewTest)
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

	developmentEnvExample := readFile(t, filepath.Join(destination, ".env.development.example"))
	if !strings.Contains(developmentEnvExample, "DATABASE_URL=postgres://localhost/ledger_development?sslmode=disable") {
		t.Errorf("GenerateApp(default app) .env.development.example = %q, want Postgres database URL", developmentEnvExample)
	}

	testEnvExample := readFile(t, filepath.Join(destination, ".env.test.example"))
	if !strings.Contains(testEnvExample, "DATABASE_URL=postgres://localhost/test?sslmode=disable") {
		t.Errorf("GenerateApp(default app) .env.test.example = %q, want Postgres test database URL", testEnvExample)
	}

	dbTest := readFile(t, filepath.Join(destination, "internal", "db", "db_test.go"))
	if !strings.Contains(dbTest, `t.Setenv("DATABASE_URL", "postgres://localhost/test?sslmode=disable")`) {
		t.Errorf("GenerateApp(default app) internal/db/db_test.go = %q, want Postgres test database URL", dbTest)
	}

	dbTestHelper := readFile(t, filepath.Join(destination, "internal", "db", "dbtest", "dbtest.go"))
	if !strings.Contains(dbTestHelper, `t.Skip("DATABASE_URL is required for Postgres database tests")`) {
		t.Errorf("GenerateApp(default app) internal/db/dbtest/dbtest.go = %q, want explicit Postgres test database skip", dbTestHelper)
	}

	dbCommandTest := readFile(t, filepath.Join(destination, "internal", "db", "command_test.go"))
	if !strings.Contains(dbCommandTest, `t.Skip("db seed integration test requires a configured Postgres test database")`) {
		t.Errorf("GenerateApp(default app) internal/db/command_test.go = %q, want explicit Postgres seed test skip", dbCommandTest)
	}
	migrateTest := readFile(t, filepath.Join(destination, "internal", "db", "migrate_test.go"))
	if !strings.Contains(migrateTest, `t.Skip("migration integration test requires a configured Postgres test database")`) {
		t.Errorf("GenerateApp(default app) internal/db/migrate_test.go = %q, want explicit Postgres migration test skip", migrateTest)
	}

	sqlcConfig := readFile(t, filepath.Join(destination, "sqlc.yaml"))
	if !strings.Contains(sqlcConfig, `engine: "postgresql"`) {
		t.Errorf("GenerateApp(default app) sqlc.yaml = %q, want Postgres engine", sqlcConfig)
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
	runGo(t, destination, "run", "github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0", "generate")
	runGo(t, destination, "run", "github.com/a-h/templ/cmd/templ@v0.3.1020", "generate")
	runGo(t, destination, "run", "./cmd/smoke", "db", "seed")
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
