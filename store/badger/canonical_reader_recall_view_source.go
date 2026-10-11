package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalViewSources(txn *badger.Txn, scope memory.Scope, view *canonicalRecallView) error {
	seenSources := make(map[string]struct{}, len(view.revision.Evidence))
	for _, evidence := range view.revision.Evidence {
		if _, exists := seenSources[evidence.SourceID]; exists {
			continue
		}

		seenSources[evidence.SourceID] = struct{}{}
		if err := checkDeniedSource(txn, scope, evidence.SourceID); err != nil {
			return err
		}

		source, err := loadCanonicalSourceRecord(txn, scope, evidence.SourceID)
		if err != nil {
			return err
		}

		payload, err := loadCanonicalSourcePayload(txn, source.Payload)
		if err != nil {
			return err
		}

		ref, conversationID, ok := canonicalSourceRef(payload, scope, evidence.SourceID)
		if ok {
			view.sourceRefs = append(view.sourceRefs, ref)

			view.sourceIDs = append(view.sourceIDs, evidence.SourceID)
			if conversationID != "" {
				view.conversations = appendUnique(view.conversations, conversationID)
			}
		}
	}

	return nil
}
