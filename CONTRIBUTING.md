# Contributing

This repository uses Beads for durable task tracking and is currently being
advanced through the Prism story in `boyan-vqzg`.

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
go run ./cmd/boyan migrate balda --help
go list -deps ./...
```

If `golangci-lint` is available in your environment, run it before handoff:

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
- `docs/MIGRATION.md`
- `docs/OPERATIONS.md`
- `boyan.example.yaml`

Documentation should describe only currently supported local behavior. Do not
claim hosted, multi-tenant, or container features that do not exist.

## Repository rules

- Use Beads for durable work tracking.
- Keep Boyan independent from `github.com/normahq/balda`.
- Do not commit or push unless explicitly authorized.
- Keep `pack/callee/**` untouched.
