package migrate

import (
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
