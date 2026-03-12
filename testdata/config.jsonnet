{
  simplemq: {
    api_url: "",
  },
  request: {
    queue: "request-queue",
    api_key: "test-api-key",
    polling_interval: "100ms",
  },
  response: {
    queue: "response-queue",
    api_key: "test-api-key",
  },
  handlers: [
    {
      name: "echo",
      match: {
        "rabbitmq.routing_key": "echo",
      },
      command: ["cat"],
      timeout: "5s",
      blocking: true,
    },
    {
      name: "upper",
      match: {
        "rabbitmq.routing_key": "upper",
      },
      command: ["tr", "a-z", "A-Z"],
      timeout: "5s",
      blocking: false,
      max_concurrency: 3,
    },
  ],
}
