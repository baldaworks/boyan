package memory

func (r RecallRecord) validateRecallFilters(request RecallRequest) error {
	if request.Kind != nil && r.Kind != *request.Kind {
		return PermanentError(CodeNotFound, "recall record does not match kind filter", nil)
	}

	if request.Category != nil && (r.Category == nil || *r.Category != *request.Category) {
		return PermanentError(CodeNotFound, "recall record does not match category filter", nil)
	}

	if request.MemoryKey != "" && r.MemoryKey != request.MemoryKey {
		return PermanentError(CodeNotFound, "recall record does not match memory filter", nil)
	}

	return r.validateRecallSourceFilters(request)
}

func (r RecallRecord) validateRecallSourceFilters(request RecallRequest) error {
	if request.SourceID != "" && !containsString(r.SourceIDs, request.SourceID) {
		return PermanentError(CodeNotFound, "recall record does not match source filter", nil)
	}

	if request.ConversationID != "" &&
		!containsString(r.ConversationIDs, request.ConversationID) {
		return PermanentError(CodeNotFound, "recall record does not match conversation filter", nil)
	}

	return nil
}
