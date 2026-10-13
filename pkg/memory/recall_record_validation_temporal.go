package memory

import "time"

func (r RecallRecord) validateRecallTemporalWindow(request RecallRequest, now time.Time) error {
	effective := now.UTC()
	if request.AsOf != nil {
		effective = request.AsOf.UTC()
	}

	if !TemporalEligibleAt(r.Temporal, effective) {
		return PermanentError(CodeNotFound, "recall record is not eligible at effective time", nil)
	}

	return nil
}
