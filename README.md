# cloudflaregraphqlreceiver

OpenTelemetry Collector receiver that scrapes the [Cloudflare Analytics GraphQL API](https://developers.cloudflare.com/analytics/graphql-api/) and exposes metrics for firewall events, HTTP requests, load balancing, health checks, and logpush jobs.

## Metrics

| Metric | Description |
|--------|-------------|
| `cloudflare.firewall.events` | Firewall events grouped by zone, host, and action |
| `cloudflare.firewall.events.total` | Total firewall events per zone |
| `cloudflare.http.requests` | HTTP requests grouped by zone and host |
| `cloudflare.http.requests.total` | Total HTTP requests per zone |
| `cloudflare.loadbalancing.requests` | Load balancing requests grouped by zone, LB name, and proxy status |
| `cloudflare.loadbalancing.requests.total` | Total load balancing requests per zone |
| `cloudflare.healthcheck.events` | Health check events grouped by zone, FQDN, and status |
| `cloudflare.healthcheck.events.total` | Total health check events per zone |
| `cloudflare.logpush.events` | Logpush events grouped by zone and status |
| `cloudflare.logpush.events.total` | Total logpush events per zone |

All metrics are gauges covering the previous full UTC day (midnight-to-midnight window).

## Requirements

- Go 1.21+
- [OpenTelemetry Collector Builder (ocb)](https://github.com/open-telemetry/opentelemetry-collector/tree/main/cmd/builder) v0.96.0

## Building

### 1. Install ocb

```bash
go install go.opentelemetry.io/collector/cmd/builder@v0.96.0
```

### 2. Build the collector binary

```bash
builder --config builder-config.yaml
```

The binary is written to `./dist/otelcol-cloudflaregraphql`.

## Configuration

```yaml
receivers:
  cloudflarereceiver:
    # Cloudflare GraphQL API endpoint (default shown)
    endpoint: https://api.cloudflare.com/client/v4/graphql

    # API token with Analytics:Read permission — "Bearer <token>" format
    api_key: "Bearer ${env:CF_API_TOKEN}"

    # Cloudflare account email
    username: "${env:CF_API_EMAIL}"

    # Zones to collect metrics for
    zones:
      - name: example.com
        tag: <zone-id>   # found in the Cloudflare dashboard

    items_limit: 10000       # max rows per query (Cloudflare limit: 10000)
    timeout: 30s
    retries: 3
    collection_interval: 3600s

processors:
  batch:

exporters:
  prometheus:
    endpoint: "0.0.0.0:9090"

service:
  pipelines:
    metrics:
      receivers: [cloudflarereceiver]
      processors: [batch]
      exporters: [prometheus]
```

See [example-config.yaml](example-config.yaml) for a full example.

### Finding zone IDs

Zone IDs (tags) are available in the Cloudflare dashboard under **Overview** for each domain, or via the REST API:

```bash
curl -s -H "Authorization: Bearer $CF_API_TOKEN" \
  "https://api.cloudflare.com/client/v4/zones" | jq '.result[] | {name, id}'
```

## Running

```bash
CF_API_TOKEN=your_token CF_API_EMAIL=you@example.com \
  ./dist/otelcol-cloudflare --config config.yaml
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
