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
