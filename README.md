# RouteForge

RouteForge is a Go-based, YAML-driven manager for Nginx reverse proxies. The `rforge` CLI validates and renders routing configuration, controls the Docker Compose project in the current directory, and initializes a self-building Nginx runtime. The runtime watches configuration changes and applies validated updates without restarting the container.

## Requirements

- Go 1.26+ to build or install the CLI from source
- Docker Engine with Docker Compose v2 only for the Nginx deployment
- Network access to pull the Go and Nginx base images when building the Nginx runtime image

## Quick start

Install from a RouteForge source checkout, then initialize a proxy project:

```sh
go install ./cmd/rforge
mkdir my-proxy
cd my-proxy
rforge init
rforge up
rforge status
```

`rforge init` writes `routeforge.yaml`, `compose.yml`, and `.env.example`. It also writes `.routeforge/Dockerfile`, `.routeforge/entrypoint`, and a Go source bundle under `.routeforge/src`. Compose builds the runtime for Linux from that bundle; it does not copy or try to run the host's `rforge` binary inside the container.

The generated Compose project publishes ports 80 and 443 by default. Set `HTTP_PORT` and `HTTPS_PORT` in a `.env` file to change the host ports. The supplied Compose configuration adds `host.docker.internal:host-gateway` for Linux hosts and mounts the project directory read-only at `/config`.

After upgrading the CLI, refresh the generated runtime files before rebuilding:

```sh
go install ./cmd/rforge
rforge init --force
rforge up --build
```

`--force` overwrites the generated configuration and Compose files too. Back up local edits to those files before using it.

## CLI reference

Commands run from the current directory. RouteForge does not search parent directories for configuration or Compose files.

| Command | Action |
| --- | --- |
| `rforge init` | Create starter YAML, Compose, and runtime build files |
| `rforge init --force` | Replace generated starter files and refresh the runtime source bundle |
| `rforge up` | Build/start the Compose services detached |
| `rforge up --build` | Rebuild the runtime image and start detached |
| `rforge up -d` | Accepted as an explicit detached start |
| `rforge down` | Stop the Compose project |
| `rforge restart` | Restart Compose services |
| `rforge status` / `rforge ps` | Show Compose service status |
| `rforge logs` / `rforge logs -f` | Show or follow Compose logs |
| `rforge validate` | Parse and validate the selected YAML file |
| `rforge generate` | Print generated Nginx configuration |
| `rforge reload` | Validate/apply the config and signal Nginx in the running proxy container |
| `rforge config` | Print the selected YAML file |
| `rforge version` | Print CLI version information |

Compose discovery checks `compose.yml` first, then `compose.yaml`. Use `-f/--file` to select a Compose file and `-c/--config` to select a YAML file for configuration commands. Compose lifecycle commands call Docker directly without a shell; Docker errors are reported to the caller.

Install from a source checkout with `go install ./cmd/rforge`, or build a local binary with:

```sh
go build -o rforge ./cmd/rforge
```

## Configuration schema

Configuration references: [English](docs/configuration.md) · [فارسی](docs/configuration.fa.md).

RouteForge currently supports schema version `1`. The following example configures two virtual hosts and named upstreams:

```yaml
version: 1

global:
  default_port: 8080
  worker_connections: 1024
  proxy:
    connect_timeout: 60s
    read_timeout: 3600s
    send_timeout: 3600s

hot_reload:
  enabled: true
  debounce: 2s
  poll_interval: 1s
  rollback_on_failure: true

upstreams:
  frontend:
    address: host.docker.internal:3000
  api:
    address: host.docker.internal:9000
  ticketing:
    address: host.docker.internal:3001

servers:
  - name: app.localhost
    # Omit listen to use global.default_port; explicit ports override it.
    routes:
      - path: /api
        upstream: api
        match: segment_prefix
      - path: /ticketing
        upstream: ticketing
        match: segment_prefix
        redirect_slash: true
        preserve_path: true
        websocket: true
      - path: /socket.io
        upstream: api
        match: segment_prefix
        websocket: true
      - path: /
        upstream: frontend
        match: prefix
```

### Supported settings

- `global.default_port` for servers that omit `listen` (defaults to 80), `global.worker_connections`, and global proxy connect/read/send timeouts
- Named upstreams, each with an `address`
- Multiple servers, with `name`, optional `listen` port (defaults to `global.default_port`, itself defaulting to 80), optional TLS certificate/key paths, and routes
- Route match modes: `exact`, `prefix`, `segment_prefix`, and regular expression `regex`
- Per-route upstream, path preservation (enabled by default), optional rewrite and trailing slash redirect
- WebSocket upgrade headers and per-route proxy timeout overrides
- Custom request proxy headers and response headers
- `${NAME}` and `$NAME` environment variable substitutions; referenced variables must be set
- Hot reload settings in the YAML schema

Schema version 1 uses strict YAML decoding and rejects unknown fields and duplicate mapping keys. The original unversioned layout is also accepted: scalar upstream addresses and route `prefix: true` settings are converted to schema version 1 (`prefix: true` maps to segment-prefix matching). Validation rejects unsupported schema versions, missing upstreams, invalid ports and timeout values, unsupported match modes, malformed route paths, and unsafe header values. Arbitrary Nginx directives are not accepted.

`segment_prefix` routes both the base path and its descendants, so `/ticketing` and `/ticketing/api` stay on the ticketing upstream rather than matching a broader frontend or separate `/api` route. Set `rewrite` to a literal absolute path to replace the matched prefix upstream. For example, `path: /ticketing/api`, `match: segment_prefix`, and `rewrite: /api` map `/ticketing/api` to `/api` and `/ticketing/api/users` to `/api/users`. Leave `redirect_slash` disabled when the upstream has its own canonical slash behavior. Enabling it redirects `/ticketing` to `/ticketing/`; do not enable it if the upstream redirects that URL back to `/ticketing`, or the two redirects will loop. Query strings, including Socket.IO polling parameters, are retained by Nginx by default.

## Runtime and hot reload

The runtime performs these steps on container startup:

1. Load and validate `/config/routeforge.yaml` (or `ROUTEFORGE_CONFIG`).
2. Render a candidate Nginx configuration.
3. Run `nginx -t` against an isolated staging prefix.
4. Install the validated configuration, then start Nginx.
5. Watch the mounted directory for file events and periodically compare file contents to catch atomic replacement saves.

The initial configuration is installed before Nginx starts. Later valid changes are debounced, validated, atomically installed, and followed by a graceful reload signal. Invalid changes are logged and leave the active configuration intact; the watcher continues running so a later corrected file can be applied. `rforge reload` performs the same validate/apply flow against the running Compose service without restarting the container.

Nginx does not provide a transactional acknowledgement that every new worker adopted a reload. A successful signal means Nginx accepted the reload signal; inspect runtime logs and service health for further evidence. Graceful reloads allow old workers to drain. Long-lived WebSocket connections may keep old workers alive until those connections close.

The runtime image does not need access to the Docker socket and exposes no management HTTP endpoint. Runtime logs use structured JSON on stdout and can be followed with `rforge logs -f`.

## Docker usage

The initialized project's normal workflow is:

```sh
rforge up --build
rforge ps
rforge logs -f
rforge reload
rforge down
```

The root checkout also has a Dockerfile and `compose.yml` for development from the RouteForge source tree:

```sh
docker compose up -d --build
```

The initialized-project Dockerfile uses a Go build stage followed by an Nginx runtime stage. If image building fails while resolving `golang:1.26-alpine` or `nginx:1.27-alpine`, check registry access and Docker's configured registry mirror, then retry the build.

## Troubleshooting

### `initial config invalid`

The container logs include the underlying cause in the JSON `error` field. Check the configuration and rendered Nginx syntax:

```sh
rforge validate
rforge generate
docker compose logs proxy
```

An empty or invalid `/run/nginx.pid` during startup indicates a runtime image with old startup behavior. Upgrade the CLI, regenerate the runtime files, and rebuild:

```sh
go install ./cmd/rforge
rforge init --force
rforge up --build
```

### Runtime binary or source build errors

The initialized runtime image compiles the bundled `.routeforge/src` module for Linux. If its Docker build reports missing Go packages, refresh that bundle with `rforge init --force`, then rebuild with `rforge up --build`.

### Upstream is unreachable

Confirm that the upstream address is reachable from the container. For a service on the Docker host, use `host.docker.internal` and keep the generated `extra_hosts` entry on Linux. Check `rforge logs` for Nginx and reload validation errors.

## GitHub releases

The GitHub Actions release workflow runs when a semantic version tag such as `v1.2.3` is pushed. Before tagging, add that version's notes to `changelogs.md` under a heading like `## [v1.2.3] - 2026-10-18`. The workflow extracts the section up to the next level-two heading. If the matching section is missing or empty, release creation fails instead of publishing incomplete notes.

For example:

```markdown
## [v1.2.3] - 2026-10-18

- Add the new routing feature.
- Fix configuration reload behavior.

## [Unreleased]
```

Then commit the changelog and push the tag:

```sh
git tag v1.2.3
git push origin v1.2.3
```

The workflow builds standalone binaries for Linux AMD64/ARM64, macOS AMD64/ARM64, and Windows AMD64, adds a SHA-256 checksum file, and creates or updates the GitHub Release with the extracted notes and binaries. Ensure GitHub Actions is enabled and the repository's `GITHUB_TOKEN` has permission to create releases (`contents: write` is scoped to the release job).

## GitLab CI

The `.gitlab-ci.yml` pipeline remains available for GitLab users. It runs `go vet`, standard and race Go test jobs, cross-platform binary builds, and a Buildx multi-architecture image build. Container publishing requires registry variables and a runner that supports Docker Buildx. Configure registry publishing and any GitLab release policy separately if using that pipeline.

## Security

- Do not put secrets in values that will be expanded into generated Nginx configuration.
- Keep YAML, TLS key, and certificate file permissions restricted.
- RouteForge does not accept unrestricted Nginx directives or expose an unauthenticated reload API.
- The runtime container does not mount `/var/run/docker.sock`.

## Contributing

Format Go changes with `gofmt`, run `go test ./...` and `go vet ./...`, and add focused tests for behavior changes. Keep generated Nginx output deterministic and document user-visible configuration changes.

## Native mode (v0.2.0)

The CLI can also run a standalone Go proxy without Docker or Nginx:

```sh
go build -o rforge ./cmd/rforge
./rforge serve --engine native --config routeforge.yaml
```

`serve` stays in the foreground, handles Ctrl-C/SIGTERM with graceful HTTP shutdown, and watches the selected file. The listener ports, host names, and HTTP/TLS mode are fixed at startup; changing them is rejected and requires a restart. Route and upstream changes are compiled first and atomically activated, so existing requests finish against the route they already selected. Set `hot_reload.debounce` and `hot_reload.poll_interval` to configure the change debounce and fallback polling interval. A failed parse, validation, regex compilation, or certificate load keeps the previous configuration active.

Native TLS uses each server's existing `tls.certificate` and `tls.key` values. SNI selects the configured virtual host certificate. Certificates can be replaced at the configured paths and reloaded, while listener identity stays fixed. The engine uses TLS 1.2 or newer. No automatic certificate provisioning is provided.

The built-in `/healthz` endpoint returns `200 ok` for configured virtual hosts. Set `global.health_path` to change that path; RouteForge rejects values that are not absolute paths or that contain query/fragment syntax. Native mode supports HTTP reverse proxying, HTTP/1.1 WebSocket upgrades, fixed configured upstream targets, header overrides, and existing route matching/rewrite rules. It streams responses. Connect and response-header timeouts are applied; Go's standard transport does not offer Nginx's idle-between-every-read proxy timeout semantics or proxy send timeout, so those timeout fields are not behaviorally identical in native mode. Route regexes follow the Nginx renderer's case-insensitive behavior. Request bodies have no schema-configurable size limit; HTTP headers are capped at 1 MiB.

`up`, `down`, `restart`, `status`, `logs`, and `reload` continue to manage the Docker Compose Nginx deployment. `serve --engine nginx` is intentionally rejected: manage that engine with the existing Compose commands. `generate` continues to render Nginx configuration. Native mode does not require Docker/Nginx, but the current `init` command still creates the backward-compatible Compose project alongside its YAML file.

### Migration from Nginx

Use the same version 1 YAML file and first run `rforge validate`, then `rforge serve --engine native`. Existing `name`, `listen`, TLS, upstream, route, timeout, and custom-header settings are read directly. Keep the listener set unchanged during a native process; restart after changing a port, virtual host, or TLS enablement. Check applications that depend on Nginx-only timeout details or extra Nginx features before switching production traffic. You can return to the existing Nginx deployment at any time with `rforge up`.
