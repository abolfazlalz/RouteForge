FROM docker.espandarco.com/library/nginx:1.27-alpine

RUN apk add --no-cache python3 py3-yaml py3-jinja2 && mkdir -p /var/lib/nginx-yaml-generator
COPY render.py hot_reload.py nginx.conf.j2 /app/
ENTRYPOINT ["python3", "/app/hot_reload.py"]
