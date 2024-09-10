package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/andybalholm/brotli"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	sentryHost := os.Getenv("SENTRY_HOST")
	if sentryHost == "" {
		log.Fatal("SENTRY_HOST environment variable is not set")
	}

	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "7878" // Default port if not specified
	}

	// Read PROJECT_IDS from environment, split into a slice, and handle empty case
	allowedProjectIDs := strings.Split(os.Getenv("PROJECT_IDS"), ",")
	if len(allowedProjectIDs) == 1 && allowedProjectIDs[0] == "" {
		allowedProjectIDs = []string{} // Handle case where PROJECT_IDS is not set or empty
	}

	handlerPath := os.Getenv("PROXY_PATH")
	if handlerPath == "" {
		handlerPath = "/s" // Default path
	}

	compressionLevel := brotli.DefaultCompression
	if levelStr := os.Getenv("COMPRESSION_LEVEL"); levelStr != "" {
		if level, err := strconv.Atoi(levelStr); err == nil {
			compressionLevel = level
		} else {
			log.Printf("Invalid COMPRESSION_LEVEL, using default: %v", err)
		}
	}

	http.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
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

		upstreamURL := fmt.Sprintf("https://%s%s", sentryHost, r.URL.Path)
		proxyReq, err := http.NewRequest(http.MethodPost, upstreamURL, nil)
		if err != nil {
			log.Printf("Error creating proxy request: %v", err)
			http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
			return
		}
		copyHeaders(r.Header, proxyReq.Header)

		// Start compression and measure time
		startTime := time.Now()
		originalSize := len(body)

		// Compress the body using Brotli with the configured compression level
		var compressedBody bytes.Buffer
		brotliWriter := brotli.NewWriterLevel(&compressedBody, compressionLevel)

		if _, err := brotliWriter.Write(body); err != nil {
			log.Printf("Error writing to Brotli writer: %v", err)
			http.Error(w, "Error compressing request body", http.StatusInternalServerError)
			return
		}

		if err := brotliWriter.Close(); err != nil {
			log.Printf("Error closing Brotli writer: %v", err)
			http.Error(w, "Error compressing request body", http.StatusInternalServerError)
			return
		}

		// Compression stats
		compressedSize := compressedBody.Len()
		compressionTime := time.Since(startTime).Milliseconds()
		compressionRatio := float64(originalSize) / float64(compressedSize)

		log.Printf("Envelope: %d -> %d, %.2f, %d ms",
			originalSize, compressedSize, compressionRatio, compressionTime)

		// Set Content-Encoding header to Brotli
		proxyReq.Header.Set("Content-Encoding", "br")

		proxyReq.Body = io.NopCloser(bytes.NewReader(compressedBody.Bytes()))

		client := &http.Client{}
		resp, err := client.Do(proxyReq)
		if err != nil {
			log.Printf("Error tunneling to Sentry: %v", err)
			http.Error(w, "Error tunneling to Sentry", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		responseBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response from Sentry: %v", err)
			http.Error(w, "Failed to read response from Sentry", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(resp.StatusCode)
		io.Copy(w, io.NopCloser(bytes.NewReader(responseBytes)))
	})

	http.HandleFunc(handlerPath, func(w http.ResponseWriter, r *http.Request) {
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
		if err != nil || dsn.Hostname() != sentryHost {
			log.Printf("Invalid Sentry hostname: %s", dsn.Hostname())
			http.Error(w, "Invalid Sentry hostname", http.StatusForbidden)
			return
		}

		projectID := strings.TrimPrefix(dsn.Path, "/")
		if len(allowedProjectIDs) > 0 && !contains(allowedProjectIDs, projectID) {
			log.Printf("Invalid or unauthorized Sentry project ID: %s", projectID)
			http.Error(w, "Invalid or unauthorized Sentry project ID", http.StatusForbidden)
			return
		}

		upstreamURL := fmt.Sprintf("https://%s/api/%s/envelope/", sentryHost, projectID)
		proxyReq, err := http.NewRequest(http.MethodPost, upstreamURL, nil)
		if err != nil {
			log.Printf("Error creating proxy request: %v", err)
			http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
			return
		}
		copyHeaders(r.Header, proxyReq.Header)

		// Start compression and measure time
		startTime := time.Now()
		originalSize := len(body)

		// Compress the body using Brotli with the configured compression level
		var compressedBody bytes.Buffer
		brotliWriter := brotli.NewWriterLevel(&compressedBody, compressionLevel)

		if _, err := brotliWriter.Write(body); err != nil {
			log.Printf("Error writing to Brotli writer: %v", err)
			http.Error(w, "Error compressing request body", http.StatusInternalServerError)
			return
		}

		if err := brotliWriter.Close(); err != nil {
			log.Printf("Error closing Brotli writer: %v", err)
			http.Error(w, "Error compressing request body", http.StatusInternalServerError)
			return
		}

		// Compression stats
		compressedSize := compressedBody.Len()
		compressionTime := time.Since(startTime).Milliseconds()
		compressionRatio := float64(originalSize) / float64(compressedSize)

		log.Printf("Tunnel: %d -> %d, %.2f, %d ms",
			originalSize, compressedSize, compressionRatio, compressionTime)

		// Set Content-Encoding header to Brotli
		proxyReq.Header.Set("Content-Encoding", "br")

		proxyReq.Body = io.NopCloser(bytes.NewReader(compressedBody.Bytes()))

		client := &http.Client{}
		resp, err := client.Do(proxyReq)
		if err != nil {
			log.Printf("Error tunneling to Sentry: %v", err)
			http.Error(w, "Error tunneling to Sentry", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		responseBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response from Sentry: %v", err)
			http.Error(w, "Failed to read response from Sentry", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(resp.StatusCode)
		io.Copy(w, io.NopCloser(bytes.NewReader(responseBytes)))
	})

	log.Printf("Server starting on port %s at path %s\n", port, handlerPath)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func copyHeaders(src http.Header, dst http.Header) {
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
