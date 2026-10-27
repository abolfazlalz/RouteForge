# RouteForge changelog

Add a section for each release before creating its Git tag. The heading must match the tag.
For example, tag `v1.2.3` requires `## [v1.2.3] - YYYY-MM-DD` or `## v1.2.3`.
The release workflow publishes the content below that heading up to the next `## ` heading.

## [v0.1.1] - 2026-10-18

### Fixed

- Route `/ticketing/api` to the ticketing API upstream and rewrite the forwarded path to `/api`, preserving suffix paths.
- Prevent `segment_prefix` routes from falling through on the exact base path.
- Reject unsafe path rewrite targets.

## [v0.1.0] - 2026-10-18

### Added

- Initial Go-based `rforge` CLI with project initialization and Docker Compose lifecycle commands.
- Version 1 YAML configuration parsing, validation, environment substitution, and deterministic Nginx rendering.
- Docker runtime with staged Nginx validation and automatic configuration watching/reload.
- GitHub Actions release workflow for Linux, macOS, and Windows binaries with SHA-256 checksums.
- GitLab CI pipeline, example configuration, Docker setup, and project documentation.

## [Unreleased]

- Add upcoming changes here, then move them under the matching version heading before tagging.

## [v0.2.0] - 2026-10-27

### Added

- Added a pluggable engine contract and standalone native Go HTTP reverse proxy.
- Added native virtual hosts, deterministic route precedence, rewrites, header overrides, WebSocket upgrade support, TLS/SNI, structured request logs, and `/healthz`.
- Added atomic route snapshot reloads with fsnotify plus configured polling/debounce, retaining the active configuration when candidates fail.
- Added `rforge serve --engine native`; existing Compose commands continue to manage Nginx.
- Added `global.default_port` for servers that omit their per-server `listen` setting.
- Restored parsing support for the original unversioned YAML layout, including scalar upstreams and `prefix: true` routes.
- Added native router and forwarding tests and native-mode documentation.

### Limitations

- Listener identity changes require restarting `serve`.
- Native response-header/connect timeout behavior does not reproduce Nginx idle-read/send timeout behavior.
