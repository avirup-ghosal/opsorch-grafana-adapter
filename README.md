# OpsOrch Grafana Adapter

[![Version](https://img.shields.io/github/v/release/opsorch/opsorch-grafana-adapter)](https://github.com/opsorch/opsorch-grafana-adapter/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/opsorch/opsorch-grafana-adapter)](https://github.com/opsorch/opsorch-grafana-adapter/blob/main/go.mod)
[![License](https://img.shields.io/github/license/opsorch/opsorch-grafana-adapter)](https://github.com/opsorch/opsorch-grafana-adapter/blob/main/LICENSE)
[![CI](https://github.com/opsorch/opsorch-grafana-adapter/workflows/CI/badge.svg)](https://github.com/opsorch/opsorch-grafana-adapter/actions)

This adapter integrates OpsOrch with Grafana Loki, enabling log querying, filtering, and discovery through the Loki HTTP API.

## Capabilities

This adapter provides one primary capability:

1. **Log Provider**: Query log streams and entries from Grafana Loki

## Features

### Logs
- **Log Query**: Execute LogQL queries via structured expressions
- **QueryScope Support**: Automatically map service and environment to Loki stream labels
- **Filtering**: Label-based stream filtering with exact match operators (`=`)
- **Search**: Full-text search over log message lines (`|=`)
- **Metadata Mapping**: Support for dynamic label injection via OpsOrch Metadata
- **Range Queries**: Query logs over precise time ranges with configurable limits

### Version Compatibility

- **Adapter Version**: 0.1.0
- **Requires OpsOrch Core**: >=0.1.0
- **Grafana Loki**: 2.x+
- **Go Version**: 1.21+

## Configuration

### Log Provider Configuration

The log adapter requires the following configuration:

| Field | Type | Required | Description | Default |
|-------|------|----------|-------------|---------|
| `url` | string | Yes | The base URL of the Loki server (e.g., `http://localhost:3100`) | - |

### Example Configuration

**Log Adapter - JSON format:**
```json
{
  "url": "http://localhost:3100"
}
```
**Environment variables (Log):**
```bash
export OPSORCH_LOG_PLUGIN=/path/to/bin/logplugin
export OPSORCH_LOG_CONFIG='{"url":"http://localhost:3100"}'
```
## Field Mapping

### Log Adapter

#### Query Mapping

| OpsOrch Field | LogQL Mapping | Notes |
|---------------|----------------|-------|
| `LogQuery.Expression.Filters` | Pipeline line filter | Parsed via `| json`. Uses `=` by default. `regex` and `contains` operators map to `=~` with regex matching. |
| `LogQuery.Expression.Search` | Pipeline line filter | Converted to `\|= "search_term"` syntax |
| `LogQuery.Scope.Service` | Stream selector | Adds `service="<name>"` stream selector |
| `LogQuery.Scope.Environment` | Stream selector | Adds `env="<name>"` stream selector |
| `LogQuery.Metadata` | Stream selector | Injects arbitrary key/value pairs.|
| `LogQuery.Limit` | `limit` API param | Maximum number of log lines to return |
| `LogQuery.Start` /`End` | `start` /`end` API param | Time window for the query |
#### Response Normalization

| Loki Field | OpsOrch Field | Notes |
|------------------|---------------|-------|
| `stream` | `Labels` | Extracted log stream labels |
| `values[0]`(Timestamp) | `Timestamp` | Converted from nanosecond string to `timeTime` |
| `values[1]` (Message) | `Message` | Raw log line text  |

## Usage

### In-Process Mode

Import the log adapter and register the grafana provider explicitly with Opsorch Core:

```go
import (
    corelog "github.com/opsorch/opsorch-core/log"
    adapterlog "github.com/opsorch/opsorch-grafana-adapter/log"
)

func init() {
    if err := corelog.RegisterProvider("grafana", func(cfg map[string]any) (corelog.Provider, error) {
        return adapterlog.New(cfg)
    }); err != nil {
        panic(err)
    }
}
```

Configure via environment variables:

```bash
export OPSORCH_LOG_PROVIDER=grafana
export OPSORCH_LOG_CONFIG='{"url":"http://localhost:3100"}'
```

### Plugin Mode

Build the plugin binaries:

```bash
make plugin
```

This builds one plugin binary in `./bin/`:
- `logplugin`

Configure OpsOrch Core to use the plugin:

```bash
# Log Plugin
export OPSORCH_LOG_PLUGIN=/path/to/bin/logplugin
export OPSORCH_LOG_CONFIG='{"url":"http://localhost:3100"}'
```

### Docker Deployment

Download pre-built plugin binaries from [GitHub Releases](https://github.com/opsorch/opsorch-grafana-adapter/releases):

```dockerfile
FROM ghcr.io/opsorch/opsorch-core:latest
WORKDIR /opt/opsorch

# Download plugin binary
ADD https://github.com/opsorch/opsorch-grafana-adapter/releases/download/v0.1.0/logplugin-linux-amd64 ./plugins/logplugin
RUN chmod +x ./plugins/*

# Configure plugins
ENV OPSORCH_LOG_PLUGIN=/opt/opsorch/plugins/logplugin
```

## Query Examples

### Basic Log Query with Filters

```json
{
  "expression": {
    "filters": [
      {"field": "app", "operator": "=", "value": "frontend"}
    ]
  },
  "start": "2024-01-01T00:00:00Z",
  "end": "2024-01-01T01:00:00Z",
  "limit": 100
}
```

Generates LogQL: `{service_name=~".+"} | json | app="frontend"`

### Query with Search

```json
{
  "expression": {
    "filters": [
      {"field": "app", "operator": "=", "value": "backend"}
    ],
    "search": "database timeout"
  },
  "start": "2024-01-01T00:00:00Z",
  "end": "2024-01-01T01:00:00Z",
  "limit": 100
}
```

Generates LogQL: `{service_name=~".+"} | json | app="backend" |= "database timeout"`

### Query with Scope and Metadata

```json
{
  "scope": {
    "service": "payment-api",
    "environment": "prod"
  },
  "metadata": {
    "cluster": "us-east-1"
  },
  "start": "2024-01-01T00:00:00Z",
  "end": "2024-01-01T01:00:00Z"
}
```

Generates LogQL: `{service="payment-api", env="prod", cluster="us-east-1"}`

### Query with Contains Operator

```json
{
  "expression": {
    "filters": [
      {"field": "message", "operator": "contains", "value": "database"}
    ]
  },
  "start": "2024-01-01T00:00:00Z",
  "end": "2024-01-01T01:00:00Z"
}
```
Generates LogQL: `{service_name=~".+"} | json | message=~".*database.*"`

## Development

### Prerequisites

- Go 1.21 or later
- Access to a Loki instance (for integration tests)

### Building

```bash
# Download dependencies
go mod download

# Run unit tests
make test

# Build all packages
make build

# Build plugin binaries
make plugin

# Run integration tests (requires Loki)
make integ
```


### Testing

**Unit Tests:**
```bash
make test
```

**Integration Tests:**

Integration tests require running a Grafana Loki instance. You can use Docker:

**Prerequisites:**
- Docker installed
- Loki running on localhost:3100

**Setup with Docker:**
```bash
# Start Loki
docker run --rm -d -p 3100:3100 --name loki grafana/loki

# Set environment variables
export LOKI_URL=http://localhost:3100

# Run integration tests
make integ

# Clean up
docker stop loki
```

**What the tests do:**
- **Self-Seeding**: Automatically injects mock log data into the Loki API via the push endpoint before querying, ensuring predictable test runs without relying on pre-existing data.
- **Log tests**: Query Loki streams, test exact label-based filtering, full-text search (|=), and Scope/Metadata mapping.
- **Assertion checks**: Validates the exact labels, strings, and array lengths returned by the queries.

**Expected behavior:**
- Tests verify basic queries and safe fallback defaults work
- Tests verify exact stream filtering (e.g., {app="opsorch"})
- Tests verify full-text search inside log lines (e.g., |= "error")
- Tests verify Scope and Metadata map correctly to stream labels
- Tests fail immediately with an exit code of 1 if assertions are not met, enforcing strict CI/CD gates


### Project Structure

```
opsorch-grafana-adapter/
├── log/                      # Log provider implementation
│   ├── grafana_provider.go      # Core provider logic
│   └── grafana_provider_test.go
│                     
│   
├── cmd/
│   ├── logplugin/           # Log plugin entrypoint
│       └── main.go
│   
│      
├── integ/                       # Integration tests
│   └── log.go   
│         
│       
├── Makefile
└── README.md
```

**Key Components:**

- **log/grafana_provider.go**: Implements log.Provider interface, builds LogQL queries and executes range queries
- **cmd/logplugin**: JSON-RPC plugin wrapper for log provider

## CI/CD & Pre-Built Binaries

The repository includes GitHub Actions workflows:

- **CI** (`ci.yml`): Runs tests (including integration tests with Loki) and linting on every push/PR to main
- **Release** (`release.yml`): Manual workflow that:
  - Runs tests and linting
  - Creates version tags (patch/minor/major)
  - Builds multi-arch binaries for the log plugin (linux-amd64, linux-arm64, darwin-amd64, darwin-arm64)
  - Publishes binaries as GitHub release assets

### Downloading Pre-Built Binaries

Pre-built plugin binaries are available from [GitHub Releases](https://github.com/opsorch/opsorch-grafana-adapter/releases).

**Supported platforms:**
- Linux (amd64, arm64)
- macOS (amd64, arm64)

**Available binaries:**
- `logplugin-{platform}-{arch}`

## Plugin RPC Contract

OpsOrch Core communicates with the plugins over stdin/stdout using JSON-RPC.

### Message Format

**Request:**
```json
{
  "method": "{capability}.{operation}",
  "config": { /* decrypted configuration */ },
  "payload": { /* method-specific request body */ }
}
```

**Response:**
```json
{
  "result": { /* method-specific result */ },
  "error": "optional error message"
}
```
### Configuration Injection

The `config` field contains the decrypted configuration map from `OPSORCH_{CAPABILITY}_CONFIG`. The plugin receives this on every request, so it never stores secrets on disk.

### Supported Methods

#### Log Plugin

- `log.query`: Execute a log query against Loki

**Example - log.query:**
```json
{
  "method": "log.query",
  "config": {"url": "http://localhost:3100"},
  "payload": {
    "expression": {
      "filters": [
        {"field": "app", "operator": "=", "value": "frontend"}
      ],
      "search": "timeout"
    },
    "start": "2024-01-01T00:00:00Z",
    "end": "2024-01-01T01:00:00Z",
    "limit": 100
  }
}
```

**Response:**
```json
{
  "result": {
    "entries": [
      {
        "timestamp": "2024-01-01T00:30:15Z",
        "labels": {"app": "frontend", "env": "prod"},
        "message": "error: database connection timeout"
      }
    ]
  }
}
```
## Security Considerations

1. **Network access**: Ensure Grafana Loki is accessible from OpsOrch Core
2. **Authentication**: If Loki requires authentication (e.g., Basic Auth via a reverse proxy), configure it appropriately
3. **TLS**: Use HTTPS URLs for production deployments
4. **Firewall rules**: Restrict access to Loki to authorized systems only
5. **Query limits**: Be mindful of querying massive time ranges or high limits without stream selectors, as this can overload Loki's memory and disk I/O.

## License

Apache 2.0

See LICENSE file in the repository root.
