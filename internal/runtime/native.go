package runtime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/routeforge/routeforge/internal/config"
	"github.com/routeforge/routeforge/internal/engine/native"
)

// ServeNative starts the standalone engine and watches its configuration file.
func ServeNative(ctx context.Context, path string, logger *slog.Logger) error {
	c, err := config.Load(path)
	if err != nil {
		return err
	}
	proxy := native.New(logger)
	if err = proxy.Apply(c); err != nil {
		return err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	active := c
	watchErr := make(chan error, 1)
	if active.HotReload.Enabled == nil || *active.HotReload.Enabled {
		go watchNative(serveCtx, path, proxy, active, logger, watchErr)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- proxy.Serve(serveCtx) }()
	select {
	case err = <-serveErr:
		return err
	case err = <-watchErr:
		if err != nil {
			return err
		}
		return nil
	}
}
func watchNative(ctx context.Context, path string, proxy *native.Engine, initial *config.Config, logger *slog.Logger, done chan<- error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		done <- err
		return
	}
	defer watcher.Close()
	if err = watcher.Add(filepath.Dir(path)); err != nil {
		done <- err
		return
	}
	active := initial
	content, _ := os.ReadFile(path)
	digest := sha256.Sum256(content)
	pending := time.Time{}
	poll := parseInterval(active.HotReload.PollInterval, time.Second)
	debounce := parseInterval(active.HotReload.Debounce, 2*time.Second)
	if poll <= 0 {
		poll = time.Second
	}
	if debounce < 0 {
		debounce = 0
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case ev := <-watcher.Events:
			if filepath.Clean(ev.Name) == filepath.Clean(path) {
				pending = time.Now()
			}
		case err := <-watcher.Errors:
			if logger != nil {
				logger.Error("configuration watcher error", "error", err)
			}
		case <-ticker.C:
			b, readErr := os.ReadFile(path)
			if readErr == nil {
				d := sha256.Sum256(b)
				if d != digest && pending.IsZero() {
					pending = time.Now()
				}
			}
			if pending.IsZero() || time.Since(pending) < debounce {
				continue
			}
			pending = time.Time{}
			candidate, loadErr := config.Load(path)
			if loadErr == nil && !sameListeners(active, candidate) {
				loadErr = fmt.Errorf("listener ports, virtual hosts, or TLS mode changed; restart native serve to apply")
			}
			if loadErr == nil {
				loadErr = proxy.Apply(candidate)
			}
			if loadErr != nil {
				if logger != nil {
					logger.Error("configuration reload rejected", "error", loadErr)
				}
				continue
			}
			active = candidate
			content, _ = os.ReadFile(path)
			digest = sha256.Sum256(content)
			if logger != nil {
				logger.Info("configuration reloaded")
			}
		}
	}
}
func parseInterval(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}
func sameListeners(a, b *config.Config) bool {
	type ident struct {
		Name string
		Port int
		TLS  bool
	}
	makeSet := func(c *config.Config) []ident {
		out := make([]ident, 0, len(c.Servers))
		for _, s := range c.Servers {
			port := s.Listen
			if port == 0 {
				port = c.Global.DefaultPort
				if port == 0 {
					port = 80
				}
			}
			out = append(out, ident{s.Name, port, s.TLS != nil})
		}
		return out
	}
	return reflect.DeepEqual(makeSet(a), makeSet(b))
}
