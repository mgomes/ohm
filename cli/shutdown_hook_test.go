package cli

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestServerCommandRunsShutdownHooksAfterServing(t *testing.T) {
	var order []string
	command := ServerCommand(&testHandler{},
		WithServerRunner(func(_ context.Context, _ *http.Server, _ time.Duration) error {
			order = append(order, "serve")
			return nil
		}),
		WithShutdownHook(func(context.Context) error {
			order = append(order, "first")
			return nil
		}),
		WithShutdownHook(func(context.Context) error {
			order = append(order, "second")
			return nil
		}),
	)

	if err := command.Run(context.Background(), IO{}, nil); err != nil {
		t.Fatalf("command.Run(ctx, io, nil) error = %v, want nil", err)
	}

	want := []string{"serve", "second", "first"}
	if len(order) != len(want) {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("hook order = %v, want %v", order, want)
		}
	}
}

func TestServerCommandJoinsShutdownHookErrors(t *testing.T) {
	serveErr := errors.New("serve failed")
	hookErr := errors.New("hook failed")
	command := ServerCommand(&testHandler{},
		WithServerRunner(func(_ context.Context, _ *http.Server, _ time.Duration) error {
			return serveErr
		}),
		WithShutdownHook(func(context.Context) error {
			return hookErr
		}),
	)

	err := command.Run(context.Background(), IO{}, nil)
	if !errors.Is(err, serveErr) {
		t.Errorf("command.Run error = %v, want it to wrap serveErr", err)
	}
	if !errors.Is(err, hookErr) {
		t.Errorf("command.Run error = %v, want it to wrap hookErr", err)
	}
}

func TestRunShutdownHooksBoundsContext(t *testing.T) {
	var deadlineSet bool
	err := runShutdownHooks([]func(context.Context) error{
		func(ctx context.Context) error {
			_, deadlineSet = ctx.Deadline()
			return nil
		},
	}, time.Second)
	if err != nil {
		t.Fatalf("runShutdownHooks error = %v, want nil", err)
	}
	if !deadlineSet {
		t.Errorf("shutdown hook context deadline set = false, want true")
	}
}
