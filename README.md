# Sentry Tunnel

A high-performance, lightweight HTTP proxy for [Sentry](https://sentry.io) that compresses error events and telemetry data using Brotli compression before forwarding them to Sentry's servers.

## Why Use Sentry Tunnel?

### Ad Blockers & Privacy Extensions
Many ad blockers and privacy-focused browser extensions block requests to Sentry's domains (`*.ingest.sentry.io`). By routing Sentry traffic through your own domain, you can ensure error tracking works for all users.

### Bandwidth Optimization
Sentry Tunnel compresses all payloads using Brotli compression, significantly reducing bandwidth usage—especially beneficial for high-traffic applications.

### Security & Privacy
- Keep your Sentry DSN hidden from client-side code
- Control which project IDs are allowed
- Add an additional security layer between clients and Sentry

### Network Restrictions
Some corporate networks or regions may block direct connections to Sentry. A tunnel on your own infrastructure bypasses these restrictions.

## Features

- **Brotli Compression**: Compresses all payloads with configurable compression levels (up to 100x compression ratio for repetitive data)
- **Connection Pooling**: Efficient HTTP client with connection reuse
- **Project ID Allowlist**: Restrict which Sentry projects can be accessed
- **Graceful Shutdown**: Proper handling of SIGINT/SIGTERM for containerized deployments
- **Configurable**: All settings via environment variables
- **Lightweight**: Single binary, minimal dependencies
- **Full Sentry Support**: Supports all Sentry envelope types (errors, transactions, spans, sessions, replays, profiles, and more)

## Supported Sentry Data Types

Sentry Tunnel supports **all** Sentry envelope item types:

| Type | Description |
|------|-------------|
| **Error Events** | Exception and error tracking |
| **Transactions** | Performance monitoring transactions |
| **Spans** | Detailed performance spans within transactions |
| **Sessions** | Release health and session tracking |
| **Session Replays** | User session replay recordings |
| **Profiles** | Performance profiling data |
| **Attachments** | File attachments (logs, screenshots, etc.) |
| **User Feedback** | User-submitted feedback and comments |
| **Check-ins** | Cron job and scheduled task monitoring |
| **Client Reports** | SDK-side metrics and discarded events |

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/yourusername/sentry-tunnel.git
cd sentry-tunnel

# Build
go build -o sentry-tunnel .

# Run
export SENTRY_HOST=o123456.ingest.sentry.io
./sentry-tunnel
```

### Using Docker

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o sentry-tunnel .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/sentry-tunnel .
EXPOSE 7878
CMD ["./sentry-tunnel"]
```

```bash
# Build
docker build -t sentry-tunnel .

# Run
docker run -d \
  -p 7878:7878 \
  -e SENTRY_HOST=o123456.ingest.sentry.io \
  sentry-tunnel
```

### Using Docker Compose

```yaml
version: '3.8'
services:
  sentry-tunnel:
    build: .
    ports:
      - "7878:7878"
    environment:
      - SENTRY_HOST=o123456.ingest.sentry.io
      - PROXY_PORT=7878
      - PROXY_PATH=/s
      - COMPRESSION_LEVEL=6
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:7878/health"]
      interval: 30s
      timeout: 10s
      retries: 3
```

## Configuration

All configuration is done via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SENTRY_HOST` | **Yes** | - | Sentry ingest hostname (e.g., `o123456.ingest.sentry.io`) |
| `PROXY_PORT` | No | `7878` | Port the tunnel listens on |
| `PROXY_PATH` | No | `/s` | URL path for the tunnel endpoint |
| `SENTRY_HOST_PROTO` | No | `https` | Protocol for upstream Sentry connection |
| `SENTRY_HOST_HEADER` | No | `SENTRY_HOST` value | Host header sent to Sentry |
| `COMPRESSION_LEVEL` | No | `11` | Brotli compression level (0-11) |
| `PROJECT_IDS` | No | - | Comma-separated list of allowed project IDs |

### Compression Levels

| Level | Speed | Ratio | Use Case |
|-------|-------|-------|----------|
| `0-3` | Fast | Lower | High-throughput, latency-sensitive |
| `4-6` | Balanced | Medium | General purpose (recommended) |
| `7-9` | Slow | High | Bandwidth-constrained environments |
| `10-11` | Slowest | Highest | Maximum compression |

## Usage

### Endpoints

Sentry Tunnel exposes two endpoints:

#### 1. Tunnel Endpoint (Default: `/s`)

The primary endpoint for Sentry SDK tunnel configuration. Extracts the DSN from the envelope header and routes to the appropriate project.

```
POST /s
Content-Type: application/x-sentry-envelope

{"event_id":"...","dsn":"https://key@o123456.ingest.sentry.io/123456",...}
{"type":"event"}
{...event payload...}
```

#### 2. API Proxy Endpoint (`/api/*`)

Direct proxy to Sentry's API endpoints. Preserves the original path.

```
POST /api/123456/envelope/
Content-Type: application/x-sentry-envelope

{...envelope payload...}
```

## Client SDK Configuration

### JavaScript (Browser)

```javascript
import * as Sentry from "@sentry/browser";

Sentry.init({
  dsn: "https://your-key@o123456.ingest.sentry.io/123456",
  tunnel: "https://your-domain.com/s",
});
```

### JavaScript (Node.js)

```javascript
const Sentry = require("@sentry/node");

Sentry.init({
  dsn: "https://your-key@o123456.ingest.sentry.io/123456",
  tunnel: "https://your-domain.com/s",
});
```

### React

```javascript
import * as Sentry from "@sentry/react";

Sentry.init({
  dsn: "https://your-key@o123456.ingest.sentry.io/123456",
  tunnel: "https://your-domain.com/s",
  integrations: [
    Sentry.browserTracingIntegration(),
    Sentry.replayIntegration(),
  ],
  tracesSampleRate: 1.0,
  replaysSessionSampleRate: 0.1,
  replaysOnErrorSampleRate: 1.0,
});
```

### Next.js

```javascript
// sentry.client.config.js
import * as Sentry from "@sentry/nextjs";

Sentry.init({
  dsn: "https://your-key@o123456.ingest.sentry.io/123456",
  tunnel: "/api/sentry-tunnel", // or your tunnel URL
  tracesSampleRate: 1.0,
});
```

```javascript
// pages/api/sentry-tunnel.js (if proxying through Next.js API route)
export default async function handler(req, res) {
  try {
    const response = await fetch("https://your-tunnel-domain.com/s", {
      method: "POST",
      body: req.body,
      headers: {
        "Content-Type": req.headers["content-type"],
      },
    });
    res.status(response.status).send(await response.text());
  } catch (error) {
    res.status(500).send("Tunnel error");
  }
}
```

### Vue.js

```javascript
import * as Sentry from "@sentry/vue";
import { createApp } from "vue";

const app = createApp(App);

Sentry.init({
  app,
  dsn: "https://your-key@o123456.ingest.sentry.io/123456",
  tunnel: "https://your-domain.com/s",
  integrations: [
    Sentry.browserTracingIntegration(),
  ],
  tracesSampleRate: 1.0,
});
```

### Angular

```typescript
import * as Sentry from "@sentry/angular";

Sentry.init({
  dsn: "https://your-key@o123456.ingest.sentry.io/123456",
  tunnel: "https://your-domain.com/s",
  integrations: [
    Sentry.browserTracingIntegration(),
  ],
  tracesSampleRate: 1.0,
});
```

### Python

```python
import sentry_sdk

sentry_sdk.init(
    dsn="https://your-key@o123456.ingest.sentry.io/123456",
    tunnel="https://your-domain.com/s",
    traces_sample_rate=1.0,
)
```

### Go

```go
import "github.com/getsentry/sentry-go"

err := sentry.Init(sentry.ClientOptions{
    Dsn: "https://your-key@o123456.ingest.sentry.io/123456",
    // Note: Go SDK doesn't have native tunnel support
    // Use a custom transport or proxy configuration
})
```

### Ruby

```ruby
Sentry.init do |config|
  config.dsn = "https://your-key@o123456.ingest.sentry.io/123456"
  config.tunnel = "https://your-domain.com/s"
  config.traces_sample_rate = 1.0
end
```

### PHP

```php
\Sentry\init([
    'dsn' => 'https://your-key@o123456.ingest.sentry.io/123456',
    'tunnel' => 'https://your-domain.com/s',
    'traces_sample_rate' => 1.0,
]);
```

### Java (Spring Boot)

```java
import io.sentry.Sentry;

Sentry.init(options -> {
    options.setDsn("https://your-key@o123456.ingest.sentry.io/123456");
    // Configure HTTP proxy to point to your tunnel
});
```

### Swift (iOS)

```swift
import Sentry

SentrySDK.start { options in
    options.dsn = "https://your-key@o123456.ingest.sentry.io/123456"
    // Note: iOS SDK requires custom transport for tunnel support
}
```

### Kotlin (Android)

```kotlin
import io.sentry.android.core.SentryAndroid

SentryAndroid.init(context) { options ->
    options.dsn = "https://your-key@o123456.ingest.sentry.io/123456"
    // Note: Android SDK requires custom transport for tunnel support
}
```

### Flutter

```dart
import 'package:sentry_flutter/sentry_flutter.dart';

await SentryFlutter.init(
  (options) {
    options.dsn = 'https://your-key@o123456.ingest.sentry.io/123456';
    // Configure tunnel via custom transport
  },
  appRunner: () => runApp(MyApp()),
);
```

## Deployment Examples

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sentry-tunnel
spec:
  replicas: 3
  selector:
    matchLabels:
      app: sentry-tunnel
  template:
    metadata:
      labels:
        app: sentry-tunnel
    spec:
      containers:
      - name: sentry-tunnel
        image: your-registry/sentry-tunnel:latest
        ports:
        - containerPort: 7878
        env:
        - name: SENTRY_HOST
          value: "o123456.ingest.sentry.io"
        - name: COMPRESSION_LEVEL
          value: "6"
        - name: PROJECT_IDS
          value: "123456,789012"
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "128Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 7878
          initialDelaySeconds: 5
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 7878
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: sentry-tunnel
spec:
  selector:
    app: sentry-tunnel
  ports:
  - port: 80
    targetPort: 7878
  type: ClusterIP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: sentry-tunnel
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "10m"
spec:
  rules:
  - host: sentry-tunnel.your-domain.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: sentry-tunnel
            port:
              number: 80
```

### AWS ECS (Fargate)

```json
{
  "family": "sentry-tunnel",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "256",
  "memory": "512",
  "containerDefinitions": [
    {
      "name": "sentry-tunnel",
      "image": "your-ecr-repo/sentry-tunnel:latest",
      "portMappings": [
        {
          "containerPort": 7878,
          "protocol": "tcp"
        }
      ],
      "environment": [
        {
          "name": "SENTRY_HOST",
          "value": "o123456.ingest.sentry.io"
        },
        {
          "name": "COMPRESSION_LEVEL",
          "value": "6"
        }
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/sentry-tunnel",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "ecs"
        }
      }
    }
  ]
}
```

### Nginx Reverse Proxy

```nginx
upstream sentry_tunnel {
    server 127.0.0.1:7878;
    keepalive 32;
}

server {
    listen 443 ssl http2;
    server_name sentry-tunnel.your-domain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://sentry_tunnel;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";

        # Increase timeouts for large payloads
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;

        # Allow larger request bodies
        client_max_body_size 10m;
    }
}
```

### Caddy

```caddyfile
sentry-tunnel.your-domain.com {
    reverse_proxy localhost:7878 {
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
    }
}
```

### Traefik

```yaml
# docker-compose.yml with Traefik
version: '3.8'
services:
  sentry-tunnel:
    build: .
    environment:
      - SENTRY_HOST=o123456.ingest.sentry.io
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.sentry-tunnel.rule=Host(`sentry-tunnel.your-domain.com`)"
      - "traefik.http.routers.sentry-tunnel.tls=true"
      - "traefik.http.routers.sentry-tunnel.tls.certresolver=letsencrypt"
      - "traefik.http.services.sentry-tunnel.loadbalancer.server.port=7878"
```

## Security Considerations

### Project ID Allowlist

Restrict which Sentry projects can use your tunnel:

```bash
export PROJECT_IDS=123456,789012,345678
```

This prevents unauthorized use of your tunnel for other Sentry projects.

### Rate Limiting

Consider adding rate limiting at the reverse proxy level:

```nginx
# Nginx rate limiting
limit_req_zone $binary_remote_addr zone=sentry:10m rate=100r/s;

location / {
    limit_req zone=sentry burst=200 nodelay;
    proxy_pass http://sentry_tunnel;
}
```

### CORS Configuration

If your tunnel is on a different domain than your application, configure CORS at the reverse proxy:

```nginx
location / {
    add_header 'Access-Control-Allow-Origin' 'https://your-app.com' always;
    add_header 'Access-Control-Allow-Methods' 'POST, OPTIONS' always;
    add_header 'Access-Control-Allow-Headers' 'Content-Type' always;

    if ($request_method = 'OPTIONS') {
        return 204;
    }

    proxy_pass http://sentry_tunnel;
}
```

### Network Security

- Deploy the tunnel in a private network when possible
- Use TLS for all connections
- Consider IP allowlisting for the tunnel endpoint
- Monitor for unusual traffic patterns

## Monitoring & Observability

### Logging

Sentry Tunnel logs compression statistics for each request:

```
2024/01/15 10:30:45 Tunnel: 15234 -> 1892, 8.05, 12 ms
2024/01/15 10:30:46 Envelope: 8456 -> 956, 8.85, 8 ms
```

Format: `{endpoint}: {original_size} -> {compressed_size}, {compression_ratio}, {compression_time_ms}`

### Metrics to Monitor

- Request rate (requests/second)
- Compression ratio (higher is better)
- Response latency
- Error rate (5xx responses)
- Upstream Sentry response times

### Health Checks

The tunnel responds to requests and can be health-checked via any endpoint. For Kubernetes/Docker, use:

```bash
curl -X POST http://localhost:7878/s -d '{"dsn":"test"}' || exit 1
```

## Performance Tuning

### Connection Pooling

The tunnel uses connection pooling by default:

- Max idle connections: 100
- Idle connection timeout: 90 seconds
- TLS handshake timeout: 10 seconds
- Response header timeout: 10 seconds

### Compression Level Selection

| Traffic Volume | Recommended Level | Reason |
|----------------|-------------------|--------|
| < 100 req/s | `9-11` | Maximum compression, latency acceptable |
| 100-1000 req/s | `5-6` | Balanced performance |
| > 1000 req/s | `1-3` | Minimize CPU usage |

### Memory Considerations

Each request loads the full body into memory for compression. For high-traffic deployments:

- Set appropriate memory limits
- Consider multiple replicas for horizontal scaling
- Monitor memory usage under load

## Troubleshooting

### Common Issues

#### Events Not Appearing in Sentry

1. Check tunnel logs for errors
2. Verify `SENTRY_HOST` is correct
3. Ensure the DSN in your client matches your Sentry project
4. Check if `PROJECT_IDS` is blocking your project

#### High Latency

1. Reduce `COMPRESSION_LEVEL`
2. Check network connectivity to Sentry
3. Increase resources (CPU/memory)
4. Add more replicas

#### 502 Bad Gateway

1. Verify Sentry is accessible from the tunnel
2. Check DNS resolution
3. Verify `SENTRY_HOST_PROTO` is correct (http vs https)

#### Connection Timeouts

1. Increase timeout values in the reverse proxy
2. Check firewall rules
3. Verify Sentry's status at https://status.sentry.io

### Debug Mode

For debugging, check the logs for detailed compression statistics and any error messages:

```bash
docker logs -f sentry-tunnel
```

## Testing

Run the test suite:

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run with coverage
go test -cover ./...

# Run benchmarks
go test -bench=. ./...
```

### Test Coverage

The test suite covers:

- Configuration loading
- Brotli compression (various levels and data types)
- Header forwarding
- API handler behavior
- Tunnel handler behavior
- DSN parsing and validation
- Project ID allowlist
- All Sentry envelope types
- Concurrent request handling
- Error scenarios

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Setup

```bash
# Clone
git clone https://github.com/yourusername/sentry-tunnel.git
cd sentry-tunnel

# Install dependencies
go mod download

# Run tests
go test ./...

# Build
go build .

# Run locally
SENTRY_HOST=o123456.ingest.sentry.io ./sentry-tunnel
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [Sentry](https://sentry.io) for their excellent error tracking platform
- [Brotli](https://github.com/google/brotli) compression algorithm by Google
- [andybalholm/brotli](https://github.com/andybalholm/brotli) Go implementation

## Related Resources

- [Sentry Tunnel Documentation](https://docs.sentry.io/platforms/javascript/troubleshooting/#using-the-tunnel-option)
- [Sentry Envelope Format](https://develop.sentry.dev/sdk/envelopes/)
- [Brotli Compression](https://github.com/google/brotli)
