# Releasing

Boyan already supports local builds and local distribution. Remote publication,
clean-clone recovery, and the first externally resolvable module revision are
tracked separately by `boyan-vqzg.12`.

This file covers the current local release shape only.

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
./dist/boyan migrate balda --help
```

## Required local gates

Before treating a change as releaseable locally, run:

```bash
go test ./...
go test -race ./...
go build ./...
git diff --check
```

Recommended additional checks:

```bash
go list -deps ./...
go tool golangci-lint run ./...
```

The dependency graph must not introduce `github.com/normahq/balda`.

## Distribution expectations

Current supported distribution modes:

- `go install ./cmd/boyan`
- `go build -o ./dist/boyan ./cmd/boyan`

Current unsupported claims:

- published GitHub release artifacts
- module tags consumable from a remote public repository
- container images
- hosted deployment metadata

Do not document or advertise those until `boyan-vqzg.12` is complete.

## Release checklist for task 8

1. CLI help is coherent for `boyan`, `boyan serve`, and `boyan migrate balda`.
2. `boyan.example.yaml` matches the actual loader keys.
3. README, API, migration, and operations docs match the current binary.
4. Local verification passes.
5. No Balda dependency is introduced.
