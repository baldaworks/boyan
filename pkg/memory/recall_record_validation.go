package memory

import "time"

// Validate verifies hydrated canonical recall data before it is exposed.
func (r RecallRecord) Validate(request RecallRequest, now time.Time) error {
	if err := r.validateRecallIdentity(request); err != nil {
		return err
	}
	if err := r.validateRecallLegacyCompatibility(); err != nil {
		return err
	}
	if err := r.validateRecallFilters(request); err != nil {
		return err
	}
	if err := r.validateRecallProvenance(); err != nil {
		return err
	}
	return r.validateRecallTemporalWindow(request, now)
}
