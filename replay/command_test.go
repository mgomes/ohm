package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgomes/ohm"
	"github.com/mgomes/ohm/cli"
)

type replayCommandContextKey struct{}

func TestCommandReplaysSnapshotFile(t *testing.T) {
	app := ohm.New()
	app.Get("/posts/{id}", func(req *ohm.Request) error {
		req.PlainText(http.StatusOK, "post "+req.Param("id"))
		return nil
	})

	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/posts/42",
	})

	var stdout bytes.Buffer
	command := Command(app)
	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{path})
	if err != nil {
		t.Fatalf("Command(app).Run(ctx, io, %v) error = %v, want nil", []string{path}, err)
	}

	want := "Status: 200 OK\n\npost 42"
	if stdout.String() != want {
		t.Errorf("Command(app).Run(ctx, io, %v) stdout = %q, want %q", []string{path}, stdout.String(), want)
	}
}

func TestCommandWritesExpectedResponse(t *testing.T) {
	app := ohm.New()
	app.Get("/posts/{id}", func(req *ohm.Request) error {
		req.PlainText(http.StatusCreated, "post "+req.Param("id"))
		return nil
	})

	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/posts/42",
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := Command(app)
	err := command.Run(context.Background(), cli.IO{Stdout: &stdout, Stderr: &stderr}, []string{"--write-expected", path})
	if err != nil {
		t.Fatalf("Command(app).Run(ctx, io, %v) error = %v, want nil", []string{"--write-expected", path}, err)
	}

	wantStdout := "Status: 201 Created\n\npost 42"
	if stdout.String() != wantStdout {
		t.Errorf("Command(app).Run(ctx, io, --write-expected) stdout = %q, want %q", stdout.String(), wantStdout)
	}
	if stderr.String() != "Updated "+path+"\n" {
		t.Errorf("Command(app).Run(ctx, io, --write-expected) stderr = %q, want update message", stderr.String())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", data, err)
	}
	if snapshot.ExpectedResponse == nil {
		t.Fatalf("snapshot.ExpectedResponse = nil, want response expectation")
	}
	if snapshot.ExpectedResponse.Status != http.StatusCreated {
		t.Errorf("snapshot.ExpectedResponse.Status = %d, want %d", snapshot.ExpectedResponse.Status, http.StatusCreated)
	}
	if !snapshot.ExpectedResponse.BodyOmitted {
		t.Errorf("snapshot.ExpectedResponse.BodyOmitted = false, want true")
	}
	if len(snapshot.ExpectedResponse.Body) != 0 {
		t.Errorf("snapshot.ExpectedResponse.Body = %q, want empty", snapshot.ExpectedResponse.Body)
	}
	if snapshot.ExpectedResponse.Headers["Content-Type"][0] != "text/plain; charset=utf-8" {
		t.Errorf("snapshot.ExpectedResponse.Headers[Content-Type] = %v, want text/plain", snapshot.ExpectedResponse.Headers["Content-Type"])
	}
}

func TestCommandWritesScrubbedExpectedResponseBodyWithExplicitOptIn(t *testing.T) {
	app := ohm.New()
	app.Get("/login", func(req *ohm.Request) error {
		req.JSON(http.StatusCreated, map[string]string{
			"username": "ada",
			"token":    "secret",
		})
		return nil
	})

	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/login",
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := Command(app)
	err := command.Run(context.Background(), cli.IO{Stdout: &stdout, Stderr: &stderr}, []string{"--write-expected", "--write-expected-body", path})
	if err != nil {
		t.Fatalf("Command(app).Run(ctx, io, %v) error = %v, want nil", []string{"--write-expected", "--write-expected-body", path}, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", data, err)
	}
	if snapshot.ExpectedResponse == nil {
		t.Fatalf("snapshot.ExpectedResponse = nil, want response expectation")
	}
	if snapshot.ExpectedResponse.BodyOmitted {
		t.Fatalf("snapshot.ExpectedResponse.BodyOmitted = true, want false")
	}
	if bytes.Contains(snapshot.ExpectedResponse.Body, []byte("secret")) {
		t.Fatalf("snapshot.ExpectedResponse.Body = %q, want sensitive values redacted", snapshot.ExpectedResponse.Body)
	}
	var body map[string]string
	if err := json.Unmarshal(snapshot.ExpectedResponse.Body, &body); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v, want nil", snapshot.ExpectedResponse.Body, err)
	}
	if body["username"] != "ada" {
		t.Errorf("snapshot.ExpectedResponse.Body username = %q, want %q", body["username"], "ada")
	}
	if body["token"] != "[REDACTED]" {
		t.Errorf("snapshot.ExpectedResponse.Body token = %q, want [REDACTED]", body["token"])
	}
}

func TestCommandWarnsAboutUncontrolledBoundaries(t *testing.T) {
	app := ohm.New()
	app.Get("/posts/{id}", func(req *ohm.Request) error {
		req.PlainText(http.StatusOK, "post "+req.Param("id"))
		return nil
	})

	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/posts/42",
		UncontrolledBoundaries: []Boundary{
			BoundaryClock,
			BoundaryDatabaseState,
			BoundaryClock,
		},
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := Command(app)
	err := command.Run(context.Background(), cli.IO{Stdout: &stdout, Stderr: &stderr}, []string{path})
	if err != nil {
		t.Fatalf("Command(app).Run(ctx, io, %v) error = %v, want nil", []string{path}, err)
	}

	if stdout.String() != "Status: 200 OK\n\npost 42" {
		t.Errorf("Command(app).Run(ctx, io, %v) stdout = %q, want replay output", []string{path}, stdout.String())
	}
	wantStderr := "Warning: replay snapshot records uncontrolled clock boundary; results may not be deterministic.\n" +
		"Warning: replay snapshot records uncontrolled database_state boundary; results may not be deterministic.\n"
	if stderr.String() != wantStderr {
		t.Errorf("Command(app).Run(ctx, io, %v) stderr = %q, want determinism warning", []string{path}, stderr.String())
	}
}

func TestCommandRejectsUnknownBoundaryMetadata(t *testing.T) {
	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/posts/42",
		ControlledBoundaries: []Boundary{
			Boundary("network"),
		},
	})

	command := Command(http.NewServeMux())
	err := command.Run(context.Background(), cli.IO{}, []string{path})
	if err == nil {
		t.Fatalf("Command(handler).Run(ctx, io, %v) error = nil, want non-nil", []string{path})
	}
	if !errors.Is(err, ErrUnknownBoundary) {
		t.Errorf("Command(handler).Run(ctx, io, %v) error = %v, want ErrUnknownBoundary", []string{path}, err)
	}
}

func TestCommandPropagatesContextToReplayRequest(t *testing.T) {
	app := ohm.New()
	app.Get("/context", func(req *ohm.Request) error {
		value, _ := req.HTTPRequest().Context().Value(replayCommandContextKey{}).(string)
		req.PlainText(http.StatusOK, value)
		return nil
	})

	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/context",
	})

	var stdout bytes.Buffer
	command := Command(app)
	ctx := context.WithValue(context.Background(), replayCommandContextKey{}, "from-context")
	err := command.Run(ctx, cli.IO{Stdout: &stdout}, []string{path})
	if err != nil {
		t.Fatalf("Command(app).Run(ctx, io, %v) error = %v, want nil", []string{path}, err)
	}

	want := "Status: 200 OK\n\nfrom-context"
	if stdout.String() != want {
		t.Errorf("Command(app).Run(ctx, io, %v) stdout = %q, want %q", []string{path}, stdout.String(), want)
	}
}

func TestCommandReportsImplicitOKStatus(t *testing.T) {
	app := ohm.New()
	app.Get("/empty", func(*ohm.Request) error {
		return nil
	})

	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/empty",
	})

	var stdout bytes.Buffer
	command := Command(app)
	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{path})
	if err != nil {
		t.Fatalf("Command(app).Run(ctx, io, %v) error = %v, want nil", []string{path}, err)
	}

	want := "Status: 200 OK\n"
	if stdout.String() != want {
		t.Errorf("Command(app).Run(ctx, io, %v) stdout = %q, want %q", []string{path}, stdout.String(), want)
	}
}

func TestCommandRejectsWrongArgumentCount(t *testing.T) {
	command := Command(http.NewServeMux())
	err := command.Run(context.Background(), cli.IO{}, nil)
	if !errors.Is(err, cli.ErrUsage) {
		t.Fatalf("Command(handler).Run(ctx, io, nil) error = %v, want ErrUsage", err)
	}

	err = command.Run(context.Background(), cli.IO{}, []string{"one.json", "two.json"})
	if !errors.Is(err, cli.ErrUsage) {
		t.Fatalf("Command(handler).Run(ctx, io, %v) error = %v, want ErrUsage", []string{"one.json", "two.json"}, err)
	}
}

func TestCommandRejectsUnknownFlag(t *testing.T) {
	command := Command(http.NewServeMux())
	err := command.Run(context.Background(), cli.IO{}, []string{"--bad", "snapshot.json"})
	if !errors.Is(err, cli.ErrUsage) {
		t.Fatalf("Command(handler).Run(ctx, io, --bad) error = %v, want ErrUsage", err)
	}
}

func TestCommandRejectsInvalidSnapshotFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}

	command := Command(http.NewServeMux())
	err := command.Run(context.Background(), cli.IO{}, []string{path})
	if err == nil {
		t.Fatalf("Command(handler).Run(ctx, io, %v) error = nil, want non-nil", []string{path})
	}
	if !strings.Contains(err.Error(), "decode replay snapshot") {
		t.Errorf("Command(handler).Run(ctx, io, %v) error = %v, want decode context", []string{path}, err)
	}
}

func TestCommandRejectsUnknownSnapshotFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	body := []byte(`{"version":1,"method":"GET","path":"/","uncontrolled_boundary":["clock"]}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
	}

	command := Command(http.NewServeMux())
	err := command.Run(context.Background(), cli.IO{}, []string{path})
	if err == nil {
		t.Fatalf("Command(handler).Run(ctx, io, %v) error = nil, want non-nil", []string{path})
	}
	if !strings.Contains(err.Error(), `unknown field "uncontrolled_boundary"`) {
		t.Errorf("Command(handler).Run(ctx, io, %v) error = %v, want unknown field context", []string{path}, err)
	}
}

func TestCommandRequiresHandler(t *testing.T) {
	path := writeSnapshot(t, Snapshot{
		Version: snapshotVersion,
		Method:  http.MethodGet,
		Path:    "/posts/42",
	})

	command := Command(nil)
	err := command.Run(context.Background(), cli.IO{}, []string{path})
	if err == nil {
		t.Fatalf("Command(nil).Run(ctx, io, %v) error = nil, want non-nil", []string{path})
	}
	if !strings.Contains(err.Error(), "replay handler is required") {
		t.Errorf("Command(nil).Run(ctx, io, %v) error = %v, want handler requirement", []string{path}, err)
	}
}

func writeSnapshot(t *testing.T, snapshot Snapshot) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "snapshot.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%q) error = %v, want nil", path, err)
	}

	if err := json.NewEncoder(file).Encode(snapshot); err != nil {
		t.Fatalf("json.Encode(%q) error = %v, want nil", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("file.Close(%q) error = %v, want nil", path, err)
	}
	return path
}
