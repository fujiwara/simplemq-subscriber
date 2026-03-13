{
  rabbitmq: { url: 'amqp://guest:guest@rabbitmq:5672/' },
  simplemq: { api_url: 'http://simplemq-localserver:18080' },
  bridges: [
    {
      name: 'request',
      from: {
        rabbitmq: {
          queue: 'rpc-request',
          exchange: 'commands',
          exchange_type: 'topic',
          routing_key: '#',
        },
      },
      to: [{ simplemq: { queue: 'smq-request', api_key: 'test-key' } }],
    },
    {
      name: 'response',
      from: {
        simplemq: { queue: 'smq-response', api_key: 'test-key', polling_interval: '500ms' },
      },
      to: [{ rabbitmq: {} }],
    },
  ],
}
