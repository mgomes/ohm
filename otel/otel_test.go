package otel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
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

func TestBuildResourceKeepsDefaultServiceName(t *testing.T) {
	res, err := buildResource(context.Background(), config{})
	if err != nil {
		t.Fatalf("buildResource(ctx, config{}) error = %v, want nil", err)
	}

	value, ok := res.Set().Value(semconv.ServiceNameKey)
	if !ok {
		t.Fatalf("resource has no %q attribute, want the SDK default fallback", semconv.ServiceNameKey)
	}
	if value.AsString() == "" {
		t.Errorf("%q = empty, want a non-empty default", semconv.ServiceNameKey)
	}
}

func TestBuildResourceUsesConfiguredServiceName(t *testing.T) {
	res, err := buildResource(context.Background(), config{serviceName: "ohm-app"})
	if err != nil {
		t.Fatalf("buildResource error = %v, want nil", err)
	}

	value, ok := res.Set().Value(semconv.ServiceNameKey)
	if !ok || value.AsString() != "ohm-app" {
		t.Errorf("%q = %q (present=%v), want %q", semconv.ServiceNameKey, value.AsString(), ok, "ohm-app")
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
