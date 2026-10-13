package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type boundaryActiveScanner interface {
	ScanActiveMemory(ctx context.Context, request memory.ActiveMemoryScanRequest) ([]memory.ActiveCanonicalMemory, error)
}

type boundaryHydrator func(context.Context, memory.Scope, []memory.ActiveCanonicalMemory) ([]memory.RecallRecord, error)

func collectBoundaryRecallRecords(ctx context.Context,
	scope memory.Scope,
	scanner boundaryActiveScanner,
	hydrate boundaryHydrator) ([]memory.RecallRecord,
	error) {
	records := make([]memory.RecallRecord, 0, memory.MaxSnapshotItems)
	cursor := ""

	for {
		if err := canonicalReaderContext(ctx); err != nil {
			return nil, err
		}

		page,
			err := scanner.ScanActiveMemory(ctx,
			memory.ActiveMemoryScanRequest{Scope: scope,
				AfterItemID: cursor,
				Limit:       min(memory.MaxCanonicalReadRecords, uint32(memory.MaxSnapshotItems-len(records)+1))})
		if err != nil {
			return nil, err
		}

		if len(page) == 0 {
			return records, canonicalReaderContext(ctx)
		}

		if err := validateBoundaryPage(scope, cursor, page); err != nil {
			return nil, err
		}

		hydrated, err := hydrate(ctx, scope, page)
		if err != nil {
			return nil, err
		}

		if len(records)+len(hydrated) > memory.MaxSnapshotItems {
			return nil,
				memory.PermanentError(memory.CodeLimitExceeded,
					"canonical boundary view exceeds model item limit",
					nil)
		}

		if err := validateBoundaryHydration(scope, hydrated); err != nil {
			return nil, err
		}

		records = append(records, hydrated...)
		cursor = page[len(page)-1].Item.ItemID

		if err := canonicalReaderContext(ctx); err != nil {
			return nil, err
		}
	}
}

func validateBoundaryPage(scope memory.Scope, cursor string, page []memory.ActiveCanonicalMemory) error {
	if len(page) > memory.MaxCanonicalReadRecords {
		return memory.PermanentError(memory.CodeLimitExceeded,
			"boundary scan page exceeds limit",
			nil)
	}

	for _, active := range page {
		if err := active.Item.Validate(); err != nil {
			return err
		}

		if active.Item.Scope != scope {
			return memory.PermanentError(memory.CodeScopeViolation,
				"boundary scan scope does not match",
				nil)
		}

		id := active.Item.ItemID
		// TupleKey orders by byte length before the ID contents. Treat the cursor
		// in that same encoded order; raw lexical ordering loses mixed-kind IDs.
		if len(id) < len(cursor) || len(id) == len(cursor) && id <= cursor {
			return memory.PermanentError(memory.CodeStoreFailure,
				"boundary cursor did not advance",
				nil)
		}

		cursor = id
	}

	return nil
}

func validateBoundaryHydration(scope memory.Scope, hydrated []memory.RecallRecord) error {
	for _, record := range hydrated {
		if record.Scope != scope {
			return memory.PermanentError(memory.CodeScopeViolation,
				"boundary record scope does not match",
				nil)
		}
	}

	return nil
}
