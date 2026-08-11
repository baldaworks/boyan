package projectionsync

import (
	"context"
	"testing"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeViewBuilderKeepsActiveRecentAndChangedReadsBounded(t *testing.T) {
	scope := Scope{Key: "telegram:view", Kind: ScopeKindPersonal}
	store := &boundedProjectionStore{
		state: ScopeState{SchemaVersion: CanonicalSchemaVersionV1, Scope: scope, Version: 4, ChangeSeq: 4},
		active: []ActiveCanonicalMemory{
			{Item: projectionViewItem(scope, "item-1"), RevisionID: "revision-1"},
			{Item: projectionViewItem(scope, "item-2"), RevisionID: "revision-2"},
		},
		changes: []ScopeChange{
			{Sequence: 1, OperationID: "operation-1", OccurredAt: projectionViewTime(1), RevisionIDs: []string{"revision-1"}},
			{Sequence: 2, OperationID: "operation-2", OccurredAt: projectionViewTime(2), RevisionIDs: []string{"revision-2"}},
			{Sequence: 3, OperationID: "operation-3", OccurredAt: projectionViewTime(3), RevisionIDs: []string{"revision-3"}},
		},
		revisions: map[string]MemoryRevision{
			"revision-1": projectionViewRevision("revision-1", "item-1", 1),
			"revision-2": projectionViewRevision("revision-2", "item-2", 1),
			"revision-3": projectionViewRevision("revision-3", "item-3", 1),
		},
	}
	builder, err := NewScopeViewBuilder(store)
	if err != nil {
		t.Fatalf("NewScopeViewBuilder() error = %v", err)
	}
	view, err := builder.Build(context.Background(), ProjectionViewRequest{
		Scope:        scope,
		ActiveLimit:  1,
		RecentAfter:  1,
		RecentLimit:  1,
		ChangedAfter: 2,
		ChangedLimit: 1,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(view.Active) != 1 || view.Active[0].Item.ItemID != "item-1" {
		t.Fatalf("active view = %#v", view.Active)
	}
	if !view.ActiveTruncated {
		t.Fatal("active view did not report a bounded continuation")
	}
	if len(view.Recent) != 1 || view.Recent[0].RevisionID != "revision-2" {
		t.Fatalf("recent view = %#v", view.Recent)
	}
	if len(view.Changed) != 1 || view.Changed[0].RevisionID != "revision-3" {
		t.Fatalf("changed view = %#v", view.Changed)
	}
	if err := view.Validate(); err != nil {
		t.Fatalf("view.Validate() error = %v", err)
	}
}
