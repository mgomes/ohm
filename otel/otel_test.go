package otel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
)

func TestSetupInstallsPropagatorAndShutdown(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")

	shutdown, err := Setup(context.Background(),
		WithServiceName("ohm-test"),
		WithServiceVersion("0.0.0"),
		WithEnvironment("test"),
	)
	if err != nil {
		t.Fatalf("Setup(ctx) error = %v, want nil", err)
	}
	if shutdown == nil {
		t.Fatalf("Setup(ctx) shutdown = nil, want non-nil")
	}
	t.Cleanup(func() {
		if err := shutdown(context.Background()); err != nil {
			t.Errorf("shutdown(ctx) error = %v, want nil", err)
		}
	})

	fields := otel.GetTextMapPropagator().Fields()
	if !contains(fields, "traceparent") {
		t.Errorf("propagator fields = %v, want to contain %q", fields, "traceparent")
	}
}

func TestSetupRejectsUnknownExporter(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "definitely-not-an-exporter")

	if _, err := Setup(context.Background()); err == nil {
		t.Fatalf("Setup(ctx) error = nil, want non-nil for unknown exporter")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
