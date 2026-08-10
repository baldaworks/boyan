// Package memory defines the standalone Boyan wire, canonical, derived, and
// ordered-ingest contracts that are safe to reuse from other Go modules.
//
// The standalone service composes higher-level application logic under
// `internal/`, while `pkg/memory` stays focused on public transport-neutral
// and adapter-neutral contracts.
package memory
