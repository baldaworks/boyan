package memory

func validateCanonicalMutationLifecycle(events []LifecycleEvent) error {
	lifecycleIDs := make([]string, 0, len(events))
	for _, event := range events {
		if err := event.Validate(); err != nil {
			return err
		}
		lifecycleIDs = append(lifecycleIDs, event.EventID)
	}
	return validateUniqueCanonicalIDs(lifecycleIDs, "canonical lifecycle event")
}

func validateCanonicalMutationHeads(heads []ItemHead) error {
	headItemIDs := make([]string, 0, len(heads))
	for _, head := range heads {
		if err := head.Validate(); err != nil {
			return err
		}
		headItemIDs = append(headItemIDs, head.ItemID)
	}
	return validateUniqueCanonicalIDs(headItemIDs, "canonical item head")
}

func validateCanonicalMutationDelivery(delivery []DeliveryOutboxRecord) error {
	deliveryIDs := make([]string, 0, len(delivery))
	for _, record := range delivery {
		if err := record.Validate(); err != nil {
			return err
		}
		deliveryIDs = append(deliveryIDs, record.DeliveryID)
	}
	return validateUniqueCanonicalIDs(deliveryIDs, "canonical delivery outbox")
}

func validateCanonicalMutationPayloads(payloads []CanonicalPayload) error {
	payloadIDs := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		if err := payload.Validate(); err != nil {
			return err
		}
		payloadIDs = append(payloadIDs, payload.Ref.ID)
	}
	return validateUniqueCanonicalIDs(payloadIDs, "canonical payload")
}
