// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRejectsAndRecordsConnectWithoutForwarding(t *testing.T) {
	var log bytes.Buffer
	request := httptest.NewRequest(http.MethodConnect, "https://github.com:443", nil)
	request.Host = "github.com:443"
	response := httptest.NewRecorder()
	rejectProxy(&log).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want503", response.Code)
	}
	if log.String() != "CONNECT github.com:443\n" {
		t.Fatalf("unexpected proxy request log: %q", log.String())
	}
}

func TestHTTPClientCannotReachGitHubThroughFixture(t *testing.T) {
	var log bytes.Buffer
	fixture := httptest.NewServer(rejectProxy(&log))
	defer fixture.Close()
	proxyURL, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	response, err := client.Get("https://github.com/grzegorzrzeznikiewicz/orchestrator-board/releases/latest/download/orchestrator-board-linux-x64.AppImage")
	if response != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err == nil || !strings.Contains(err.Error(), "Service Unavailable") {
		t.Fatalf("expected controlled CONNECT failure, got %v", err)
	}
	if log.String() != "CONNECT github.com:443\n" {
		t.Fatalf("unexpected target: %q", log.String())
	}
}
