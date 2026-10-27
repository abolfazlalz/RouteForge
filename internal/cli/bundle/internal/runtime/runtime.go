package runtime

import (
	"crypto/sha256"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/routeforge/routeforge/internal/config"
	"github.com/routeforge/routeforge/internal/generator"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Manager struct {
	ConfigPath, Active string
	Logger             *slog.Logger
	Reload             bool
	mu                 sync.Mutex
	last               [32]byte
}

func (m *Manager) Apply() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, e := config.Load(m.ConfigPath)
	if e != nil {
		return e
	}
	b, e := generator.Render(c)
	if e != nil {
		return e
	}
	root, e := os.MkdirTemp("", "rforge-candidate-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(root)
	// Nginx resolves its default logs and temporary paths relative to -p.
	// Create the prefix directories so `nginx -t` validates the candidate rather
	// than failing because its isolated staging tree is incomplete.
	for _, dir := range []string{"logs", "client_body_temp", "proxy_temp", "fastcgi_temp", "uwsgi_temp", "scgi_temp"} {
		if e = os.MkdirAll(filepath.Join(root, dir), 0755); e != nil {
			return fmt.Errorf("prepare nginx validation directory %s: %w", dir, e)
		}
	}
	cand := filepath.Join(root, "nginx.conf")
	if e = os.WriteFile(cand, b, 0600); e != nil {
		return e
	}
	if out, e := exec.Command("nginx", "-t", "-q", "-p", root, "-c", cand).CombinedOutput(); e != nil {
		return fmt.Errorf("nginx validation: %s: %w", out, e)
	}
	if e = os.MkdirAll(filepath.Dir(m.Active), 0755); e != nil {
		return e
	}
	old, _ := os.ReadFile(m.Active)
	tmp := m.Active + ".candidate"
	if e = os.WriteFile(tmp, b, 0644); e != nil {
		return e
	}
	if e = os.Rename(tmp, m.Active); e != nil {
		return e
	}
	if m.Reload {
		if out, x := exec.Command("nginx", "-s", "reload").CombinedOutput(); x != nil {
			rollback := c.HotReload.RollbackOnFailure == nil || *c.HotReload.RollbackOnFailure
			if rollback {
				_ = os.WriteFile(m.Active+".rollback", old, 0644)
				_ = os.Rename(m.Active+".rollback", m.Active)
			}
			return fmt.Errorf("nginx reload: %s: %w", out, x)
		}
	}
	src, _ := os.ReadFile(m.ConfigPath)
	m.last = sha256.Sum256(src)
	if m.Logger != nil {
		m.Logger.Info("validated configuration applied")
	}
	return nil
}
func (m *Manager) Watch(done <-chan struct{}) error {
	w, e := fsnotify.NewWatcher()
	if e != nil {
		return e
	}
	defer w.Close()
	if e = w.Add(filepath.Dir(m.ConfigPath)); e != nil {
		return e
	}
	initial, err := config.Load(m.ConfigPath)
	if err != nil {
		return err
	}
	poll := parseDuration(initial.HotReload.PollInterval, time.Second)
	debounce := parseDuration(initial.HotReload.Debounce, 2*time.Second)
	if poll <= 0 {
		poll = time.Second
	}
	if debounce < 0 {
		debounce = 0
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	pending := time.Time{}
	for {
		select {
		case <-done:
			return nil
		case ev := <-w.Events:
			if filepath.Base(ev.Name) == filepath.Base(m.ConfigPath) {
				pending = time.Now()
			}
		case err := <-w.Errors:
			if m.Logger != nil {
				m.Logger.Error("watcher error", "error", err)
			}
		case <-tick.C:
			b, err := os.ReadFile(m.ConfigPath)
			if err == nil {
				h := sha256.Sum256(b)
				if h != m.last && pending.IsZero() {
					pending = time.Now()
				}
			}
			if !pending.IsZero() && time.Since(pending) >= debounce {
				pending = time.Time{}
				if latest, readErr := os.ReadFile(m.ConfigPath); readErr == nil && sha256.Sum256(latest) == m.last {
					continue
				}
				if err = m.Apply(); err != nil && m.Logger != nil {
					m.Logger.Error("configuration rejected", "error", err)
				} else if err == nil {
					if c, loadErr := config.Load(m.ConfigPath); loadErr == nil {
						poll = parseDuration(c.HotReload.PollInterval, time.Second)
						debounce = parseDuration(c.HotReload.Debounce, 2*time.Second)
						if poll <= 0 {
							poll = time.Second
						}
						if debounce < 0 {
							debounce = 0
						}
						tick.Reset(poll)
					}
				}
			}
		}
	}
}
func parseDuration(value string, fallback time.Duration) time.Duration {
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}
func Run() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := &Manager{ConfigPath: getenv("ROUTEFORGE_CONFIG", "/config/routeforge.yaml"), Active: "/etc/nginx/nginx.conf", Logger: logger}
	if e := m.Apply(); e != nil {
		logger.Error("initial config invalid", "error", e)
		os.Exit(1)
	}
	cmd := exec.Command("nginx", "-g", "daemon off;")
	if e := cmd.Start(); e != nil {
		logger.Error("nginx start failed", "error", e)
		os.Exit(1)
	}
	// The initial candidate was installed before Nginx started. Subsequent
	// watcher applies can now signal the live master process.
	m.Reload = true
	done := make(chan struct{})
	if c, loadErr := config.Load(m.ConfigPath); loadErr == nil && (c.HotReload.Enabled == nil || *c.HotReload.Enabled) {
		go m.Watch(done)
	}
	e := cmd.Wait()
	close(done)
	if e != nil {
		logger.Error("nginx exited", "error", e)
		os.Exit(1)
	}
}
func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

var _ = config.Config{}
