package canonicalcore_test

import (
	"context"
	"sort"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func testProcessorDerivation() DerivationRef {
	return DerivationRef{Pipeline: "pipeline-v1", Policy: "policy-v1", Prompt: "prompt-v1", Model: "model-v1"}
}

type canonicalExtractorFunc func(context.Context, CanonicalExtractionRequest) ([]SemanticCandidate, error)

func (f canonicalExtractorFunc) ExtractCanonicalSemantics(
	ctx context.Context,
	request CanonicalExtractionRequest,
) (
	[]SemanticCandidate,
	error,
) {
	return f(ctx, request)
}

type processorTestStore struct {
	state    ScopeState
	active   []ActiveCanonicalMemory
	mutation CanonicalMutation
}

func (s *processorTestStore) LoadScopeState(_ context.Context, scope Scope) (ScopeState, error) {
	if s.state.Scope == (Scope{}) {
		s.state = ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope}
	}

	return s.state, nil
}

func (s *processorTestStore) ApplyCanonicalMutation(
	_ context.Context,
	mutation CanonicalMutation,
) (
	CanonicalMutationOutcome,
	error,
) {
	if err := mutation.Validate(); err != nil {
		return CanonicalMutationOutcome{}, err
	}

	if mutation.ExpectedScopeVersion != s.state.Version {
		return CanonicalMutationOutcome{}, PermanentError(CodeConflict, "test scope version changed", nil)
	}

	s.mutation = mutation
	s.state.Version++
	s.state.ChangeSeq++

	for _, revision := range mutation.Revisions {
		item := MemoryItem{ItemID: revision.ItemID, Scope: mutation.Scope, Kind: MemoryKindState}
		for _, existing := range s.active {
			if existing.Item.ItemID == revision.ItemID {
				item = existing.Item

				break
			}
		}

		for _, created := range mutation.Items {
			if created.ItemID == revision.ItemID {
				item = created

				break
			}
		}

		s.active = []ActiveCanonicalMemory{
			{
				Item:       item,
				RevisionID: revision.RevisionID,
				Revision:   revision.Revision,
				Evidence:   revision.Evidence,
			},
		}
	}

	outcome := CanonicalMutationOutcome{ScopeVersion: s.state.Version, ChangeSeq: s.state.ChangeSeq}
	for _, revision := range mutation.Revisions {
		outcome.RevisionIDs = append(outcome.RevisionIDs, revision.RevisionID)
	}

	return outcome, nil
}

func (s *processorTestStore) ScanScopeChanges(context.Context, Scope, uint64, uint32) ([]ScopeChange, error) {
	return nil, nil
}

func (s *processorTestStore) LoadCanonicalRevisions(
	context.Context,
	CanonicalRevisionReadRequest,
) (
	[]MemoryRevision,
	error,
) {
	return nil, nil
}

func (s *processorTestStore) ScanActiveHeads(context.Context, ActiveHeadScanRequest) ([]ItemHead, error) {
	return nil, nil
}

func (s *processorTestStore) ScanActiveMemory(_ context.Context,
	request ActiveMemoryScanRequest) ([]ActiveCanonicalMemory,
	error) {
	ordered := append([]ActiveCanonicalMemory(nil), s.active...)
	sort.Slice(ordered,
		func(i,
			j int) bool {
			a,
				b := ordered[i].Item.ItemID,
				ordered[j].Item.ItemID

			return len(a) < len(b) || len(a) == len(b) && a < b
		})

	page := make([]ActiveCanonicalMemory, 0, request.Limit)

	for _, active := range ordered {
		id := active.Item.ItemID
		if len(id) < len(request.AfterItemID) || len(id) == len(request.AfterItemID) && id <= request.AfterItemID {
			continue
		}

		if len(page) == int(request.Limit) {
			break
		}

		page = append(page, active)
	}

	return page, nil
}

func (s *processorTestStore) ClaimDeliveryOutbox(context.Context, DeliveryClaimRequest) ([]ClaimedDelivery, error) {
	return nil, nil
}

func (s *processorTestStore) SettleDeliveryOutbox(context.Context, DeliverySettlementRequest) error {
	return nil
}
