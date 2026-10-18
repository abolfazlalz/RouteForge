package generator

import (
	"bytes"
	"github.com/routeforge/routeforge/internal/config"
	"sort"
	"strings"
	"text/template"
)

const source = `worker_processes auto;
events { worker_connections {{.Connections}}; }
http {
 map $http_upgrade $connection_upgrade { default upgrade; '' close; }
{{range .Servers}} server { listen {{.Listen}}{{if .TLS}} ssl{{end}}; server_name {{.Name}};
{{if .TLS}} ssl_certificate {{.TLS.Certificate}}; ssl_certificate_key {{.TLS.Key}};{{end}}
{{range .Routes}}{{if .Redirect}} location = {{.Path}} { return 301 {{.Path}}/; }{{end}}
 location {{.Modifier}}{{.Path}}{{.Suffix}} { proxy_pass http://{{.Address}}{{.PassURI}}; proxy_http_version 1.1;
 proxy_set_header Host $http_host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; proxy_set_header X-Forwarded-Proto $scheme;
{{if .Websocket}} proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection $connection_upgrade;{{end}}
{{range .Headers}} proxy_set_header {{.Name}} {{.Value}};{{end}}{{range .Response}} add_header {{.Name}} {{.Value}} always;{{end}}
{{if .Connect}} proxy_connect_timeout {{.Connect}};{{end}}{{if .Read}} proxy_read_timeout {{.Read}};{{end}}{{if .Send}} proxy_send_timeout {{.Send}};{{end}} }
{{end}} }
{{end}}}`

type pair struct{ Name, Value string }
type rt struct {
	Path, Modifier, Suffix, Address, PassURI string
	Redirect, Websocket                      bool
	Headers, Response                        []pair
	Connect, Read, Send                      string
}
type srv struct {
	Name   string
	Listen int
	TLS    *config.TLS
	Routes []rt
}
type model struct {
	Connections int
	Servers     []srv
}

func Render(c *config.Config) ([]byte, error) {
	m := model{Connections: c.Global.WorkerConnections}
	if m.Connections < 1 {
		m.Connections = 1024
	}
	for _, s := range c.Servers {
		v := srv{Name: s.Name, Listen: s.Listen, TLS: s.TLS}
		if v.Listen == 0 {
			v.Listen = 80
		}
		routes := append([]config.Route(nil), s.Routes...)
		rank := func(r config.Route) int {
			switch r.Match {
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
		sort.SliceStable(routes, func(i, j int) bool {
			return rank(routes[i]) < rank(routes[j]) || rank(routes[i]) == rank(routes[j]) && len(routes[i].Path) > len(routes[j].Path)
		})
		for _, r := range routes {
			q := rt{Path: r.Path, Address: c.Upstreams[r.Upstream].Address, Redirect: r.RedirectSlash, Websocket: r.Websocket, Connect: first(r.ConnectTimeout, c.Global.Proxy.ConnectTimeout), Read: first(r.ReadTimeout, c.Global.Proxy.ReadTimeout), Send: first(r.SendTimeout, c.Global.Proxy.SendTimeout)}
			switch r.Match {
			case "exact":
				q.Modifier = "= "
			case "regex":
				q.Modifier = "~* "
			case "segment_prefix":
				q.Modifier = "^~ "
				q.Suffix = "/"
			default:
				q.Modifier = "^~ "
			}
			preserve := r.PreservePath == nil || *r.PreservePath
			if r.Rewrite != "" {
				q.PassURI = r.Rewrite
			} else if !preserve {
				q.PassURI = "/"
			}
			q.Headers = headers(r.ProxyHeaders)
			q.Response = headers(r.ResponseHeaders)
			// A segment-prefix location ending in "/" does not match the base
			// path itself. Proxy the exact base path too unless it is explicitly
			// redirected; otherwise it falls through to a broader route.
			if r.Match == "segment_prefix" && !r.RedirectSlash {
				exact := q
				exact.Modifier = "= "
				exact.Suffix = ""
				if r.Rewrite != "" {
					// Nginx replaces the matched location prefix with the URI in
					// proxy_pass. Use a slash-terminated URI for descendants, but
					// keep the exact base-path mapping slash-free.
					target := strings.TrimSuffix(r.Rewrite, "/")
					if target == "" {
						target = "/"
					}
					exact.PassURI = target
					q.PassURI = strings.TrimSuffix(target, "/") + "/"
				}
				v.Routes = append(v.Routes, exact)
			}
			v.Routes = append(v.Routes, q)
		}
		m.Servers = append(m.Servers, v)
	}
	t, e := template.New("nginx").Parse(source)
	if e != nil {
		return nil, e
	}
	var b bytes.Buffer
	e = t.Execute(&b, m)
	return b.Bytes(), e
}
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func headers(m map[string]string) []pair {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	o := make([]pair, 0, len(ks))
	for _, k := range ks {
		v := m[k]
		if strings.Contains(v, "$") {
			v = "\"" + v + "\""
		}
		o = append(o, pair{k, v})
	}
	return o
}
