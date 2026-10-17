package semantic

// CandidateDisposition describes the exact owned candidate observed by release.
type CandidateDisposition string

const (
	CandidateMissing   CandidateDisposition = "missing"
	CandidateReleased  CandidateDisposition = "released"
	CandidatePublished CandidateDisposition = "published"
)

// CandidateRelease classifies release without granting authority over peers.
// Epoch is positive only for Published; that disposition never deletes rows.
type CandidateRelease struct {
	Disposition CandidateDisposition
	Epoch       uint64
}
