//go:build integration

package forgetops_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type sqlForgetStore interface {
	forgetops.CanonicalForgetStore
	forgetops.CanonicalEraseRecoveryStore
	m.ScopeEraseStore
	PrepareSourcePrivacy(ctx context.Context) error
	ApplyCanonicalMutation(ctx context.Context, mutation m.CanonicalMutation) (m.CanonicalMutationOutcome, error)
	LoadPayload(ctx context.Context, ref m.PayloadRef) ([]byte, error)
}

func TestSQLSourceJournalRetryAndRecovery(t *testing.T) {
	for _, native := range []bool{false, true} {
		name := "payload-dependent"
		if native {
			name = "native"
		}

		t.Run(name, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, f *storefixture.Fixture) {
				runSQLSourceRecovery(t, f, native)
			})
		})
	}
}

func seedSQLForgetSource(t *testing.T, s sqlForgetStore, native bool) (m.ForgetSourceCommand, m.CanonicalSourceRecord) {
	t.Helper()
	sourceJournalNoError(t, s.PrepareSourcePrivacy(t.Context()))

	scope := m.Scope{Key: "forget:sql-recovery", Kind: m.ScopeKindPersonal}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turn, err := m.NewTurn(scope, m.ConversationRef{ConversationID: "conversation",
		AgentConversationID: "agent"}, "turn", at, "private", "ack")
	sourceJournalNoError(t, err)

	command := m.ForgetSourceCommand{SchemaVersion: m.DerivedSchemaVersionV1, ForgottenAt: at,
		Source: m.SourceRef{Scope: scope, ExportID: turn.ExportID, ConversationID: "conversation", SourceTurnID: "turn"}}
	record := m.SourceRecord{SchemaVersion: m.DerivedSchemaVersionV1, Ref: command.Source,
		State: m.SourceStateActive, Turn: &turn}
	sourceJournalNoError(t, record.Validate())
	data, err := json.Marshal(record)
	sourceJournalNoError(t, err)

	hash := sha256.Sum256(data)

	source := m.CanonicalSourceRecord{Scope: scope, SourceID: command.Source.ExportID, Sensitivity: m.SensitivityStandard,
		Retention: m.RetentionClassStandard, Payload: m.PayloadRef{ID: "source",
			Digest: hex.EncodeToString(hash[:]), ByteSize: uint32(len(data))}}
	if native {
		source.SourceID = m.TurnSourceID(command.Source.ExportID)
	}

	_, err = s.ApplyCanonicalMutation(t.Context(),
		m.CanonicalMutation{SchemaVersion: m.CanonicalSchemaVersionV1, Scope: scope,
			Operation: m.OperationRecord{OperationID: "initial", Fingerprint: "fp", CommittedAt: at},
			Sources:   []m.CanonicalSourceRecord{source}, Payloads: []m.CanonicalPayload{{Ref: source.Payload, Data: data}}})
	sourceJournalNoError(t, err)

	return command, source
}

func runSQLSourceRecovery(t *testing.T, f *storefixture.Fixture, native bool) {
	t.Helper()

	s := f.Store.(sqlForgetStore)
	command, source := seedSQLForgetSource(t, s, native)
	r := sourceJournalRequest(t, command, source.SourceID, 1)
	_, err := s.BeginScopeErase(t.Context(), r)
	sourceJournalNoError(t, err)

	for {
		j, err := s.AdvanceScopeErase(t.Context(), r.Scope, r.OperationID)
		sourceJournalNoError(t, err)

		if j.Stage == m.ScopeEraseScrubMessages {
			break
		}
	}

	sourceJournalNoError(t, f.Store.Close())
	reopened, err := f.Open(t.Context())
	sourceJournalNoError(t, err)

	defer reopened.Close()

	s = reopened.(sqlForgetStore)
	sourceJournalNoError(t, forgetops.RecoverPendingScopeErases(t.Context(), s))
	adapter, err := forgetops.NewCanonicalForgetAdapter(s)
	sourceJournalNoError(t, err)
	first, err := adapter.ForgetSource(t.Context(), command)
	sourceJournalNoError(t, err)

	command.ForgottenAt = command.ForgottenAt.Add(time.Hour)
	replay, err := adapter.ForgetSource(t.Context(), command)
	sourceJournalNoError(t, err)

	if !reflect.DeepEqual(first, replay) || first.SourceCount != 1 || first.Sources[0] != command.Source {
		t.Fatalf("outcomes=%+v %+v", first, replay)
	}

	if _, err := s.LoadPayload(t.Context(), source.Payload); err == nil {
		t.Fatal("recovery retained source payload")
	}

	assertSQLPayloadDependentResolver(t, s, command, native)

	pending, _, err := s.ListPendingScopeErases(t.Context(), nil, 1)
	sourceJournalNoError(t, err)

	if len(pending) != 0 {
		t.Fatalf("pending=%+v %v", pending, err)
	}

	scopeOut, err := adapter.ForgetScope(t.Context(), m.ForgetScopeCommand{SchemaVersion: m.DerivedSchemaVersionV1,
		Scope: command.Source.Scope, RequestID: "scope-after-source", ForgottenAt: command.ForgottenAt})
	sourceJournalNoError(t, err)

	if scopeOut.ScopeVersion != 3 || scopeOut.SourceCount != 1 {
		t.Fatalf("scope-after-source=%+v", scopeOut)
	}
}

func assertSQLPayloadDependentResolver(t *testing.T, s sqlForgetStore, command m.ForgetSourceCommand, native bool) {
	t.Helper()

	if !native {
		if _, err := s.CanonicalSourceID(t.Context(), command.Source.Scope, command.Source); err == nil {
			t.Fatal("fixture does not exercise missing resolver payload")
		}
	}
}
