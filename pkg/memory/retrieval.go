package memory

// ReferenceTrust classifies recalled content at the package boundary.
type ReferenceTrust string

const (
	// ReferenceTrustUntrusted marks content as data, never executable instructions.
	ReferenceTrustUntrusted ReferenceTrust = "untrusted_reference"
)
