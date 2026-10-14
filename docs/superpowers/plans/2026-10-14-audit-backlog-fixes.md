# Audit Backlog Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix every open audit bug and preserve each reproduced failure as a regression test.

**Architecture:** Keep fixes at the narrow public boundary that owns each contract. Share validation and cancellation helpers only inside their existing package, and extend durable formats only where replay needs authoritative data.

**Tech Stack:** Go 1.24+, net/http, Cobra, Viper, Badger, Bleve, MCP SDK.

**Spec:** Beads issues `boyan-7oh`, `boyan-kc9`, `boyan-0mx`, `boyan-4jf`, `boyan-r0i`, `boyan-5od`, `boyan-d5b`, `boyan-ocf`, `boyan-zfl`, `boyan-ne1`, `boyan-2ir`, `boyan-dqv`, `boyan-nd3`, `boyan-2by`, and `boyan-q13`.

## Global Constraints

- Use TDD for every behavior change.
- Preserve stable public error codes and redact provider/storage details.
- Keep backward compatibility for persisted records and public zero-value configuration defaults.
- Close Beads issues only after their regression tests and package suites pass.
- Merge to `main`, verify the merged tree with all seven release gates, then push.

## Review Focus

- Redirect policy for custom and default HTTP clients.
- Compatibility of legacy forget outcomes without a persisted timestamp.
- Restore fencing after empty or malformed streams.
- Close-versus-operation and initialization-versus-cancellation races.
- Cross-record integrity validation without rejecting valid incremental history.

---

### Task 1: Typed HTTP client contracts

**Files:** Modify `client/http/*.go`, `pkg/memory/ingest_http_validation.go`; test `client/http/*_test.go`.

**Interfaces:** Produces transport error classification, origin-bound redirect policy, and closed wire-enum validation.

- [ ] Add failing tests for deadlines, redirects, and unknown wire codes.
- [ ] Run the focused tests and confirm all fail for the audited reasons.
- [ ] Implement the minimal client and validation changes.
- [ ] Run `go test ./client/http ./pkg/memory -count=1`.
- [ ] Commit.

### Task 2: Configuration, constructor, and CLI validation

**Files:** Modify `internal/standalonecfg/*.go`, `pkg/boyan/config.go`, `pkg/boyan/host.go`, `cmd/boyan/serve_command.go`; corresponding tests.

**Interfaces:** Produces strict YAML decoding, negative-bound rejection, early constructor cancellation, and no positional arguments.

- [ ] Add failing tests for every audited input.
- [ ] Run focused tests and confirm expected failures.
- [ ] Implement validation at the owning boundaries.
- [ ] Run `go test ./internal/standalonecfg ./pkg/boyan ./cmd/boyan -count=1`.
- [ ] Commit.

### Task 3: Canonical maintenance safety

**Files:** Modify `store/badger/canonical_maintenance_validation.go`, native restore stream code, and maintenance tests.

**Interfaces:** Produces panic-free typed-nil validation and rejection of empty native backups.

- [ ] Add and run failing typed-nil and empty-stream tests.
- [ ] Implement narrow validation and first-frame enforcement.
- [ ] Run `go test ./store/badger -count=1`.
- [ ] Commit.

### Task 4: Bleve lifecycle

**Files:** Modify `index/bleve` lifecycle/search/generation code and tests.

**Interfaces:** Produces an atomic closed state consumed by public projection operations.

- [ ] Add and run failing post-Close tests.
- [ ] Implement closed-state admission checks.
- [ ] Run normal and race tests for `./index/bleve`.
- [ ] Commit.

### Task 5: Durable forget acknowledgement

**Files:** Modify `pkg/memory/forget.go`, canonical forget persistence paths, `mcp/forget_*.go`, compatibility and MCP tests.

**Interfaces:** Produces an authoritative durable `ForgottenAt` used by MCP output.

- [ ] Add failing immediate and restart replay tests.
- [ ] Add backward-compatible durable timestamp handling and use it in MCP.
- [ ] Run `go test ./pkg/memory ./store/badger ./mcp -count=1`.
- [ ] Commit.

### Task 6: Derivation cancellation

**Files:** Modify `internal/derivation` runtime/invocation code and `internal/structuredderive` common invocation path; corresponding tests.

**Interfaces:** Produces cancelable single-flight initialization and rejection of late success.

- [ ] Add and run failing waiter and late-success tests.
- [ ] Implement context-aware initialization and post-return context checks.
- [ ] Run normal and race tests for both packages.
- [ ] Commit.

### Task 7: Canonical replay integrity

**Files:** Modify Badger integrity and replay lookup paths; add corruption/replay tests.

**Interfaces:** Produces cross-record replay validation and consistent `store_failure` classification.

- [ ] Add and run failing inconsistent and malformed replay tests.
- [ ] Bind outcomes to durable state/change records and wrap stored corruption.
- [ ] Run `go test ./store/badger ./internal/canonicalcore -count=1`.
- [ ] Commit.

### Task 8: Integration and delivery

**Files:** Update documentation only when wire or compatibility behavior changed.

**Interfaces:** Produces the integrated release candidate on `main`.

- [ ] Run the focused aggregate suite and review the complete branch diff.
- [ ] Run a fresh independent whole-branch review and fix material findings with TDD.
- [ ] Merge to `main`.
- [ ] Run all seven release gates on merged `main`.
- [ ] Close every completed Beads issue, sync tracker, push `main`, and verify the remote revision.
