# Contributing

This repository is still in bootstrap. Keep changes small, traceable to Beads,
and consistent with the active Prism story.

## Prerequisites

- Go 1.26.5
- `bd` for issue tracking

## Workflow

1. Inspect the active work in Beads:
   `bd ready --json`, `bd show <id>`, `bd update <id> --claim --json`
2. Follow repository instructions in [AGENTS.md](AGENTS.md).
3. Keep Boyan independent from `github.com/normahq/balda`.

## Local verification

```bash
go test ./...
go test -race ./...
go list -deps ./...
go tool golangci-lint run ./...
git -C . diff --check
```

## Notes

- Use Beads for all durable task tracking.
- Do not commit or push unless explicitly authorized.
