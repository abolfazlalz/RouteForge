// Package router compiles RouteForge routing rules into immutable match tables.
package router

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/routeforge/routeforge/internal/config"
)

// CompiledRoute is a validated route with its compiled regular expression.
type CompiledRoute struct {
	Config config.Route
	Regexp *regexp.Regexp
}

// CompileRoutes applies the precedence used by the Nginx generator: exact,
// regex, longest segment-prefix, then longest prefix, preserving declaration order on ties.
func CompileRoutes(routes []config.Route) ([]CompiledRoute, error) {
	out := make([]CompiledRoute, 0, len(routes))
	for _, r := range routes {
		m := r.Match
		if m == "" {
			m = "prefix"
		}
		cr := CompiledRoute{Config: r}
		if m == "regex" {
			re, err := regexp.Compile("(?i)" + r.Path)
			if err != nil {
				return nil, fmt.Errorf("compile route regex %q: %w", r.Path, err)
			}
			cr.Regexp = re
		}
		out = append(out, cr)
	}
	rank := func(r CompiledRoute) int {
		switch r.Config.Match {
		case "exact":
			return 0
		case "regex":
			return 1
		case "segment_prefix":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return len(out[i].Config.Path) > len(out[j].Config.Path)
	})
	return out, nil
}

// Match returns the first route matching path under the compiled precedence.
func Match(routes []CompiledRoute, path string) (CompiledRoute, bool) {
	for _, r := range routes {
		p := r.Config.Path
		switch r.Config.Match {
		case "exact":
			if path == p {
				return r, true
			}
		case "regex":
			if r.Regexp != nil && r.Regexp.MatchString(path) {
				return r, true
			}
		case "segment_prefix":
			if path == p || strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
				return r, true
			}
		default:
			if strings.HasPrefix(path, p) {
				return r, true
			}
		}
	}
	return CompiledRoute{}, false
}
