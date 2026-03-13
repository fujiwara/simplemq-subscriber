package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Set up OTel tracing
	shutdown, err := setupTracing(ctx)
	if err != nil {
		log.Printf("warning: failed to setup tracing: %v", err)
	} else {
		defer shutdown(context.Background())
	}

	// Connect to RabbitMQ with retry
	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		rabbitmqURL = "amqp://guest:guest@localhost:5672/"
	}

	var conn *amqp.Connection
	for i := range 30 {
		conn, err = amqp.Dial(rabbitmqURL)
		if err == nil {
			break
		}
		log.Printf("waiting for RabbitMQ (attempt %d/30): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer ch.Close()

	// Declare exclusive auto-delete reply queue
	replyQueue, err := ch.QueueDeclare(
		"",    // auto-generated name
		false, // durable
		true,  // auto-delete
		true,  // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to declare reply queue: %w", err)
	}

	// Consume from reply queue
	msgs, err := ch.Consume(
		replyQueue.Name,
		"",    // consumer tag
		true,  // auto-ack
		true,  // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to consume from reply queue: %w", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Send first request immediately
	if err := sendRPC(ctx, ch, replyQueue.Name, msgs); err != nil {
		log.Printf("RPC error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return nil
		case <-ticker.C:
			if err := sendRPC(ctx, ch, replyQueue.Name, msgs); err != nil {
				log.Printf("RPC error: %v", err)
			}
		}
	}
}

func sendRPC(ctx context.Context, ch *amqp.Channel, replyTo string, msgs <-chan amqp.Delivery) error {
	tracer := otel.Tracer("rpc-client")
	ctx, span := tracer.Start(ctx, "rpc-call")
	defer span.End()

	// Extract traceparent from current span context
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	traceparent := carrier.Get("traceparent")

	corrID := uuid.New().String()
	body := "hello world"
	headers := amqp.Table{}
	if traceparent != "" {
		headers["traceparent"] = traceparent
	}

	// Publish span
	_, publishSpan := tracer.Start(ctx, "rabbitmq publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", "commands"),
			attribute.String("messaging.rabbitmq.destination.routing_key", "upper"),
			attribute.String("messaging.message.correlation_id", corrID),
		),
	)

	log.Printf("sending request: %q (correlation_id=%s)", body, corrID)
	err := ch.PublishWithContext(ctx,
		"commands", // exchange
		"upper",    // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType:   "text/plain",
			CorrelationId: corrID,
			ReplyTo:       replyTo,
			Headers:       headers,
			Body:          []byte(body),
		},
	)
	publishSpan.End()
	if err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	// Receive span
	_, receiveSpan := tracer.Start(ctx, "rabbitmq receive",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.message.correlation_id", corrID),
		),
	)

	timeout := time.After(30 * time.Second)
	select {
	case msg := <-msgs:
		receiveSpan.SetAttributes(attribute.Int("messaging.message.body_size", len(msg.Body)))
		receiveSpan.End()
		log.Printf("response body: %q", string(msg.Body))
		if msg.Headers != nil {
			for k, v := range msg.Headers {
				log.Printf("response header: %s=%v", k, v)
			}
		}
	case <-timeout:
		receiveSpan.End()
		return fmt.Errorf("timeout waiting for response")
	}

	return nil
}

func setupTracing(ctx context.Context) (func(context.Context) error, error) {
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("rpc-client"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}
