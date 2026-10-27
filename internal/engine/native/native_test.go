package native

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/routeforge/routeforge/internal/config"
)

// TestProxyRewriteHeadersAndQuery exercises the configured native forwarding path.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestProxyRewriteHeadersAndQuery exercises configured forwarding without binding a socket.
func TestProxyRewriteHeadersAndQuery(t *testing.T) {
	var got string
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.transportOverride = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		got = r.Method + " " + r.URL.RequestURI() + " " + string(body) + " " + r.Header.Get("X-Configured")
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Upstream": []string{"yes"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})
	c := &config.Config{Version: 1, Upstreams: map[string]config.Upstream{"api": {Address: "http://upstream.test"}}, Servers: []config.Server{{Name: "app.test", Listen: 80, Routes: []config.Route{{Path: "/ticketing/api", Match: "segment_prefix", Upstream: "api", Rewrite: "/api", ProxyHeaders: map[string]string{"X-Configured": "route"}, ResponseHeaders: map[string]string{"X-Response": "configured"}}}}}}
	if err := e.Apply(c); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://app.test/ticketing/api/users?q=1", strings.NewReader("payload"))
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{Port: 80}))
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if want := "POST /api/users?q=1 payload route"; got != want {
		t.Fatalf("forwarded request %q; want %q", got, want)
	}
	if w.Header().Get("X-Response") != "configured" {
		t.Fatal("response header was not applied")
	}
}

// TestInvalidApplyKeepsPreviousSnapshot ensures a failed candidate does not replace active routing.
func TestInvalidApplyKeepsPreviousSnapshot(t *testing.T) {
	e := New(nil)
	good := &config.Config{Version: 1, Upstreams: map[string]config.Upstream{"a": {Address: "127.0.0.1:1"}}, Servers: []config.Server{{Name: "app.test", Routes: []config.Route{{Path: "/", Upstream: "a"}}}}}
	if err := e.Apply(good); err != nil {
		t.Fatal(err)
	}
	bad := &config.Config{Version: 2}
	if err := e.Apply(bad); err == nil {
		t.Fatal("expected candidate validation error")
	}
	if e.active.Load() == nil || len(e.active.Load().hosts) == 0 {
		t.Fatal("active snapshot was lost")
	}
}
