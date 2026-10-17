//nolint:cyclop,funlen,gocognit,gocyclo,lll,maintidx // Finite operation samples retain setup, outcome and resource observations together.
package sqlitegate_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/internal/memoryread"
	standalone "github.com/baldaworks/boyan/internal/standaloneruntime"
	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/normahq/runtime/v2/agentconfig"
	runtimeconfig "github.com/normahq/runtime/v2/appconfig"
)

// Single-operation samples preserve all observations rather than use a testing
// calibration loop that would consume an erase fixture on its first attempt.
func matchedSample(name string, action func() error) error {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	start := time.Now()
	err := action()
	elapsed := time.Since(start)

	runtime.ReadMemStats(&after)
	fmt.Printf("Benchmark%s-4\t1\t%d ns/op\t%d B/op\t%d allocs/op\n", name, elapsed.Nanoseconds(), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)

	if err != nil {
		fmt.Printf("OUTCOME %s error_type=%T context_cancel=%t deadline=%t\n", name, err, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
	}

	return err
}

func TestMatchedLargeIdentityCosts(t *testing.T) {
	if os.Getenv("BOYAN_TASK19_COSTS") != "1" {
		t.Skip("explicit finite cost measurement; not a normal correctness gate")
	}

	for repetition := range 10 {
		t.Run(fmt.Sprint(repetition), func(t *testing.T) {
			c, err := seedMatchedCorpus(t.Context(), "oversized", 3)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = c.canonical.Close(); _ = os.RemoveAll(c.root) })

			backend, err := openMatchedBackend(t.Context(), c)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = backend.close() })

			if len(c.records[1].ConversationIDs[0]) != 2097153 {
				t.Fatal("not canonical oversized identity")
			}

			id := c.records[1].RevisionID

			if err := matchedSample("Identity/HydrateOne", func() error {
				records, err := c.reader.LoadRecallRecords(t.Context(), c.scope, []string{id})
				if err == nil && len(records) != 1 {
					return fmt.Errorf("hydration missing")
				}

				return err
			}); err != nil {
				t.Fatal(err)
			}

			for _, op := range []string{"InitialBuild", "IncrementalCopy", "Rebuild"} {
				if err := matchedSample("Identity/"+op, func() error {
					if op == "IncrementalCopy" {
						return backend.copy(t.Context())
					}

					return backend.rebuild(t.Context(), "identity-"+op)
				}); err != nil {
					t.Fatal(err)
				}
			}

			request := m.RecallRequest{Scope: c.scope, Query: "engineer", Limit: 10, ConversationID: c.conversation, AsOf: &matchedAt}

			service, _ := memoryread.NewRecallService(c.reader, backend.lexical())
			if response, err := service.Search(t.Context(), request); err != nil || len(response.Results) != 1 {
				t.Fatalf("oversized exact filter count=%d err=%v", len(response.Results), err)
			}

			t.Logf("IDENTITY bytes=%d open=%v max_hydration_ids=%d authority_revision=%d source=%d structural=%d", len(c.conversation), backend.bytes(), c.reader.maxIDs, c.reader.revisionDenials, c.reader.sourceDenials, c.reader.structures)

			if err := backend.close(); err != nil {
				t.Fatal(err)
			}

			t.Logf("IDENTITY closed=%v", backend.bytes())
		})
	}
}

func seedMatchedMemberships(ctx context.Context) (*matchedCorpus, error) {
	c, err := seedMatchedCorpus(ctx, "memberships", 3)
	if err != nil {
		return nil, err
	}

	var evidence []m.EvidenceRef

	for i := range 16 {
		id := fmt.Sprintf("fanout-source-%03d", i)
		message := fmt.Sprintf("fanout-message-%03d", i)
		conversation := fmt.Sprintf("fanout-conversation-%03d-", i) + strings.Repeat("c", 150000)
		encoded, _ := json.Marshal(map[string]any{"export_id": "fanout-export-" + id, "conversation_id": conversation, "source_turn_id": "fanout-turn-" + id, "completed_at": matchedAt})
		source := payloadFor("fanout-payload-"+id, encoded)
		msg := payloadFor("fanout-payload-"+message, []byte("evidence"))

		outcome, err := c.canonical.ApplyCanonicalMutation(ctx, m.CanonicalMutation{SchemaVersion: m.CanonicalSchemaVersionV1, Scope: c.scope, ExpectedScopeVersion: c.seq, Operation: m.OperationRecord{OperationID: "fanout-op-" + id, Fingerprint: "fanout-fp-" + id, CommittedAt: matchedAt}, Sources: []m.CanonicalSourceRecord{{SourceID: id, Scope: c.scope, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard, Payload: source.Ref}}, Messages: []m.MessageRecord{{MessageID: message, SourceID: id, Role: m.MessageRoleUser, Payload: msg.Ref}}, Payloads: []m.CanonicalPayload{source, msg}})
		if err != nil {
			return nil, err
		}

		c.seq = outcome.ChangeSeq

		evidence = append(evidence, m.EvidenceRef{SourceID: id, MessageID: message, Role: m.MessageRoleUser, StartByte: 0, EndByte: 8, AssertionMode: m.AssertionModeUser})
	}

	id := "bench-00003"
	category := m.AtomCategoryDecision
	encoded, _ := json.Marshal(m.CanonicalCompatibilityPayload{SchemaVersion: m.CanonicalCompatibilitySchemaVersion, Kind: m.DerivedKindAtom, Category: &category, Text: "membership fanout", LegacyItemID: id, LegacyRevisionID: id, LegacyOperationID: "op-" + id})
	payload := payloadFor("payload-"+id, encoded)
	revision := m.MemoryRevision{SchemaVersion: m.MemorySchemaVersion, RevisionID: id, ItemID: id, Revision: 1, Temporal: m.Temporal{ObservedAt: matchedAt}, Evidence: evidence, Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard, Payload: payload.Ref}

	outcome, err := c.canonical.ApplyCanonicalMutation(ctx, m.CanonicalMutation{SchemaVersion: m.CanonicalSchemaVersionV1, Scope: c.scope, ExpectedScopeVersion: c.seq, Operation: m.OperationRecord{OperationID: "op-" + id, Fingerprint: "fp-" + id, CommittedAt: matchedAt}, Items: []m.MemoryItem{{ItemID: id, Scope: c.scope, Kind: m.MemoryKindState, MemoryKey: m.MemoryKey(id)}}, Revisions: []m.MemoryRevision{revision}, Heads: []m.ItemHead{{ItemID: id, RevisionID: id}}, Payloads: []m.CanonicalPayload{payload}})
	if err != nil {
		return nil, err
	}

	c.seq = outcome.ChangeSeq
	c.revisions = append(c.revisions, revision)

	c.records = nil
	for _, revision := range c.revisions {
		records, err := c.reader.LoadRecallRecords(ctx, c.scope, []string{revision.RevisionID})
		if err != nil || len(records) != 1 {
			return nil, fmt.Errorf("membership hydration count=%d: %w", len(records), err)
		}

		if err := records[0].Validate(m.RecallRequest{Scope: c.scope, Query: "membership", Limit: 10, AsOf: &matchedAt}, matchedAt); err != nil {
			return nil, err
		}

		c.records = append(c.records, records[0])
	}

	return c, nil
}

func TestMatchedCanonicalMembershipCosts(t *testing.T) {
	if os.Getenv("BOYAN_TASK19_COSTS") != "1" {
		t.Skip("explicit finite cost measurement")
	}

	for repetition := range 10 {
		t.Run(fmt.Sprint(repetition), func(t *testing.T) {
			c, err := seedMatchedMemberships(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = c.canonical.Close(); _ = os.RemoveAll(c.root) })

			backend, err := openMatchedBackend(t.Context(), c)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = backend.close() })

			big := c.records[3]
			if len(big.ConversationIDs) != 16 || len(big.Evidence) != 16 {
				t.Fatal("not actual canonical membership fanout")
			}

			total := 0
			for _, id := range big.ConversationIDs {
				total += len(id)
			}

			if total <= 2097152 {
				t.Fatal("aggregate metadata did not exceed target")
			}

			for _, op := range []string{"HydrateOne", "InitialBuild", "IncrementalCopy", "Rebuild"} {
				if err := matchedSample("CanonicalMembership/"+op, func() error {
					switch op {
					case "HydrateOne":
						_, err := c.reader.LoadRecallRecords(t.Context(), c.scope, []string{big.RevisionID})

						return err
					case "IncrementalCopy":
						return backend.copy(t.Context())
					default:
						return backend.rebuild(t.Context(), "membership-"+op)
					}
				}); err != nil {
					t.Fatal(err)
				}
			}

			t.Logf("MEMBERSHIPS actual_canonical=true records=4 source_memberships=16 conversation_memberships=16 aggregate_conversation_bytes=%d individual_conversation_bytes=%d open=%v authority_revision=%d authority_source=%d structures=%d max_hydration_ids=%d", total, len(big.ConversationIDs[0]), backend.bytes(), c.reader.revisionDenials, c.reader.sourceDenials, c.reader.structures, c.reader.maxIDs)
		})
	}
}

func matchedRuntimeConfig(c *matchedCorpus) standalone.Config {
	return standalone.Config{StateDir: c.root, Log: standalone.LogConfig{Level: "error"}, HTTP: standalone.HTTPConfig{Listen: "127.0.0.1:0", RequestTimeout: 15 * time.Second, ShutdownTimeout: 10 * time.Second, MaxBodyBytes: 1 << 20}, Derivation: standalone.DerivationConfig{Provider: "memory", WorkingDir: c.root, Timeout: 30 * time.Second, MaxOutputBytes: 262144, Runtime: runtimeconfig.RuntimeConfig{Providers: map[string]agentconfig.Config{"memory": {Type: agentconfig.AgentTypeCodexACP, CodexACP: &agentconfig.ACPConfig{Model: "memory-fast"}}}}}, Maintenance: standalone.MaintenanceConfig{ValueLogGCInterval: 6 * time.Hour, DiscardRatio: 0.5}}
}

func TestMatchedRuntimeHistoryCosts(t *testing.T) {
	if os.Getenv("BOYAN_TASK19_COSTS") != "1" {
		t.Skip("explicit finite cost measurement; not a normal correctness gate")
	}

	for _, fixture := range []struct {
		name     string
		count    int
		preerase bool
	}{{"denial-free", 64, false}, {"sparse-denials", 64, false}, {"many-denials-truncated", 2100, true}, {"lexical8200", 8200, false}, {"oversized", 3, false}, {"memberships", 4, false}} {
		for repetition := range 10 {
			t.Run(fmt.Sprintf("%s/%d", fixture.name, repetition), func(t *testing.T) {
				name := fixture.name
				if fixture.name == "oversized" {
					name = "oversized"
				}

				c, err := seedMatchedCorpus(t.Context(), name, fixture.count)
				if fixture.name == "memberships" {
					if c != nil {
						_ = c.canonical.Close()
						_ = os.RemoveAll(c.root)
					}

					c, err = seedMatchedMemberships(t.Context())
				}

				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() { _ = os.RemoveAll(c.root) })

				backend, err := openMatchedBackend(t.Context(), c)
				if err != nil {
					t.Fatal(err)
				}

				if err := backend.rebuild(t.Context(), "history-initial"); err != nil {
					t.Fatal(err)
				}

				if err := backend.copy(t.Context()); err != nil {
					t.Fatal(err)
				}

				if fixture.count <= 64 && fixture.name != "oversized" && fixture.name != "memberships" {
					if err := backend.semantic(t.Context(), "ready"); err != nil {
						t.Fatal(err)
					}
				}

				if fixture.name == "sparse-denials" {
					if err := c.canonical.DenyRevision(t.Context(), c.scope, c.revisions[0].RevisionID, matchedAt); err != nil {
						t.Fatal(err)
					}
				}

				beforeFamilies, err := matchedRuntimeCounts(c)
				if err != nil {
					t.Fatal(err)
				}

				command := m.ForgetScopeCommand{SchemaVersion: m.DerivedSchemaVersionV1, Scope: c.scope, RequestID: "cost-erase", ForgottenAt: matchedAt.Add(time.Hour)}

				var original m.ForgetOutcome

				if fixture.preerase {
					adapter, err := forgetops.NewCanonicalForgetAdapter(c.canonical)
					if err != nil {
						t.Fatal(err)
					}

					original, err = adapter.ForgetScope(t.Context(), command)
					if err != nil {
						t.Fatal(err)
					}

					if !original.ReferencesTruncated {
						t.Fatal("expected >2048 truncated outcome")
					}
				}

				t.Logf("HISTORY fixture=%s records=%d retained_open=%v truncated=%t canonical_seq=%d", fixture.name, len(c.records), backend.bytes(), original.ReferencesTruncated, c.seq)

				if err := backend.close(); err != nil {
					t.Fatal(err)
				}

				if err := c.canonical.Close(); err != nil {
					t.Fatal(err)
				}

				var r *standalone.Runtime

				if err := matchedSample("Runtime/"+fixture.name+"/ExistingStartup", func() error {
					var err error

					r, err = standalone.New(matchedRuntimeConfig(c))
					if err != nil {
						return err
					}

					return r.Start(t.Context())
				}); err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() { _ = r.Stop(context.Background()) })

				if !r.Ready() {
					t.Fatal("startup not ready")
				}

				afterStartup, err := matchedRuntimeCounts(c)
				if err != nil {
					t.Fatal(err)
				}

				t.Logf("FAMILIES before=%v after_startup=%v embeddings_disabled_existing_semantics_metadata_only=true", beforeFamilies, afterStartup)

				var outcome m.ForgetOutcome

				label := "PublicErase"
				if fixture.preerase {
					label = "PublicReplayAfterStartup"
				}

				if err := matchedSample("Runtime/"+fixture.name+"/"+label, func() error {
					var err error

					outcome, err = r.ForgetScope(t.Context(), command)

					return err
				}); err != nil {
					t.Fatal(err)
				}

				if fixture.preerase && !reflect.DeepEqual(original, outcome) {
					t.Fatal("startup/replay changed original outcome")
				}

				if err := matchedSample("Runtime/"+fixture.name+"/SameOperationReplay", func() error {
					replay, err := r.ForgetScope(t.Context(), command)
					if err == nil && !reflect.DeepEqual(outcome, replay) {
						return fmt.Errorf("replay outcome changed")
					}

					return err
				}); err != nil {
					t.Fatal(err)
				}

				if response, err := r.Search(t.Context(), m.RecallRequest{Scope: c.scope, Query: "engineer", Limit: 10, AsOf: &matchedAt}); err != nil || len(response.Results) != 0 {
					t.Fatalf("post erase consumer count=%d error=%v", len(response.Results), err)
				}

				if err := r.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}

				after, err := matchedRuntimeCounts(c)
				if err != nil {
					t.Fatal(err)
				}

				for family, count := range after {
					if count != 0 {
						t.Fatalf("successful joined public erase retained %s=%d", family, count)
					}
				}

				t.Logf("FAMILIES after_public_erase_replay=%v", after)
				t.Logf("HISTORY closed=%v old_public_completion_is_not_joined_SQL_retirement=%t", backend.bytes(), matchedEngine() == "old")
			})
		}
	}
}

func matchedRuntimeCounts(c *matchedCorpus) (map[string]int, error) {
	if matchedEngine() == "old" {
		return nil, nil
	}

	db, err := sql.Open("sqlite", "file:"+filepath.Join(c.root, "search", "search.sqlite")+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	out := map[string]int{}

	for _, family := range []string{"lexical_record", "lexical_fts", "lexical_source", "lexical_conversation", "semantic_chunk", "semantic_coverage", "semantic_source", "semantic_conversation"} {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM " + family).Scan(&count); err != nil {
			return nil, err
		}

		out[family] = count
	}

	return out, nil
}
