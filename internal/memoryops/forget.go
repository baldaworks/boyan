package memoryops

// ForgetService is the portable mutating application boundary.  The
// canonical store adapter owns the atomic denial/cascade transaction; this
// service keeps the capability explicit and prevents transport packages from
// reaching into concrete storage.
type ForgetService struct {
	operations Forgetter
	projection ProjectionSyncer
}

var _ Forgetter = (*ForgetService)(nil)
