package config

import (
	"fmt"
	"testing"
)

// TestParseDefaultsListenerPort verifies omitted schema v1 ports resolve to HTTP port 80.
func TestParseDefaultsListenerPort(t *testing.T) {
	c, err := Parse([]byte("version: 1\nupstreams:\n  app:\n    address: localhost:8080\nservers:\n  - name: app.test\n    routes:\n      - path: /\n        upstream: app\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Servers[0].Listen != 80 {
		t.Fatalf("default listen = %d; want 80", c.Servers[0].Listen)
	}
}

// TestParseUsesConfiguredDefaultPort verifies the global default is applied only to omitted listeners.
func TestParseUsesConfiguredDefaultPort(t *testing.T) {
	c, err := Parse([]byte("version: 1\nglobal:\n  default_port: 8088\nupstreams:\n  app:\n    address: localhost:8080\nservers:\n  - name: default.test\n    routes:\n      - path: /\n        upstream: app\n  - name: explicit.test\n    listen: 9090\n    routes:\n      - path: /\n        upstream: app\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Servers[0].Listen; got != 8088 {
		t.Fatalf("default listener = %d; want 8088", got)
	}
	if got := c.Servers[1].Listen; got != 9090 {
		t.Fatalf("explicit listener = %d; want 9090", got)
	}
}

// TestParseRejectsInvalidDefaultPort verifies the configured default is a TCP port.
func TestParseRejectsInvalidDefaultPort(t *testing.T) {
	for _, port := range []int{-1, 65536} {
		input := fmt.Sprintf("version: 1\nglobal:\n  default_port: %d\nupstreams:\n  app:\n    address: localhost:8080\nservers:\n  - name: app.test\n    routes:\n      - path: /\n        upstream: app\n", port)
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("Parse accepted default port %d", port)
		}
	}
}

// TestParseLegacyConfig supports the original scalar-upstream and prefix-boolean layout.
func TestParseLegacyConfig(t *testing.T) {
	input := []byte(`upstreams:
  frontend: host.docker.internal:3000
  ticketing: host.docker.internal:3001
hot_reload:
  enabled: true
  debounce: 2s
  poll_interval: 1s
servers:
  - name: app.test
    routes:
      - {path: /ticketing, upstream: ticketing, prefix: true, redirect_slash: true, websocket: true}
      - {path: /, upstream: frontend}
`)
	c, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 1 {
		t.Fatalf("converted schema version = %d; want 1", c.Version)
	}
	if c.Servers[0].Listen != 80 {
		t.Fatalf("legacy default listen = %d; want 80", c.Servers[0].Listen)
	}
	if c.Servers[0].Routes[0].Match != "segment_prefix" {
		t.Fatalf("legacy prefix match = %q; want segment_prefix", c.Servers[0].Routes[0].Match)
	}
	if c.Upstreams["ticketing"].Address != "host.docker.internal:3001" {
		t.Fatalf("legacy upstream was not converted: %#v", c.Upstreams["ticketing"])
	}
}
