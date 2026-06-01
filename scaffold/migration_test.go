package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateMigrationWritesTimestampedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "migrations")
	path, err := GenerateMigration(Migration{
		Name: "create_posts",
		Dir:  dir,
		Now: func() time.Time {
			return time.Date(2026, 6, 1, 2, 3, 4, 0, time.FixedZone("test", -4*60*60))
		},
	})
	if err != nil {
		t.Fatalf("GenerateMigration(create_posts) error = %v, want nil", err)
	}

	wantPath := filepath.Join(dir, "20260601060304_create_posts.sql")
	if path != wantPath {
		t.Errorf("GenerateMigration(create_posts) path = %q, want %q", path, wantPath)
	}

	body := readFile(t, path)
	if !strings.Contains(body, "-- +goose Up") {
		t.Errorf("GenerateMigration(create_posts) body = %q, want goose up annotation", body)
	}
	if !strings.Contains(body, "-- +goose Down") {
		t.Errorf("GenerateMigration(create_posts) body = %q, want goose down annotation", body)
	}
}

func TestGenerateMigrationRejectsInvalidName(t *testing.T) {
	_, err := GenerateMigration(Migration{
		Name: "CreatePosts",
		Dir:  t.TempDir(),
	})
	if err == nil {
		t.Fatalf("GenerateMigration(invalid name) error = nil, want non-nil")
	}
}

func TestGenerateMigrationDoesNotOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	now := func() time.Time {
		return time.Date(2026, 6, 1, 2, 3, 4, 0, time.UTC)
	}
	path := filepath.Join(dir, "20260601020304_create_posts.sql")
	if err := os.WriteFile(path, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(existing migration) error = %v, want nil", err)
	}

	_, err := GenerateMigration(Migration{
		Name: "create_posts",
		Dir:  dir,
		Now:  now,
	})
	if err == nil {
		t.Fatalf("GenerateMigration(existing migration) error = nil, want non-nil")
	}
	if got := readFile(t, path); got != "keep\n" {
		t.Errorf("GenerateMigration(existing migration) existing file = %q, want %q", got, "keep\n")
	}
}
