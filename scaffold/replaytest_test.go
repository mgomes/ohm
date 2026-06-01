package scaffold

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgomes/ohm/replay"
)

func TestGenerateReplayTestWritesRegressionTest(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	if err := GenerateApp(App{
		Destination: destination,
		Module:      "example.com/journal",
		Database:    DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	}); err != nil {
		t.Fatalf("GenerateApp(journal) error = %v, want nil", err)
	}

	snapshotPath := filepath.Join(destination, "tmp", "replays", "home-page.json")
	writeReplaySnapshot(t, snapshotPath, replay.Snapshot{
		Version: 1,
		Method:  http.MethodGet,
		Path:    "/",
		ExpectedResponse: &replay.ExpectedResponse{
			Status: http.StatusOK,
			Headers: map[string][]string{
				"Content-Type": {"text/html; charset=utf-8"},
			},
			Body: []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Journal</title></head><body><main><h1>Welcome to Journal</h1></main></body></html>`),
		},
	})

	root := repoRoot(t)
	t.Chdir(destination)
	result, err := GenerateReplayTest(ReplayTest{SnapshotPath: snapshotPath})
	if err != nil {
		t.Fatalf("GenerateReplayTest(home-page snapshot) error = %v, want nil", err)
	}

	wantPath := filepath.Join("internal", "replaytests", "home_page_replay_test.go")
	if result.CreatedFile != wantPath {
		t.Errorf("GenerateReplayTest(home-page snapshot) created file = %q, want %q", result.CreatedFile, wantPath)
	}

	body := readFile(t, filepath.Join(destination, result.CreatedFile))
	if !strings.Contains(body, "func TestHomePageReplay(t *testing.T)") {
		t.Errorf("GenerateReplayTest(home-page snapshot) test = %q, want derived test name", body)
	}
	if !strings.Contains(body, `"example.com/journal/internal/app"`) {
		t.Errorf("GenerateReplayTest(home-page snapshot) test = %q, want generated app import", body)
	}
	if !strings.Contains(body, `replay.Run(app.New().HTTPHandler(), snapshot)`) {
		t.Errorf("GenerateReplayTest(home-page snapshot) test = %q, want replay assertion", body)
	}

	runGo(t, destination, "mod", "edit", "-replace", "github.com/mgomes/ohm="+root)
	runGo(t, destination, "mod", "tidy")
	runGo(t, destination, "test", "./...")
}

func TestGenerateReplayTestRequiresExpectedResponse(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	if err := GenerateApp(App{
		Destination: destination,
		Module:      "example.com/journal",
		Database:    DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	}); err != nil {
		t.Fatalf("GenerateApp(journal) error = %v, want nil", err)
	}

	snapshotPath := filepath.Join(destination, "login.json")
	writeReplaySnapshot(t, snapshotPath, replay.Snapshot{
		Version: 1,
		Method:  http.MethodGet,
		Path:    "/",
	})

	t.Chdir(destination)
	_, err := GenerateReplayTest(ReplayTest{SnapshotPath: snapshotPath})
	if err == nil {
		t.Fatalf("GenerateReplayTest(snapshot without expected response) error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "missing expected_response") {
		t.Errorf("GenerateReplayTest(snapshot without expected response) error = %v, want expected_response context", err)
	}
	if _, statErr := os.Stat(filepath.Join(destination, "internal", "replaytests", "login_replay_test.go")); !os.IsNotExist(statErr) {
		t.Errorf("GenerateReplayTest(snapshot without expected response) file stat error = %v, want not exist", statErr)
	}
}

func TestGenerateReplayTestDoesNotOverwriteExistingFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "journal")
	if err := GenerateApp(App{
		Destination: destination,
		Module:      "example.com/journal",
		Database:    DatabaseSQLite,
		OhmVersion:  "v0.0.0",
	}); err != nil {
		t.Fatalf("GenerateApp(journal) error = %v, want nil", err)
	}

	snapshotPath := filepath.Join(destination, "login.json")
	writeReplaySnapshot(t, snapshotPath, replay.Snapshot{
		Version: 1,
		Method:  http.MethodGet,
		Path:    "/",
		ExpectedResponse: &replay.ExpectedResponse{
			Status: http.StatusOK,
		},
	})

	testPath := filepath.Join(destination, "internal", "replaytests", "login_replay_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(testPath), err)
	}
	if err := os.WriteFile(testPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(existing replay test) error = %v, want nil", err)
	}

	t.Chdir(destination)
	_, err := GenerateReplayTest(ReplayTest{SnapshotPath: snapshotPath})
	if err == nil {
		t.Fatalf("GenerateReplayTest(existing login replay test) error = nil, want non-nil")
	}
	if got := readFile(t, testPath); got != "keep\n" {
		t.Errorf("GenerateReplayTest(existing login replay test) file = %q, want %q", got, "keep\n")
	}
}

func writeReplaySnapshot(t *testing.T, path string, snapshot replay.Snapshot) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent(snapshot) error = %v, want nil", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}
}
