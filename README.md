# Gin FinAPI

[English](README.md) | [简体中文](README.zh-CN.md)

A Go HTTP query service built with Gin. Redis stores API credentials, product permissions, and variable mappings;
MongoDB stores gzip/CSV query data. Optional MySQL provides persistent configuration, fallback reads, and Redis synchronization.

## Stack and endpoints

- Go `1.24.0`, toolchain `go1.24.10`; Gin `1.11.0`. Dependencies are pinned in [go.mod](go.mod).
- `GET /health`: liveness check, not a database readiness check.
- `POST /api`: data queries protected by API Key authentication, request signatures, and product authorization.
- `DELETE /config/cache`: clears this process's configuration cache. Disabled by default; requires a separate management token.

## Quick start

Requirements: Go, Redis, and MongoDB. MySQL is disabled by default. Run commands from the repository root:

```bash
go mod download
cp .env.example .env
```

Do not overwrite an existing `.env`. Configure your environment and provision API credentials,
product permissions, variable mappings, and query data through an authorized process, then start the service:

```bash
go run main.go
```

The default port is `8082`:

```bash
curl http://127.0.0.1:8082/health
```

The application does not initialize business data automatically. The Mongo initializer rebuilds collections;
never run it without explicit authorization. Enabling MySQL enables Redis synchronization by default:
verify the target and overwrite scope first. Business requests require freshly generated signatures;
see the API reference and importable test cases below.

## Development checks

```bash
go mod verify
gofmt -l $(git ls-files '*.go')
go vet ./...
go test -race ./...
go build ./...
git diff --check
```

## Documentation

The detailed documents below are currently in Simplified Chinese.

- [API and authentication](doc/knowledge/api_reference.md): requests, responses, signatures, phone lookup rules, and cache management.
- [Configuration and storage](doc/knowledge/configuration.md): environment variables, caching, Redis records, and optional MySQL.
- [Development, deployment, and logging](doc/knowledge/operations.md): project layout, quality gates, lifecycle, log format, and stage timing.
- [Initialization and synchronization tools](cmd/README.md), [Apifox test cases](tests/APIFOX.md), and [isolated integration tests](tests/integration/README.md).
- [Full documentation index](doc/README.md), [AI coding entry point](AGENTS.md), and [project rules](doc/rules/README.md).
