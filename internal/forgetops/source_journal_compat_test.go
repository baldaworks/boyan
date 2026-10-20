//go:build integration

package forgetops_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestSourceJournalCompatibility(t *testing.T) {
	runSourceJournalAuthorities(t, func(t *testing.T, backend canonicalsqltest.Backend) {
		t.Run("fresh", func(t *testing.T) { sourceJournalFreshCompatibility(t, backend) })
		t.Run("mismatches", func(t *testing.T) { sourceJournalMismatchCompatibility(t, backend) })
		t.Run("boundaries", func(t *testing.T) { sourceJournalBoundaryCompatibility(t, backend) })
		t.Run("load-errors", func(t *testing.T) { sourceJournalLoadCompatibility(t, backend) })
		t.Run("legacy", func(t *testing.T) { sourceJournalLegacyCompatibility(t, backend) })
	})
}

func sourceJournalFreshCompatibility(t *testing.T, backend canonicalsqltest.Backend) {
	for _, native := range []bool{true, false} {
		name := "migrated-current-runner"
		if native {
			name = "native-first-and-replay"
		}

		t.Run(name, func(t *testing.T) {
			store, command, source, _ := sourceJournalFixture(t, native, backend)
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

func sourceJournalMismatchCompatibility(t *testing.T, backend canonicalsqltest.Backend) {
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
			store, command, _, _ := sourceJournalFixture(t, true, backend)
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

func sourceJournalBoundaryCompatibility(t *testing.T, backend canonicalsqltest.Backend) {
	t.Run("missing-journal-normal-not-found", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, false, backend)
		before := sourceJournalBytes(t, store)
		command.Source.ExportID = "unknown-export"
		_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		sourceJournalError(t, err, m.CodeNotFound)

		if !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatal("missing-source resolution mutated canonical state")
		}
	})
	t.Run("cancellation-with-existing-journal", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, false, backend)
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
		store, command, _, _ := sourceJournalFixture(t, false, backend)
		before := sourceJournalBytes(t, store)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := sourceJournalAdapter(t, store).ForgetSource(ctx, command)
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatalf("canceled request mutated state or lost context identity: %v", err)
		}
	})
}

func sourceJournalLoadCompatibility(t *testing.T, backend canonicalsqltest.Backend) {
	t.Run("closed-store-load-failure", func(t *testing.T) { sourceJournalClosedLoad(t, backend) })
	t.Run("invalid-command-before-load", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, true, backend)
		before := sourceJournalBytes(t, store)
		command.Source.ExportID = ""

		_, err := sourceJournalAdapter(t, store).ForgetSource(context.Background(), command)
		if err == nil || !reflect.DeepEqual(before, sourceJournalBytes(t, store)) {
			t.Fatalf("invalid command accepted or mutated state: %v", err)
		}
	})
	t.Run("corrupt-journal-load-failure", func(t *testing.T) {
		store, command, _, _ := sourceJournalFixture(t, true, backend)
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

func sourceJournalLegacyCompatibility(t *testing.T, backend canonicalsqltest.Backend) {
	for _, truncated := range []bool{false, true} {
		name := "legacy-bounded-adoption"
		if truncated {
			name = "legacy-truncated-rejected"
		}

		t.Run(name, func(t *testing.T) {
			store, command, source, _ := sourceJournalFixture(t, true, backend)
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
func sourceJournalRewrite(t *testing.T, store *sourceJournalStore, recordType string, change func([]byte) []byte) {
	t.Helper()
	sourceJournalSQLRewrite(t, store.database, recordType, change)
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
