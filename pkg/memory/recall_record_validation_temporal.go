package memory

import "time"

func (r RecallRecord) validateRecallTemporalWindow(request RecallRequest, now time.Time) error {
	if request.AsOf != nil {
		asOf := request.AsOf.UTC()
		if r.Temporal.ValidFrom != nil && asOf.Before(r.Temporal.ValidFrom.UTC()) {
			return PermanentError(CodeNotFound, "recall record is not valid at as_of", nil)
		}
		if r.Temporal.ValidUntil != nil && asOf.After(r.Temporal.ValidUntil.UTC()) {
			return PermanentError(CodeNotFound, "recall record expired at as_of", nil)
		}
		if r.Temporal.ExpiresAt != nil && !asOf.Before(r.Temporal.ExpiresAt.UTC()) {
			return PermanentError(CodeNotFound, "recall record expired at as_of", nil)
		}
		return nil
	}
	if r.Temporal.ExpiresAt != nil && now.After(r.Temporal.ExpiresAt.UTC()) {
		return PermanentError(CodeNotFound, "recall record has expired", nil)
	}
	return nil
}
