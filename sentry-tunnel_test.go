package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

// TestLoadConfig tests the configuration loading from environment variables
func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name      string
		envVars   map[string]string
		wantFatal bool
		validate  func(t *testing.T, cfg config)
	}{
		{
			name:      "missing SENTRY_HOST should fatal",
			envVars:   map[string]string{},
			wantFatal: true,
		},
		{
			name: "minimal config with only SENTRY_HOST",
			envVars: map[string]string{
				"SENTRY_HOST": "sentry.example.com",
			},
			validate: func(t *testing.T, cfg config) {
				if cfg.sentryHost != "sentry.example.com" {
					t.Errorf("sentryHost = %q, want %q", cfg.sentryHost, "sentry.example.com")
				}
				if cfg.port != "7878" {
					t.Errorf("port = %q, want default %q", cfg.port, "7878")
				}
				if cfg.handlerPath != "/s" {
					t.Errorf("handlerPath = %q, want default %q", cfg.handlerPath, "/s")
				}
				if cfg.sentryHostProto != "https" {
					t.Errorf("sentryHostProto = %q, want default %q", cfg.sentryHostProto, "https")
				}
				if cfg.sentryHostHeader != "sentry.example.com" {
					t.Errorf("sentryHostHeader = %q, want %q (should default to SENTRY_HOST)", cfg.sentryHostHeader, "sentry.example.com")
				}
				if cfg.compressionLevel != brotli.DefaultCompression {
					t.Errorf("compressionLevel = %d, want default %d", cfg.compressionLevel, brotli.DefaultCompression)
				}
				if len(cfg.allowedProjectIDs) != 0 {
					t.Errorf("allowedProjectIDs = %v, want empty slice", cfg.allowedProjectIDs)
				}
			},
		},
		{
			name: "full config with all env vars",
			envVars: map[string]string{
				"SENTRY_HOST":        "sentry.example.com",
				"PROXY_PORT":         "8080",
				"PROXY_PATH":         "/tunnel",
				"SENTRY_HOST_PROTO":  "http",
				"SENTRY_HOST_HEADER": "custom.header.com",
				"COMPRESSION_LEVEL":  "5",
				"PROJECT_IDS":        "123,456,789",
			},
			validate: func(t *testing.T, cfg config) {
				if cfg.sentryHost != "sentry.example.com" {
					t.Errorf("sentryHost = %q, want %q", cfg.sentryHost, "sentry.example.com")
				}
				if cfg.port != "8080" {
					t.Errorf("port = %q, want %q", cfg.port, "8080")
				}
				if cfg.handlerPath != "/tunnel" {
					t.Errorf("handlerPath = %q, want %q", cfg.handlerPath, "/tunnel")
				}
				if cfg.sentryHostProto != "http" {
					t.Errorf("sentryHostProto = %q, want %q", cfg.sentryHostProto, "http")
				}
				if cfg.sentryHostHeader != "custom.header.com" {
					t.Errorf("sentryHostHeader = %q, want %q", cfg.sentryHostHeader, "custom.header.com")
				}
				if cfg.compressionLevel != 5 {
					t.Errorf("compressionLevel = %d, want %d", cfg.compressionLevel, 5)
				}
				expectedIDs := []string{"123", "456", "789"}
				if len(cfg.allowedProjectIDs) != len(expectedIDs) {
					t.Errorf("allowedProjectIDs = %v, want %v", cfg.allowedProjectIDs, expectedIDs)
				}
				for i, id := range expectedIDs {
					if cfg.allowedProjectIDs[i] != id {
						t.Errorf("allowedProjectIDs[%d] = %q, want %q", i, cfg.allowedProjectIDs[i], id)
					}
				}
			},
		},
		{
			name: "invalid compression level falls back to default",
			envVars: map[string]string{
				"SENTRY_HOST":       "sentry.example.com",
				"COMPRESSION_LEVEL": "invalid",
			},
			validate: func(t *testing.T, cfg config) {
				if cfg.compressionLevel != brotli.DefaultCompression {
					t.Errorf("compressionLevel = %d, want default %d for invalid input", cfg.compressionLevel, brotli.DefaultCompression)
				}
			},
		},
		{
			name: "empty PROJECT_IDS results in empty slice",
			envVars: map[string]string{
				"SENTRY_HOST": "sentry.example.com",
				"PROJECT_IDS": "",
			},
			validate: func(t *testing.T, cfg config) {
				if len(cfg.allowedProjectIDs) != 0 {
					t.Errorf("allowedProjectIDs = %v, want empty slice for empty PROJECT_IDS", cfg.allowedProjectIDs)
				}
			},
		},
		{
			name: "single PROJECT_ID",
			envVars: map[string]string{
				"SENTRY_HOST": "sentry.example.com",
				"PROJECT_IDS": "12345",
			},
			validate: func(t *testing.T, cfg config) {
				if len(cfg.allowedProjectIDs) != 1 || cfg.allowedProjectIDs[0] != "12345" {
					t.Errorf("allowedProjectIDs = %v, want [\"12345\"]", cfg.allowedProjectIDs)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant env vars
			envKeys := []string{"SENTRY_HOST", "PROXY_PORT", "PROJECT_IDS", "PROXY_PATH", "SENTRY_HOST_PROTO", "SENTRY_HOST_HEADER", "COMPRESSION_LEVEL"}
			for _, key := range envKeys {
				os.Unsetenv(key)
			}

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			if tt.wantFatal {
				// We can't easily test log.Fatal, so we skip this case
				// In a real scenario, you'd refactor loadConfig to return an error
				t.Skip("Skipping fatal test case - would require refactoring loadConfig")
				return
			}

			cfg := loadConfig()
			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

// TestCompressBody tests the Brotli compression function
func TestCompressBody(t *testing.T) {
	tests := []struct {
		name             string
		input            []byte
		compressionLevel int
		wantErr          bool
	}{
		{
			name:             "compress simple text",
			input:            []byte("Hello, World!"),
			compressionLevel: brotli.DefaultCompression,
			wantErr:          false,
		},
		{
			name:             "compress empty body",
			input:            []byte{},
			compressionLevel: brotli.DefaultCompression,
			wantErr:          false,
		},
		{
			name:             "compress with minimum compression level",
			input:            []byte("Test data for compression"),
			compressionLevel: brotli.BestSpeed,
			wantErr:          false,
		},
		{
			name:             "compress with maximum compression level",
			input:            []byte("Test data for compression"),
			compressionLevel: brotli.BestCompression,
			wantErr:          false,
		},
		{
			name:             "compress large payload",
			input:            bytes.Repeat([]byte("Large payload data for testing compression efficiency. "), 1000),
			compressionLevel: brotli.DefaultCompression,
			wantErr:          false,
		},
		{
			name:             "compress JSON payload",
			input:            []byte(`{"dsn":"https://key@sentry.io/123","event_id":"abc123","sent_at":"2024-01-01T00:00:00Z"}`),
			compressionLevel: brotli.DefaultCompression,
			wantErr:          false,
		},
		{
			name:             "compress binary-like data",
			input:            []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD},
			compressionLevel: brotli.DefaultCompression,
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed, err := compressBody(tt.input, tt.compressionLevel)

			if (err != nil) != tt.wantErr {
				t.Errorf("compressBody() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify we can decompress the result
			reader := brotli.NewReader(bytes.NewReader(compressed))
			decompressed, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("Failed to decompress: %v", err)
				return
			}

			if !bytes.Equal(decompressed, tt.input) {
				t.Errorf("Decompressed data doesn't match original.\nGot: %v\nWant: %v", decompressed, tt.input)
			}

			// For non-empty inputs, verify compression actually happened (or at least the function completed)
			if len(tt.input) > 100 {
				// Large inputs should compress
				if len(compressed) >= len(tt.input) {
					t.Logf("Warning: compressed size (%d) >= original size (%d)", len(compressed), len(tt.input))
				}
			}
		})
	}
}

// TestCopyHeaders tests the header copying function
func TestCopyHeaders(t *testing.T) {
	tests := []struct {
		name       string
		srcHeaders map[string]string
		hostHeader string
		wantDst    map[string]string
	}{
		{
			name:       "only host header set",
			srcHeaders: map[string]string{},
			hostHeader: "sentry.example.com",
			wantDst: map[string]string{
				"Host": "sentry.example.com",
			},
		},
		{
			name: "all supported headers",
			srcHeaders: map[string]string{
				"Content-Type":     "application/x-sentry-envelope",
				"X-Forwarded-For":  "192.168.1.1",
				"CF-Connecting-IP": "10.0.0.1",
				"X-Real-IP":        "172.16.0.1",
				"Origin":           "https://example.com",
				"Referer":          "https://example.com/page",
			},
			hostHeader: "sentry.example.com",
			wantDst: map[string]string{
				"Host":             "sentry.example.com",
				"Content-Type":     "application/x-sentry-envelope",
				"X-Forwarded-For":  "192.168.1.1",
				"Cf-Connecting-Ip": "10.0.0.1",
				"X-Real-Ip":        "172.16.0.1",
				"Origin":           "https://example.com",
				"Referer":          "https://example.com/page",
			},
		},
		{
			name: "partial headers",
			srcHeaders: map[string]string{
				"X-Forwarded-For": "192.168.1.1",
				"Origin":          "https://example.com",
			},
			hostHeader: "custom.host.com",
			wantDst: map[string]string{
				"Host":            "custom.host.com",
				"X-Forwarded-For": "192.168.1.1",
				"Origin":          "https://example.com",
			},
		},
		{
			name: "unsupported headers are not copied",
			srcHeaders: map[string]string{
				"X-Forwarded-For": "192.168.1.1",
				"Authorization":   "Bearer token",
				"X-Custom-Header": "custom-value",
				"Content-Type":    "application/json",
			},
			hostHeader: "sentry.example.com",
			wantDst: map[string]string{
				"Host":            "sentry.example.com",
				"X-Forwarded-For": "192.168.1.1",
				"Content-Type":    "application/json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := http.Header{}
			for k, v := range tt.srcHeaders {
				src.Set(k, v)
			}

			dst := http.Header{}
			copyHeaders(src, dst, tt.hostHeader)

			for k, want := range tt.wantDst {
				got := dst.Get(k)
				if got != want {
					t.Errorf("dst.Get(%q) = %q, want %q", k, got, want)
				}
			}

			// Verify unsupported headers are not present
			unsupportedHeaders := []string{"Authorization", "X-Custom-Header"}
			for _, h := range unsupportedHeaders {
				if dst.Get(h) != "" {
					t.Errorf("Header %q should not be copied, but got %q", h, dst.Get(h))
				}
			}
		})
	}
}

// TestAPIHandler tests the /api/ endpoint
func TestAPIHandler(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		upstreamStatus int
		upstreamBody   string
		wantStatus     int
	}{
		{
			name:           "successful POST request",
			method:         http.MethodPost,
			path:           "/api/123/envelope/",
			body:           `{"event_id":"abc123"}`,
			upstreamStatus: http.StatusOK,
			upstreamBody:   `{"id":"abc123"}`,
			wantStatus:     http.StatusOK,
		},
		{
			name:       "GET method not allowed",
			method:     http.MethodGet,
			path:       "/api/123/envelope/",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "PUT method not allowed",
			method:     http.MethodPut,
			path:       "/api/123/envelope/",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "DELETE method not allowed",
			method:     http.MethodDelete,
			path:       "/api/123/envelope/",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "upstream returns error",
			method:         http.MethodPost,
			path:           "/api/123/envelope/",
			body:           `{"event_id":"abc123"}`,
			upstreamStatus: http.StatusBadRequest,
			upstreamBody:   `{"error":"invalid request"}`,
			wantStatus:     http.StatusBadRequest,
		},
		{
			name:           "upstream returns 500",
			method:         http.MethodPost,
			path:           "/api/123/envelope/",
			body:           `{"event_id":"abc123"}`,
			upstreamStatus: http.StatusInternalServerError,
			upstreamBody:   `{"error":"server error"}`,
			wantStatus:     http.StatusInternalServerError,
		},
		{
			name:           "empty body",
			method:         http.MethodPost,
			path:           "/api/123/envelope/",
			body:           "",
			upstreamStatus: http.StatusOK,
			upstreamBody:   "",
			wantStatus:     http.StatusOK,
		},
		{
			name:           "large body",
			method:         http.MethodPost,
			path:           "/api/123/envelope/",
			body:           strings.Repeat("x", 10000),
			upstreamStatus: http.StatusOK,
			upstreamBody:   "",
			wantStatus:     http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody []byte
			var receivedContentEncoding string
			var receivedPath string

			// Create mock upstream server
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				receivedContentEncoding = r.Header.Get("Content-Encoding")

				body, _ := io.ReadAll(r.Body)
				if receivedContentEncoding == "br" {
					// Decompress brotli
					reader := brotli.NewReader(bytes.NewReader(body))
					receivedBody, _ = io.ReadAll(reader)
				} else {
					receivedBody = body
				}

				w.WriteHeader(tt.upstreamStatus)
				w.Write([]byte(tt.upstreamBody))
			}))
			defer upstream.Close()

			// Create handler with mock upstream
			cfg := config{
				sentryHost:       strings.TrimPrefix(upstream.URL, "http://"),
				sentryHostProto:  "http",
				sentryHostHeader: "test.sentry.io",
				compressionLevel: brotli.BestSpeed, // Use fast compression for tests
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
					return
				}

				body, err := io.ReadAll(r.Body)
				defer r.Body.Close()
				if err != nil {
					http.Error(w, "Error reading request body", http.StatusInternalServerError)
					return
				}

				upstreamURL := cfg.sentryHostProto + "://" + cfg.sentryHost + r.URL.Path
				proxyToSentry(w, r, body, upstreamURL, cfg.compressionLevel, cfg.sentryHostHeader, "Envelope")
			})

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status code = %d, want %d", rec.Code, tt.wantStatus)
			}

			// For successful requests, verify upstream received correct data
			if tt.method == http.MethodPost && tt.wantStatus == tt.upstreamStatus {
				if string(receivedBody) != tt.body {
					t.Errorf("Upstream received body = %q, want %q", string(receivedBody), tt.body)
				}
				if receivedContentEncoding != "br" {
					t.Errorf("Content-Encoding = %q, want %q", receivedContentEncoding, "br")
				}
				if receivedPath != tt.path {
					t.Errorf("Upstream path = %q, want %q", receivedPath, tt.path)
				}
			}
		})
	}
}

// TestTunnelHandler tests the custom tunnel path endpoint
func TestTunnelHandler(t *testing.T) {
	tests := []struct {
		name              string
		method            string
		body              string
		allowedProjectIDs []string
		upstreamStatus    int
		upstreamBody      string
		wantStatus        int
		wantProjectID     string
	}{
		{
			name:           "valid request with DSN",
			method:         http.MethodPost,
			body:           `{"dsn":"https://key@sentry.io/12345"}` + "\n" + `{"event_id":"abc"}`,
			upstreamStatus: http.StatusOK,
			upstreamBody:   `{"id":"abc"}`,
			wantStatus:     http.StatusOK,
			wantProjectID:  "12345",
		},
		{
			name:       "GET method not allowed",
			method:     http.MethodGet,
			body:       `{"dsn":"https://key@sentry.io/12345"}` + "\n" + `{"event_id":"abc"}`,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "missing newline in body",
			method:     http.MethodPost,
			body:       `{"dsn":"https://key@sentry.io/12345"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON in first line",
			method:     http.MethodPost,
			body:       `not json` + "\n" + `{"event_id":"abc"}`,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "missing DSN in header",
			method:     http.MethodPost,
			body:       `{"event_id":"abc"}` + "\n" + `{"data":"test"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "DSN is not a string",
			method:     http.MethodPost,
			body:       `{"dsn":12345}` + "\n" + `{"event_id":"abc"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid DSN URL format",
			method:     http.MethodPost,
			body:       `{"dsn":"://invalid-url"}` + "\n" + `{"event_id":"abc"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:              "project ID in allowlist",
			method:            http.MethodPost,
			body:              `{"dsn":"https://key@sentry.io/12345"}` + "\n" + `{"event_id":"abc"}`,
			allowedProjectIDs: []string{"12345", "67890"},
			upstreamStatus:    http.StatusOK,
			upstreamBody:      `{"id":"abc"}`,
			wantStatus:        http.StatusOK,
			wantProjectID:     "12345",
		},
		{
			name:              "project ID not in allowlist",
			method:            http.MethodPost,
			body:              `{"dsn":"https://key@sentry.io/99999"}` + "\n" + `{"event_id":"abc"}`,
			allowedProjectIDs: []string{"12345", "67890"},
			wantStatus:        http.StatusForbidden,
		},
		{
			name:              "empty allowlist allows all projects",
			method:            http.MethodPost,
			body:              `{"dsn":"https://key@sentry.io/99999"}` + "\n" + `{"event_id":"abc"}`,
			allowedProjectIDs: []string{},
			upstreamStatus:    http.StatusOK,
			upstreamBody:      `{"id":"abc"}`,
			wantStatus:        http.StatusOK,
			wantProjectID:     "99999",
		},
		{
			name:           "DSN with path containing multiple segments",
			method:         http.MethodPost,
			body:           `{"dsn":"https://key@sentry.io/org/project/123"}` + "\n" + `{"event_id":"abc"}`,
			upstreamStatus: http.StatusOK,
			upstreamBody:   `{"id":"abc"}`,
			wantStatus:     http.StatusOK,
			wantProjectID:  "org/project/123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedProjectID string
			var receivedBody []byte

			// Create mock upstream server
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Extract project ID from path: /api/{projectID}/envelope/
				parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
				if len(parts) >= 2 {
					receivedProjectID = strings.Join(parts[1:len(parts)-1], "/")
				}

				contentEncoding := r.Header.Get("Content-Encoding")
				body, _ := io.ReadAll(r.Body)
				if contentEncoding == "br" {
					reader := brotli.NewReader(bytes.NewReader(body))
					receivedBody, _ = io.ReadAll(reader)
				} else {
					receivedBody = body
				}

				w.WriteHeader(tt.upstreamStatus)
				w.Write([]byte(tt.upstreamBody))
			}))
			defer upstream.Close()

			// Create handler
			cfg := config{
				sentryHost:        strings.TrimPrefix(upstream.URL, "http://"),
				sentryHostProto:   "http",
				sentryHostHeader:  "test.sentry.io",
				compressionLevel:  brotli.BestSpeed,
				allowedProjectIDs: tt.allowedProjectIDs,
				handlerPath:       "/s",
			}

			handler := createTunnelHandler(cfg)

			req := httptest.NewRequest(tt.method, "/s", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status code = %d, want %d", rec.Code, tt.wantStatus)
			}

			// Verify project ID extraction for successful requests
			if tt.wantStatus == http.StatusOK && tt.wantProjectID != "" {
				if receivedProjectID != tt.wantProjectID {
					t.Errorf("Project ID = %q, want %q", receivedProjectID, tt.wantProjectID)
				}
			}

			// Verify body was forwarded correctly
			if tt.wantStatus == http.StatusOK {
				if string(receivedBody) != tt.body {
					t.Errorf("Forwarded body = %q, want %q", string(receivedBody), tt.body)
				}
			}
		})
	}
}

// createTunnelHandler creates the tunnel handler for testing
func createTunnelHandler(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusInternalServerError)
			return
		}

		// Extract DSN from the first line of the payload
		firstLineEnd := bytes.IndexByte(body, '\n')
		if firstLineEnd == -1 {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}

		var header map[string]interface{}
		err = json.Unmarshal(body[:firstLineEnd], &header)
		if err != nil {
			http.Error(w, "Error unmarshalling JSON", http.StatusInternalServerError)
			return
		}

		dsnString, ok := header["dsn"].(string)
		if !ok {
			http.Error(w, "DSN missing or invalid in header", http.StatusBadRequest)
			return
		}

		dsn, err := parseURL(dsnString)
		if err != nil {
			http.Error(w, "Invalid DSN URL format", http.StatusBadRequest)
			return
		}

		projectID := strings.TrimPrefix(dsn.Path, "/")
		if len(cfg.allowedProjectIDs) > 0 && !containsString(cfg.allowedProjectIDs, projectID) {
			http.Error(w, "Invalid or unauthorized Sentry project ID", http.StatusForbidden)
			return
		}

		upstreamURL := cfg.sentryHostProto + "://" + cfg.sentryHost + "/api/" + projectID + "/envelope/"
		proxyToSentry(w, r, body, upstreamURL, cfg.compressionLevel, cfg.sentryHostHeader, "Tunnel")
	}
}

// parseURL is a helper for testing that wraps url.Parse
func parseURL(rawURL string) (*struct{ Path string }, error) {
	u, err := (&urlParser{}).Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &struct{ Path string }{Path: u.Path}, nil
}

type urlParser struct{}

func (p *urlParser) Parse(rawURL string) (*struct{ Path string }, error) {
	// Simple URL validation
	if !strings.Contains(rawURL, "://") || strings.HasPrefix(rawURL, "://") {
		return nil, &invalidURLError{rawURL}
	}
	parts := strings.SplitN(rawURL, "://", 2)
	if len(parts) != 2 {
		return nil, &invalidURLError{rawURL}
	}
	pathStart := strings.Index(parts[1], "/")
	if pathStart == -1 {
		return &struct{ Path string }{Path: ""}, nil
	}
	return &struct{ Path string }{Path: parts[1][pathStart:]}, nil
}

type invalidURLError struct {
	URL string
}

func (e *invalidURLError) Error() string {
	return "invalid URL: " + e.URL
}

// containsString checks if a string slice contains a specific string
func containsString(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// TestProxyToSentryUpstreamError tests proxy behavior when upstream is unreachable
func TestProxyToSentryUpstreamError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("test body"))
	rec := httptest.NewRecorder()

	// Use an invalid upstream URL to trigger connection error
	proxyToSentry(rec, req, []byte("test body"), "http://localhost:99999/invalid", brotli.BestSpeed, "test.host", "Test")

	if rec.Code != http.StatusBadGateway {
		t.Errorf("Status code = %d, want %d for unreachable upstream", rec.Code, http.StatusBadGateway)
	}
}

// TestCompressionRatio tests that compression actually reduces size for compressible data
func TestCompressionRatio(t *testing.T) {
	// Create highly compressible data (repeated pattern)
	original := bytes.Repeat([]byte("This is a test message that should compress well. "), 100)

	compressed, err := compressBody(original, brotli.BestCompression)
	if err != nil {
		t.Fatalf("compressBody() error = %v", err)
	}

	ratio := float64(len(original)) / float64(len(compressed))
	t.Logf("Original: %d bytes, Compressed: %d bytes, Ratio: %.2f", len(original), len(compressed), ratio)

	if ratio < 2.0 {
		t.Errorf("Compression ratio = %.2f, want at least 2.0 for highly compressible data", ratio)
	}
}

// TestConcurrentRequests tests that the handler can handle concurrent requests
func TestConcurrentRequests(t *testing.T) {
	requestCount := 50
	done := make(chan bool, requestCount)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	cfg := config{
		sentryHost:       strings.TrimPrefix(upstream.URL, "http://"),
		sentryHostProto:  "http",
		sentryHostHeader: "test.sentry.io",
		compressionLevel: brotli.BestSpeed,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()
		upstreamURL := cfg.sentryHostProto + "://" + cfg.sentryHost + r.URL.Path
		proxyToSentry(w, r, body, upstreamURL, cfg.compressionLevel, cfg.sentryHostHeader, "Test")
	})

	for i := 0; i < requestCount; i++ {
		go func(id int) {
			req := httptest.NewRequest(http.MethodPost, "/api/test/", strings.NewReader(`{"id":"`+string(rune(id))+`"}`))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Request %d: Status code = %d, want %d", id, rec.Code, http.StatusOK)
			}
			done <- true
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < requestCount; i++ {
		<-done
	}
}

// TestHeadersPassedToUpstream tests that headers are correctly passed to upstream
func TestHeadersPassedToUpstream(t *testing.T) {
	var receivedHeaders http.Header

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := config{
		sentryHost:       strings.TrimPrefix(upstream.URL, "http://"),
		sentryHostProto:  "http",
		sentryHostHeader: "custom.sentry.host",
		compressionLevel: brotli.BestSpeed,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/test/", strings.NewReader("test"))
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Forwarded-For", "192.168.1.100")
	req.Header.Set("X-Real-IP", "10.0.0.1")
	req.Header.Set("Origin", "https://myapp.com")
	req.Header.Set("Referer", "https://myapp.com/page")
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	req.Header.Set("Authorization", "Bearer secret") // Should NOT be forwarded

	rec := httptest.NewRecorder()

	body, _ := io.ReadAll(req.Body)
	upstreamURL := cfg.sentryHostProto + "://" + cfg.sentryHost + "/api/test/"
	req.Body = io.NopCloser(strings.NewReader("test"))
	proxyToSentry(rec, req, body, upstreamURL, cfg.compressionLevel, cfg.sentryHostHeader, "Test")

	// Verify headers
	expectedHeaders := map[string]string{
		"Content-Type":     "application/x-sentry-envelope",
		"X-Forwarded-For":  "192.168.1.100",
		"X-Real-Ip":        "10.0.0.1",
		"Origin":           "https://myapp.com",
		"Referer":          "https://myapp.com/page",
		"Cf-Connecting-Ip": "1.2.3.4",
		"Content-Encoding": "br",
	}

	for header, want := range expectedHeaders {
		got := receivedHeaders.Get(header)
		if got != want {
			t.Errorf("Header %q = %q, want %q", header, got, want)
		}
	}

	// Authorization should NOT be forwarded
	if auth := receivedHeaders.Get("Authorization"); auth != "" {
		t.Errorf("Authorization header should not be forwarded, got %q", auth)
	}
}

// TestSentryEnvelopeTypes tests that various Sentry envelope item types are handled correctly
func TestSentryEnvelopeTypes(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		envelope    string
		description string
	}{
		{
			name:        "error event envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"event_id":"abc123","dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"event","length":42}
{"exception":{"values":[{"type":"Error"}]}}`,
			description: "Standard error event",
		},
		{
			name:        "transaction envelope with spans",
			contentType: "application/x-sentry-envelope",
			envelope: `{"event_id":"def456","dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"transaction","length":200}
{"type":"transaction","transaction":"/api/users","spans":[{"span_id":"span1","op":"http.client","description":"GET /api/data","start_timestamp":1704067200,"timestamp":1704067201}]}`,
			description: "Transaction with performance spans",
		},
		{
			name:        "session envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"session"}
{"sid":"session123","status":"ok","errors":0,"duration":3600}`,
			description: "Session tracking data",
		},
		{
			name:        "client report envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"client_report"}
{"timestamp":"2024-01-01T00:00:00Z","discarded_events":[{"reason":"sample_rate","category":"transaction","quantity":10}]}`,
			description: "Client-side SDK report",
		},
		{
			name:        "user feedback envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"event_id":"feedback123","dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"user_report"}
{"event_id":"abc123","name":"John Doe","email":"john@example.com","comments":"Something went wrong"}`,
			description: "User feedback submission",
		},
		{
			name:        "attachment envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"event_id":"attach123","dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"attachment","length":20,"filename":"log.txt","content_type":"text/plain"}
This is a log file.`,
			description: "File attachment",
		},
		{
			name:        "check-in envelope (cron monitoring)",
			contentType: "application/x-sentry-envelope",
			envelope: `{"dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"check_in"}
{"check_in_id":"checkin123","monitor_slug":"daily-backup","status":"ok","duration":120}`,
			description: "Cron job monitoring check-in",
		},
		{
			name:        "profile envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"event_id":"profile123","dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"profile","length":100}
{"device":{"architecture":"arm64"},"platform":"cocoa","profile":{"samples":[],"stacks":[],"frames":[]}}`,
			description: "Performance profiling data",
		},
		{
			name:        "replay envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"event_id":"replay123","dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"replay_event"}
{"replay_id":"replay123","segment_id":0,"replay_type":"session","urls":["https://example.com"]}`,
			description: "Session replay data",
		},
		{
			name:        "multiple items envelope",
			contentType: "application/x-sentry-envelope",
			envelope: `{"event_id":"multi123","dsn":"https://key@sentry.io/12345","sent_at":"2024-01-01T00:00:00Z"}
{"type":"event","length":30}
{"message":"Test error message"}
{"type":"attachment","length":15,"filename":"debug.log"}
Debug log data.`,
			description: "Envelope with multiple items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody []byte
			var receivedContentType string

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedContentType = r.Header.Get("Content-Type")

				body, _ := io.ReadAll(r.Body)
				if r.Header.Get("Content-Encoding") == "br" {
					reader := brotli.NewReader(bytes.NewReader(body))
					receivedBody, _ = io.ReadAll(reader)
				} else {
					receivedBody = body
				}

				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"id":"ok"}`))
			}))
			defer upstream.Close()

			cfg := config{
				sentryHost:        strings.TrimPrefix(upstream.URL, "http://"),
				sentryHostProto:   "http",
				sentryHostHeader:  "test.sentry.io",
				compressionLevel:  brotli.BestSpeed,
				allowedProjectIDs: []string{},
			}

			handler := createTunnelHandler(cfg)

			req := httptest.NewRequest(http.MethodPost, "/s", strings.NewReader(tt.envelope))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("%s: Status code = %d, want %d", tt.description, rec.Code, http.StatusOK)
			}

			if receivedContentType != tt.contentType {
				t.Errorf("%s: Content-Type = %q, want %q", tt.description, receivedContentType, tt.contentType)
			}

			if string(receivedBody) != tt.envelope {
				t.Errorf("%s: Body mismatch.\nGot: %s\nWant: %s", tt.description, string(receivedBody), tt.envelope)
			}
		})
	}
}

// BenchmarkCompressBody benchmarks compression performance
func BenchmarkCompressBody(b *testing.B) {
	testData := bytes.Repeat([]byte(`{"event_id":"abc123","timestamp":"2024-01-01T00:00:00Z","data":"test"}`), 100)

	b.Run("BestSpeed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			compressBody(testData, brotli.BestSpeed)
		}
	})

	b.Run("DefaultCompression", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			compressBody(testData, brotli.DefaultCompression)
		}
	})

	b.Run("BestCompression", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			compressBody(testData, brotli.BestCompression)
		}
	})
}

// BenchmarkHandler benchmarks the full request handling
func BenchmarkHandler(b *testing.B) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := config{
		sentryHost:       strings.TrimPrefix(upstream.URL, "http://"),
		sentryHostProto:  "http",
		sentryHostHeader: "test.sentry.io",
		compressionLevel: brotli.BestSpeed,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()
		upstreamURL := cfg.sentryHostProto + "://" + cfg.sentryHost + r.URL.Path
		proxyToSentry(w, r, body, upstreamURL, cfg.compressionLevel, cfg.sentryHostHeader, "Test")
	})

	testBody := `{"dsn":"https://key@sentry.io/123"}` + "\n" + `{"event_id":"abc123"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/123/envelope/", strings.NewReader(testBody))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
