package subscriber

import (
	"context"
	"log/slog"
	"testing"

	"github.com/fujiwara/mqbridge"
)

func newTestMetrics(t *testing.T) *Metrics {
	t.Helper()
	m, err := newMetrics()
	if err != nil {
		t.Fatalf("failed to create metrics: %v", err)
	}
	return m
}

func TestHandlerMatch(t *testing.T) {
	m := newTestMetrics(t)
	h := NewHandler(HandlerConfig{
		Name: "test",
		Match: map[string]string{
			"rabbitmq.routing_key":  "deploy",
			"rabbitmq.header.x-env": "production",
		},
		Command: []string{"echo"},
	}, slog.Default(), m)

	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			name: "full match",
			headers: map[string]string{
				"rabbitmq.routing_key":  "deploy",
				"rabbitmq.header.x-env": "production",
				"extra":                 "ignored",
			},
			want: true,
		},
		{
			name: "partial match",
			headers: map[string]string{
				"rabbitmq.routing_key": "deploy",
			},
			want: false,
		},
		{
			name: "wrong value",
			headers: map[string]string{
				"rabbitmq.routing_key":  "deploy",
				"rabbitmq.header.x-env": "staging",
			},
			want: false,
		},
		{
			name:    "nil headers",
			headers: nil,
			want:    false,
		},
		{
			name:    "empty headers",
			headers: map[string]string{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &mqbridge.Message{Body: []byte("test"), Headers: tt.headers}
			if got := h.Match(msg); got != tt.want {
				t.Errorf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandlerExecute(t *testing.T) {
	m := newTestMetrics(t)
	h := NewHandler(HandlerConfig{
		Name:    "echo",
		Match:   map[string]string{"k": "v"},
		Command: []string{"cat"},
		Timeout: "5s",
	}, slog.Default(), m)

	msg := &mqbridge.Message{
		Body: []byte("hello world"),
		Headers: map[string]string{
			"rabbitmq.routing_key": "test.key",
			"rabbitmq.exchange":    "test-exchange",
		},
	}

	ctx := context.Background()
	result, err := h.Execute(ctx, msg)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if string(result.Body) != "hello world" {
		t.Errorf("body: expected %q, got %q", "hello world", string(result.Body))
	}
	// Headers should be preserved
	if result.Headers["rabbitmq.routing_key"] != "test.key" {
		t.Errorf("routing_key header: expected %q, got %q", "test.key", result.Headers["rabbitmq.routing_key"])
	}
	if result.Headers["rabbitmq.exchange"] != "test-exchange" {
		t.Errorf("exchange header: expected %q, got %q", "test-exchange", result.Headers["rabbitmq.exchange"])
	}
}

func TestHandlerExecuteTransform(t *testing.T) {
	m := newTestMetrics(t)
	h := NewHandler(HandlerConfig{
		Name:    "upper",
		Match:   map[string]string{"k": "v"},
		Command: []string{"tr", "a-z", "A-Z"},
		Timeout: "5s",
	}, slog.Default(), m)

	msg := &mqbridge.Message{
		Body:    []byte("hello"),
		Headers: map[string]string{"k": "v"},
	}

	result, err := h.Execute(context.Background(), msg)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if string(result.Body) != "HELLO" {
		t.Errorf("body: expected %q, got %q", "HELLO", string(result.Body))
	}
}

func TestHandlerExecuteFailure(t *testing.T) {
	m := newTestMetrics(t)
	h := NewHandler(HandlerConfig{
		Name:    "fail",
		Match:   map[string]string{"k": "v"},
		Command: []string{"false"},
		Timeout: "5s",
	}, slog.Default(), m)

	msg := &mqbridge.Message{Body: []byte("test"), Headers: map[string]string{"k": "v"}}
	_, err := h.Execute(context.Background(), msg)
	if err == nil {
		t.Error("expected error from failing command")
	}
}

func TestHandlerExecuteTimeout(t *testing.T) {
	m := newTestMetrics(t)
	h := NewHandler(HandlerConfig{
		Name:    "slow",
		Match:   map[string]string{"k": "v"},
		Command: []string{"sleep", "10"},
		Timeout: "100ms",
	}, slog.Default(), m)

	msg := &mqbridge.Message{Body: []byte("test"), Headers: map[string]string{"k": "v"}}
	_, err := h.Execute(context.Background(), msg)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestHeadersToEnv(t *testing.T) {
	env := headersToEnv(map[string]string{
		"rabbitmq.routing_key":     "test.key",
		"rabbitmq.header.x-custom": "value",
	})
	expected := map[string]string{
		"SIMPLEMQ_HEADER_RABBITMQ_ROUTING_KEY":     "test.key",
		"SIMPLEMQ_HEADER_RABBITMQ_HEADER_X_CUSTOM": "value",
	}
	envMap := make(map[string]string)
	for _, e := range env {
		for i, c := range e {
			if c == '=' {
				envMap[e[:i]] = e[i+1:]
				break
			}
		}
	}
	for k, v := range expected {
		if envMap[k] != v {
			t.Errorf("env %s: expected %q, got %q", k, v, envMap[k])
		}
	}
}
