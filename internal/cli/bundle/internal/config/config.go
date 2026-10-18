// Package config loads and validates RouteForge YAML configurations.
package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	Version   int                 `yaml:"version"`
	Global    Global              `yaml:"global"`
	HotReload HotReload           `yaml:"hot_reload"`
	Upstreams map[string]Upstream `yaml:"upstreams"`
	Servers   []Server            `yaml:"servers"`
}
type Global struct {
	WorkerConnections int          `yaml:"worker_connections"`
	Proxy             ProxyOptions `yaml:"proxy"`
}
type ProxyOptions struct {
	ConnectTimeout string `yaml:"connect_timeout"`
	ReadTimeout    string `yaml:"read_timeout"`
	SendTimeout    string `yaml:"send_timeout"`
}
type HotReload struct {
	Enabled           *bool  `yaml:"enabled"`
	Debounce          string `yaml:"debounce"`
	PollInterval      string `yaml:"poll_interval"`
	RollbackOnFailure *bool  `yaml:"rollback_on_failure"`
}
type Upstream struct {
	Address string `yaml:"address"`
}
type Server struct {
	Name   string  `yaml:"name"`
	Listen int     `yaml:"listen"`
	TLS    *TLS    `yaml:"tls"`
	Routes []Route `yaml:"routes"`
}
type TLS struct {
	Certificate string `yaml:"certificate"`
	Key         string `yaml:"key"`
}
type Route struct {
	Path            string            `yaml:"path"`
	Upstream        string            `yaml:"upstream"`
	Match           string            `yaml:"match"`
	Websocket       bool              `yaml:"websocket"`
	RedirectSlash   bool              `yaml:"redirect_slash"`
	PreservePath    *bool             `yaml:"preserve_path"`
	Rewrite         string            `yaml:"rewrite"`
	ProxyHeaders    map[string]string `yaml:"proxy_headers"`
	ResponseHeaders map[string]string `yaml:"response_headers"`
	ConnectTimeout  string            `yaml:"connect_timeout"`
	ReadTimeout     string            `yaml:"read_timeout"`
	SendTimeout     string            `yaml:"send_timeout"`
}

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
var safeHeaderName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// Load parses strict YAML, resolves environment variables, and validates the schema.
func Load(path string) (*Config, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	return Parse(b)
}

// Parse parses and validates YAML bytes.
func Parse(b []byte) (*Config, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(b, &node); err != nil {
		return nil, err
	}
	if err := duplicates(&node); err != nil {
		return nil, err
	}
	var resolveErr error
	b = envPattern.ReplaceAllFunc(b, func(m []byte) []byte {
		parts := envPattern.FindSubmatch(m)
		name := string(parts[1])
		if name == "" {
			name = string(parts[2])
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			resolveErr = fmt.Errorf("environment variable %q is not set", name)
			return m
		}
		return []byte(value)
	})
	if resolveErr != nil {
		return nil, resolveErr
	}
	var c Config
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}
func duplicates(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if seen[k] {
				return fmt.Errorf("duplicate YAML key %q", k)
			}
			seen[k] = true
			if err := duplicates(n.Content[i+1]); err != nil {
				return err
			}
		}
	}
	for _, ch := range n.Content {
		if err := duplicates(ch); err != nil {
			return err
		}
	}
	return nil
}

// Validate rejects unsupported or unsafe routing configuration.
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported configuration version %d", c.Version)
	}
	if len(c.Upstreams) == 0 || len(c.Servers) == 0 {
		return fmt.Errorf("upstreams and servers must not be empty")
	}
	for name, u := range c.Upstreams {
		if name == "" || u.Address == "" || strings.ContainsAny(u.Address, ";{} \t\n") {
			return fmt.Errorf("invalid upstream %q", name)
		}
	}
	for _, s := range c.Servers {
		if s.Name == "" {
			return fmt.Errorf("server name is required")
		}
		if s.Listen == 0 {
			s.Listen = 80
		}
		if s.Listen < 1 || s.Listen > 65535 {
			return fmt.Errorf("invalid listen port for %s", s.Name)
		}
		seen := map[string]bool{}
		for _, r := range s.Routes {
			if r.Path == "" || r.Path[0] != '/' || strings.ContainsAny(r.Path, "{};\n") {
				return fmt.Errorf("invalid route path %q", r.Path)
			}
			if _, ok := c.Upstreams[r.Upstream]; !ok {
				return fmt.Errorf("route %s references undefined upstream %q", r.Path, r.Upstream)
			}
			m := r.Match
			if m == "" {
				m = "prefix"
			}
			if m != "exact" && m != "prefix" && m != "segment_prefix" && m != "regex" {
				return fmt.Errorf("unsupported match %q", m)
			}
			key := m + ":" + r.Path
			if seen[key] {
				return fmt.Errorf("duplicate route %s", r.Path)
			}
			seen[key] = true
			for k, v := range r.ProxyHeaders {
				if !safeHeaderName.MatchString(k) || strings.ContainsAny(v, "\r\n;{}") {
					return fmt.Errorf("unsafe proxy header %q", k)
				}
			}
			for k, v := range r.ResponseHeaders {
				if !safeHeaderName.MatchString(k) || strings.ContainsAny(v, "\r\n;{}") {
					return fmt.Errorf("unsafe response header %q", k)
				}
			}
			for _, t := range []string{r.ConnectTimeout, r.ReadTimeout, r.SendTimeout} {
				if t != "" {
					if _, e := time.ParseDuration(t); e != nil {
						return fmt.Errorf("invalid timeout %q", t)
					}
				}
			}
		}
	}
	for _, t := range []string{c.Global.Proxy.ConnectTimeout, c.Global.Proxy.ReadTimeout, c.Global.Proxy.SendTimeout, c.HotReload.Debounce, c.HotReload.PollInterval} {
		if t != "" {
			if _, e := time.ParseDuration(t); e != nil {
				return fmt.Errorf("invalid timeout %q", t)
			}
		}
	}
	return nil
}
