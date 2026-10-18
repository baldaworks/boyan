package canonicalsql

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Catches stale compatibility publication and Trace-style denial errors at
// the fresh barrier. The retained writes occur between real native snapshots.
func TestCompatibilityFinalAuthorityContract(t *testing.T) {
	for _, kind := range []string{"unchanged", "source-denial", "revision-denial", "sensitive",
		"revision-sensitive", "missing-source", "missing-revision", "missing-item", "missing-head",
		"scope-change", "head-change", "head-corruption", "raw-content", "canceled", "unprepared",
		"restore-loading", "erase-guard", "missing-message", "missing-source-payload"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runCompatibilityFinalAuthority(t, owner, kind)
			})
		})
	}
}

func runCompatibilityFinalAuthority(t *testing.T, owner *Owner, kind string) {
	t.Helper()
	h := prepareCompatibilityHistory(t, owner)
	graph := traceGraph{captured: readerSnapshot{sources: make(map[string]memory.CanonicalSourceRecord),
		revisions: make(map[string]memory.MemoryRevision)}}

	var (
		payload memory.CanonicalCompatibilityPayload
		found   bool
	)
	if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		var err error

		payload, found, err = loadCompatibilitySnapshot(ctx, tx, h.scope, h.revision.RevisionID, &graph)

		return err
	}); err != nil || !found || payload.Text != "compatibility memory" {
		t.Fatalf("initial snapshot found=%v payload=%+v err=%v", found, payload, err)
	}

	alterReaderBarrier(t, owner, h, kind)
	alterTraceBarrier(t, owner, h, kind)
	alterCompatibilityBarrier(t, owner, h, kind)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if kind == "canceled" {
		cancel()
	}

	got, found, err := owner.publishCompatibility(ctx, graph, payload, found)
	assertCompatibilityPublication(t, kind, payload, got, found, err)
}

func assertCompatibilityPublication(t *testing.T, kind string, previous, got memory.CanonicalCompatibilityPayload,
	found bool, err error) {
	t.Helper()

	if kind == "unchanged" {
		if err != nil || !found || !reflect.DeepEqual(got, previous) {
			t.Fatalf("valid publication=%+v found=%v err=%v", got, found, err)
		}

		return
	}

	if found || !reflect.DeepEqual(got, memory.CanonicalCompatibilityPayload{}) {
		t.Fatalf("obsolete compatibility exposed: %+v found=%v err=%v", got, found, err)
	}

	assertCompatibilityFinalError(t, kind, err)
}

func assertCompatibilityFinalError(t *testing.T, kind string, err error) {
	t.Helper()

	if kind == "canceled" {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation identity lost: %v", err)
		}

		return
	}

	want, class := compatibilityFinalErrorClass(kind)
	if want == "" {
		if err != nil {
			t.Fatalf("denied/missing compatibility must be absent without error: %v", err)
		}

		return
	}

	code, gotClass, ok := memory.ClassifyError(err)
	if !ok || code != want || gotClass != class {
		t.Fatalf("error=%v code/class=%s/%s want=%s/%s", err, code, gotClass, want, class)
	}
}

func compatibilityFinalErrorClass(kind string) (memory.ErrorCode, memory.ErrorClass) {
	switch kind {
	case "head-corruption", "raw-content", "missing-message", "missing-source-payload":
		return memory.CodeStoreFailure, memory.ErrorClassPermanent
	case "scope-change", "head-change", "erase-guard":
		return memory.CodeConflict, memory.ErrorClassRetryable
	case "unprepared", "restore-loading":
		return memory.CodeUnavailable, memory.ErrorClassRetryable
	default:
		return "", ""
	}
}

func prepareCompatibilityHistory(t *testing.T, owner *Owner) privacyHistory {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	compat := memory.CanonicalCompatibilityPayload{SchemaVersion: memory.CanonicalCompatibilitySchemaVersion,
		Kind: memory.DerivedKindProfile, Text: "compatibility memory", LegacyItemID: "legacy-item",
		LegacyRevisionID: "legacy-revision", LegacyOperationID: "legacy-operation"}

	data, err := json.Marshal(compat)
	if err != nil {
		t.Fatal(err)
	}

	h.revision.Payload = privacyRef("compatibility", data)

	h.payloads = append(h.payloads, memory.CanonicalPayload{Ref: h.revision.Payload, Data: data})
	if _, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h)); err != nil {
		t.Fatal(err)
	}

	return h
}

func alterCompatibilityBarrier(t *testing.T, owner *Owner, h privacyHistory, kind string) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		if key, found := compatibilityRemovedKey(h, kind); found {
			return tx.Delete(ctx, key)
		}

		switch kind {
		case "revision-sensitive":
			revision := h.revision
			revision.Sensitivity = memory.SensitivitySensitive
			key, _ := privacyKey(h.scope, Revision, revision.RevisionID)

			return putPrivacyRecord(ctx, tx, key, Revision, revision)
		case "erase-guard":
			return putPrivacyRecord(ctx, tx, Key{Family: EraseGuard}, EraseGuard, map[string]any{"pending": 1})
		default:
			return nil
		}
	}); err != nil {
		t.Fatal(err)
	}

	if kind == "restore-loading" {
		rawExec(t, owner.connection,
			"INSERT INTO "+owner.connection.table("restore_control")+"(singleton,control) VALUES(1,$1)", []byte("loading"))
	}
}

func compatibilityRemovedKey(h privacyHistory, kind string) (Key, bool) {
	key := func(family Family, id string) Key {
		return Key{Family: family, Scope: h.scope, Parts: [][]byte{[]byte(id)}}
	}
	keys := map[string]Key{
		"missing-source":         key(Source, h.source.SourceID),
		"missing-revision":       key(Revision, h.revision.RevisionID),
		"missing-item":           key(Item, h.item.ItemID),
		"missing-head":           key(Head, h.item.ItemID),
		"missing-message":        key(Message, h.messages[0].MessageID),
		"missing-source-payload": {Family: Payload, Parts: [][]byte{[]byte(h.source.Payload.ID)}},
		"unprepared":             {Family: SourcePrivacyReady},
	}
	removed, found := keys[kind]

	return removed, found
}
