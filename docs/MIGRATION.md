# Migration

Boyan includes one offline migration command for current Balda canonical state.

## Command

```bash
boyan migrate balda --source /path/to/balda/session-memory --state-dir /path/to/boyan-state
```

## What it does

The command:

1. validates source and target paths
2. verifies the source canonical store
3. exports a logical snapshot into `migration/`
4. imports it into the Boyan canonical store
5. verifies imported record count and digest
6. rebuilds the Bleve projection
7. writes a content-free migration receipt

## Target layout

The target state root is expected to contain:

```text
<state_dir>/
├── badger/
├── bleve/
└── migration/
```

The canonical store is opened at `<state_dir>/badger`.

## Restart safety

The migration keeps receipts and the logical export under
`<state_dir>/migration/`.

If the command is interrupted, rerun the same command against the same source
and target. The receipt and export artifact are used to resume safely.

## Safety rules

The command fails closed when:

- the source path is missing, aliased, or symlinked
- the source store is locked or invalid
- the target path overlaps the source
- the target is non-empty without the matching migration state
- the export artifact is missing or corrupted
- record count or digest verification fails

The source remains authoritative until you explicitly cut over to Boyan.

## Serving after migration

`boyan serve` checks migration state at startup and readiness time. An
incomplete migration receipt blocks service readiness until the migration is
completed or the invalid migration state is cleared.
