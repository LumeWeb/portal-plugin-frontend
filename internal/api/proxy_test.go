package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestNewGatewayProxy_InvalidURL(t *testing.T) {
	_, err := newGatewayProxy("://bad", zap.NewNop())
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestNewGatewayProxy_ProxiesRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Got-Host", r.Host)
		w.Header().Set("X-Got-X-Forwarded-Host", r.Header.Get("X-Forwarded-Host"))
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newGatewayProxy(backend.URL, zap.NewNop())
	if err != nil {
		t.Fatalf("newGatewayProxy: %v", err)
	}

	frontend := httptest.NewServer(proxy)
	defer frontend.Close()

	req := httptest.NewRequest(http.MethodGet, "/test-path", nil)
	req.Host = "example.com"
	req.RemoteAddr = "1.2.3.4:5678"

	rec := httptest.NewRecorder()
	frontend.Config.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if got := rec.Header().Get("X-Got-X-Forwarded-Host"); got != "example.com" {
		t.Errorf("X-Forwarded-Host = %q, want %q", got, "example.com")
	}

	if got := rec.Header().Get("X-Got-Host"); got != "example.com" {
		t.Errorf("Host = %q, want %q (original host preserved for vhost/SNI)", got, "example.com")
	}
}

func TestNewGatewayProxy_BadGateway(t *testing.T) {
	proxy, err := newGatewayProxy("http://127.0.0.1:1", zap.NewNop())
	if err != nil {
		t.Fatalf("newGatewayProxy: %v", err)
	}

	frontend := httptest.NewServer(proxy)
	defer frontend.Close()

	resp, err := http.Get(frontend.URL + "/test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestNewGatewayProxy_StripSpoofedForwardedHost(t *testing.T) {
	var receivedForwardedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedForwardedHost = r.Header.Get("X-Forwarded-Host")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newGatewayProxy(backend.URL, zap.NewNop())
	if err != nil {
		t.Fatalf("newGatewayProxy: %v", err)
	}

	frontend := httptest.NewServer(proxy)
	defer frontend.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "real.example.com"
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-Host", "spoofed.example.com")

	rec := httptest.NewRecorder()
	frontend.Config.Handler.ServeHTTP(rec, req)

	if receivedForwardedHost != "real.example.com" {
		t.Errorf("X-Forwarded-Host = %q, want %q (spoofed value should be stripped)", receivedForwardedHost, "real.example.com")
	}
}

func TestNewGatewayProxy_StripSpoofedForwardedFor(t *testing.T) {
	var receivedForwardedFor string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedForwardedFor = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := newGatewayProxy(backend.URL, zap.NewNop())
	if err != nil {
		t.Fatalf("newGatewayProxy: %v", err)
	}

	frontend := httptest.NewServer(proxy)
	defer frontend.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.com"
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")

	rec := httptest.NewRecorder()
	frontend.Config.Handler.ServeHTTP(rec, req)

	if strings.Contains(receivedForwardedFor, "9.9.9.9") {
		t.Errorf("X-Forwarded-For = %q, should not contain spoofed %q", receivedForwardedFor, "9.9.9.9")
	}
	if !strings.Contains(receivedForwardedFor, "10.0.0.1") {
		t.Errorf("X-Forwarded-For = %q, want it to contain %q", receivedForwardedFor, "10.0.0.1")
	}
}

func TestNewGatewayProxy_PathPrefixJoining(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)
	gatewayURL := backendURL.Scheme + "://" + backendURL.Host + "/ipfs/QmHash"

	proxy, err := newGatewayProxy(gatewayURL, zap.NewNop())
	if err != nil {
		t.Fatalf("newGatewayProxy: %v", err)
	}

	frontend := httptest.NewServer(proxy)
	defer frontend.Close()

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	req.Host = "example.com"
	req.RemoteAddr = "10.0.0.1:1234"

	rec := httptest.NewRecorder()
	frontend.Config.Handler.ServeHTTP(rec, req)

	want := "/ipfs/QmHash/index.html"
	if receivedPath != want {
		t.Errorf("path = %q, want %q", receivedPath, want)
	}
}
