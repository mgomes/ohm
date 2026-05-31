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
