package memory

func validateCanonicalMutationSources(scope Scope, sources []CanonicalSourceRecord) error {
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return err
		}

		if source.Scope != scope {
			return invalidDerived("canonical source scope does not match mutation")
		}

		sourceIDs = append(sourceIDs, source.SourceID)
	}

	return validateUniqueCanonicalIDs(sourceIDs, "canonical source")
}

func validateCanonicalMutationMessages(messages []MessageRecord) error {
	messageIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		if err := message.Validate(); err != nil {
			return err
		}

		messageIDs = append(messageIDs, message.MessageID)
	}

	return validateUniqueCanonicalIDs(messageIDs, "canonical message")
}

func validateCanonicalMutationItems(scope Scope, items []MemoryItem) error {
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		if err := item.Validate(); err != nil {
			return err
		}

		if item.Scope != scope {
			return invalidDerived("canonical item scope does not match mutation")
		}

		itemIDs = append(itemIDs, item.ItemID)
	}

	return validateUniqueCanonicalIDs(itemIDs, "canonical item")
}

func validateCanonicalMutationRevisions(revisions []MemoryRevision) error {
	revisionIDs := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		if err := revision.Validate(); err != nil {
			return err
		}

		revisionIDs = append(revisionIDs, revision.RevisionID)
	}

	return validateUniqueCanonicalIDs(revisionIDs, "canonical revision")
}
