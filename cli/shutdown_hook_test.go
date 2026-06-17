package cli

import (
	"context"
	"errors"
	"net"
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

func TestRunHTTPServerUsesFreshHookContextAfterFailedDrain(t *testing.T) {
	addr := freeTCPAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	releaseHandler := make(chan struct{})
	handlerStarted := make(chan struct{}, 1)
	hookErrCh := make(chan error, 1)
	runErrCh := make(chan error, 1)
	requestDone := make(chan struct{})

	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			select {
			case handlerStarted <- struct{}{}:
			default:
			}
			<-releaseHandler
		}),
	}

	go func() {
		runErrCh <- RunHTTPServer(ctx, server, time.Millisecond, []ShutdownHook{
			func(ctx context.Context) error {
				if err := ctx.Err(); err != nil {
					hookErrCh <- err
					return nil
				}
				if _, ok := ctx.Deadline(); !ok {
					hookErrCh <- errors.New("hook context has no deadline")
					return nil
				}
				hookErrCh <- nil
				return nil
			},
		})
	}()

	waitForTCPServer(t, addr)
	go func() {
		resp, err := http.Get("http://" + addr)
		if err == nil {
			_ = resp.Body.Close()
		}
		close(requestDone)
	}()
	defer func() {
		close(releaseHandler)
		select {
		case <-requestDone:
		case <-time.After(time.Second):
			t.Errorf("blocked request did not finish after handler release")
		}
	}()

	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatalf("handler did not start before shutdown")
	}

	cancel()

	var runErr error
	select {
	case runErr = <-runErrCh:
	case <-time.After(time.Second):
		t.Fatalf("RunHTTPServer did not return after shutdown timeout")
	}
	if !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("RunHTTPServer error = %v, want context deadline exceeded", runErr)
	}

	select {
	case err := <-hookErrCh:
		if err != nil {
			t.Fatalf("shutdown hook context error = %v, want nil", err)
		}
	default:
		t.Fatalf("shutdown hook did not run")
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

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(tcp, 127.0.0.1:0) error = %v, want nil", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close() error = %v, want nil", err)
	}
	return addr
}

func waitForTCPServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 10*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		lastErr = err
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("server %s did not accept TCP connections: %v", addr, lastErr)
}
