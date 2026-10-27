// Package native implements RouteForge's standalone Go reverse proxy.
package native

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/routeforge/routeforge/internal/config"
	"github.com/routeforge/routeforge/internal/engine"
	"github.com/routeforge/routeforge/internal/router"
)

var _ engine.Engine = (*Engine)(nil)

type hostRoutes struct {
	routes []router.CompiledRoute
	tls    *config.TLS
}
type snapshot struct {
	hosts        map[int]map[string]hostRoutes
	certificates map[string]*tls.Certificate
	upstreams    map[string]string
	proxy        config.ProxyOptions
	healthPath   string
}

// Engine serves HTTP and HTTPS using immutable atomic routing snapshots.
type Engine struct {
	logger            *slog.Logger
	active            atomic.Pointer[snapshot]
	mu                sync.Mutex
	servers           []*http.Server
	listeners         []net.Listener
	transports        []*http.Transport
	transportCache    map[string]*http.Transport
	transportOverride http.RoundTripper
}

// New constructs a native engine with a structured logger.
func New(logger *slog.Logger) *Engine {
	return &Engine{logger: logger, transportCache: map[string]*http.Transport{}}
}

// Apply compiles the full candidate before atomically switching active requests.
func (e *Engine) Apply(c *config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	n := &snapshot{hosts: map[int]map[string]hostRoutes{}, certificates: map[string]*tls.Certificate{}, upstreams: map[string]string{}, proxy: c.Global.Proxy, healthPath: "/healthz"}
	if c.Global.HealthPath != "" {
		n.healthPath = c.Global.HealthPath
	}
	for name, u := range c.Upstreams {
		n.upstreams[name] = u.Address
	}
	for _, s := range c.Servers {
		port := s.Listen
		if port == 0 {
			port = c.Global.DefaultPort
			if port == 0 {
				port = 80
			}
		}
		key := strings.ToLower(strings.TrimSuffix(s.Name, "."))
		if n.hosts[port] == nil {
			n.hosts[port] = map[string]hostRoutes{}
		}
		if _, exists := n.hosts[port][key]; exists {
			return fmt.Errorf("duplicate virtual host %q on port %d", s.Name, port)
		}
		routes, err := router.CompileRoutes(s.Routes)
		if err != nil {
			return err
		}
		n.hosts[port][key] = hostRoutes{routes: routes, tls: s.TLS}
		if s.TLS != nil {
			cert, err := tls.LoadX509KeyPair(s.TLS.Certificate, s.TLS.Key)
			if err != nil {
				return fmt.Errorf("load TLS certificate for %s: %w", s.Name, err)
			}
			n.certificates[key] = &cert
		}
	}
	e.active.Store(n)
	return nil
}

// Serve binds each configured port and serves its current routing snapshot.
func (e *Engine) Serve(ctx context.Context) error {
	s := e.active.Load()
	if s == nil {
		return fmt.Errorf("native engine has no configuration")
	}
	e.mu.Lock()
	for port := range s.hosts {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			e.mu.Unlock()
			return fmt.Errorf("listen on %d: %w", port, err)
		}
		srv := &http.Server{Handler: e, ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 1 << 20, IdleTimeout: 90 * time.Second}
		e.listeners = append(e.listeners, ln)
		e.servers = append(e.servers, srv)
		if e.portTLS(port) {
			tc := &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				name := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
				if current := e.active.Load(); current != nil {
					if c := current.certificates[name]; c != nil {
						return c, nil
					}
				}
				return nil, fmt.Errorf("no TLS certificate for SNI %q", hello.ServerName)
			}}
			ln = tls.NewListener(ln, tc)
		}
		go func(server *http.Server, l net.Listener) {
			if err := server.Serve(l); err != nil && err != http.ErrServerClosed && e.logger != nil {
				e.logger.Error("native listener stopped", "error", err)
			}
		}(srv, ln)
	}
	e.mu.Unlock()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return e.Shutdown(shutdownCtx)
}
func (e *Engine) portTLS(port int) bool {
	s := e.active.Load()
	for _, h := range s.hosts[port] {
		if h.tls != nil {
			return true
		}
	}
	return false
}

// Shutdown gracefully stops all listeners and closes idle upstream connections.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var first error
	for _, s := range e.servers {
		if err := s.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	for _, t := range e.transports {
		t.CloseIdleConnections()
	}
	return first
}

// ServeHTTP selects a configured virtual host and proxies the matched route.
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s := e.active.Load()
	if s == nil {
		http.Error(w, "proxy not ready", http.StatusServiceUnavailable)
		return
	}
	host := strings.ToLower(strings.TrimSuffix(r.Host, "."))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	port := listenerPort(r)
	vh, ok := s.hosts[port][host]
	if !ok {
		vh, ok = s.hosts[port]["_"]
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == s.healthPath {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
		return
	}
	matched, ok := router.Match(vh.routes, r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	route := matched.Config
	if route.RedirectSlash && route.Path != "/" && !strings.HasSuffix(route.Path, "/") && r.URL.Path == route.Path {
		http.Redirect(w, r, route.Path+"/", http.StatusMovedPermanently)
		return
	}
	proxy, e2 := e.proxyFor(route, matched, s)
	if e2 != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	start := time.Now()
	rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	proxy.ServeHTTP(rec, r)
	if e.logger != nil {
		e.logger.Info("proxied request", "method", r.Method, "host", r.Host, "path", r.URL.Path, "route", route.Path, "upstream", route.Upstream, "status", rec.status, "duration", time.Since(start))
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int)        { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return h.Hijack()
}
func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}
func listenerPort(r *http.Request) int {
	if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if t, ok := a.(*net.TCPAddr); ok {
			return t.Port
		}
	}
	if r.TLS != nil {
		return 443
	}
	return 80
}

func (e *Engine) proxyFor(route config.Route, match router.CompiledRoute, snap *snapshot) (*httputil.ReverseProxy, error) {
	address := snap.upstreams[route.Upstream]
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	target, err := url.Parse(address)
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("invalid upstream %q", route.Upstream)
	}
	connect := duration(route.ConnectTimeout, snap.proxy.ConnectTimeout, 10*time.Second)
	response := duration(route.ReadTimeout, snap.proxy.ReadTimeout, 0)
	cacheKey := address + "|" + connect.String() + "|" + response.String()
	e.mu.Lock()
	transport := e.transportCache[cacheKey]
	if transport == nil {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: connect, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: false, MaxIdleConns: 256, MaxIdleConnsPerHost: 32, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: response}
		e.transportCache[cacheKey] = transport
		e.transports = append(e.transports, transport)
	}
	e.mu.Unlock()
	roundTripper := http.RoundTripper(transport)
	if e.transportOverride != nil {
		roundTripper = e.transportOverride
	}
	return &httputil.ReverseProxy{Transport: roundTripper, FlushInterval: -1, ErrorLog: nil, Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		pr.Out.Host = pr.In.Host
		pr.Out.Header.Del("Forwarded")
		pr.Out.Header.Del("X-Forwarded-For")
		pr.Out.Header.Del("X-Forwarded-Host")
		pr.Out.Header.Del("X-Forwarded-Proto")
		pr.Out.Header.Del("X-Real-IP")
		pr.SetXForwarded()
		rewritePath(pr.Out.URL, pr.In.URL.Path, route, match.Config.Path)
		for k, v := range route.ProxyHeaders {
			pr.Out.Header.Set(k, v)
		}
	}, ModifyResponse: func(res *http.Response) error {
		for k, v := range route.ResponseHeaders {
			res.Header.Set(k, v)
		}
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		if e.logger != nil {
			e.logger.Error("upstream request failed", "route", route.Path, "upstream", route.Upstream, "error", err)
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}}, nil
}
func duration(route, global string, fallback time.Duration) time.Duration {
	v := route
	if v == "" {
		v = global
	}
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
func rewritePath(u *url.URL, original string, r config.Route, path string) {
	if r.Rewrite != "" {
		prefix := path
		if r.Match == "segment_prefix" {
			prefix = strings.TrimSuffix(path, "/")
		}
		suffix := strings.TrimPrefix(original, prefix)
		base := strings.TrimSuffix(r.Rewrite, "/")
		if base == "" {
			base = "/"
		}
		if base == "/" {
			u.Path = "/" + strings.TrimLeft(suffix, "/")
		} else {
			u.Path = base + suffix
		}
		if u.Path == "" {
			u.Path = "/"
		}
	} else if r.PreservePath != nil && !*r.PreservePath {
		u.Path = "/"
	}
}
