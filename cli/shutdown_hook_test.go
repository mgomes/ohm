package cli

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestServerCommandRunsShutdownHooks(t *testing.T) {
	var ran bool
	command := ServerCommand(&testHandler{},
		WithAddr("127.0.0.1:invalid"),
		WithShutdownHook(func(context.Context) error {
			ran = true
			return nil
		}),
	)

	// The invalid address makes the server stop immediately, exercising the
	// real default runner's shutdown path end to end.
	_ = command.Run(context.Background(), IO{}, nil)
	if !ran {
		t.Errorf("shutdown hook ran = false, want true")
	}
}

func TestServerCommandAcceptsNamedShutdownHooks(t *testing.T) {
	type namedShutdownHook func(context.Context) error

	var ran bool
	hook := namedShutdownHook(func(context.Context) error {
		ran = true
		return nil
	})
	command := ServerCommand(&testHandler{},
		WithShutdownHook(hook),
		WithServerRunner(func(ctx context.Context, _ *http.Server, _ time.Duration, hooks []ShutdownHook) error {
			if len(hooks) != 1 {
				t.Errorf("WithShutdownHook(named hook) stored %d hooks, want 1", len(hooks))
				return nil
			}
			return hooks[0](ctx)
		}),
	)

	if err := command.Run(context.Background(), IO{}, nil); err != nil {
		t.Fatalf("ServerCommand(...).Run error = %v, want nil", err)
	}
	if !ran {
		t.Errorf("named shutdown hook ran = false, want true")
	}
}

func TestRunHTTPServerRunsHooksWithinSharedDeadline(t *testing.T) {
	var hookDeadline bool
	server := &http.Server{Addr: "127.0.0.1:invalid"}

	err := RunHTTPServer(context.Background(), server, time.Second, []ShutdownHook{
		func(ctx context.Context) error {
			_, hookDeadline = ctx.Deadline()
			return nil
		},
	})
	if err == nil {
		t.Fatalf("RunHTTPServer with invalid address error = nil, want bind error")
	}
	if !hookDeadline {
		t.Errorf("shutdown hook context deadline set = false, want true")
	}
}

func TestRunHTTPServerJoinsServeAndHookErrors(t *testing.T) {
	hookErr := errors.New("hook failed")
	server := &http.Server{Addr: "127.0.0.1:invalid"}

	err := RunHTTPServer(context.Background(), server, time.Second, []ShutdownHook{
		func(context.Context) error { return hookErr },
	})
	if !errors.Is(err, hookErr) {
		t.Errorf("RunHTTPServer error = %v, want it to wrap hookErr", err)
	}
}

func TestRunShutdownHooksReverseOrder(t *testing.T) {
	var order []string
	err := runShutdownHooks(context.Background(), []ShutdownHook{
		func(context.Context) error { order = append(order, "first"); return nil },
		func(context.Context) error { order = append(order, "second"); return nil },
	})
	if err != nil {
		t.Fatalf("runShutdownHooks error = %v, want nil", err)
	}

	want := []string{"second", "first"}
	if len(order) != len(want) {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("hook order = %v, want %v", order, want)
		}
	}
}
