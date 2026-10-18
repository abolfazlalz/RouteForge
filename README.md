# YAML-driven Nginx

Start the proxy once, then edit `config.yaml` at any time:

```sh
docker compose up -d --build
```

`config.yaml` is directory-bind-mounted into the container. Its supervisor hashes contents (so
rename-based editor saves work), debounces changes, renders a candidate in an isolated temporary
tree, and runs `nginx -t` before atomically installing it. It then asks the master process for a
graceful reload, allowing existing connections to drain. Invalid YAML or invalid generated Nginx
is logged and leaves the running service and last known-good file untouched.

Override the defaults without rebuilding:

```env
NGINX_AUTO_RELOAD=true
NGINX_RELOAD_DEBOUNCE=2
NGINX_RELOAD_POLL_INTERVAL=1
```

Follow activity with `docker compose logs -f proxy`. A successful `nginx -s reload` means the
signal was delivered; Nginx does not offer a transactional acknowledgement that every new worker
has adopted the configuration. If sending the signal itself fails, the prior file is restored and
a best-effort reload of it is attempted.

To check the live configuration explicitly:

```sh
docker compose exec proxy nginx -t
```

Both domains share frontend 3000 and ticketing 3001, while API, storage and Socket.IO route to their corresponding backends. The `/ticketing/` prefix is preserved for applications configured with that base path.
