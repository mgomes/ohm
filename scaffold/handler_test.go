package scaffold

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGenerateHandlerWritesFilesAndRegistersRoute(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	if err := GenerateApp(App{
		Destination: destination,
		Module:      "example.com/journal",
		Database:    DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	}); err != nil {
		t.Fatalf("GenerateApp(journal) error = %v, want nil", err)
	}

	handlersDir := filepath.Join(destination, "internal", "handlers")
	result, err := GenerateHandler(Handler{Name: "Posts", Dir: handlersDir})
	if err != nil {
		t.Fatalf("GenerateHandler(Posts) error = %v, want nil", err)
	}

	wantCreated := []string{
		filepath.Join(handlersDir, "posts.go"),
		filepath.Join(handlersDir, "posts_test.go"),
	}
	if !slices.Equal(result.CreatedFiles, wantCreated) {
		t.Errorf("GenerateHandler(Posts) created files = %v, want %v", result.CreatedFiles, wantCreated)
	}
	if result.RegisterFile != filepath.Join(handlersDir, "routes.go") {
		t.Errorf("GenerateHandler(Posts) register file = %q, want %q", result.RegisterFile, filepath.Join(handlersDir, "routes.go"))
	}
	if !result.RegisterUpdated {
		t.Errorf("GenerateHandler(Posts) register updated = false, want true")
	}
	if result.RoutePath != "/posts" {
		t.Errorf("GenerateHandler(Posts) route path = %q, want %q", result.RoutePath, "/posts")
	}

	routes := readFile(t, filepath.Join(handlersDir, "routes.go"))
	if !strings.Contains(routes, `application.Get("/posts", PostsIndex)`) {
		t.Errorf("GenerateHandler(Posts) routes.go = %q, want posts route registration", routes)
	}

	posts := readFile(t, filepath.Join(handlersDir, "posts.go"))
	if !strings.Contains(posts, "func PostsIndex(req *ohm.Request) error") {
		t.Errorf("GenerateHandler(Posts) posts.go = %q, want PostsIndex handler", posts)
	}

	root := repoRoot(t)
	runGo(t, destination, "mod", "edit", "-replace", "github.com/mgomes/ohm="+root)
	runGo(t, destination, "mod", "tidy")
	runGo(t, destination, "test", "./...")
}

func TestGenerateHandlerUsesRegisterParameterName(t *testing.T) {
	dir := t.TempDir()
	registerPath := filepath.Join(dir, "routes.go")
	if err := os.WriteFile(registerPath, []byte(`package handlers

import "github.com/mgomes/ohm"

func Register(app *ohm.App) {
	app.Get("/", Home)
}

func Home(req *ohm.Request) error {
	return nil
}
`), 0o644); err != nil {
		t.Fatalf("os.WriteFile(register file) error = %v, want nil", err)
	}

	_, err := GenerateHandler(Handler{Name: "Posts", Dir: dir})
	if err != nil {
		t.Fatalf("GenerateHandler(Posts) error = %v, want nil", err)
	}

	body := readFile(t, registerPath)
	if !strings.Contains(body, `app.Get("/posts", PostsIndex)`) {
		t.Errorf("GenerateHandler(Posts) routes.go = %q, want route registered through app parameter", body)
	}
}

func TestGenerateHandlerRejectsInvalidName(t *testing.T) {
	_, err := GenerateHandler(Handler{Name: "Posts!", Dir: t.TempDir()})
	if err == nil {
		t.Fatalf("GenerateHandler(invalid name) error = nil, want non-nil")
	}
}

func TestGenerateHandlerDoesNotOverwriteExistingFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	if err := GenerateApp(App{
		Destination: destination,
		Module:      "example.com/journal",
		Database:    DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	}); err != nil {
		t.Fatalf("GenerateApp(journal) error = %v, want nil", err)
	}

	handlersDir := filepath.Join(destination, "internal", "handlers")
	existingPath := filepath.Join(handlersDir, "posts.go")
	if err := os.WriteFile(existingPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(existing posts.go) error = %v, want nil", err)
	}

	_, err := GenerateHandler(Handler{Name: "Posts", Dir: handlersDir})
	if err == nil {
		t.Fatalf("GenerateHandler(existing Posts) error = nil, want non-nil")
	}
	if got := readFile(t, existingPath); got != "keep\n" {
		t.Errorf("GenerateHandler(existing Posts) posts.go = %q, want %q", got, "keep\n")
	}
	routes := readFile(t, filepath.Join(handlersDir, "routes.go"))
	if strings.Contains(routes, `application.Get("/posts", PostsIndex)`) {
		t.Errorf("GenerateHandler(existing Posts) routes.go = %q, want no posts route registration", routes)
	}
	if _, statErr := os.Stat(filepath.Join(handlersDir, "posts_test.go")); !os.IsNotExist(statErr) {
		t.Errorf("GenerateHandler(existing Posts) posts_test.go stat error = %v, want not exist", statErr)
	}
}

func TestGenerateHandlerRejectsUnnamedRegisterParameter(t *testing.T) {
	dir := t.TempDir()
	registerPath := filepath.Join(dir, "routes.go")
	if err := os.WriteFile(registerPath, []byte(`package handlers

import "github.com/mgomes/ohm"

func Register(*ohm.App) {
}
`), 0o644); err != nil {
		t.Fatalf("os.WriteFile(register file) error = %v, want nil", err)
	}

	_, err := GenerateHandler(Handler{Name: "Posts", Dir: dir})
	if err == nil {
		t.Fatalf("GenerateHandler(unnamed register parameter) error = nil, want non-nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "posts.go")); !os.IsNotExist(statErr) {
		t.Errorf("GenerateHandler(unnamed register parameter) posts.go stat error = %v, want not exist", statErr)
	}
	if got := readFile(t, registerPath); !strings.Contains(got, "func Register(*ohm.App)") {
		t.Errorf("GenerateHandler(unnamed register parameter) routes.go = %q, want original register signature", got)
	}
}
