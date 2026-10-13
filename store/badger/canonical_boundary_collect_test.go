package badger //nolint:testpackage // Exercises bounded collector ports and actual boundary-view assembly.

import (
	"context"
	"fmt"
	"testing"
	"time"

	derive "github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type boundaryTestScanner struct {
	count        int
	calls        int
	nonadvancing bool
	cancel       context.CancelFunc
}

func (s *boundaryTestScanner) ScanActiveMemory(_ context.Context,
	request m.ActiveMemoryScanRequest) ([]m.ActiveCanonicalMemory,
	error) {
	s.calls++
	if s.cancel != nil && s.calls == 2 {
		s.cancel()
	}

	start := 0
	if request.AfterItemID != "" {
		if _,
			err := fmt.Sscanf(request.AfterItemID,
			"item-%05d",
			&start); err != nil {
			return nil,
				err
		}

		start++
	}

	if s.nonadvancing && s.calls == 2 {
		start = 0
	}

	page := make([]m.ActiveCanonicalMemory, 0, request.Limit)
	for i := start; i < s.count && len(page) < int(request.Limit); i++ {
		id := fmt.Sprintf("item-%05d", i)
		page = append(page,
			m.ActiveCanonicalMemory{Item: m.MemoryItem{Scope: request.Scope,
				ItemID:    id,
				Kind:      m.MemoryKindState,
				MemoryKey: m.MemoryKey(id)},
				RevisionID: "revision-" + id,
				Revision:   1})
	}

	return page, nil
}

func boundaryTestHydrate(_ context.Context, scope m.Scope, page []m.ActiveCanonicalMemory) ([]m.RecallRecord, error) {
	records := make([]m.RecallRecord, 0, len(page))
	for _, active := range page {
		record := m.RecallRecord{Scope: scope,
			ItemID:     active.Item.ItemID,
			RevisionID: active.RevisionID,
			Revision:   1,
			Kind:       m.MemoryKindState,
			State:      m.RevisionStateActive,
			CreatedAt: time.Date(2026,
				time.October,
				12,
				0,
				0,
				0,
				0,
				time.UTC),
			Text: active.Item.ItemID,
			SourceRefs: []m.SourceRef{{Scope: scope,
				ExportID:       "export",
				ConversationID: "conversation",
				SourceTurnID:   "turn"}}}
		if active.Item.ItemID == "item-02046" {
			kind := m.DerivedKindScenario
			record.LegacyKind = &kind
			record.TopicKey = "topic"
			record.Title = "Scenario"
			record.LegacyParents = []m.RevisionRef{{ItemID: "parent",
				RevisionID: "parent-revision"}}
		}

		if active.Item.ItemID == "item-02047" {
			kind := m.DerivedKindProfile
			record.LegacyKind = &kind
			record.LegacyParents = []m.RevisionRef{{ItemID: "parent",
				RevisionID: "parent-revision"}}
		}

		records = append(records, record)
	}

	return records, nil
}

func TestBoundaryCollectorAggregateModelLimit(t *testing.T) {
	t.Parallel()

	scope := m.Scope{Key: "test:model", Kind: m.ScopeKindPersonal}

	for _, count := range []int{0, 257, 2048, 2049} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			scanner := &boundaryTestScanner{count: count}

			records, err := collectBoundaryRecallRecords(context.Background(), scope, scanner, boundaryTestHydrate)
			if count > m.MaxSnapshotItems {
				code,
					_,
					ok := m.ClassifyError(err)
				if !ok || code != m.CodeLimitExceeded {
					t.Fatalf("overflow: %v",
						err)
				}

				return
			}

			requireTraceNoError(t, err, "collect: %v")
			view, err := buildCanonicalBoundaryView(context.Background(), scope, 1, records)
			requireTraceNoError(t, err, "view: %v")

			total := len(view.Snapshot.Atoms) + len(view.Snapshot.Scenarios) + len(view.Snapshot.Profiles)
			if total != count {
				t.Fatalf("omitted model records: %d want %d", total, count)
			}

			if count == 2048 && (len(view.Snapshot.Scenarios) != 1 ||
				len(view.Snapshot.Profiles) != 1) {
				t.Fatal("layers not included in aggregate count")
			}

			if count == 2048 {
				assertBoundarySerializedLimit(t, view.Snapshot)
			}
		})
	}
}

func TestBoundaryCollectorCursorAndCancellation(t *testing.T) {
	t.Parallel()

	scope := m.Scope{Key: "test:model", Kind: m.ScopeKindPersonal}
	scanner := &boundaryTestScanner{count: 257, nonadvancing: true}
	_, err := collectBoundaryRecallRecords(context.Background(), scope, scanner, boundaryTestHydrate)

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeStoreFailure {
		t.Fatalf("nonadvancing cursor accepted: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scanner = &boundaryTestScanner{count: 256, cancel: cancel}

	_, err = collectBoundaryRecallRecords(ctx, scope, scanner, boundaryTestHydrate)
	if err == nil {
		t.Fatal("cancelled view succeeded")
	}
}

type oversizeBoundaryInvoker struct{ calls int }

func (i *oversizeBoundaryInvoker) Invoke(context.Context,
	derive.StructuredInvocation) ([]byte,
	error) {
	i.calls++

	return []byte(`{"output":[]}`),
		nil
}
func (*oversizeBoundaryInvoker) Close(context.Context) error { return nil }

func assertBoundarySerializedLimit(t *testing.T, snapshot m.ScopeSnapshot) {
	t.Helper()

	invoker := &oversizeBoundaryInvoker{}
	deriver, err := derive.NewDeriver(invoker)
	requireTraceNoError(t, err, "deriver: %v")
	boundary,
		err := m.NewBoundary(snapshot.Scope,
		m.ConversationRef{ConversationID: "conversation",
			AgentConversationID: "agent"},
		"boundary",
		m.BoundaryReasonClose,
		time.Now().UTC())
	requireTraceNoError(t, err, "boundary: %v")

	request := m.ScenarioSynthesisRequest{SchemaVersion: m.DerivedSchemaVersionV1,
		Derivation: m.LegacyDerivationRef(),
		Boundary:   boundary,
		View: m.ScopeView{SchemaVersion: snapshot.SchemaVersion,
			Scope:     snapshot.Scope,
			Version:   snapshot.Version,
			Atoms:     snapshot.Atoms,
			Scenarios: snapshot.Scenarios,
			Profiles:  snapshot.Profiles}}
	_, err = deriver.SynthesizeScenarios(context.Background(), request)

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeLimitExceeded || invoker.calls != 0 {
		t.Fatalf("2048 valid records violated byte budget: calls=%d err=%v",
			invoker.calls,
			err)
	}
}

func TestBoundaryCollectorRejectsUnexpectedHydratedRevision(t *testing.T) {
	t.Parallel()

	scope := m.Scope{Key: "test:hydration", Kind: m.ScopeKindPersonal}

	for _, change := range []string{"revision", "item", "duplicate", "inactive"} {
		t.Run(change, func(t *testing.T) {
			hydrate := func(ctx context.Context, requested m.Scope, page []m.ActiveCanonicalMemory) ([]m.RecallRecord, error) {
				records, err := boundaryTestHydrate(ctx, requested, page)

				switch change {
				case "revision":
					records[0].RevisionID = "unrequested-revision"
				case "item":
					records[0].ItemID = "unrequested-item"
				case "duplicate":
					records = append(records, records[0])
				case "inactive":
					records[0].State = m.RevisionStateSuperseded
				}

				return records, err
			}

			_, err := collectBoundaryRecallRecords(context.Background(), scope, &boundaryTestScanner{count: 1}, hydrate)
			if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeStoreFailure {
				t.Fatalf("unrequested/inactive hydration accepted: %v", err)
			}
		})
	}
}

func TestBoundaryCollectorBoundsHeadsWhenHydrationOmitsRecords(t *testing.T) {
	t.Parallel()

	scanner := &boundaryTestScanner{count: m.MaxSnapshotItems + 1}
	hydrate := func(context.Context, m.Scope, []m.ActiveCanonicalMemory) ([]m.RecallRecord, error) {
		return nil, nil
	}

	_, err := collectBoundaryRecallRecords(context.Background(),
		m.Scope{Key: "test:omitted-hydration", Kind: m.ScopeKindPersonal}, scanner, hydrate)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeLimitExceeded {
		t.Fatalf("omitted hydration bypassed active-head budget: calls=%d error=%v", scanner.calls, err)
	}
}
