package subscriber

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Metrics holds OpenTelemetry metric instruments.
type Metrics struct {
	messagesReceived  metric.Int64Counter
	messagesProcessed metric.Int64Counter
	messageErrors     metric.Int64Counter
	messagesDropped   metric.Int64Counter
	commandDuration   metric.Float64Histogram
}

func newMetrics() (*Metrics, error) {
	meter := otel.Meter("github.com/fujiwara/simplemq-subscriber")

	received, err := meter.Int64Counter("simplemq_subscriber.messages.received",
		metric.WithDescription("Messages received from request queue"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create messages.received counter: %w", err)
	}

	processed, err := meter.Int64Counter("simplemq_subscriber.messages.processed",
		metric.WithDescription("Messages successfully processed"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create messages.processed counter: %w", err)
	}

	errors, err := meter.Int64Counter("simplemq_subscriber.messages.errors",
		metric.WithDescription("Message processing errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create messages.errors counter: %w", err)
	}

	dropped, err := meter.Int64Counter("simplemq_subscriber.messages.dropped",
		metric.WithDescription("Messages dropped due to no matching handler"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create messages.dropped counter: %w", err)
	}

	duration, err := meter.Float64Histogram("simplemq_subscriber.command.duration",
		metric.WithDescription("Command execution duration (seconds)"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create command.duration histogram: %w", err)
	}

	return &Metrics{
		messagesReceived:  received,
		messagesProcessed: processed,
		messageErrors:     errors,
		messagesDropped:   dropped,
		commandDuration:   duration,
	}, nil
}

// setupMeterProvider initializes the OpenTelemetry MeterProvider if OTEL_EXPORTER_OTLP_ENDPOINT is set.
func setupMeterProvider(ctx context.Context) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noop, nil
	}

	protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	var exporter sdkmetric.Exporter
	var err error
	switch protocol {
	case "grpc":
		exporter, err = otlpmetricgrpc.New(ctx)
	case "http/protobuf", "":
		exporter, err = otlpmetrichttp.New(ctx)
	default:
		return noop, fmt.Errorf("unsupported OTEL_EXPORTER_OTLP_PROTOCOL: %s", protocol)
	}
	if err != nil {
		return noop, fmt.Errorf("failed to create OTLP metrics exporter: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
	)
	otel.SetMeterProvider(provider)

	if protocol == "" {
		protocol = "http/protobuf"
	}
	slog.Info("OpenTelemetry metrics enabled", "protocol", protocol)
	return provider.Shutdown, nil
}
