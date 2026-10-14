package badger //nolint:testpackage // Exercises source authority at real trace read boundaries.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestTraceWholeSourcePrivacyDenial(t *testing.T) {
	for _, state := range []string{sourcePrivacySensitive, sourcePrivacyUnknown, "missing", "malformed"} {
		t.Run(state, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, mutation.Sources[0].SourceID)

			err := store.db.Update(func(txn *badger.Txn) error {
				if state == "missing" {
					return txn.Delete(key)
				}

				record, _, e := loadSourcePrivacy(txn, mutation.Sources[0])
				if e != nil {
					return e
				}

				record.State = state

				return putCanonicalRecord(txn, key, badgerRecordSourcePrivacy, record)
			})
			if err != nil {
				t.Fatal(err)
			}

			reader, _ := NewCanonicalReader(store)
			response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))

			want := m.CodeForgotten
			if state == "malformed" {
				want = m.CodeStoreFailure
			}

			assertTracePrivacyCode(t, response, err, want)

			encoded, _ := json.Marshal(response)
			if strings.Contains(string(encoded), "protected memory") {
				t.Fatal("partial content escaped")
			}
		})
	}
}

func TestTraceFinalPrivacyBarrierInterleavings(t *testing.T) {
	for _, action := range []string{"sensitive", "forget", "erase", "cancel", "version", "binding"} {
		t.Run(action, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			reader, _ := NewCanonicalReader(store)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			paused, resume := make(chan struct{}), make(chan struct{})
			reads := 0
			reader.beforeTracePayloadRead = func() {
				reads++
				if reads == 3 {
					close(paused)
					<-resume
				}
			}

			type result struct {
				response m.TraceResponse
				err      error
			}

			done := make(chan result, 1)

			go func() {
				response, err := reader.Trace(ctx, privacyTraceRequest(mutation))
				done <- result{response, err}
			}()

			<-paused
			applyTraceInterleaving(t, store, mutation, action, cancel)
			close(resume)

			got := <-done
			assertTraceChanged(t, got.response, got.err, action)
		})
	}
}

func applyTraceInterleaving(
	t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation, action string, cancel context.CancelFunc,
) {
	t.Helper()

	var err error

	switch action {
	case "sensitive":
		_, err = store.ApplyCanonicalMutation(context.Background(), lateTraceSensitive(mutation))
	case "forget":
		err = store.DenySource(context.Background(), mutation.Scope,
			mutation.Sources[0].SourceID, mutation.Operation.CommittedAt)
	case "erase":
		_, err = store.BeginScopeErase(context.Background(), m.ScopeEraseRequest{
			Scope: mutation.Scope, OperationID: "erase-trace", Fingerprint: "erase-trace",
			ExpectedScopeVersion: 1, ForgottenAt: mutation.Operation.CommittedAt,
		})
	case "binding":
		err = replaceTraceSourceBinding(store, mutation)
	case "cancel":
		cancel()
	case "version":
		later := lateTraceSensitive(mutation)
		later.Revisions[0].Sensitivity = m.SensitivityStandard
		_, err = store.ApplyCanonicalMutation(context.Background(), later)
	}

	if err != nil {
		t.Fatal(err)
	}
}

func lateTraceSensitive(mutation m.CanonicalMutation) m.CanonicalMutation {
	later := mutation
	later.ExpectedScopeVersion = 1
	later.Operation.OperationID = "trace-sensitive-later"
	later.Operation.Fingerprint = "trace-sensitive-later"
	later.Sources = nil
	later.Messages = nil
	later.Payloads = nil

	later.Revisions = append([]m.MemoryRevision(nil), mutation.Revisions...)
	later.Revisions[0].RevisionID = "trace-sensitive-later"
	later.Revisions[0].Sensitivity = m.SensitivitySensitive
	later.Heads = []m.ItemHead{{ItemID: later.Revisions[0].ItemID, RevisionID: later.Revisions[0].RevisionID}}

	return later
}

func TestTraceWriteAfterFinalBarrierKeepsCompletedRead(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	reader, _ := NewCanonicalReader(store)
	reader.afterTracePrivacyBarrier = func() {
		if _, err := store.ApplyCanonicalMutation(context.Background(), lateTraceSensitive(mutation)); err != nil {
			t.Fatal(err)
		}
	}

	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if err != nil || len(response.Sources) != 1 || len(response.Revisions) != 1 {
		t.Fatalf("completed read revoked: %+v %v", response, err)
	}

	reader.afterTracePrivacyBarrier = nil

	response, err = reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if err == nil || len(response.Sources) != 0 {
		t.Fatal("later sensitive write not enforced")
	}
}

func replaceTraceSourceBinding(store *CanonicalStore, mutation m.CanonicalMutation) error {
	return store.db.Update(func(txn *badger.Txn) error {
		source := mutation.Sources[0]

		record, _, err := loadSourcePrivacy(txn, source)
		if err != nil {
			return err
		}

		source.Payload = mutation.Revisions[0].Payload
		record.Payload = source.Payload

		key, _ := canonicalKey(mutation.Scope, badgerRecordSource, source.SourceID)
		if err := putCanonicalRecord(txn, key, badgerRecordSource, source); err != nil {
			return err
		}

		return writeSourcePrivacy(txn, record)
	})
}

func TestTraceOldUnsupportedHistoryDeniesBeforeContext(t *testing.T) {
	for _, kind := range []string{"opaque-source", "incomplete-evidence", "mixed-same", "mixed-separate"} {
		t.Run(kind, func(t *testing.T) {
			store, mutation := historicalPrivacyFixture(t, kind)
			if err := store.PrepareSourcePrivacy(context.Background()); err != nil {
				t.Fatal(err)
			}

			reader, _ := NewCanonicalReader(store)
			reader.beforeTracePayloadRead = func() { t.Fatal("uncertified context was read") }
			response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
			assertTracePrivacyCode(t, response, err, m.CodeForgotten)
		})
	}
}

func TestTraceMaxNodesRejectsBeforeContext(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	reader, _ := NewCanonicalReader(store)
	reader.beforeTracePayloadRead = func() { t.Fatal("context read beyond MaxNodes") }
	request := privacyTraceRequest(mutation)
	request.MaxNodes = 1
	response, err := reader.Trace(context.Background(), request)
	assertTracePrivacyCode(t, response, err, m.CodeLimitExceeded)
}

func assertTracePrivacyCode(t *testing.T, response m.TraceResponse, err error, want m.ErrorCode) {
	t.Helper()

	code, _, ok := m.ClassifyError(err)
	if !ok || code != want || len(response.Sources) != 0 || len(response.Revisions) != 0 {
		t.Fatalf("response=%+v err=%v want=%s", response, err, want)
	}
}

func TestTraceLateSensitiveSurvivesRestart(t *testing.T) {
	store, mutation := privacyFixture(t)

	dir := store.db.Opts().Dir
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ApplyCanonicalMutation(context.Background(), lateTraceSensitive(mutation)); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenCanonicalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	reader, _ := NewCanonicalReader(reopened)
	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	assertTracePrivacyCode(t, response, err, m.CodeForgotten)

	err = reopened.db.View(func(txn *badger.Txn) error {
		source, found, err := loadHistoricalPrivacySource(txn, mutation.Scope, mutation.Sources[0].SourceID)
		if err != nil {
			return err
		}

		if !found || source.Payload != mutation.Sources[0].Payload {
			t.Fatal("immutable source changed")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertTraceChanged(t *testing.T, response m.TraceResponse, err error, action string) {
	t.Helper()

	if err == nil || len(response.Sources) != 0 || len(response.Revisions) != 0 {
		t.Fatalf("stale trace action=%s response=%+v err=%v", action, response, err)
	}

	code, _, ok := m.ClassifyError(err)
	if !ok || (code != m.CodeForgotten && code != m.CodeConflict && code != m.CodeTimeout) {
		t.Fatalf("unexpected barrier error %v", err)
	}
}
