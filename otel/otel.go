// Package otel wires OpenTelemetry for an Ohm application.
//
// Setup configures a tracer provider with an OTLP exporter, installs the global
// tracer provider and text-map propagators, and returns a shutdown function for
// graceful flushing during server shutdown. Ohm's core and application code use
// the OpenTelemetry API directly; until Setup runs, that API returns a no-op
// tracer, so instrumentation can be written unconditionally with no overhead.
//
// Exporter selection follows the standard OpenTelemetry environment variables
// (for example OTEL_TRACES_EXPORTER, OTEL_EXPORTER_OTLP_ENDPOINT, and
// OTEL_EXPORTER_OTLP_PROTOCOL), so deployments configure their backend without
// code changes.
package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// Shutdown flushes and releases telemetry resources. It should be called during
// graceful server shutdown so buffered spans are exported before exit.
type Shutdown func(context.Context) error

type config struct {
	serviceName    string
	serviceVersion string
	environment    string
	resourceAttrs  []attribute.KeyValue
	sampler        sdktrace.Sampler
	propagator     propagation.TextMapPropagator
}

// Option configures Setup.
type Option func(*config)

// WithServiceName sets the service.name resource attribute.
func WithServiceName(name string) Option {
	return func(c *config) {
		if name != "" {
			c.serviceName = name
		}
	}
}

// WithServiceVersion sets the service.version resource attribute.
func WithServiceVersion(version string) Option {
	return func(c *config) {
		if version != "" {
			c.serviceVersion = version
		}
	}
}

// WithEnvironment sets the deployment.environment.name resource attribute.
func WithEnvironment(name string) Option {
	return func(c *config) {
		if name != "" {
			c.environment = name
		}
	}
}

// WithResourceAttributes appends additional resource attributes.
func WithResourceAttributes(attrs ...attribute.KeyValue) Option {
	return func(c *config) {
		c.resourceAttrs = append(c.resourceAttrs, attrs...)
	}
}

// WithSampler overrides the tracer provider sampler.
func WithSampler(sampler sdktrace.Sampler) Option {
	return func(c *config) {
		if sampler != nil {
			c.sampler = sampler
		}
	}
}

// WithPropagator overrides the global text-map propagator. The default
// propagates W3C trace context and baggage.
func WithPropagator(propagator propagation.TextMapPropagator) Option {
	return func(c *config) {
		if propagator != nil {
			c.propagator = propagator
		}
	}
}

// Setup installs OpenTelemetry for the process and returns a shutdown function.
//
// Setup is opt-in: applications that never call it keep the no-op tracer and pay
// no telemetry cost. The returned Shutdown is safe to call once; calling it more
// than once returns the result of the first call's underlying providers.
func Setup(ctx context.Context, opts ...Option) (Shutdown, error) {
	cfg := config{
		propagator: propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build resource: %w", err)
	}

	exporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create span exporter: %w", err)
	}

	providerOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	}
	if cfg.sampler != nil {
		providerOpts = append(providerOpts, sdktrace.WithSampler(cfg.sampler))
	}
	provider := sdktrace.NewTracerProvider(providerOpts...)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(cfg.propagator)

	return provider.Shutdown, nil
}

func buildResource(ctx context.Context, cfg config) (*resource.Resource, error) {
	attrs := make([]attribute.KeyValue, 0, len(cfg.resourceAttrs)+3)
	if cfg.serviceName != "" {
		attrs = append(attrs, semconv.ServiceName(cfg.serviceName))
	}
	if cfg.serviceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.serviceVersion))
	}
	if cfg.environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(cfg.environment))
	}
	attrs = append(attrs, cfg.resourceAttrs...)

	detected, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, err
	}

	// Merge over the SDK default so the default service.name fallback survives
	// when neither an option nor the environment provides one. Detected values
	// take precedence on overlapping keys.
	merged, err := resource.Merge(resource.Default(), detected)
	if err != nil {
		return nil, err
	}
	return merged, nil
}
