# RouteForge Configuration Reference

This document describes the YAML file used by `rforge validate`, `rforge generate`, and `rforge serve`. The current versioned schema is version 1. For compatibility, RouteForge also accepts the original unversioned format; see [Legacy configuration compatibility](#legacy-configuration-compatibility).

## Complete example

```yaml
version: 1

global:
  # Used by servers without their own listen port. Defaults to 80.
  default_port: 8080

  # Used only when generating Nginx configuration. Native ignores this setting.
  worker_connections: 1024

  # Native health-check path. Defaults to /healthz.
  health_path: /healthz

  proxy:
    connect_timeout: 10s
    read_timeout: 60s
    send_timeout: 60s

hot_reload:
  enabled: true
  debounce: 500ms
  poll_interval: 1s
  rollback_on_failure: true

upstreams:
  web:
    address: 127.0.0.1:3000
  api:
    address: 127.0.0.1:8081
  secure_api:
    address: https://api.internal:8443

servers:
  - name: app.example.test
    # Omit listen to use global.default_port.
    listen: 8088
    routes:
      - path: /api/v2
        upstream: api
        match: segment_prefix
        rewrite: /v2
        proxy_headers:
          X-Proxy-Source: routeforge
        response_headers:
          X-Content-Type-Options: nosniff
        connect_timeout: 3s
        read_timeout: 30s

      - path: /socket.io
        upstream: api
        match: segment_prefix
        websocket: true

      - path: /legacy
        upstream: web
        match: exact
        redirect_slash: true

      - path: /
        upstream: web
        match: prefix

  - name: secure.example.test
    listen: 8443
    tls:
      certificate: ./certs/server.crt
      key: ./certs/server.key
    routes:
      - path: /
        upstream: secure_api
        match: prefix
```

Run it with the native engine:

```sh
rforge validate --config routeforge.yaml
rforge serve --engine native --config routeforge.yaml
```

To inspect generated Nginx configuration:

```sh
rforge generate --config routeforge.yaml
```

## Configuration tree

```text
version
├── global
│   ├── default_port
│   ├── worker_connections
│   ├── health_path
│   └── proxy
│       ├── connect_timeout
│       ├── read_timeout
│       └── send_timeout
├── hot_reload
│   ├── enabled
│   ├── debounce
│   ├── poll_interval
│   └── rollback_on_failure
├── upstreams
│   └── <upstream-name>
│       └── address
└── servers[]
    ├── name
    ├── listen
    ├── tls
    │   ├── certificate
    │   └── key
    └── routes[]
        ├── path
        ├── upstream
        ├── match
        ├── websocket
        ├── redirect_slash
        ├── preserve_path
        ├── rewrite
        ├── proxy_headers
        ├── response_headers
        ├── connect_timeout
        ├── read_timeout
        └── send_timeout
```

## Root fields

| Field | Type | Requirement and behavior |
| --- | --- | --- |
| `version` | integer | Required in the versioned format. Currently must be `1`. |
| `global` | mapping | Global settings; all child fields are optional. |
| `hot_reload` | mapping | File-watcher settings; all child fields are optional. |
| `upstreams` | name-to-upstream mapping | At least one upstream is required. Routes refer to upstreams by name. |
| `servers` | list | At least one virtual server is required. |

## `global` settings

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `default_port` | integer | `80` | TCP port used for servers without `listen`. Must be in the range `1`–`65535`. An explicit per-server `listen` takes precedence. |
| `worker_connections` | integer | `1024` in the generator | Rendered as Nginx `worker_connections`. The native engine does not use this setting. |
| `health_path` | string | `/healthz` in native mode | Native health endpoint path. Configured virtual hosts return `200` with body `ok`. Must start with `/` and cannot contain a query or fragment. The current Nginx generator does not create this endpoint. |
| `proxy` | mapping | empty | Default upstream connection and response timeouts. A route can override each timeout. |

### `global.proxy`

Timeouts are Go duration strings such as `500ms`, `10s`, or `2m`.

- `connect_timeout`: time allowed to establish an upstream connection.
- `read_timeout`: upstream response read timeout.
- `send_timeout`: timeout for sending a request.

Timeout behavior is not identical between the two engines. In native mode, `read_timeout` becomes the response-header timeout. It does not fully reproduce Nginx's timeout between every read of a streaming response, and `send_timeout` is not fully equivalent. If your application depends on those details, verify it under real traffic before migrating.

## `hot_reload` settings

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `enabled` | boolean | `true` | Enables automatic watching when the process starts. When `false`, changes are not automatically applied while it runs. |
| `debounce` | duration | `2s` | Delay after a file change before reading and validating the candidate configuration. |
| `poll_interval` | duration | `1s` | Interval for checking file contents, including saves that do not produce a filesystem event. |
| `rollback_on_failure` | boolean | `true` | In the Nginx runtime, restores the previous active config file if Nginx reload fails. Native mode never activates an invalid candidate and retains its previous snapshot. |

Native mode combines fsnotify with polling. It parses, validates, and compiles a candidate before atomically activating it. Requests that already selected a route continue using that route. Listener port, host name, or TLS-mode changes cannot be applied while native `serve` is running; such changes are rejected and require a restart.

## Upstreams

```yaml
upstreams:
  catalog:
    address: 127.0.0.1:9000
  identity:
    address: https://identity.internal:9443
```

- The mapping key is the upstream's name. Each route must refer to a declared name.
- For compatibility with Nginx, the recommended address format is `host:port`.
- Native mode also accepts URLs with `http://` or `https://` schemes.
- Clients cannot choose the destination. The destination comes only from a configured upstream.
- In Docker, an upstream address must be reachable from inside the container. In native mode, it must be reachable from the host.

## Virtual servers

Each item in `servers` defines a virtual host.

| Field | Type | Requirement and behavior |
| --- | --- | --- |
| `name` | string | Required. Used to match the HTTP Host header and, for TLS, should correspond to the certificate's SNI name. |
| `listen` | integer | Optional TCP port. Defaults to `global.default_port`, or `80` if that is omitted. |
| `tls` | mapping | Optional. Certificate and key enable TLS on the server's listener. |
| `routes` | list | Routes for this virtual host. Each route must refer to a declared upstream. |

Multiple servers may share a port and are selected by `name`. In native mode, if TLS is enabled for a host on a port, the listener on that port uses TLS; all virtual hosts sharing that listener must be appropriate for TLS. Use separate ports for separate listeners. Native mode does not automatically choose port `443`: set `listen: 443` explicitly for HTTPS unless you intentionally set `global.default_port` to `443`.

### TLS

```yaml
servers:
  - name: secure.example.test
    listen: 443
    tls:
      certificate: /etc/routeforge/tls/fullchain.pem
      key: /etc/routeforge/tls/private.key
```

Both paths are required and must be readable by the process. Native mode uses TLS 1.2 or newer and selects the certificate using SNI. On reload, certificate and key files are read again, but changing the port, host, or TLS mode requires a restart. Automatic certificate issuance is not provided. When using Docker, mount the certificate files inside the container.

## Routes

Each route requires `path` and `upstream`.

### `path` and `upstream`

- `path` must start with `/`.
- `upstream` must name an entry under `upstreams`; routes cannot contain arbitrary upstream destinations.
- Avoid duplicate routes with the same `match` mode and `path`.

### Match modes

If `match` is omitted, it defaults to `prefix`. Route precedence is:

1. `exact`
2. `regex`
3. `segment_prefix`, longest path first
4. `prefix`, longest path first

Declaration order is retained for equal-ranked routes.

| Value | Behavior | Example |
| --- | --- | --- |
| `exact` | Matches only the exact path. | `/login` matches `/login` only. |
| `prefix` | Matches any path that starts with the path text; segment boundaries are not checked. | `/api` can also match `/apix`. |
| `segment_prefix` | Matches the path and descendants at a `/` boundary. | Matches `/ticketing` and `/ticketing/users`, but not `/ticketing-old`. |
| `regex` | Matches a regular expression. | `^/items/[0-9]+$` |

Native mode uses Go's RE2 regular expressions and matches them case-insensitively. Lookarounds and backreferences supported by some PCRE engines are not available. Nginx uses its own regular-expression engine. For portability, use simple expressions supported by both engines.

For routes such as `/ticketing` and `/ticketing/api`, `segment_prefix` usually avoids matching similar names such as `/ticketing-old`. Longest-path precedence makes `/ticketing/api` win over `/ticketing`.

### Path rewriting and preservation

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `preserve_path` | boolean | `true` | Forwards the incoming path. If `false`, the upstream path is set to `/`. |
| `rewrite` | string | empty | Literal absolute path used to rewrite the route path. Must start with `/`; query strings and fragments are not allowed here. |

Example that removes a path prefix:

```yaml
- path: /ticketing/api
  match: segment_prefix
  upstream: ticketing_api
  rewrite: /api
```

| Incoming request | Upstream path |
| --- | --- |
| `/ticketing/api` | `/api` |
| `/ticketing/api/users` | `/api/users` |
| `/ticketing/api/users?page=2` | `/api/users?page=2` |

The request query string is preserved. Use `rewrite` with literal routes, preferably `segment_prefix`; rewrite behavior for regex routes is not guaranteed to be equivalent between native and Nginx.

### Trailing slash redirects

`redirect_slash: true` permanently redirects an exact route path to the same path with a trailing `/`. For example, `path: /ticketing` redirects `/ticketing` to `/ticketing/`. Use this only when the upstream will not redirect the trailing-slash URL back to the original path, which could create a redirect loop.

### WebSockets

```yaml
- path: /socket.io
  match: segment_prefix
  upstream: realtime
  websocket: true
```

This enables the Upgrade headers in generated Nginx configuration. Native mode uses the WebSocket upgrade support in `httputil.ReverseProxy`. Verify timeout behavior and long-lived connections with your service; a connection may remain open across a configuration reload.

### Headers

```yaml
proxy_headers:
  X-Route-Name: api
response_headers:
  X-Content-Type-Options: nosniff
```

- `proxy_headers` sets or replaces headers sent to the upstream.
- `response_headers` sets or replaces headers on the upstream response.
- Header names may contain only ASCII letters, digits, and hyphens.
- Header values cannot contain newlines, `;`, `{`, or `}`.
- Use fixed values or environment substitution; these fields are not a way to add arbitrary Nginx directives.
- Native mode clears incoming `Forwarded` and `X-Forwarded-*` headers before generating its own proxy headers, preventing client-supplied values from being trusted. Values configured in `proxy_headers` are applied afterward.

### Route timeouts

A route may set `connect_timeout`, `read_timeout`, and `send_timeout`. These override the corresponding values under `global.proxy`. Values use Go duration syntax such as `2s`, `250ms`, or `1m`. An omitted route value uses the global value or the engine default.

## Environment variables

`${NAME}` and `$NAME` are replaced with environment variables before YAML parsing. Every referenced variable must be set; it may be empty if it exists.

```yaml
upstreams:
  app:
    address: ${APP_HOST}:${APP_PORT}
servers:
  - name: ${PUBLIC_HOST}
```

Substitution happens before YAML parsing. If a value contains YAML-sensitive characters such as `:`, `#`, or `: `, quote it appropriately or use simple values. Do not put secrets in public configuration files or logs; restrict access to configuration and TLS private-key files.

## Legacy configuration compatibility

Unversioned files using scalar upstream addresses and `prefix: true` routes are accepted:

```yaml
upstreams:
  frontend: localhost:3000
  ticketing: localhost:3001
servers:
  - name: app.local
    routes:
      - path: /ticketing
        upstream: ticketing
        prefix: true
        redirect_slash: true
      - path: /
        upstream: frontend
```

The loader converts this layout to schema version 1. `prefix: true` becomes `match: segment_prefix`. Use `version: 1`, object-form upstreams with `address`, and `match` for new configurations. In version 1, unknown keys, duplicate mapping keys, and the legacy route field `prefix` are errors.

## Engine differences and migration notes

- **Native:** Binds ports directly on the host, provides `health_path`, and reloads routes in memory. It does not use `worker_connections`.
- **Nginx:** `rforge generate` renders Nginx configuration. `up`, `down`, `restart`, `status`, `logs`, and `reload` continue to manage the existing Docker Compose deployment. The Nginx generator does not create the `global.health_path` endpoint.
- Go and Nginx timeout behavior is not identical in every case. Review the timeout section before migrating.
- Do not change `listen`, TLS, or virtual hosts while native mode is running; restart the process with the new configuration.
- Rebuild the CLI after upgrading RouteForge or changing the schema so the updated parser is used.

## Common errors

- **Undefined upstream:** `routes[].upstream` must match a name under `upstreams`.
- **Unexpected route selected:** Check the `match` mode and the exact/regex/segment-prefix/prefix precedence. Use `segment_prefix` to enforce path-segment boundaries.
- **Port cannot be opened:** Confirm the port is valid and available, and that the process can bind it. Ports such as 80 and 443 may require elevated privileges.
- **TLS certificate cannot be loaded:** Check file paths, read permissions, certificate/key pairing, and that SNI matches `servers[].name`.
- **`field ... not found`:** The binary may be stale or the key may be misspelled. Rebuild the binary and compare the key with this reference.
- **Unversioned file rejected:** The legacy format accepts scalar upstreams and recognized legacy fields only. To migrate, add `version: 1`, convert upstreams to `address` objects, and change `prefix: true` to `match: segment_prefix`.
