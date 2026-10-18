FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/rforge ./cmd/rforge
FROM nginx:1.27-alpine
COPY --from=build /out/rforge /usr/local/bin/rforge
COPY docker-entrypoint.sh /usr/local/bin/routeforge-entrypoint
RUN chmod 755 /usr/local/bin/routeforge-entrypoint
ENTRYPOINT ["/usr/local/bin/routeforge-entrypoint"]
