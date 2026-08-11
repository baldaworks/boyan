package bleve

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *BleveCanonicalApplier) Apply(ctx context.Context, scope memory.Scope, generationID string, _ []memory.ScopeChange, revisions []memory.MemoryRevision) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	generation, err := a.generation(ctx, generationID)
	if err != nil {
		return err
	}
	if len(revisions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		ids = append(ids, revision.RevisionID)
	}
	records, err := a.reader.LoadRecallRecords(ctx, scope, ids)
	if err != nil {
		return err
	}
	byID := make(map[string]memory.RecallRecord, len(records))
	for _, record := range records {
		byID[record.RevisionID] = record
	}
	for _, revision := range revisions {
		record, ok := byID[revision.RevisionID]
		if !ok {
			if err := generation.Delete(ctx, revision.RevisionID); err != nil {
				return err
			}
			continue
		}
		category := record.Category
		document := memory.RecallProjectionDocument{
			Scope: record.Scope, ItemID: record.ItemID, RevisionID: record.RevisionID,
			Revision: record.Revision, Kind: record.Kind, Category: category,
			MemoryKey: record.MemoryKey, Text: record.Text, CreatedAt: record.CreatedAt,
			Temporal: record.Temporal, Sensitivity: record.Sensitivity, Retention: record.Retention,
			SourceIDs: append([]string(nil), record.SourceIDs...), ConversationIDs: append([]string(nil), record.ConversationIDs...),
			ScopeChangeSeq: record.ScopeChangeSeq,
		}
		if err := generation.Index(ctx, document); err != nil {
			return err
		}
	}
	return nil
}
