package standaloneruntime //nolint:testpackage // Exercises the actual standalone adapter against native SQL data.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalops"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func seedSQLBoundary(
	t *testing.T, runtime *Runtime,
) (*canonicalops.CanonicalBoundaryStore, m.Turn, m.IngestOutcome, m.IngestOutcome) {
	t.Helper()
	adapter, _ := configureSQLRuntimePipeline(t, runtime)
	requireNoError(t, runtime.Start(t.Context()), "start: %v")
	turn := sqlRuntimeTurn(t, m.Scope{Key: "runtime:boundary", Kind: m.ScopeKindPersonal})
	native, err := runtime.IngestSequencedTurn(t.Context(), m.SequencedTurn{ProducerID: "producer", ScopeSequence: 1,
		Fingerprint: "turn", Turn: turn})
	requireNoError(t, err, "turn: %v")
	boundary, err := m.NewBoundary(turn.Scope, turn.Conversation, "first", m.BoundaryReasonClose, time.Now().UTC())
	requireNoError(t, err, "boundary: %v")
	outcome, err := runtime.ApplySequencedBoundary(t.Context(), m.SequencedBoundary{
		ProducerID: "producer", ScopeSequence: 2, Fingerprint: "boundary", Boundary: boundary})
	requireNoError(t, err, "nonempty boundary: %v")

	return adapter, turn, native, outcome
}

//nolint:cyclop,funlen,gocognit // One bounded matrix compares legitimate fallback with every native abort class.
func TestSQLRuntimeBoundaryFallbackMatrix(t *testing.T) {
	for _, mode := range []string{"native-absence", "legacy", "superseded",
		"source-denied", "revision-sensitive", "missing-source",
		"payload-missing", "payload-corrupt", "head-corrupt", "unprepared", "loading", "fence", "canceled", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
				runtime, err := New(cfg)
				requireNoError(t, err, "new: %v")
				t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
				adapter, turn, native, boundary := seedSQLBoundary(t, runtime)

				operation := boundary.OperationIDs[0]
				if mode == "native-absence" {
					operation = native.OperationIDs[0]
				}

				record, found, err := runtime.service.store.LoadCanonicalOperation(t.Context(), turn.Scope, operation)
				requireNoError(t, err, "operation: %v")

				if !found || len(record.Outcome.RevisionIDs) != 1 {
					t.Fatal("nonempty operation control missing")
				}

				revisions, err := runtime.service.store.LoadCanonicalRevisions(t.Context(), m.CanonicalRevisionReadRequest{
					Scope: turn.Scope, RevisionIDs: record.Outcome.RevisionIDs})
				requireNoError(t, err, "revision: %v")

				if len(revisions) != 1 {
					t.Fatal("nonempty revision control missing")
				}

				want := m.RevisionRef{ItemID: revisions[0].ItemID, RevisionID: revisions[0].RevisionID}
				if mode == "superseded" {
					supersedeSQLBoundary(t, runtime, turn, want)
				}

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				if mode == "canceled" {
					cancel()
				}

				alterSQLBoundaryReplay(t, cfg, runtime, turn, revisions[0], mode)

				got, err := adapter.LookupOperation(ctx, m.OperationLookup{SchemaVersion: m.DerivedSchemaVersionV1,
					Scope: turn.Scope, OperationID: operation, Stage: m.OperationStageScenarios})
				if sqlBoundaryFallbackAllowed(mode) {
					if err != nil || !got.Found || !reflect.DeepEqual(got.Outcome.Revisions, []m.RevisionRef{want}) {
						t.Fatalf("legitimate historical/canonical identity fallback=%+v %v", got, err)
					}
				} else {
					if err == nil || got.Found || len(got.Outcome.Revisions) != 0 {
						t.Fatalf("technical failure synthesized fallback=%+v %v", got, err)
					}

					if mode == "canceled" && !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation identity lost: %v", err)
					}
				}
			})
		})
	}
}

func sqlBoundaryFallbackAllowed(mode string) bool {
	switch mode {
	case "native-absence", "legacy", "superseded", "source-denied", "revision-sensitive", "missing-source":
		return true
	default:
		return false
	}
}

func supersedeSQLBoundary(t *testing.T, runtime *Runtime, turn m.Turn, prior m.RevisionRef) {
	t.Helper()

	boundary, err := m.NewBoundary(turn.Scope, turn.Conversation, "second", m.BoundaryReasonClose, time.Now().UTC())
	requireNoError(t, err, "later boundary: %v")
	_, err = runtime.ApplySequencedBoundary(t.Context(), m.SequencedBoundary{ProducerID: "producer", ScopeSequence: 3,
		Fingerprint: "later", Boundary: boundary})
	requireNoError(t, err, "later nonempty generation: %v")
	view, err := runtime.service.reader.LoadCanonicalBoundaryView(t.Context(), turn.Scope)
	requireNoError(t, err, "later view: %v")

	if len(view.Snapshot.Scenarios) != 1 || view.Snapshot.Scenarios[0].Meta.RevisionID == prior.RevisionID {
		t.Fatal("historical compatibility control was not superseded")
	}
}

//nolint:cyclop,funlen // Each branch introduces exactly one native fixture fault without faking reader results.
func alterSQLBoundaryReplay(
	t *testing.T, cfg Config, runtime *Runtime, turn m.Turn, revision m.MemoryRevision, mode string,
) {
	t.Helper()

	switch mode {
	case "source-denied":
		err := runtimeStore(t, runtime).DenySource(t.Context(), turn.Scope, turn.SourceID, time.Now().UTC())
		requireNoError(t, err, "deny source: %v")

		return
	case "unavailable":
		requireNoError(t, runtime.service.store.Close(), "close authority: %v")

		return
	case "fence":
		state, err := runtime.service.store.LoadScopeState(t.Context(), turn.Scope)
		requireNoError(t, err, "scope: %v")
		_, err = runtimeStore(t, runtime).BeginScopeErase(t.Context(), m.ScopeEraseRequest{Scope: turn.Scope,
			OperationID: "pending", Fingerprint: "pending", ExpectedScopeVersion: state.Version, ForgottenAt: time.Now().UTC()})
		requireNoError(t, err, "pending fence: %v")

		return
	case "native-absence", "legacy", "superseded", "canceled":
		return
	}

	p := nativeRuntimeProbe(t, cfg)
	if mode == "loading" {
		err := p.exec("INSERT INTO "+p.table("restore_control")+"(singleton,control) VALUES(1,$1)",
			[]byte(`{"version":1,"phase":"loading"}`))
		requireNoError(t, err, "durable loading: %v")

		return
	}

	err := p.connection.Update(t.Context(), func(tx *canonicalsql.Tx) error {
		key := canonicalsql.Key{Scope: turn.Scope}

		var value any

		switch mode {
		case "missing-source":
			key.Family, key.Parts = canonicalsql.Source, [][]byte{[]byte(turn.SourceID)}

			return tx.Delete(t.Context(), key)
		case "unprepared":
			return tx.Delete(t.Context(), canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})
		case "payload-missing", "payload-corrupt":
			key = canonicalsql.Key{Family: canonicalsql.Payload, Parts: [][]byte{[]byte(revision.Payload.ID)}}
			if mode == "payload-missing" {
				return tx.Delete(t.Context(), key)
			}

			value = []byte("corrupt compatibility")
		case "revision-sensitive":
			key.Family, key.Parts = canonicalsql.Revision, [][]byte{[]byte(revision.RevisionID)}
			revision.Sensitivity = m.SensitivitySensitive
			value = revision
		case "head-corrupt":
			key.Family, key.Parts = canonicalsql.Head, [][]byte{[]byte(revision.ItemID)}
			value = m.ItemHead{ItemID: "other", RevisionID: revision.RevisionID}
		}

		data, err := canonicalsql.Encode(key.Family, value)
		if err != nil {
			return err
		}

		return tx.Put(t.Context(), key, data)
	})
	requireNoError(t, err, "raw fixture transition: %v")
}
