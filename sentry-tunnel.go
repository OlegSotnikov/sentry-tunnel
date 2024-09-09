package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/DataDog/zstd"
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

	// Set the proxy mode, default is "bypass"
	proxyMode := os.Getenv("PROXY_MODE")
	if proxyMode == "" {
		proxyMode = "bypass"
	}

	http.HandleFunc(handlerPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body []byte
		var err error

		switch proxyMode {
		case "compress":
			body, err = compressRequestBody(r)
			if err != nil {
				log.Printf("Error compressing request body: %v", err)
				http.Error(w, "Error compressing request body", http.StatusInternalServerError)
				return
			}
			sendCompressed(w, r, body, sentryHost, handlerPath)

		case "decompress":
			body, err = decompressRequestBody(r)
			if err != nil {
				log.Printf("Error decompressing request body: %v", err)
				http.Error(w, "Error decompressing request body", http.StatusInternalServerError)
				return
			}
			parseAndForward(w, r, body, sentryHost, allowedProjectIDs)

		case "bypass":
			body, err = io.ReadAll(r.Body)
			if err != nil {
				log.Printf("Error reading request body: %v", err)
				http.Error(w, "Error reading request body", http.StatusInternalServerError)
				return
			}
			parseAndForward(w, r, body, sentryHost, allowedProjectIDs)

		default:
			http.Error(w, "Invalid proxy mode", http.StatusInternalServerError)
			return
		}

	})

	log.Printf("Server starting on port %s at path %s in %s mode\n", port, handlerPath, proxyMode)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func parseAndForward(w http.ResponseWriter, r *http.Request, body []byte, sentryHost string, allowedProjectIDs []string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	//body, err := ioutil.ReadAll(r.Body)
	//defer r.Body.Close()
	//if err != nil {
	//	log.Printf("Error reading request body: %v", err)
	//	http.Error(w, "Error reading request body", http.StatusInternalServerError)
	//	return
	//}

	// Extract DSN from the first line of the payload
	firstLineEnd := bytes.IndexByte(body, '\n')
	if firstLineEnd == -1 {
		log.Println("Received request body without a newline")
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	var header map[string]interface{}
	err := json.Unmarshal(body[:firstLineEnd], &header)
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
	proxyReq, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		return
	}
	copyHeaders(r.Header, proxyReq.Header)

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
}

func sendCompressed(w http.ResponseWriter, r *http.Request, body []byte, sentryHost string, handlerPath string) {
	upstreamURL := fmt.Sprintf("https://%s%s", sentryHost, handlerPath)

	// Create a new request with the compressed body
	proxyReq, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		return
	}

	copyHeaders(r.Header, proxyReq.Header)
	proxyReq.Header.Set("Content-Encoding", "zstd")
	proxyReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))

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

// Decompress the request body using DataDog Zstandard with timing
func decompressRequestBody(r *http.Request) ([]byte, error) {
	start := time.Now() // Start timer
	contentEncoding := r.Header.Get("Content-Encoding")
	var body []byte
	var err error

	if contentEncoding == "zstd" {
		compressedBody, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}

		// Decompress the body
		body, err = zstd.Decompress(nil, compressedBody)
	} else {
		// If no compression, just read the body as is
		body, err = io.ReadAll(r.Body)
	}

	duration := time.Since(start) // Calculate time taken
	if err != nil {
		log.Printf("Error during decompression: %v, Time taken: %s", err, duration)
	} else {
		log.Printf("Decompressed body size: %d bytes, Time taken: %s", len(body), duration)
	}

	return body, err
}

// Compress the request body using DataDog Zstandard with timing
func compressRequestBody(r *http.Request) ([]byte, error) {
	start := time.Now() // Start timer
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	originalSize := len(body)

	// Get compression level from environment, default to 3 if not set
	compressionLevelStr := os.Getenv("COMPRESSION_LEVEL")
	compressionLevel := 3 // Default compression level
	if compressionLevelStr != "" {
		parsedLevel, err := strconv.Atoi(compressionLevelStr)
		if err != nil {
			log.Printf("Invalid compression level: %s, using default level: %d", compressionLevelStr, compressionLevel)
		} else {
			compressionLevel = parsedLevel
		}
	}

	// Compress using DataDog's Zstandard at the given compression level
	compressedBody, err := zstd.CompressLevel(nil, body, compressionLevel)
	duration := time.Since(start) // Calculate time taken
	if err != nil {
		log.Printf("Error during compression: %v, Time taken: %s", err, duration)
		return nil, err
	}

	compressedSize := len(compressedBody)
	log.Printf("Compressed body size: %d bytes, Original size: %d bytes, Compression ratio: %.2f, Time taken: %s",
		compressedSize, originalSize, float64(originalSize)/float64(compressedSize), duration)

	return compressedBody, nil
}
