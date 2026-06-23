package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const defaultShutdownTimeout = 10 * time.Second

var notifySignalContext = signal.NotifyContext

// ShutdownHook releases resources during graceful shutdown. During a graceful
// drain, hooks run after the server stops serving, within the remaining
// shutdown budget. If the drain fails and the server is force-closed, hooks run
// with a fresh bounded context and may overlap abandoned handler goroutines that
// are still unwinding.
type ShutdownHook func(context.Context) error

// ServerRunner runs an HTTP server and, once it stops, runs the shutdown hooks
// within the shutdown budget. A runner owns the full lifecycle, so custom
// runners are responsible for invoking the hooks they are given.
type ServerRunner func(ctx context.Context, server *http.Server, shutdownTimeout time.Duration, hooks []ShutdownHook) error

type serverConfig struct {
	name            string
	addr            string
	shutdownTimeout time.Duration
	runner          ServerRunner
	shutdownHooks   []ShutdownHook
}

// ServerOption configures ServerCommand.
type ServerOption func(*serverConfig)

// WithAddr configures the default server address.
func WithAddr(addr string) ServerOption {
	return func(cfg *serverConfig) {
		if addr != "" {
			cfg.addr = addr
		}
	}
}

// WithShutdownTimeout configures graceful shutdown timeout.
func WithShutdownTimeout(timeout time.Duration) ServerOption {
	return func(cfg *serverConfig) {
		if timeout > 0 {
			cfg.shutdownTimeout = timeout
		}
	}
}

// WithServerRunner configures the server runner.
func WithServerRunner(runner ServerRunner) ServerOption {
	return func(cfg *serverConfig) {
		if runner != nil {
			cfg.runner = runner
		}
	}
}

// WithShutdownHook registers a hook run during graceful shutdown. Hooks run in
// reverse registration order. During a graceful drain they share the shutdown
// budget with the server; after a forced close they get a fresh bounded context
// because the drain context has already expired.
func WithShutdownHook(hook func(context.Context) error) ServerOption {
	return func(cfg *serverConfig) {
		if hook != nil {
			cfg.shutdownHooks = append(cfg.shutdownHooks, ShutdownHook(hook))
		}
	}
}

// ServerCommand returns a command that boots an HTTP server.
func ServerCommand(handler http.Handler, opts ...ServerOption) Command {
	cfg := serverConfig{
		name:            "server",
		addr:            "127.0.0.1:3000",
		shutdownTimeout: defaultShutdownTimeout,
		runner:          RunHTTPServer,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return Command{
		Name:    cfg.name,
		Summary: "start the HTTP server",
		Usage:   fmt.Sprintf("server [-addr %s]", cfg.addr),
		Run: func(ctx context.Context, commandIO IO, args []string) error {
			if ctx == nil {
				ctx = context.Background()
			}
			commandIO = commandIO.withDefaults()
			flags := flag.NewFlagSet(cfg.name, flag.ContinueOnError)
			var flagOutput bytes.Buffer
			flags.SetOutput(&flagOutput)
			addr := flags.String("addr", cfg.addr, "HTTP listen address")
			if err := flags.Parse(args); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					return ErrHelp
				}
				if flagOutput.Len() > 0 {
					if _, writeErr := commandIO.Stderr.Write(flagOutput.Bytes()); writeErr != nil {
						return fmt.Errorf("write flag usage: %w", writeErr)
					}
				}
				return fmt.Errorf("%w: %v", ErrUsage, err)
			}
			if flags.NArg() > 0 {
				return fmt.Errorf("%w: server does not accept positional arguments", ErrUsage)
			}

			ctx, stopSignals := notifySignalContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stopSignals()
			stopSignalReset := context.AfterFunc(ctx, stopSignals)
			defer stopSignalReset()

			server := &http.Server{
				Addr:              *addr,
				BaseContext:       requestBaseContext(ctx),
				Handler:           handler,
				ReadHeaderTimeout: 5 * time.Second,
			}
			return cfg.runner(ctx, server, cfg.shutdownTimeout, cfg.shutdownHooks)
		},
	}
}

// runShutdownHooks runs hooks in reverse registration order within ctx, which
// carries the shared shutdown deadline.
func runShutdownHooks(ctx context.Context, hooks []ShutdownHook) error {
	var errs []error
	for i := range len(hooks) {
		hook := hooks[len(hooks)-1-i]
		if err := hook(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RunHTTPServer runs server until it stops or ctx is canceled, then runs the
// shutdown hooks. The drain and hooks share a single deadline during graceful
// shutdown. If the drain fails, the server is force-closed and hooks receive a
// fresh bounded context so cleanup does not inherit an already-expired deadline.
func RunHTTPServer(ctx context.Context, server *http.Server, shutdownTimeout time.Duration, hooks []ShutdownHook) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if server == nil {
		return fmt.Errorf("server is required")
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}
	if server.BaseContext == nil {
		server.BaseContext = requestBaseContext(ctx)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		return errors.Join(err, runShutdownHooks(shutdownCtx, hooks))
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		hookCtx := shutdownCtx
		hookCancel := func() {}
		serveErr := server.Shutdown(shutdownCtx)
		if serveErr != nil {
			if closeErr := server.Close(); closeErr != nil {
				serveErr = errors.Join(serveErr, closeErr)
			}
			serveErr = fmt.Errorf("shutdown server: %w", serveErr)
			hookCtx, hookCancel = context.WithTimeout(context.Background(), shutdownTimeout)
		} else if drainErr := <-errCh; drainErr != nil && !errors.Is(drainErr, http.ErrServerClosed) {
			serveErr = drainErr
		}
		defer hookCancel()

		return errors.Join(serveErr, runShutdownHooks(hookCtx, hooks))
	}
}

func requestBaseContext(ctx context.Context) func(net.Listener) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithoutCancel(ctx)
	return func(net.Listener) context.Context {
		return ctx
	}
}
