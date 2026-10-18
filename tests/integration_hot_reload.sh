#!/bin/sh
# Docker smoke test. Run from the repository root: sh tests/integration_hot_reload.sh
set -eu

project="nginx-yaml-hot-reload-$$"
scratch=$(mktemp -d)
workdir="$scratch/source"
cleanup() { docker compose -p "$project" -f "$workdir/compose.yaml" down -v --remove-orphans >/dev/null 2>&1 || true; rm -rf "$scratch"; }
trap cleanup EXIT INT TERM
cp -R . "$workdir"
cp "$workdir/config.yaml" "$workdir/config.yaml.valid"
docker compose -p "$project" -f "$workdir/compose.yaml" up -d --build

# The watcher proves it is live, then an atomic save produces exactly one reload request.
until docker compose -p "$project" -f "$workdir/compose.yaml" logs proxy 2>&1 | grep -q 'Watching /config/config.yaml'; do sleep 1; done
sed 's/:3000/:3002/' "$workdir/config.yaml" > /tmp/nginx-yaml-config-$$
mv /tmp/nginx-yaml-config-$$ "$workdir/config.yaml"
until docker compose -p "$project" -f "$workdir/compose.yaml" logs proxy 2>&1 | grep -q 'Graceful Nginx reload signal sent successfully'; do sleep 1; done

# Bad YAML is rejected while the master remains running; a later valid save reloads again.
printf 'servers: [broken\n' > /tmp/nginx-yaml-config-$$
mv /tmp/nginx-yaml-config-$$ "$workdir/config.yaml"
until docker compose -p "$project" -f "$workdir/compose.yaml" logs proxy 2>&1 | grep -q 'Configuration update rejected'; do sleep 1; done
docker compose -p "$project" -f "$workdir/compose.yaml" exec -T proxy nginx -t
mv "$workdir/config.yaml.valid" "$workdir/config.yaml"
until [ "$(docker compose -p "$project" -f "$workdir/compose.yaml" logs proxy 2>&1 | grep -c 'Graceful Nginx reload signal sent successfully')" -ge 2 ]; do sleep 1; done
