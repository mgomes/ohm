package env

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDefaultFiles(t *testing.T) {
	got := DefaultFiles("")
	want := []string{".env", ".env.development", ".env.local", ".env.development.local"}

	if !slices.Equal(got, want) {
		t.Errorf("DefaultFiles(\"\") = %v, want %v", got, want)
	}
}

func TestLoaderLoadMergesFilesInOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "DATABASE_URL=base\nSHARED=base\n")
	writeFile(t, filepath.Join(dir, ".env.development"), "SHARED=development\n")
	writeFile(t, filepath.Join(dir, ".env.local"), "LOCAL=true\n")
	writeFile(t, filepath.Join(dir, ".env.development.local"), "SHARED=local-development\n")

	got, err := Loader{Dir: dir, Environment: "development"}.Load()
	if err != nil {
		t.Fatalf("Loader.Load() error = %v, want nil", err)
	}

	want := map[string]string{
		"DATABASE_URL": "base",
		"SHARED":       "local-development",
		"LOCAL":        "true",
	}
	if !maps.Equal(got, want) {
		t.Errorf("Loader.Load() = %v, want %v", got, want)
	}
}

func TestLoaderLoadEmptyFilesSkipsDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "DATABASE_URL=file\n")

	got, err := Loader{Dir: dir, Files: []string{}}.Load()
	if err != nil {
		t.Fatalf("Loader.Load() error = %v, want nil", err)
	}

	if len(got) != 0 {
		t.Errorf("Loader.Load() = %v, want empty map", got)
	}
}

func TestLoaderApplyDoesNotOverwriteExistingValues(t *testing.T) {
	existing := map[string]string{"EXISTING": "process"}
	set := map[string]string{}
	loader := Loader{
		LookupEnv: func(key string) (string, bool) {
			value, ok := existing[key]
			return value, ok
		},
		SetEnv: func(key string, value string) error {
			set[key] = value
			return nil
		},
	}

	err := loader.Apply(map[string]string{
		"EXISTING": "file",
		"NEW":      "loaded",
	})
	if err != nil {
		t.Fatalf("Loader.Apply(%v) error = %v, want nil", existing, err)
	}

	want := map[string]string{"NEW": "loaded"}
	if !maps.Equal(set, want) {
		t.Errorf("Loader.Apply(%v) set values = %v, want %v", existing, set, want)
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}
