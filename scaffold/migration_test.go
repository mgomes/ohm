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

	gotPath, err := GenerateMigration(Migration{
		Name: "create_posts",
		Dir:  dir,
		Now:  now,
	})
	if err != nil {
		t.Fatalf("GenerateMigration(existing migration) error = %v, want nil", err)
	}
	wantPath := filepath.Join(dir, "20260601020305_create_posts.sql")
	if gotPath != wantPath {
		t.Errorf("GenerateMigration(existing migration) path = %q, want %q", gotPath, wantPath)
	}
	if got := readFile(t, path); got != "keep\n" {
		t.Errorf("GenerateMigration(existing migration) existing file = %q, want %q", got, "keep\n")
	}
}

func TestGenerateMigrationAvoidsDuplicateVersionsWithinSameSecond(t *testing.T) {
	dir := t.TempDir()
	now := func() time.Time {
		return time.Date(2026, 6, 1, 2, 3, 4, 0, time.UTC)
	}

	first, err := GenerateMigration(Migration{
		Name: "create_posts",
		Dir:  dir,
		Now:  now,
	})
	if err != nil {
		t.Fatalf("GenerateMigration(create_posts) error = %v, want nil", err)
	}
	second, err := GenerateMigration(Migration{
		Name: "add_posts_index",
		Dir:  dir,
		Now:  now,
	})
	if err != nil {
		t.Fatalf("GenerateMigration(add_posts_index) error = %v, want nil", err)
	}

	wantFirst := filepath.Join(dir, "20260601020304_create_posts.sql")
	wantSecond := filepath.Join(dir, "20260601020305_add_posts_index.sql")
	if first != wantFirst {
		t.Errorf("GenerateMigration(create_posts) path = %q, want %q", first, wantFirst)
	}
	if second != wantSecond {
		t.Errorf("GenerateMigration(add_posts_index) path = %q, want %q", second, wantSecond)
	}
}

func TestGenerateMigrationCollisionAdvancesByClockSecond(t *testing.T) {
	dir := t.TempDir()
	now := func() time.Time {
		return time.Date(2026, 6, 1, 2, 3, 59, 0, time.UTC)
	}
	existingPath := filepath.Join(dir, "20260601020359_create_posts.sql")
	if err := os.WriteFile(existingPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(existing migration) error = %v, want nil", err)
	}

	path, err := GenerateMigration(Migration{
		Name: "add_posts_index",
		Dir:  dir,
		Now:  now,
	})
	if err != nil {
		t.Fatalf("GenerateMigration(add_posts_index) error = %v, want nil", err)
	}

	wantPath := filepath.Join(dir, "20260601020400_add_posts_index.sql")
	if path != wantPath {
		t.Errorf("GenerateMigration(add_posts_index) path = %q, want %q", path, wantPath)
	}
}
