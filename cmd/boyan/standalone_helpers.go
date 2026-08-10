package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func compatibilityPayloadRef(kind, identity string, payload memory.CanonicalCompatibilityPayload) (memory.CanonicalPayload, memory.PayloadRef, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return memory.CanonicalPayload{}, memory.PayloadRef{}, fmt.Errorf("encode %s compatibility payload: %w", kind, err)
	}
	return canonicalPayload("compatibility:"+kind+":"+identity, data)
}

func canonicalPayload(identity string, data []byte) (memory.CanonicalPayload, memory.PayloadRef, error) {
	if len(data) == 0 {
		return memory.CanonicalPayload{}, memory.PayloadRef{}, fmt.Errorf("canonical payload data is empty")
	}
	digest := sha256.Sum256(data)
	ref := memory.PayloadRef{
		ID:       stableStandaloneID("payload", identity),
		Digest:   hex.EncodeToString(digest[:]),
		ByteSize: uint32(len(data)),
	}
	payload := memory.CanonicalPayload{Ref: ref, Data: append([]byte(nil), data...)}
	if err := payload.Validate(); err != nil {
		return memory.CanonicalPayload{}, memory.PayloadRef{}, err
	}
	return payload, ref, nil
}

func collectOperationRevisions(request memory.CommitRequest) []memory.RevisionRef {
	revisions := make([]memory.RevisionRef, 0, len(request.Atoms)+len(request.Scenarios)+len(request.Profiles))
	for _, atom := range request.Atoms {
		revisions = append(revisions, memory.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID})
	}
	for _, scenario := range request.Scenarios {
		revisions = append(revisions, memory.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID})
	}
	for _, profile := range request.Profiles {
		revisions = append(revisions, memory.RevisionRef{ItemID: profile.Meta.ItemID, RevisionID: profile.Meta.RevisionID})
	}
	return revisions
}

func operationCommittedAt(request memory.CommitRequest) time.Time {
	for _, scenario := range request.Scenarios {
		if !scenario.Meta.CreatedAt.IsZero() {
			return scenario.Meta.CreatedAt
		}
	}
	for _, profile := range request.Profiles {
		if !profile.Meta.CreatedAt.IsZero() {
			return profile.Meta.CreatedAt
		}
	}
	for _, atom := range request.Atoms {
		if !atom.Meta.CreatedAt.IsZero() {
			return atom.Meta.CreatedAt
		}
	}
	return time.Now().UTC()
}

func stableStandaloneFingerprint(kind string, value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return stableStandaloneID(kind, "marshal-error")
	}
	return stableStandaloneID(kind, string(data))
}

func stableStandaloneID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte{0})
		hash.Write([]byte(part))
	}
	return "boyan-standalone:" + hex.EncodeToString(hash.Sum(nil))
}

func evidenceKey(ref memory.EvidenceRef) string {
	return strings.Join([]string{
		ref.SourceID,
		ref.MessageID,
		string(ref.Role),
		fmt.Sprintf("%d", ref.StartByte),
		fmt.Sprintf("%d", ref.EndByte),
		string(ref.AssertionMode),
		ref.TextDigest,
	}, ":")
}

func appendUniqueRefs(values []memory.RevisionRef, ref memory.RevisionRef) []memory.RevisionRef {
	for _, existing := range values {
		if existing == ref {
			return values
		}
	}
	return append(values, ref)
}

func appendUniqueStrings(values []string, next []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range next {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func revisionIDs(refs []memory.RevisionRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.RevisionID)
	}
	return ids
}

func cloneOptionalRef(ref *memory.RevisionRef) *memory.RevisionRef {
	if ref == nil {
		return nil
	}
	copyOf := *ref
	return &copyOf
}

func derivedMemoryKey(itemID string) memory.MemoryKey {
	return memory.MemoryKey("derived:" + itemID)
}
