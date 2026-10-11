# Releasing

Boyan already supports local builds and local distribution. This document
describes the current standalone release shape only.

## Local release outputs

The standalone artifact is the `boyan` binary built from `./cmd/boyan`.

Build it locally:

```bash
go build -o ./dist/boyan ./cmd/boyan
```

Validate the command surface:

```bash
./dist/boyan --help
./dist/boyan serve --help
```

## Required local gates

Before treating a change as releaseable locally, run:

```bash
go test ./...
go test -race ./...
go build ./...
go tool golangci-lint run ./...
go tool go-arch-lint check --project-path .
go mod verify
git diff --check
```

Recommended additional checks:

`golangci-lint` is pinned as a Go tool dependency in `go.mod`, so the local
lint path stays versioned with the repository.

```bash
go list -deps ./...
```

The dependency graph must not introduce legacy integration-only dependencies.

## Distribution expectations

Current supported distribution modes:

- `go install ./cmd/boyan`
- `go build -o ./dist/boyan ./cmd/boyan`

Current unsupported claims:

- published GitHub release artifacts
- module tags consumable from a remote public repository
- container images
- hosted deployment metadata

Do not document or advertise those until the repository actually supports
them.

## Release checklist

1. CLI help is coherent for `boyan` and `boyan serve`.
2. `boyan.example.yaml` matches the actual loader keys.
3. README, API, architecture, and operations docs match the current binary.
4. Local verification passes, including lint and architecture checks.
5. Runtime regressions cover MCP streaming, bounded uploads, repeated waits,
   resource release, incremental sync, recovery and generation retention.
6. No legacy integration-only dependency is introduced.
