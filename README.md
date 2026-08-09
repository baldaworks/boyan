# Boyan

Boyan is a standalone local service for grounded temporal memory for AI agents.

This repository is in bootstrap state. The local Beads workspace and Prism
story graph have been transferred, and the initial repository scaffold is in
place. Remote repository creation and clone-recovery verification are still
pending.

## Current scope

- Go module: `github.com/baldaworks/boyan`
- Binary entrypoint: `./cmd/boyan`
- Task tracking: Beads (`bd`)
- Status: compileable scaffold only; product implementation follows in later
  tasks

## Local usage

```bash
go run ./cmd/boyan
```

Expected output:

```text
boyan bootstrap scaffold
```

## Development

- Follow [AGENTS.md](AGENTS.md) for repository-specific rules.
- Use Beads for durable task tracking.
- Keep Boyan independent from `github.com/normahq/balda`.

## License

MIT. See [LICENSE](LICENSE).
