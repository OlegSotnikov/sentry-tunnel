# Sentry Tunnel

[![CI](https://github.com/OlegSotnikov/sentry-tunnel/actions/workflows/ci.yml/badge.svg)](https://github.com/OlegSotnikov/sentry-tunnel/actions/workflows/ci.yml)
[![Docker Image](https://img.shields.io/docker/v/appmasterio/sentry-tunnel?label=docker&sort=semver)](https://hub.docker.com/r/appmasterio/sentry-tunnel)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A high-performance HTTP proxy for [Sentry](https://sentry.io) that compresses error events and telemetry data using Brotli compression before forwarding them to Sentry's servers.

## Why Use Sentry Tunnel?

### Ad Blockers & Privacy Extensions
Many ad blockers and privacy-focused browser extensions block requests to Sentry's domains (`*.ingest.sentry.io`). By routing Sentry traffic through your own domain, you can ensure error tracking works for all users.

### Bandwidth Optimization
Compresses all payloads using Brotli compression, reducing bandwidth usage—especially beneficial for high-traffic applications.

### Network Restrictions
Some corporate networks or regions may block direct connections to Sentry. A tunnel on your own infrastructure bypasses these restrictions.

> **Note:** The tunnel does not hide your Sentry DSN from client-side code. The DSN is still visible in your JavaScript bundle. The primary benefit is bypassing ad blockers and network restrictions.

## Features

- **Brotli Compression**: Configurable compression levels (0-11)
- **Connection Pooling**: Efficient HTTP client with keep-alive
- **Project ID Allowlist**: Restrict which Sentry projects can use the tunnel
- **Graceful Shutdown**: Proper SIGINT/SIGTERM handling
- **Lightweight**: Single static binary, ~6MB

## Supported Sentry Data Types

All Sentry envelope item types are supported:

- Error Events
- Transactions & Spans
- Sessions
- Session Replays
- Profiles
- Attachments
- User Feedback
- Cron Check-ins
- Client Reports

## Installation

### Docker

The published image is `FROM scratch`: the static binary, a CA bundle and the
timezone database, with no shell and no package manager. It runs as uid 65532
and is built for `linux/amd64` and `linux/arm64` under one tag.

```bash
docker run -d -p 7878:7878 \
  -e SENTRY_HOST=o123456.ingest.sentry.io \
  appmasterio/sentry-tunnel:latest
```

Pin a release rather than `latest` for anything you care about:

```bash
docker pull appmasterio/sentry-tunnel:v1.0.0
```

### Docker Compose

```yaml
services:
  sentry-tunnel:
    image: appmasterio/sentry-tunnel:v1.0.0
    ports:
      - "7878:7878"
    environment:
      - SENTRY_HOST=o123456.ingest.sentry.io
    restart: unless-stopped
```

### Prebuilt binaries

Each [release](https://github.com/OlegSotnikov/sentry-tunnel/releases) carries
static binaries for Linux and macOS on amd64 and arm64, plus `SHA256SUMS`.

### From source

```bash
git clone https://github.com/OlegSotnikov/sentry-tunnel.git
cd sentry-tunnel

# Static binary — CGO_ENABLED=0 is what makes it run in a scratch container
CGO_ENABLED=0 go build -ldflags "-s -w" -o sentry-tunnel .

# Run
export SENTRY_HOST=o123456.ingest.sentry.io
./sentry-tunnel
```

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SENTRY_HOST` | **Yes** | - | Sentry ingest hostname (e.g., `o123456.ingest.sentry.io`) |
| `PROXY_PORT` | No | `7878` | Port to listen on |
| `PROXY_PATH` | No | `/s` | URL path for tunnel endpoint |
| `SENTRY_HOST_PROTO` | No | `https` | Protocol for upstream connection |
| `SENTRY_HOST_HEADER` | No | `SENTRY_HOST` | Host header sent to Sentry |
| `COMPRESSION_LEVEL` | No | `11` | Brotli compression level (0-11) |
| `PROJECT_IDS` | No | - | Comma-separated allowed project IDs |

### Compression Levels

| Level | Use Case |
|-------|----------|
| `0-3` | High throughput, low latency |
| `4-6` | Balanced (recommended for most cases) |
| `7-11` | Maximum compression, higher CPU |

## Endpoints

### Tunnel Endpoint (`/s` by default)

Primary endpoint for Sentry SDK `tunnel` option. Extracts DSN from envelope header.

```
POST /s
Content-Type: application/x-sentry-envelope
```

### API Proxy (`/api/*`)

Direct proxy preserving the original path.

```
POST /api/123456/envelope/
```

## Client Configuration

### JavaScript

```javascript
import * as Sentry from "@sentry/browser";

Sentry.init({
  dsn: "https://key@o123456.ingest.sentry.io/123456",
  tunnel: "https://your-domain.com/s",
});
```

### React / Vue / Angular

```javascript
Sentry.init({
  dsn: "https://key@o123456.ingest.sentry.io/123456",
  tunnel: "https://your-domain.com/s",
  integrations: [Sentry.browserTracingIntegration()],
  tracesSampleRate: 1.0,
});
```

### Python

```python
import sentry_sdk

sentry_sdk.init(
    dsn="https://key@o123456.ingest.sentry.io/123456",
    tunnel="https://your-domain.com/s",
)
```

### Other SDKs

Most Sentry SDKs support the `tunnel` option. Refer to [Sentry's documentation](https://docs.sentry.io/platforms/javascript/troubleshooting/#using-the-tunnel-option) for your specific platform.

## Reverse Proxy Example (Nginx)

```nginx
location /s {
    proxy_pass http://127.0.0.1:7878;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Connection "";
}
```

## Security

### Project ID Allowlist

Restrict which projects can use your tunnel:

```bash
export PROJECT_IDS=123456,789012
```

### Rate Limiting

Add rate limiting at your reverse proxy to prevent abuse.

## Logging

Each request logs compression statistics:

```
Tunnel: 15234 -> 1892, 8.05, 12 ms
```

Format: `{original_size} -> {compressed_size}, {ratio}, {time_ms}`

## Testing

```bash
go test ./...           # Run tests
go test -cover ./...    # With coverage
go test -bench=. ./...  # With benchmarks
```

## Releasing

Pushing a `v*` tag builds the binaries, publishes a GitHub Release with
checksums, and pushes `appmasterio/sentry-tunnel:<tag>` and `:latest` for both
architectures with an SBOM and provenance attached.

```bash
git tag v1.0.0 && git push origin v1.0.0
```

## License

[MIT](LICENSE)
