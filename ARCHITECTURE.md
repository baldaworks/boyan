# Architecture

This file records the bootstrap architecture boundaries for Boyan.

## Current boundaries

- `cmd/boyan` is the only executable entrypoint in the scaffold.
- Boyan owns its module path: `github.com/baldaworks/boyan`.
- Boyan must remain independent from `github.com/normahq/balda`.
- Future HTTP ingest and MCP surfaces stay separate contracts.
- Future storage ownership remains single-writer canonical state plus a
  rebuildable projection.

## Bootstrap intent

The current repository contains compileable scaffolding only. Product
implementation is intentionally deferred to later tasks in the approved Prism
story.
