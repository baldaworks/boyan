# Contributing

This repository uses Beads for durable task tracking.

## Prerequisites

- Go 1.26.5
- `bd` for task tracking

## Workflow

1. Inspect ready work:

```bash
bd ready --json
```

2. Read the task:

```bash
bd show <id> --json
```

3. Claim it:

```bash
bd update <id> --claim --json
```

4. Make the smallest coherent change for that task.

5. Run the local checks that fit the change scope.

## Local verification

Minimum local checks for standalone changes:

```bash
go test ./...
go test -race ./...
go build ./...
git diff --check
```

Useful focused checks:

```bash
go test ./cmd/boyan
go run ./cmd/boyan --help
go run ./cmd/boyan serve --help
go list -deps ./...
```

Run the local lint command before handoff:

The repository pins `golangci-lint` as a Go tool dependency in `go.mod`. On a
fresh module-aware checkout, the `go` command will download the pinned tool on
first use.

```bash
go tool golangci-lint run ./...
```

## Documentation and operator changes

When you change the standalone surface, update the matching docs in the same
change:

- `README.md`
- `ARCHITECTURE.md`
- `RELEASING.md`
- `docs/API.md`
- `docs/OPERATIONS.md`
- `boyan.example.yaml`

Documentation should describe only currently supported local behavior. Do not
claim hosted, multi-tenant, or container features that do not exist.

## Repository rules

- Use Beads for durable work tracking.
- Keep Boyan standalone and self-contained.
- Do not commit or push unless explicitly authorized.
- Keep `pack/callee/**` untouched.
