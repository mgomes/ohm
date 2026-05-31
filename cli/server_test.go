package cli

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestServerCommandBuildsHTTPServer(t *testing.T) {
	handler := &testHandler{}
	var gotAddr string
	var gotHandler http.Handler
	command := ServerCommand(handler, WithServerRunner(func(_ context.Context, server *http.Server, _ time.Duration) error {
		gotAddr = server.Addr
		gotHandler = server.Handler
		return nil
	}))

	err := command.Run(context.Background(), IO{}, []string{"-addr", ":4000"})
	if err != nil {
		t.Fatalf("ServerCommand(handler).Run(ctx, io, %v) error = %v, want nil", []string{"-addr", ":4000"}, err)
	}
	if gotAddr != ":4000" {
		t.Errorf("ServerCommand(handler).Run(ctx, io, args) server addr = %q, want %q", gotAddr, ":4000")
	}
	if gotHandler != handler {
		t.Errorf("ServerCommand(handler).Run(ctx, io, args) server handler = %v, want %v", gotHandler, handler)
	}
}

func TestServerCommandSetsRequestBaseContext(t *testing.T) {
	type contextKey struct{}

	parent := context.WithValue(context.Background(), contextKey{}, "request")
	ctx, cancel := context.WithCancel(parent)
	command := ServerCommand(&testHandler{}, WithServerRunner(func(_ context.Context, server *http.Server, _ time.Duration) error {
		if server.BaseContext == nil {
			t.Fatalf("ServerCommand(handler).Run(ctx, io, args) server BaseContext = nil, want non-nil")
		}
		requestCtx := server.BaseContext(nil)
		if got := requestCtx.Value(contextKey{}); got != "request" {
			t.Errorf("ServerCommand(handler).Run(ctx, io, args) request context value = %v, want %v", got, "request")
		}
		cancel()
		select {
		case <-requestCtx.Done():
			t.Errorf("ServerCommand(handler).Run(ctx, io, args) request context canceled = true, want false")
		default:
		}
		return nil
	}))

	if err := command.Run(ctx, IO{}, nil); err != nil {
		t.Fatalf("ServerCommand(handler).Run(ctx, io, nil) error = %v, want nil", err)
	}
}

type testHandler struct{}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {}

func TestServerCommandRejectsPositionalArguments(t *testing.T) {
	command := ServerCommand(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	err := command.Run(context.Background(), IO{}, []string{"extra"})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("ServerCommand(handler).Run(ctx, io, %v) error = %v, want ErrUsage", []string{"extra"}, err)
	}
}

func TestRunHTTPServerRequiresServer(t *testing.T) {
	err := RunHTTPServer(context.Background(), nil, time.Second)
	if err == nil {
		t.Fatalf("RunHTTPServer(ctx, nil, timeout) error = nil, want non-nil")
	}
}

func TestRunHTTPServerSetsRequestBaseContext(t *testing.T) {
	type contextKey struct{}

	parent := context.WithValue(context.Background(), contextKey{}, "request")
	ctx, cancel := context.WithCancel(parent)
	server := &http.Server{Addr: "127.0.0.1:invalid"}

	err := RunHTTPServer(ctx, server, time.Second)
	cancel()
	if err == nil {
		t.Fatalf("RunHTTPServer(ctx, server, timeout) error = nil, want non-nil")
	}
	if server.BaseContext == nil {
		t.Fatalf("RunHTTPServer(ctx, server, timeout) server BaseContext = nil, want non-nil")
	}
	requestCtx := server.BaseContext(nil)
	if got := requestCtx.Value(contextKey{}); got != "request" {
		t.Errorf("RunHTTPServer(ctx, server, timeout) request context value = %v, want %v", got, "request")
	}
	select {
	case <-requestCtx.Done():
		t.Errorf("RunHTTPServer(ctx, server, timeout) request context canceled = true, want false")
	default:
	}
}
