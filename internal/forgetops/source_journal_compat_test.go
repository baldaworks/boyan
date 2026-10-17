package forgetops_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	db "github.com/dgraph-io/badger/v4"
)

func TestSourceJournalCompatibility(t *testing.T) {
	t.Run("fresh", sourceJournalFreshCompatibility)
	t.Run("mismatches", sourceJournalMismatchCompatibility)
	t.Run("boundaries", sourceJournalBoundaryCompatibility)
	t.Run("load-errors", sourceJournalLoadCompatibility)
	t.Run("legacy", sourceJournalLegacyCompatibility)
}

func sourceJournalFreshCompatibility(t *testing.T) {
	for _, native := range []bool{true, false} {
		name := "migrated-current-runner"
		if native {
			name = "native-first-and-replay"
		}

		t.Run(name, func(t *testing.T) {
			store, command, source, _ := sourceJournalFixture(t, native)
			request := sourceJournalRequest(t, command, source.SourceID, 1)
			_, found, err := store.LoadScopeErase(context.Background(), request.Scope, request.OperationID)
			sourceJournalNoError(t, err)

			if found {
				t.Fatal("fresh path unexpectedly has a journal")
			}

			adapter := sourceJournalAdapter(t, store)
			first, err := adapter.ForgetSource(context.Background(), command)
			sourceJournalNoError(t, err)

			command.ForgottenAt = command.ForgottenAt.Add(time.Hour)
			replay, err := adapter.ForgetSource(context.Background(), command)
			sourceJournalNoError(t, err)

			if !reflect.DeepEqual(first, replay) {
				t.Fatal("replay replaced the original outcome")
			}
		})
	}
}

func sourceJournalMismatchCompatibility(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*m.ScopeEraseJournal)
		code   m.ErrorCode
	}{
		{name: "scope", change: func(j *m.ScopeEraseJournal) { j.Scope.Kind = m.ScopeKindGroup }, code: m.CodeStoreFailure},
		{name: "operation", code: m.CodeStoreFailure,
			change: func(j *m.ScopeEraseJournal) { j.OperationID = "different-operation" }},
		{name: "fingerprint", code: m.CodeConflict,
			change: func(j *m.ScopeEraseJournal) { j.Fingerprint = "different-fingerprint" }},
		{name: "source-ref", code: m.CodeConflict,
			change: func(j *m.ScopeEraseJournal) { j.SourceRef.ConversationID = "different-conversation" }},
		{name: "kind", change: func(j *m.ScopeEraseJournal) {
			j.Kind = m.ForgetKindScope
			j.SourceID, j.SourceRef = "", m.SourceRef{}
			j.Outcome.Kind = m.ForgetKindScope
		}, code: m.CodeConflict},
		{name: "missing-source-id", change: func(j *m.ScopeEraseJournal) { j.SourceID = "" }, code: m.CodeStoreFailure},
		{name: "invalid-version", change: func(j *m.ScopeEraseJournal) { j.Version = 2 }, code: m.CodeStoreFailure},
	} {
		t.Run("reject-"+test.name, func(t *testing.T) {
			store, command, _, _ := sourceJournalFixture(t, true)
			_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
			sourceJournalNoError(t, err)
			journal := sourceJournalLoad(t, store, command)
			test.change(&journal)
			sourceJournalRewrite(t, store, "erase-journal", func([]byte) []byte {
				encoded, marshalErr := json.Marshal(journal)
				sourceJournalNoError(t, marshalErr)

				return encoded
			})
			before := sourceJournalBytes(t, store)
			outcome, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
			sourceJournalError(t, err, test.code)

			if !reflect.DeepEqual(outcome, m.ForgetOutcome{}) || !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
				t.Fatal("rejected journal changed canonical state or returned an outcome")
			}
		})
	}
}

func sourceJournalBoundaryCompatibility(t *testing.T) {
	t.Run("missing-journal-normal-not-found", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, false)
		before := sourceJournalBytes(t, store)
		command.Source.ExportID = "unknown-export"
		_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		sourceJournalError(t, err, m.CodeNotFound)

		if !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatal("missing-source resolution mutated canonical state")
		}
	})
	t.Run("cancellation-with-existing-journal", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, false)
		_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		sourceJournalNoError(t, err)
		before := sourceJournalBytes(t, store)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = sourceJournalAdapter(t, store).ForgetSource(ctx, command)
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatalf("canceled journal replay changed state or lost cancellation: %v", err)
		}
	})
	t.Run("cancellation-before-mutation", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, false)
		before := sourceJournalBytes(t, store)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := sourceJournalAdapter(t, store).ForgetSource(ctx, command)
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatalf("canceled request mutated state or lost context identity: %v", err)
		}
	})
}

func sourceJournalLoadCompatibility(t *testing.T) {
	t.Run("closed-store-load-failure", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, true)
		sourceJournalNoError(t, store.Close())
		_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		sourceJournalError(t, err, m.CodeStoreFailure)
	})
	t.Run("invalid-command-before-load", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, true)
		before := sourceJournalBytes(t, store)
		command.Source.ExportID = ""

		_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		if err == nil || !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatalf("invalid command accepted or mutated state: %v", err)
		}
	})
	t.Run("corrupt-journal-load-failure", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, true)
		_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		sourceJournalNoError(t, err)
		sourceJournalRewrite(t, store, "erase-journal", func([]byte) []byte { return []byte(`{"version":0}`) })
		before := sourceJournalBytes(t, store)
		_, err = sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		sourceJournalError(t, err, m.CodeStoreFailure)

		if !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatal("corrupt journal fallback mutated state")
		}
	})
}

func sourceJournalLegacyCompatibility(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		name := "legacy-bounded-adoption"
		if truncated {
			name = "legacy-truncated-rejected"
		}

		t.Run(name, func(t *testing.T) {
			store, command, source, _ := sourceJournalFixture(t, true)
			request := sourceJournalRequest(t, command, source.SourceID, 1)
			sourceJournalNoError(t, store.DenySource(context.Background(), request.Scope, source.SourceID, command.ForgottenAt))
			legacy, err := store.CommitCanonicalForget(context.Background(), m.CanonicalForgetCommitRequest{
				Scope: request.Scope, Kind: m.ForgetKindSource, OperationID: request.OperationID, Fingerprint: request.Fingerprint,
				ExpectedScopeVersion: 1, ForgottenAt: command.ForgottenAt, Sources: []m.SourceRef{command.Source},
			})
			sourceJournalNoError(t, err)

			if truncated {
				sourceJournalRewrite(t, store, "forget-outcome", func(encoded []byte) []byte {
					var record struct {
						Fingerprint string          `json:"fingerprint"`
						Outcome     m.ForgetOutcome `json:"outcome"`
					}
					sourceJournalNoError(t, json.Unmarshal(encoded, &record))
					record.Outcome.ReferencesTruncated = true
					record.Outcome.Sources = nil
					data, marshalErr := json.Marshal(record)
					sourceJournalNoError(t, marshalErr)

					return data
				})
			}

			before := sourceJournalBytes(t, store)
			outcome, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)

			if truncated {
				sourceJournalError(t, err, m.CodeStoreFailure)

				if !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
					t.Fatal("truncated legacy rejection mutated state")
				}

				return
			}

			sourceJournalNoError(t, err)

			if !reflect.DeepEqual(outcome, legacy) || !sourceJournalLoad(t, store, command).LegacyOutcome {
				t.Fatal("bounded legacy outcome was not adopted unchanged")
			}

			sourceJournalPayloadAbsent(t, store, source.Payload)
		})
	}
}

// Fault injection edits the actual persisted record at its discovered key; it
// does not replace storage behavior or synthesize SourceID lookup results.
func sourceJournalRewrite(t *testing.T, store *b.CanonicalStore, recordType string, change func([]byte) []byte) {
	t.Helper()

	var key, value []byte

	err := store.DB().View(func(txn *db.Txn) error {
		iterator := txn.NewIterator(db.DefaultIteratorOptions)
		defer iterator.Close()

		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			encoded, err := iterator.Item().ValueCopy(nil)
			if err != nil {
				return err
			}

			envelope, err := m.UnmarshalRecordEnvelope(encoded)
			if err != nil {
				return err
			}

			if envelope.RecordType == recordType {
				key, value = iterator.Item().KeyCopy(nil), envelope.Payload

				return nil
			}
		}

		return nil
	})
	sourceJournalNoError(t, err)

	if key == nil {
		t.Fatalf("fault target %q missing", recordType)
	}

	encoded, err := m.MarshalRecordEnvelope(recordType, change(value))
	sourceJournalNoError(t, err)
	sourceJournalNoError(t, store.DB().Update(func(txn *db.Txn) error { return txn.Set(key, encoded) }))
}

func sourceJournalError(t *testing.T, err error, code m.ErrorCode) {
	t.Helper()

	var classified *m.Error

	if !errors.As(err, &classified) || classified.Code != code || m.IsRetryable(err) {
		t.Fatalf("want permanent %s, got %v", code, err)
	}

	for _, sensitive := range []string{
		"migrated-source", "different-conversation", "different-fingerprint", "Key not found",
	} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("error exposes stored details: %v", err)
		}
	}
}
