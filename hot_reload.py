#!/usr/bin/env python3
"""PID 1 supervisor: safely render, test, atomically install and reload Nginx."""
from __future__ import annotations
import asyncio, hashlib, logging, os, shutil, signal, subprocess, sys, tempfile, time
from pathlib import Path
from dataclasses import dataclass
from render import render_config

LOG = logging.getLogger("nginx-hot-reload")

def env_bool(name, default):
    raw = os.getenv(name)
    if raw is None: return default
    if raw.lower() not in {"1", "true", "yes", "0", "false", "no"}: raise ValueError(f"{name} must be a boolean")
    return raw.lower() in {"1", "true", "yes"}

def duration(value, default):
    if value is None: return default
    text = str(value).strip(); multiplier = .001 if text.endswith("ms") else 1
    number = text[:-2] if text.endswith("ms") else text[:-1] if text.endswith("s") else text
    result = float(number) * multiplier
    if result <= 0: raise ValueError("hot_reload durations must be greater than zero")
    return result

@dataclass(frozen=True)
class Settings:
    enabled: bool = True; debounce: float = 2; poll_interval: float = 1; rollback_on_failure: bool = True
    @classmethod
    def read(cls, path):
        import yaml
        try:
            data = yaml.safe_load(path.read_text()) or {}; hot = data.get("hot_reload", {})
            if not isinstance(hot, dict): raise ValueError("hot_reload must be a mapping")
            base = cls(bool(hot.get("enabled", True)), duration(hot.get("debounce"), 2), duration(hot.get("poll_interval"), 1), bool(hot.get("rollback_on_failure", True)))
        except Exception as error:
            LOG.warning("Cannot read hot_reload settings; using safe defaults: %s", error); base = cls()
        return cls(env_bool("NGINX_AUTO_RELOAD", base.enabled), duration(os.getenv("NGINX_RELOAD_DEBOUNCE"), base.debounce), duration(os.getenv("NGINX_RELOAD_POLL_INTERVAL"), base.poll_interval), base.rollback_on_failure)

class Reloader:
    def __init__(self, config, template_dir, active, state):
        self.config, self.template_dir, self.active, self.state = map(Path, (config, template_dir, active, state))
        self.last_source_hash = None; self.lock = asyncio.Lock()
    @staticmethod
    def digest(value): return hashlib.sha256(value).hexdigest()
    def atomic_write(self, value, path):
        path.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile("w", dir=path.parent, encoding="utf-8", delete=False) as file:
            file.write(value); file.flush(); os.fsync(file.fileno()); temporary = file.name
        os.replace(temporary, path)
    def validate(self, rendered):
        root = Path(tempfile.mkdtemp(prefix="nginx-candidate-", dir="/tmp")); candidate = root / "nginx.conf"; candidate.write_text(rendered)
        result = subprocess.run(["nginx", "-t", "-q", "-p", str(root), "-c", str(candidate)], text=True, capture_output=True)
        if result.returncode:
            shutil.rmtree(root, ignore_errors=True); raise RuntimeError((result.stderr or result.stdout).strip())
        return root
    def apply(self, initial=False):
        rendered = render_config(self.config, self.template_dir)
        if not initial and self.active.exists() and self.digest(self.active.read_bytes()) == self.digest(rendered.encode()):
            LOG.info("Generated Nginx configuration is unchanged; no reload needed"); return False
        tree = self.validate(rendered)
        try:
            old = self.active.read_text() if self.active.exists() else None
            self.atomic_write(rendered, self.active)
            if initial:
                self.atomic_write(rendered, self.state / "last-known-good.conf"); LOG.info("Installed validated initial Nginx configuration"); return True
            LOG.info("Validated candidate; requesting graceful Nginx reload")
            result = subprocess.run(["nginx", "-s", "reload"], text=True, capture_output=True)
            if result.returncode:
                message = (result.stderr or result.stdout).strip()
                LOG.error("Nginx reload signal failed: %s", message)
                if old is not None and Settings.read(self.config).rollback_on_failure:
                    self.atomic_write(old, self.active); subprocess.run(["nginx", "-s", "reload"], text=True, capture_output=True)
                raise RuntimeError(message or "nginx -s reload failed")
            self.atomic_write(rendered, self.state / "last-known-good.conf"); LOG.info("Graceful Nginx reload signal sent successfully"); return True
        finally: shutil.rmtree(tree, ignore_errors=True)
    async def watch(self):
        LOG.info("Watching %s for content changes", self.config); pending = None
        while True:
            settings = Settings.read(self.config); await asyncio.sleep(settings.poll_interval)
            try: source_hash = self.digest(self.config.read_bytes())
            except OSError as error: LOG.warning("Cannot read mounted YAML; retaining active configuration: %s", error); continue
            if source_hash != self.last_source_hash:
                self.last_source_hash = source_hash; pending = time.monotonic(); LOG.info("Detected configuration content change")
            if not settings.enabled or pending is None or time.monotonic() - pending < settings.debounce: continue
            pending = None
            async with self.lock:
                try: self.apply()
                except Exception as error: LOG.error("Configuration update rejected; active Nginx remains unchanged: %s", error)

async def supervise(reloader):
    try: reloader.apply(initial=True)
    except Exception as error: LOG.error("Initial mounted configuration is invalid; using image configuration: %s", error)
    if reloader.config.exists(): reloader.last_source_hash = reloader.digest(reloader.config.read_bytes())
    nginx = await asyncio.create_subprocess_exec("nginx", "-g", "daemon off;"); watcher = asyncio.create_task(reloader.watch())
    try: return await nginx.wait()
    finally: watcher.cancel(); await asyncio.gather(watcher, return_exceptions=True)

def main():
    logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"), format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    reloader = Reloader(os.getenv("NGINX_YAML_CONFIG", "/config/config.yaml"), "/app", "/etc/nginx/nginx.conf", "/var/lib/nginx-yaml-generator")
    loop = asyncio.new_event_loop(); asyncio.set_event_loop(loop); task = loop.create_task(supervise(reloader))
    def terminate(signum, _):
        # Do not cancel the child wait: waiting for it reaps Nginx and avoids zombies.
        LOG.info("Forwarding termination signal %s to Nginx", signum)
        subprocess.run(["nginx", "-s", "quit"], capture_output=True)
    for signum in (signal.SIGTERM, signal.SIGINT): loop.add_signal_handler(signum, terminate, signum, None)
    try: return loop.run_until_complete(task)
    except asyncio.CancelledError: return 0
    finally: loop.close()
if __name__ == "__main__": sys.exit(main())
