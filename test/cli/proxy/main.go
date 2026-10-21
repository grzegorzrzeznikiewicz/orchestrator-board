// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

// A test-only HTTPS proxy which records CONNECT and always rejects it. It never
// opens an outbound connection, so the fresh-install smoke cannot reach GitHub.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func rejectProxy(output io.Writer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(output, "%s %s\n", r.Method, r.Host)
		http.Error(w, "controlled fresh-install fixture: no external connection", http.StatusServiceUnavailable)
	})
}

func main() {
	addressFile := flag.String("address-file", "", "write loopback listening address here")
	requestLog := flag.String("request-log", "", "record requests here")
	flag.Parse()
	if *addressFile == "" || *requestLog == "" {
		log.Fatal("--address-file and --request-log are required")
	}
	output, err := os.OpenFile(*requestLog, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*addressFile, []byte(listener.Addr().String()), 0o600); err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: rejectProxy(output), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.Serve(listener))
}
