package migrate

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/mgomes/ohm/cli"
)

func TestCommandRunsUp(t *testing.T) {
	runner := &fakeRunner{
		upResults: []Result{{Version: 1, Source: "001_create_users.sql"}},
	}
	var stdout bytes.Buffer
	command := Command(runner)

	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"up"})
	if err != nil {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want nil", []string{"up"}, err)
	}
	if runner.upCalls != 1 {
		t.Errorf("Command(runner).Run(ctx, io, %v) up calls = %d, want 1", []string{"up"}, runner.upCalls)
	}
	want := "Applied 1 001_create_users.sql\n"
	if stdout.String() != want {
		t.Errorf("Command(runner).Run(ctx, io, %v) stdout = %q, want %q", []string{"up"}, stdout.String(), want)
	}
}

func TestCommandRunsDown(t *testing.T) {
	runner := &fakeRunner{
		downResult: Result{Version: 1, Source: "001_create_users.sql"},
	}
	var stdout bytes.Buffer
	command := Command(runner)

	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"down"})
	if err != nil {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want nil", []string{"down"}, err)
	}
	if runner.downCalls != 1 {
		t.Errorf("Command(runner).Run(ctx, io, %v) down calls = %d, want 1", []string{"down"}, runner.downCalls)
	}
	want := "Rolled back 1 001_create_users.sql\n"
	if stdout.String() != want {
		t.Errorf("Command(runner).Run(ctx, io, %v) stdout = %q, want %q", []string{"down"}, stdout.String(), want)
	}
}

func TestCommandReportsSkippedDown(t *testing.T) {
	runner := &fakeRunner{
		downResult: Result{Skipped: true},
	}
	var stdout bytes.Buffer
	command := Command(runner)

	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"down"})
	if err != nil {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want nil", []string{"down"}, err)
	}

	want := "No migrations to roll back.\n"
	if stdout.String() != want {
		t.Errorf("Command(runner).Run(ctx, io, %v) stdout = %q, want %q", []string{"down"}, stdout.String(), want)
	}
}

func TestCommandReportsEmptyMigrationBodyRollback(t *testing.T) {
	runner := &fakeRunner{
		downResult: Result{Version: 1, Source: "001_create_users.sql", Empty: true},
	}
	var stdout bytes.Buffer
	command := Command(runner)

	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"down"})
	if err != nil {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want nil", []string{"down"}, err)
	}

	want := "Rolled back 1 001_create_users.sql\n"
	if stdout.String() != want {
		t.Errorf("Command(runner).Run(ctx, io, %v) stdout = %q, want %q", []string{"down"}, stdout.String(), want)
	}
}

func TestCommandRunsReset(t *testing.T) {
	runner := &fakeRunner{
		resetResults: []Result{
			{Version: 2, Source: "002_add_posts.sql"},
			{Version: 1, Source: "001_create_users.sql"},
		},
	}
	var stdout bytes.Buffer
	command := Command(runner)

	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"reset"})
	if err != nil {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want nil", []string{"reset"}, err)
	}
	if runner.resetCalls != 1 {
		t.Errorf("Command(runner).Run(ctx, io, %v) reset calls = %d, want 1", []string{"reset"}, runner.resetCalls)
	}
	want := "Rolled back 2 002_add_posts.sql\nRolled back 1 001_create_users.sql\n"
	if stdout.String() != want {
		t.Errorf("Command(runner).Run(ctx, io, %v) stdout = %q, want %q", []string{"reset"}, stdout.String(), want)
	}
}

func TestCommandReportsSkippedReset(t *testing.T) {
	runner := &fakeRunner{}
	var stdout bytes.Buffer
	command := Command(runner)

	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"reset"})
	if err != nil {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want nil", []string{"reset"}, err)
	}

	want := "No migrations to reset.\n"
	if stdout.String() != want {
		t.Errorf("Command(runner).Run(ctx, io, %v) stdout = %q, want %q", []string{"reset"}, stdout.String(), want)
	}
}

func TestCommandRunsStatus(t *testing.T) {
	runner := &fakeRunner{
		statuses: []Status{
			{Version: 1, State: "applied", Source: "001_create_users.sql"},
			{Version: 2, State: "pending", Source: "002_add_posts.sql"},
		},
	}
	var stdout bytes.Buffer
	command := Command(runner)

	err := command.Run(context.Background(), cli.IO{Stdout: &stdout}, []string{"status"})
	if err != nil {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want nil", []string{"status"}, err)
	}

	want := "VERSION  STATE    SOURCE\n1        applied  001_create_users.sql\n2        pending  002_add_posts.sql\n"
	if stdout.String() != want {
		t.Errorf("Command(runner).Run(ctx, io, %v) stdout = %q, want %q", []string{"status"}, stdout.String(), want)
	}
}

func TestCommandRejectsInvalidSubcommand(t *testing.T) {
	command := Command(&fakeRunner{})

	err := command.Run(context.Background(), cli.IO{Stdout: &bytes.Buffer{}}, []string{"redo"})
	if !errors.Is(err, cli.ErrUsage) {
		t.Fatalf("Command(runner).Run(ctx, io, %v) error = %v, want ErrUsage", []string{"redo"}, err)
	}
}

func TestCommandRequiresRunner(t *testing.T) {
	command := Command(nil)

	err := command.Run(context.Background(), cli.IO{Stdout: &bytes.Buffer{}}, []string{"up"})
	if err == nil {
		t.Fatalf("Command(nil).Run(ctx, io, %v) error = nil, want non-nil", []string{"up"})
	}
}

type fakeRunner struct {
	upResults    []Result
	downResult   Result
	resetResults []Result
	statuses     []Status
	upErr        error
	downErr      error
	resetErr     error
	statusErr    error
	upCalls      int
	downCalls    int
	resetCalls   int
	statusCalls  int
}

func (r *fakeRunner) Up(context.Context) ([]Result, error) {
	r.upCalls++
	return r.upResults, r.upErr
}

func (r *fakeRunner) Down(context.Context) (Result, error) {
	r.downCalls++
	return r.downResult, r.downErr
}

func (r *fakeRunner) Reset(context.Context) ([]Result, error) {
	r.resetCalls++
	return r.resetResults, r.resetErr
}

func (r *fakeRunner) Status(context.Context) ([]Status, error) {
	r.statusCalls++
	return r.statuses, r.statusErr
}
