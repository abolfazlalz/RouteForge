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
