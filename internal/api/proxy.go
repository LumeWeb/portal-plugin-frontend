package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
)

func newGatewayProxy(gatewayURL string, logger *zap.Logger) (http.Handler, error) {
	target, err := url.Parse(gatewayURL)
	if err != nil {
		return nil, fmt.Errorf("invalid gateway URL: %w", err)
	}

	resolvedHost := target.Hostname()
	addrs, err := net.DefaultResolver.LookupHost(context.Background(), resolvedHost)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve gateway host %q: %w", resolvedHost, err)
	}
	gatewayIP := addrs[0]

	port := target.Port()
	if port == "" {
		port = "443"
		if target.Scheme == "http" {
			port = "80"
		}
	}
	dialAddr := net.JoinHostPort(gatewayIP, port)

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, dialAddr)
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
			req.URL.RawPath = singleJoiningSlash(target.RawPath, req.URL.RawPath)

			if req.URL.Host == "" {
				req.URL.Host = req.Host
			}

			req.Header.Del("X-Forwarded-Host")
			req.Header.Set("X-Forwarded-Host", req.Host)

			clientIP, _, err := net.SplitHostPort(req.RemoteAddr)
			if err != nil {
				clientIP = req.RemoteAddr
			}

			req.Header.Del("X-Forwarded-For")
			req.Header.Set("X-Forwarded-For", clientIP)
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			logger.Error("gateway proxy error", zap.Error(err))

			if os.IsTimeout(err) {
				http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
				return
			}

			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
		Transport: transport,
	}

	return proxy, nil
}

func (a *API) createGatewayProxy(gatewayURL string) (http.Handler, error) {
	return newGatewayProxy(gatewayURL, a.Logger().Logger)
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
