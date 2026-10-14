package badger

import (
	"encoding/json"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func validateCanonicalOutcomeBinding(
	txn *badger.Txn,
	scope memory.Scope,
	operationID string,
	outcome memory.CanonicalMutationOutcome,
) error {
	changeKey, err := badgerScopeChangeKey(scope, outcome.ChangeSeq)
	if err != nil {
		return invalidStoredCanonicalOutcome(err)
	}

	var change memory.ScopeChange
	if err := getCanonicalRecord(txn, changeKey, badgerRecordChange, &change); err != nil {
		return invalidStoredCanonicalOutcome(err)
	}

	if err := change.Validate(); err != nil {
		return invalidStoredCanonicalOutcome(err)
	}

	scopeVersion := change.ScopeVersion
	if scopeVersion == 0 {
		// Legacy change records predate an explicit scope-version binding. The
		// canonical writers advanced both counters together, so their values match.
		scopeVersion = change.Sequence
	}
	if change.Sequence != outcome.ChangeSeq || scopeVersion != outcome.ScopeVersion ||
		change.OperationID != operationID || !slices.Equal(change.RevisionIDs, outcome.RevisionIDs) {
		return invalidStoredCanonicalOutcome(nil)
	}

	scopeKey, err := badgerScopeKey(scope)
	if err != nil {
		return invalidStoredCanonicalOutcome(err)
	}
	state, err := loadCanonicalOperationScopeState(txn, scope, scopeKey)
	if err != nil {
		return invalidStoredCanonicalOutcome(err)
	}
	if state.Version < outcome.ScopeVersion || state.ChangeSeq < outcome.ChangeSeq {
		return invalidStoredCanonicalOutcome(nil)
	}

	return nil
}

func validateStoredCanonicalOutcomeBindings(
	txn *badger.Txn,
	components []string,
	envelope memory.RecordEnvelope,
) error {
	if envelope.RecordType != badgerRecordOperation {
		return nil
	}
	if len(components) != canonicalRecordComponents {
		return invalidStoredCanonicalOutcome(nil)
	}

	var stored badgerCanonicalOperation
	if err := json.Unmarshal(envelope.Payload, &stored); err != nil {
		return invalidStoredCanonicalOutcome(err)
	}
	record := memory.CanonicalOperationRecord{Fingerprint: stored.Fingerprint, Outcome: stored.Outcome}
	if err := record.Validate(); err != nil {
		return invalidStoredCanonicalOutcome(err)
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}
	return validateCanonicalOutcomeBinding(txn, scope, components[3], stored.Outcome)
}

func invalidStoredCanonicalOutcome(err error) error {
	return memory.PermanentError(
		memory.CodeStoreFailure,
		"stored canonical operation outcome is invalid",
		err,
	)
}
