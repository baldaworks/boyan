package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

func mustCanonicalFingerprint(exportID string, derivation DerivationRef) string {
	return reconciliationID("fingerprint", exportID, derivation.Pipeline, derivation.Policy, derivation.Prompt, derivation.Model)
}
