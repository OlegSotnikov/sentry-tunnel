package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/andybalholm/brotli"
)

var (
	// You can tune these transport settings:
	defaultTransport = &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,

		// Timeouts to avoid hanging connections
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,

		// Keep these false if you want to allow keep-alives
		DisableKeepAlives:  false,
		DisableCompression: false, // only for response compression, not request
		IdleConnTimeout:    90 * time.Second,
	}

	// Reuse a single client
	httpClient = &http.Client{
		Transport: defaultTransport,
		Timeout:   10 * time.Second,
	}
)

// config holds all configuration values loaded from environment variables
type config struct {
	sentryHost       string
	sentryHostProto  string
	sentryHostHeader string
	port             string
	handlerPath      string
	compressionLevel int
	allowedProjectIDs []string
}

func loadConfig() config {
	cfg := config{}

	cfg.sentryHost = os.Getenv("SENTRY_HOST")
	if cfg.sentryHost == "" {
		log.Fatal("SENTRY_HOST environment variable is not set")
	}

	cfg.port = os.Getenv("PROXY_PORT")
	if cfg.port == "" {
		cfg.port = "7878" // Default port if not specified
	}

	// Read PROJECT_IDS from environment, split into a slice, and handle empty case
	cfg.allowedProjectIDs = strings.Split(os.Getenv("PROJECT_IDS"), ",")
	if len(cfg.allowedProjectIDs) == 1 && cfg.allowedProjectIDs[0] == "" {
		cfg.allowedProjectIDs = []string{} // Handle case where PROJECT_IDS is not set or empty
	}

	cfg.handlerPath = os.Getenv("PROXY_PATH")
	if cfg.handlerPath == "" {
		cfg.handlerPath = "/s" // Default path
	}

	cfg.sentryHostProto = os.Getenv("SENTRY_HOST_PROTO")
	if cfg.sentryHostProto == "" {
		cfg.sentryHostProto = "https" // Default
	}

	cfg.sentryHostHeader = os.Getenv("SENTRY_HOST_HEADER")
	if cfg.sentryHostHeader == "" {
		cfg.sentryHostHeader = cfg.sentryHost // Default to SENTRY_HOST
	}

	cfg.compressionLevel = brotli.DefaultCompression
	if levelStr := os.Getenv("COMPRESSION_LEVEL"); levelStr != "" {
		if level, err := strconv.Atoi(levelStr); err == nil {
			cfg.compressionLevel = level
		} else {
			log.Printf("Invalid COMPRESSION_LEVEL, using default: %v", err)
		}
	}

	return cfg
}

// compressBody compresses the given body using Brotli and returns the compressed bytes
func compressBody(body []byte, compressionLevel int) ([]byte, error) {
	var compressedBody bytes.Buffer
	brotliWriter := brotli.NewWriterLevel(&compressedBody, compressionLevel)

	if _, err := brotliWriter.Write(body); err != nil {
		return nil, fmt.Errorf("error writing to Brotli writer: %w", err)
	}

	if err := brotliWriter.Close(); err != nil {
		return nil, fmt.Errorf("error closing Brotli writer: %w", err)
	}

	return compressedBody.Bytes(), nil
}

// proxyToSentry compresses the body, sends the request to upstream, and writes the response
func proxyToSentry(w http.ResponseWriter, r *http.Request, body []byte, upstreamURL string, compressionLevel int, hostHeader string, logPrefix string) {
	proxyReq, err := http.NewRequest(http.MethodPost, upstreamURL, nil)
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		return
	}
	copyHeaders(r.Header, proxyReq.Header, hostHeader)

	// Start compression and measure time
	startTime := time.Now()
	originalSize := len(body)

	compressedBody, err := compressBody(body, compressionLevel)
	if err != nil {
		log.Printf("%v", err)
		http.Error(w, "Error compressing request body", http.StatusInternalServerError)
		return
	}

	// Compression stats
	compressedSize := len(compressedBody)
	compressionTime := time.Since(startTime).Milliseconds()
	var compressionRatio float64
	if compressedSize > 0 {
		compressionRatio = float64(originalSize) / float64(compressedSize)
	}

	log.Printf("%s: %d -> %d, %.2f, %d ms",
		logPrefix, originalSize, compressedSize, compressionRatio, compressionTime)

	// Set Content-Encoding header to Brotli
	proxyReq.Header.Set("Content-Encoding", "br")
	proxyReq.Body = io.NopCloser(bytes.NewReader(compressedBody))

	resp, err := httpClient.Do(proxyReq)
	if err != nil {
		log.Printf("Error tunneling to Sentry: %v", err)
		http.Error(w, "Error tunneling to Sentry", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func main() {
	cfg := loadConfig()

	mux := http.NewServeMux()

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Error reading request body", http.StatusInternalServerError)
			return
		}

		upstreamURL := fmt.Sprintf("%s://%s%s", cfg.sentryHostProto, cfg.sentryHost, r.URL.Path)
		proxyToSentry(w, r, body, upstreamURL, cfg.compressionLevel, cfg.sentryHostHeader, "Envelope")
	})

	mux.HandleFunc(cfg.handlerPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Error reading request body", http.StatusInternalServerError)
			return
		}

		// Extract DSN from the first line of the payload
		firstLineEnd := bytes.IndexByte(body, '\n')
		if firstLineEnd == -1 {
			log.Println("Received request body without a newline")
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}

		var header map[string]interface{}
		err = json.Unmarshal(body[:firstLineEnd], &header)
		if err != nil {
			log.Printf("Error unmarshalling JSON from first line: %v", err)
			http.Error(w, "Error unmarshalling JSON", http.StatusInternalServerError)
			return
		}

		dsnString, ok := header["dsn"].(string)
		if !ok {
			log.Println("DSN missing or invalid in header")
			http.Error(w, "DSN missing or invalid in header", http.StatusBadRequest)
			return
		}

		dsn, err := url.Parse(dsnString)
		if err != nil {
			log.Printf("Error parsing DSN URL: %v", err)
			http.Error(w, "Invalid DSN URL format", http.StatusBadRequest)
			return
		}

		projectID := strings.TrimPrefix(dsn.Path, "/")
		if len(cfg.allowedProjectIDs) > 0 && !slices.Contains(cfg.allowedProjectIDs, projectID) {
			log.Printf("Invalid or unauthorized Sentry project ID: %s", projectID)
			http.Error(w, "Invalid or unauthorized Sentry project ID", http.StatusForbidden)
			return
		}

		upstreamURL := fmt.Sprintf("%s://%s/api/%s/envelope/", cfg.sentryHostProto, cfg.sentryHost, projectID)
		proxyToSentry(w, r, body, upstreamURL, cfg.compressionLevel, cfg.sentryHostHeader, "Tunnel")
	})

	// Setup server for graceful shutdown
	srv := &http.Server{
		Addr:    ":" + cfg.port,
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server starting on port %s at path %s\n", cfg.port, cfg.handlerPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// Listen for interrupt (CTRL+C) or SIGTERM (docker stop)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Graceful shutdown with timeout
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}
	log.Println("Server stopped")
}

func copyHeaders(src http.Header, dst http.Header, hostHeader string) {
	dst.Set("Host", hostHeader)
	if ct := src.Get("Content-Type"); ct != "" {
		dst.Set("Content-Type", ct)
	}
	if xff := src.Get("X-Forwarded-For"); xff != "" {
		dst.Set("X-Forwarded-For", xff)
	}
	if cci := src.Get("CF-Connecting-IP"); cci != "" {
		dst.Set("CF-Connecting-IP", cci)
	}
	if xri := src.Get("X-Real-IP"); xri != "" {
		dst.Set("X-Real-IP", xri)
	}
	if orig := src.Get("Origin"); orig != "" {
		dst.Set("Origin", orig)
	}
	if rfr := src.Get("Referer"); rfr != "" {
		dst.Set("Referer", rfr)
	}
}
