"""Rendering and validation helpers used by the runtime supervisor."""
from __future__ import annotations

import os
import re
from pathlib import Path
from typing import Any

import yaml
from jinja2 import Environment, FileSystemLoader, StrictUndefined

ENV_VALUE = re.compile(r"\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))")


def resolve_environment(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: resolve_environment(item) for key, item in value.items()}
    if isinstance(value, list):
        return [resolve_environment(item) for item in value]
    if not isinstance(value, str):
        return value

    def replace(match: re.Match[str]) -> str:
        name = match.group(1) or match.group(2)
        if name not in os.environ:
            raise ValueError(f"Environment variable {name!r} is not set")
        return os.environ[name]
    return ENV_VALUE.sub(replace, value)


def load_config(config_path: Path) -> dict[str, Any]:
    data = yaml.safe_load(config_path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError("The YAML root must be a mapping")
    data = resolve_environment(data)
    upstreams, servers = data.get("upstreams"), data.get("servers")
    if not isinstance(upstreams, dict) or not upstreams:
        raise ValueError("upstreams must be a non-empty mapping")
    if not isinstance(servers, list) or not servers:
        raise ValueError("servers must be a non-empty list")
    for name, target in upstreams.items():
        if not isinstance(name, str) or not isinstance(target, str) or not target:
            raise ValueError("upstream names and targets must be non-empty strings")
    for server in servers:
        if not isinstance(server, dict) or not isinstance(server.get("name"), str):
            raise ValueError("every server needs a string name")
        if not isinstance(server.get("routes"), list):
            raise ValueError(f"server {server['name']!r} needs routes")
        for route in server["routes"]:
            if not isinstance(route, dict) or not isinstance(route.get("path"), str):
                raise ValueError("every route needs a string path")
            upstream = route.get("upstream")
            if upstream not in upstreams:
                raise ValueError(f"Unknown upstream: {upstream}")
            route["target"] = upstreams[upstream]
    return data


def render_config(config_path: Path, template_dir: Path) -> str:
    environment = Environment(loader=FileSystemLoader(template_dir), undefined=StrictUndefined,
                              autoescape=False, trim_blocks=True, lstrip_blocks=True)
    return environment.get_template("nginx.conf.j2").render(**load_config(config_path)) + "\n"


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("config", type=Path, nargs="?", default=Path("config.yaml"))
    parser.add_argument("--template-dir", type=Path, default=Path(__file__).resolve().parent)
    parser.add_argument("--output", type=Path, default=Path("/etc/nginx/nginx.conf"))
    args = parser.parse_args()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(render_config(args.config, args.template_dir), encoding="utf-8")
