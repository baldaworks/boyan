package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"strings"

	"github.com/baldaworks/boyan"
)

const maxCanonicalReadRecords = 256

func cloneTurn(turn boyan.Turn) boyan.Turn {
	turn.Messages = append([]boyan.Message(nil), turn.Messages...)
	return turn
}

func scopeViewFromSnapshot(snapshot boyan.ScopeSnapshot) boyan.ScopeView {
	return boyan.ScopeView{
		SchemaVersion: boyan.DerivedSchemaVersionV1,
		Scope:         snapshot.Scope,
		Version:       snapshot.Version,
		Atoms:         activeAtoms(snapshot.Atoms),
		Scenarios:     activeScenarios(snapshot.Scenarios),
		Profiles:      activeProfiles(snapshot.Profiles),
	}
}

func cloneScopeSnapshot(snapshot boyan.ScopeSnapshot) boyan.ScopeSnapshot {
	snapshot.Sources = cloneSources(snapshot.Sources)
	snapshot.Atoms = cloneAtoms(snapshot.Atoms)
	snapshot.Scenarios = cloneScenarios(snapshot.Scenarios)
	snapshot.Profiles = cloneProfiles(snapshot.Profiles)
	return snapshot
}

func cloneOperationOutcome(outcome boyan.OperationOutcome) boyan.OperationOutcome {
	outcome.Revisions = append([]boyan.RevisionRef(nil), outcome.Revisions...)
	return outcome
}

func cloneCommitRequest(request boyan.CommitRequest) boyan.CommitRequest {
	request.Sources = cloneSources(request.Sources)
	request.Atoms = cloneAtoms(request.Atoms)
	request.Scenarios = cloneScenarios(request.Scenarios)
	request.Profiles = cloneProfiles(request.Profiles)
	request.Transitions = append([]boyan.RevisionTransition(nil), request.Transitions...)
	return request
}

func cloneSources(sources []boyan.SourceRecord) []boyan.SourceRecord {
	cloned := append([]boyan.SourceRecord(nil), sources...)
	for index := range cloned {
		if cloned[index].Turn != nil {
			turn := cloneTurn(*cloned[index].Turn)
			cloned[index].Turn = &turn
		}
		if cloned[index].ForgottenAt != nil {
			forgottenAt := *cloned[index].ForgottenAt
			cloned[index].ForgottenAt = &forgottenAt
		}
	}
	return cloned
}

func cloneAtoms(atoms []boyan.Atom) []boyan.Atom {
	cloned := append([]boyan.Atom(nil), atoms...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
		if cloned[index].RelatedRevision != nil {
			ref := *cloned[index].RelatedRevision
			cloned[index].RelatedRevision = &ref
		}
	}
	return cloned
}

func activeAtoms(atoms []boyan.Atom) []boyan.Atom {
	active := make([]boyan.Atom, 0, len(atoms))
	for _, atom := range atoms {
		if atom.Meta.State == boyan.RevisionStateActive {
			active = append(active, atom)
		}
	}
	return cloneAtoms(active)
}

func cloneScenarios(scenarios []boyan.Scenario) []boyan.Scenario {
	cloned := append([]boyan.Scenario(nil), scenarios...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
	}
	return cloned
}

func activeScenarios(scenarios []boyan.Scenario) []boyan.Scenario {
	active := make([]boyan.Scenario, 0, len(scenarios))
	for _, scenario := range scenarios {
		if scenario.Meta.State == boyan.RevisionStateActive {
			active = append(active, scenario)
		}
	}
	return cloneScenarios(active)
}

func cloneProfiles(profiles []boyan.Profile) []boyan.Profile {
	cloned := append([]boyan.Profile(nil), profiles...)
	for index := range cloned {
		cloned[index].Meta = cloneRevisionMeta(cloned[index].Meta)
	}
	return cloned
}

func activeProfiles(profiles []boyan.Profile) []boyan.Profile {
	active := make([]boyan.Profile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Meta.State == boyan.RevisionStateActive {
			active = append(active, profile)
		}
	}
	return cloneProfiles(active)
}

func cloneRevisionMeta(meta boyan.RevisionMeta) boyan.RevisionMeta {
	meta.Provenance.RawSources = append([]boyan.SourceRef(nil), meta.Provenance.RawSources...)
	meta.Provenance.ParentRevisions = append([]boyan.RevisionRef(nil), meta.Provenance.ParentRevisions...)
	if meta.Supersedes != nil {
		ref := *meta.Supersedes
		meta.Supersedes = &ref
	}
	return meta
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return invalidDerived("context is required")
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return boyan.RetryableError(boyan.CodeTimeout, "derived memory operation timed out", nil)
		}
		return boyan.RetryableError(boyan.CodeTimeout, "derived memory operation was canceled", nil)
	}
	return nil
}

func turnTextExceeds(turn boyan.Turn, limit int) bool {
	remaining := limit
	for _, message := range turn.Messages {
		if len(message.Text) > remaining {
			return true
		}
		remaining -= len(message.Text)
	}
	return false
}

func validateCommittedOutcome(request boyan.CommitRequest, outcome boyan.OperationOutcome) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	if outcome.OperationID != request.OperationID || outcome.Stage != request.Stage || outcome.Scope != request.Scope {
		return boyan.PermanentError(boyan.CodeScopeViolation, "canonical commit outcome does not match the operation", nil)
	}
	if outcome.ScopeVersion != request.ExpectedScopeVersion+1 {
		return boyan.PermanentError(boyan.CodeConflict, "canonical commit returned an unexpected scope version", nil)
	}
	if len(outcome.Revisions) != len(request.Atoms)+len(request.Scenarios)+len(request.Profiles) {
		return invalidDerived("canonical commit outcome revision count does not match the request")
	}
	want := make(map[boyan.RevisionRef]struct{}, len(outcome.Revisions))
	for _, atom := range request.Atoms {
		want[boyan.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}] = struct{}{}
	}
	for _, scenario := range request.Scenarios {
		want[boyan.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}] = struct{}{}
	}
	for _, profile := range request.Profiles {
		want[boyan.RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID}] = struct{}{}
	}
	for _, ref := range outcome.Revisions {
		if _, ok := want[ref]; !ok {
			return invalidDerived("canonical commit outcome contains an unknown revision")
		}
		delete(want, ref)
	}
	return nil
}

func normalizeProjectionViewRequest(request boyan.ProjectionViewRequest) (boyan.ProjectionViewRequest, error) {
	if err := request.Scope.Validate(); err != nil {
		return boyan.ProjectionViewRequest{}, err
	}
	if request.ActiveAfterItemID != "" && !isCanonicalID(request.ActiveAfterItemID) {
		return boyan.ProjectionViewRequest{}, invalidDerived("projection active cursor is invalid")
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
			return boyan.ProjectionViewRequest{}, limitExceeded("projection view limit exceeds the canonical read bound")
		}
	}
	return out, nil
}

func validateEvidenceRefs(evidence []boyan.EvidenceRef) error {
	seen := make(map[boyan.EvidenceRef]struct{}, len(evidence))
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

func reconciliationID(kind string, parts ...string) string {
	hash := sha256.New()
	writeHashPart(hash, boyan.MemorySchemaVersionV2)
	writeHashPart(hash, kind)
	for _, part := range parts {
		writeHashPart(hash, part)
	}
	return "session-memory:v2:" + kind + ":" + hex.EncodeToString(hash.Sum(nil))
}

func writeHashPart(dst hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = dst.Write(size[:])
	_, _ = dst.Write([]byte(value))
}

func normalizeDerivedText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func newError(code boyan.ErrorCode, class boyan.ErrorClass, message string, cause error) error {
	return &boyan.Error{Code: code, Class: class, Message: strings.TrimSpace(message), Cause: cause}
}

func invalidDerived(message string) error {
	return boyan.PermanentError(boyan.CodeInvalidDerived, message, nil)
}

func limitExceeded(message string) error {
	return boyan.PermanentError(boyan.CodeLimitExceeded, message, nil)
}

func isCanonicalID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\t")
}
