# syntax=docker/dockerfile:1

# ---- web: build the Svelte UI ----
FROM node:24-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web .
RUN npm run build

# ---- build: compile the Go binary with the UI embedded ----
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /internal/web/dist ./internal/web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/chrisgreg/glance/server/internal/api.Version=${VERSION}" -o /glance ./cmd/glance

# ---- runtime: binary + CA certificates only ----
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 glance && mkdir -p /data && chown glance:glance /data
COPY --from=build /glance /usr/local/bin/glance
USER glance
VOLUME ["/data"]
EXPOSE 8080
ENV GLANCE_PORT=8080 GLANCE_DATABASE_PATH=/data/glance.db
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["glance"]
