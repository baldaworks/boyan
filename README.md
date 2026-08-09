# Boyan

Boyan is a standalone local service for grounded temporal memory for AI agents.

It owns:

- canonical temporal memory state in Badger
- a rebuildable Bleve search projection
- HTTP/JSON ingest for completed turns and explicit boundaries
- Streamable HTTP MCP tools for search, trace, and controlled forgetting
- an isolated runtime/v2-backed derivation worker

Boyan is local-first. The default service binds loopback only and stores all
state under one explicit state root.

## Current scope

- Module: `github.com/baldaworks/boyan`
- Binary: `boyan`
- Deployment model: one local process, one local state root
- Ingest API: `/v1/ingest/turns`, `/v1/ingest/boundaries`
- Health API: `/livez`, `/readyz`
- MCP endpoint: `/mcp`

Remote publication and clean-clone recovery are tracked separately by
`boyan-vqzg.12`. This repository already builds and runs locally.

## Install and build

Build the standalone binary locally:

```bash
go build -o ./bin/boyan ./cmd/boyan
```

Or install it into your Go bin path:

```bash
go install ./cmd/boyan
```

Inspect the available commands:

```bash
go run ./cmd/boyan --help
go run ./cmd/boyan serve --help
go run ./cmd/boyan migrate balda --help
```

## Quick start

1. Copy the example config:

```bash
cp -f boyan.example.yaml ./boyan.yaml
```

2. Set a buildable derivation provider in `boyan.yaml`.

3. Start the service:

```bash
go run ./cmd/boyan serve --config ./boyan.yaml
```

4. Check health:

```bash
curl -sS http://127.0.0.1:8686/livez
curl -sS http://127.0.0.1:8686/readyz
```

By default Boyan stores data in `./.state/boyan` with this layout:

```text
.state/boyan/
├── badger/
├── bleve/
└── migration/
```

## Configuration

Boyan loads configuration from:

1. built-in defaults
2. `--config <path>` YAML
3. `BOYAN_*` environment variables
4. command flags for `--state-dir` and `--listen`

See [boyan.example.yaml](boyan.example.yaml) and [docs/OPERATIONS.md](docs/OPERATIONS.md).

## APIs

Boyan exposes two external surfaces with separate responsibilities:

- HTTP ingest for durable writes
- MCP for memory operations

See [docs/API.md](docs/API.md).

## Balda migration

Boyan includes an offline migration command for current Balda canonical state:

```bash
boyan migrate balda --source /path/to/balda/session-memory --state-dir /path/to/boyan-state
```

The migration is restart-safe, non-destructive, and writes content-free
receipts under `migration/`. See [docs/MIGRATION.md](docs/MIGRATION.md).

## Repository guides

- [ARCHITECTURE.md](ARCHITECTURE.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [RELEASING.md](RELEASING.md)

## License

MIT. See [LICENSE](LICENSE).
