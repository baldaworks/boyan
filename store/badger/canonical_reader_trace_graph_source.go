package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *CanonicalReader) loadTraceSources(
	ctx context.Context,
	scope memory.Scope,
	view canonicalRecallView,
	sourcesByRef map[memory.SourceRef]memory.SourceRecord,
) error {
	for index, source := range view.sourceRefs {
		if _, exists := sourcesByRef[source]; exists {
			continue
		}

		sourceID := source.ExportID
		if index < len(view.sourceIDs) {
			sourceID = view.sourceIDs[index]
		}

		record, err := r.loadSourceRecord(ctx, scope, source, sourceID)
		if err != nil {
			return err
		}

		if record.Ref != source {
			return memory.PermanentError(memory.CodeScopeViolation, "canonical trace source identity does not match", nil)
		}

		sourcesByRef[source] = record
	}

	return nil
}
