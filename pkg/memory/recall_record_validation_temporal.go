package memory

import "time"

func (r RecallRecord) validateRecallTemporalWindow(request RecallRequest, now time.Time) error {
	effective := now.UTC()
	if request.AsOf != nil {
		effective = request.AsOf.UTC()
	}

	if r.Temporal.ObservedAt.After(effective) {
		return PermanentError(CodeNotFound, "recall record was observed after effective time", nil)
	}

	if r.Temporal.ValidFrom != nil && effective.Before(r.Temporal.ValidFrom.UTC()) {
		return PermanentError(CodeNotFound, "recall record is not yet valid at effective time", nil)
	}

	if r.Temporal.ValidUntil != nil && !effective.Before(r.Temporal.ValidUntil.UTC()) {
		return PermanentError(CodeNotFound, "recall record is no longer valid at effective time", nil)
	}

	if r.Temporal.ExpiresAt != nil && !effective.Before(r.Temporal.ExpiresAt.UTC()) {
		return PermanentError(CodeNotFound, "recall record expired at effective time", nil)
	}

	return nil
}
