package semanticeval_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	index "github.com/baldaworks/boyan/index/sqlite"
	core "github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/pkg/boyan"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	agentconfig "github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

var scope = m.Scope{Kind: m.ScopeKindPersonal, Key: "eval:public-semantic-v1"}

type extractor struct{ subject string }

func (e extractor) ExtractCanonicalSemantics(
	_ context.Context,
	req m.CanonicalExtractionRequest,
) ([]m.SemanticCandidate, error) {
	msg := req.Turn.Messages[0]

	evidence, err := m.NewEvidenceRef(
		req.Turn.SourceID,
		msg.MessageID,
		msg.Role,
		msg.Text,
		0,
		uint32(len(msg.Text)),
		m.AssertionModeUser,
	)
	if err != nil {
		return nil, err
	}

	return []m.SemanticCandidate{
		{
			Kind:      m.MemoryKindState,
			Subject:   e.subject,
			Predicate: "public fixture assertion",
			Memory: m.MemoryCandidate{
				Kind:        m.MemoryKindState,
				Statement:   msg.Text,
				Temporal:    m.Temporal{ObservedAt: req.Turn.CompletedAt},
				Evidence:    []m.EvidenceRef{evidence},
				Sensitivity: m.SensitivityStandard,
				Retention:   m.RetentionClassStandard,
			},
		},
	}, nil
}

func check(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func config(t *testing.T, endpoint string, dimensions int) boyan.Config {
	t.Helper()

	cfg := boyan.DefaultConfig()
	cfg.StateDir = filepath.Join(t.TempDir(), "state")
	cfg.HTTP.Listen = "127.0.0.1:0"
	cfg.Derivation.Provider = "memory"
	cfg.Derivation.WorkingDir = t.TempDir()
	cfg.Derivation.Runtime = runtimeconfig.RuntimeConfig{
		Providers: map[string]agentconfig.Config{
			"memory": {
				Type:     agentconfig.AgentTypeCodexACP,
				CodexACP: &agentconfig.ACPConfig{Model: "unused"},
			},
		},
	}
	cfg.Embeddings = boyan.EmbeddingsConfig{
		Enabled:       true,
		Endpoint:      endpoint,
		Model:         referenceModel,
		Revision:      referenceRevision,
		Dimensions:    dimensions,
		QueryPrefix:   "query: ",
		PassagePrefix: "passage: ",
		FailurePolicy: boyan.EmbeddingFailurePolicyLexical,
	}

	return cfg
}

//nolint:funlen // This bounded fixture uses the complete canonical commit and lexical synchronization path.
func seed(t *testing.T, cfg boyan.Config, docs []document) map[string]string {
	t.Helper()

	store, err := b.Open(filepath.Join(cfg.StateDir, "badger"))

	check(t, err)
	defer func() { check(t, store.Close()) }()

	ids := make(map[string]string)

	for _, doc := range docs {
		// All records share one creation instant so freshness cannot reorder channels.
		turn, turnErr := m.NewTurn(
			scope,
			m.ConversationRef{ConversationID: "public-corpus", AgentConversationID: "public-agent"},
			doc.id,
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			doc.text,
			"ack",
		)
		check(t, turnErr)

		processor, processorErr := core.NewCanonicalTurnProcessor(
			store,
			extractor{subject: doc.subject},
			m.PolicyRegistry{Version: "policy-v1"},
		)
		check(t, processorErr)

		outcome, processErr := processor.ProcessTurn(
			context.Background(),
			turn,
			m.LegacyDerivationRef(),
		)
		check(t, processErr)

		if len(outcome.RevisionIDs) != 1 {
			t.Fatalf("fixture=%s revision count=%d", doc.id, len(outcome.RevisionIDs))
		}

		ids[doc.id] = outcome.RevisionIDs[0]
		if outcome.RevisionIDs[0] != revisions[doc.id] {
			t.Fatalf("fixture=%s canonical revision drift", doc.id)
		}
	}

	reader, err := b.NewCanonicalReader(store)
	check(t, err)
	idx, err := index.Open(t.Context(), cfg.StateDir, index.Options{})

	check(t, err)
	defer func() { check(t, idx.Close()) }()

	applier, err := index.NewSQLiteCanonicalApplier(idx, reader, store)

	check(t, err)
	runtime, err := projection.NewProjectionRuntime(store, store, applier, "sqlite", 128)

	check(t, err)
	defer func() { check(t, runtime.Close(t.Context())) }()

	check(t, runtime.Sync(context.Background(), scope))

	return ids
}

func start(t *testing.T, cfg boyan.Config) *boyan.Host {
	t.Helper()

	host, err := boyan.New(context.Background(), cfg)
	check(t, err)
	t.Cleanup(func() { _ = host.Close() })
	check(t, host.Start(context.Background()))

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		status, statusErr := host.SemanticStatus(context.Background(), scope)
		check(t, statusErr)

		if status.State == "hybrid-ready" {
			return host
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("reference projection readiness deadline exceeded")

	return nil
}
