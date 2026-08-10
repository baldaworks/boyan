package app

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const maxCanonicalReadRecords = 256

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return invalidDerived("context is required")
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return memory.RetryableError(memory.CodeTimeout, "derived memory operation timed out", nil)
		}
		return memory.RetryableError(memory.CodeTimeout, "derived memory operation was canceled", nil)
	}
	return nil
}

func turnTextExceeds(turn memory.Turn, limit int) bool {
	remaining := limit
	for _, message := range turn.Messages {
		if len(message.Text) > remaining {
			return true
		}
		remaining -= len(message.Text)
	}
	return false
}

func validateCommittedOutcome(request memory.CommitRequest, outcome memory.OperationOutcome) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	if outcome.OperationID != request.OperationID || outcome.Stage != request.Stage || outcome.Scope != request.Scope {
		return memory.PermanentError(memory.CodeScopeViolation, "canonical commit outcome does not match the operation", nil)
	}
	if outcome.ScopeVersion != request.ExpectedScopeVersion+1 {
		return memory.PermanentError(memory.CodeConflict, "canonical commit returned an unexpected scope version", nil)
	}
	if len(outcome.Revisions) != len(request.Atoms)+len(request.Scenarios)+len(request.Profiles) {
		return invalidDerived("canonical commit outcome revision count does not match the request")
	}
	want := make(map[memory.RevisionRef]struct{}, len(outcome.Revisions))
	for _, atom := range request.Atoms {
		want[memory.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}] = struct{}{}
	}
	for _, scenario := range request.Scenarios {
		want[memory.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}] = struct{}{}
	}
	for _, profile := range request.Profiles {
		want[memory.RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID}] = struct{}{}
	}
	for _, ref := range outcome.Revisions {
		if _, ok := want[ref]; !ok {
			return invalidDerived("canonical commit outcome contains an unknown revision")
		}
		delete(want, ref)
	}
	return nil
}

func normalizeProjectionViewRequest(request memory.ProjectionViewRequest) (memory.ProjectionViewRequest, error) {
	if err := request.Scope.Validate(); err != nil {
		return memory.ProjectionViewRequest{}, err
	}
	if request.ActiveAfterItemID != "" && !isCanonicalID(request.ActiveAfterItemID) {
		return memory.ProjectionViewRequest{}, invalidDerived("projection active cursor is invalid")
	}
	out := request
	if out.ActiveLimit == 0 {
		out.ActiveLimit = 128
	}
	if out.ActiveOnly {
		out.RecentLimit = 0
		out.ChangedLimit = 0
	} else if out.RecentLimit == 0 {
		out.RecentLimit = 64
	}
	if !out.ActiveOnly && out.ChangedLimit == 0 {
		out.ChangedLimit = 64
	}
	for _, limit := range []uint32{out.ActiveLimit, out.RecentLimit, out.ChangedLimit} {
		if limit > maxCanonicalReadRecords {
			return memory.ProjectionViewRequest{}, limitExceeded("projection view limit exceeds the canonical read bound")
		}
	}
	return out, nil
}

func validateEvidenceRefs(evidence []memory.EvidenceRef) error {
	seen := make(map[memory.EvidenceRef]struct{}, len(evidence))
	for _, ref := range evidence {
		if err := ref.Validate(); err != nil {
			return err
		}
		if _, exists := seen[ref]; exists {
			return invalidDerived("memory evidence contains a duplicate reference")
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func normalizeDerivedText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func newError(code memory.ErrorCode, class memory.ErrorClass, message string, cause error) error {
	return &memory.Error{Code: code, Class: class, Message: strings.TrimSpace(message), Cause: cause}
}

func invalidDerived(message string) error {
	return memory.PermanentError(memory.CodeInvalidDerived, message, nil)
}

func limitExceeded(message string) error {
	return memory.PermanentError(memory.CodeLimitExceeded, message, nil)
}

func isCanonicalID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\t")
}
