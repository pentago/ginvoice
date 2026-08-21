// Package main implements a tiny HTTP health probe for the ginvoice Docker image.
// It reads GINVOICE_ADDR (default ":8080"), extracts the port, and GETs
// http://localhost:<port>/healthz. Exit 0 on HTTP 200, exit 1 otherwise.
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	port := portFromAddr(os.Getenv("GINVOICE_ADDR"))

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%s/healthz", port))
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

// portFromAddr extracts the port from a host:port address like ":8080" or
// "0.0.0.0:8080". Falls back to "8080" when the port is empty or missing.
func portFromAddr(addr string) string {
	if addr == "" {
		return "8080"
	}
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		if p := addr[idx+1:]; p != "" {
			return p
		}
	}
	return "8080"
}
