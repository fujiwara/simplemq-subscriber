# simplemq-subscriber

A daemon that subscribes to [SAKURA Cloud SimpleMQ](https://manual.sakura.ad.jp/cloud/manual-simplemq.html) queues, dispatches messages to external commands based on header matching, and publishes results back to a response queue.

Designed to work with [mqbridge](https://github.com/fujiwara/mqbridge) for bridging on-premises RabbitMQ messaging to the cloud.

## Architecture

```
RabbitMQ (on-premises)
  ↓ (mqbridge: RabbitMQ → SimpleMQ)
SimpleMQ [request queue]
  ↓ (simplemq-subscriber: poll & dispatch)
External command (stdin: body, env: headers)
  ↓ (simplemq-subscriber: publish result)
SimpleMQ [response queue]
  ↓ (mqbridge: SimpleMQ → RabbitMQ)
RabbitMQ (on-premises)
```

## Installation

```bash
go install github.com/fujiwara/simplemq-subscriber/cmd/simplemq-subscriber@latest
```

## Usage

```bash
# Run the subscriber daemon
simplemq-subscriber run -c config.jsonnet

# Validate configuration
simplemq-subscriber validate -c config.jsonnet

# Render configuration as JSON
simplemq-subscriber render -c config.jsonnet
```

### Options

- `-c`, `--config` (required): Config file path (Jsonnet/JSON). Env: `SIMPLEMQ_SUBSCRIBER_CONFIG`
- `--log-format`: Log format (`text` or `json`, default: `text`). Env: `SIMPLEMQ_SUBSCRIBER_LOG_FORMAT`
- `--log-level`: Log level (`debug`, `info`, `warn`, `error`, default: `info`). Env: `SIMPLEMQ_SUBSCRIBER_LOG_LEVEL`

## Configuration

Configuration is written in [Jsonnet](https://jsonnet.org/) (plain JSON is also supported).

```jsonnet
{
  simplemq: {
    api_url: "",  // optional, uses default SimpleMQ API URL
  },
  request: {
    queue: "request-queue",
    api_key: std.extVar("REQUEST_API_KEY"),
    polling_interval: "1s",  // optional, default: 1s
  },
  response: {
    queue: "response-queue",
    api_key: std.extVar("RESPONSE_API_KEY"),
  },
  handlers: [
    {
      name: "deploy",
      match: {
        "rabbitmq.routing_key": "deploy",
        "rabbitmq.header.x-env": "production",
      },
      command: ["/usr/local/bin/deploy.sh"],
      timeout: "60s",     // optional, default: 30s
      blocking: true,     // wait for completion before processing next message
    },
    {
      name: "notify",
      match: {
        "rabbitmq.routing_key": "notify",
      },
      command: ["/usr/local/bin/notify.sh"],
      timeout: "10s",
      blocking: false,       // run in background goroutine
      max_concurrency: 5,    // max concurrent executions (default: 1)
    },
  ],
}
```

### Handler Matching

- `match` defines header key-value pairs that must **all** match exactly (AND condition)
- Handlers are evaluated in order; the **first match wins**
- Messages that match no handler are logged and dropped

### Blocking vs Non-blocking

- **blocking: true** — The subscriber waits for the command to complete before polling the next message
- **blocking: false** — The command runs in a goroutine. The subscriber immediately proceeds to the next message. When `max_concurrency` is reached, the subscriber blocks until a slot is available (other subscribers can pick up messages in the meantime)

### Command Execution

- Message body is passed via **stdin**
- Message headers are passed as environment variables with the prefix `SIMPLEMQ_HEADER_` (dots and hyphens are converted to underscores, uppercased)
  - e.g., `rabbitmq.routing_key` → `SIMPLEMQ_HEADER_RABBITMQ_ROUTING_KEY`
- Command **stdout** becomes the response message body
- Command **stderr** is logged
- Original message headers are preserved in the response (for mqbridge routing back to RabbitMQ)
- If the command fails (non-zero exit), the message is **not deleted** and will be redelivered after the visibility timeout

### Secret Manager

API keys can be loaded from [SAKURA Cloud Secret Manager](https://manual.sakura.ad.jp/cloud/manual-secret-manager.html) using the `secret()` function:

```jsonnet
{
  request: {
    api_key: secret("vault-id", "api-key-name"),
  },
}
```

## Message Format

simplemq-subscriber uses the same wire format as [mqbridge](https://github.com/fujiwara/mqbridge) (`mqbridge.Message`). Messages on SimpleMQ are base64-encoded JSON with the following structure:

```json
{
  "headers": {
    "rabbitmq.exchange": "my-exchange",
    "rabbitmq.routing_key": "my.routing.key",
    "rabbitmq.header.x-custom": "value"
  },
  "body": "message body text",
  "body_encoding": "base64"
}
```

- `headers`: Key-value metadata. When originating from RabbitMQ via mqbridge, headers are prefixed with `rabbitmq.` (e.g., `rabbitmq.exchange`, `rabbitmq.routing_key`, `rabbitmq.correlation_id`, `rabbitmq.header.*` for custom AMQP headers)
- `body`: The message payload. Plain string if valid UTF-8, or base64-encoded for binary data
- `body_encoding`: Set to `"base64"` when the body is base64-encoded (binary-safe). Omitted for plain text

### Message flow detail

1. **Receive**: SimpleMQ delivers base64-encoded content → simplemq-subscriber decodes it → `mqbridge.UnmarshalMessage()` parses the JSON into `mqbridge.Message` (headers + body)
2. **Dispatch**: The `headers` are used for handler matching (e.g., match on `rabbitmq.routing_key`)
3. **Execute**: `body` is passed to the command's stdin. `headers` are available as `SIMPLEMQ_HEADER_*` environment variables
4. **Respond**: Command stdout becomes the new `body`. The original `headers` are preserved in the response so that mqbridge can route the result back to the correct RabbitMQ exchange/routing_key
5. **Publish**: The response is serialized via `mqbridge.MarshalMessage()` → base64-encoded → sent to the response queue

This ensures full round-trip compatibility: RabbitMQ → mqbridge → SimpleMQ → simplemq-subscriber → SimpleMQ → mqbridge → RabbitMQ.

## Observability

OpenTelemetry metrics are automatically enabled when `OTEL_EXPORTER_OTLP_ENDPOINT` is set.

### Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `simplemq_subscriber.messages.received` | Counter | Messages received from request queue |
| `simplemq_subscriber.messages.processed` | Counter | Messages successfully processed |
| `simplemq_subscriber.messages.errors` | Counter | Message processing errors |
| `simplemq_subscriber.messages.dropped` | Counter | Messages dropped (no matching handler) |
| `simplemq_subscriber.command.duration` | Histogram | Command execution duration (seconds) |

## LICENSE

MIT

## Author

fujiwara
