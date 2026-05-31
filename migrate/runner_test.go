package migrate

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"
)

func TestNewValidatesInputs(t *testing.T) {
	_, err := New(nil, DialectPostgres, fstest.MapFS{})
	if err == nil {
		t.Fatalf("New(nil, %q, fs) error = nil, want non-nil", DialectPostgres)
	}

	_, err = New(&sql.DB{}, DialectPostgres, nil)
	if err == nil {
		t.Fatalf("New(db, %q, nil) error = nil, want non-nil", DialectPostgres)
	}
}

func TestNewAllowsEmptyMigrationSet(t *testing.T) {
	runner, err := New(&sql.DB{}, DialectSQLite, fstest.MapFS{})
	if err != nil {
		t.Fatalf("New(db, %q, emptyFS) error = %v, want nil", DialectSQLite, err)
	}

	upResults, err := runner.Up(context.Background())
	if err != nil {
		t.Fatalf("runner.Up(ctx) error = %v, want nil", err)
	}
	if len(upResults) != 0 {
		t.Errorf("runner.Up(ctx) result count = %d, want 0", len(upResults))
	}

	statuses, err := runner.Status(context.Background())
	if err != nil {
		t.Fatalf("runner.Status(ctx) error = %v, want nil", err)
	}
	if len(statuses) != 0 {
		t.Errorf("runner.Status(ctx) status count = %d, want 0", len(statuses))
	}

	downResult, err := runner.Down(context.Background())
	if err != nil {
		t.Fatalf("runner.Down(ctx) error = %v, want nil", err)
	}
	if !downResult.Empty {
		t.Errorf("runner.Down(ctx) Empty = %t, want true", downResult.Empty)
	}
}

func TestGooseDialect(t *testing.T) {
	tests := []struct {
		dialect Dialect
		want    string
	}{
		{dialect: DialectPostgres, want: "postgres"},
		{dialect: DialectSQLite, want: "sqlite3"},
		{dialect: Dialect("mysql"), want: "mysql"},
	}

	for _, tt := range tests {
		got := string(gooseDialect(tt.dialect))
		if got != tt.want {
			t.Errorf("gooseDialect(%q) = %q, want %q", tt.dialect, got, tt.want)
		}
	}
}
