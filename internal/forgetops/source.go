package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) ForgetSource(
	ctx context.Context,
	command memory.ForgetSourceCommand,
) (memory.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	request, err := a.sourceEraseRequest(ctx, command)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	journal, err := a.store.RunScopeErase(ctx, request)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	return completedEraseOutcome(journal)
}

func (a *CanonicalForgetAdapter) sourceEraseRequest(
	ctx context.Context,
	command memory.ForgetSourceCommand,
) (memory.ScopeEraseRequest, error) {
	operationID, err := sourceForgetOperationID(command)
	if err != nil {
		return memory.ScopeEraseRequest{}, err
	}

	request := memory.ScopeEraseRequest{
		Kind: memory.ForgetKindSource, SourceRef: command.Source, Scope: command.Source.Scope,
		OperationID: operationID, ForgottenAt: command.ForgottenAt,
		Fingerprint: memory.CanonicalForgetFingerprint(
			operationID, command.Source.Scope, memory.ForgetKindSource, command.Source, "",
		),
	}

	journal, found, err := a.store.LoadScopeErase(ctx, request.Scope, operationID)
	if err != nil {
		return memory.ScopeEraseRequest{}, err
	}

	if found {
		if err := validateSourceEraseJournal(journal, request); err != nil {
			return memory.ScopeEraseRequest{}, err
		}

		// Migrated source resolution needs payload that this operation may have
		// already erased. Only its matching durable journal supplies retry identity.
		request.SourceID = journal.SourceID
		request.ExpectedScopeVersion = journal.BaselineVersion
		request.ForgottenAt = journal.ForgottenAt

		return request, nil
	}

	request.SourceID, err = a.store.CanonicalSourceID(ctx, command.Source.Scope, command.Source)
	if err != nil {
		return memory.ScopeEraseRequest{}, err
	}

	state, err := a.store.LoadScopeState(ctx, command.Source.Scope)
	if err != nil {
		return memory.ScopeEraseRequest{}, err
	}

	request.ExpectedScopeVersion = state.Version

	return request, nil
}

func validateSourceEraseJournal(journal memory.ScopeEraseJournal, request memory.ScopeEraseRequest) error {
	if err := journal.Validate(); err != nil {
		return memory.PermanentError(memory.CodeStoreFailure, "invalid scope erase journal", nil)
	}

	if journal.Scope != request.Scope || journal.OperationID != request.OperationID ||
		journal.SourceRef != request.SourceRef || journal.ForgetKind() != memory.ForgetKindSource ||
		journal.Fingerprint != request.Fingerprint {
		return memory.PermanentError(memory.CodeConflict, "scope erase fingerprint or target differs", nil)
	}

	return nil
}
