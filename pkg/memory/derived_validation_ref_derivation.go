package memory

// Validate verifies a complete, content-free derivation identity.
func (r DerivationRef) Validate() error {
	for _, field := range []string{r.Pipeline, r.Policy, r.Prompt, r.Model} {
		if !isCanonicalID(field) {
			return invalidDerived("derivation fingerprint is required")
		}
	}
	return nil
}
