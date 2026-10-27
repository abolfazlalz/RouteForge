package router

import (
	"testing"

	"github.com/routeforge/routeforge/internal/config"
)

// TestRoutePrecedence checks exact, case-insensitive regex, segment, then prefix routing.
func TestRoutePrecedence(t *testing.T) {
	routes, err := CompileRoutes([]config.Route{{Path: "/", Upstream: "root", Match: "prefix"}, {Path: "/api", Upstream: "prefix", Match: "prefix"}, {Path: "/api", Upstream: "segment", Match: "segment_prefix"}, {Path: "/api", Upstream: "exact", Match: "exact"}, {Path: "/API/[0-9]+", Upstream: "regex", Match: "regex"}})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"/api": "exact", "/api/x": "segment", "/API/42": "regex", "/apix": "prefix"}
	for path, want := range cases {
		got, ok := Match(routes, path)
		if !ok || got.Config.Upstream != want {
			t.Errorf("Match(%q) = %q, %v; want %q", path, got.Config.Upstream, ok, want)
		}
	}
}

// TestRegexCompilationRejectsInvalidPattern confirms invalid tables cannot activate.
func TestRegexCompilationRejectsInvalidPattern(t *testing.T) {
	if _, err := CompileRoutes([]config.Route{{Path: "[", Match: "regex"}}); err == nil {
		t.Fatal("expected invalid regex error")
	}
}
