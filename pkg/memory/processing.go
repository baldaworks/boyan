package memory

// ProcessingOperationID derives the stable idempotency key for one typed
// processing stage. It remains part of the portable contract used by the
// canonical processor and boundary application.
func ProcessingOperationID(stage OperationStage, exportID string, derivations ...DerivationRef) (string, error) {
	if err := stage.Validate(); err != nil {
		return "", err
	}
	if !isCanonicalID(exportID) {
		return "", invalidDerived("processing export id is required")
	}
	derivation := LegacyDerivationRef()
	if len(derivations) > 1 {
		return "", invalidDerived("one derivation reference is allowed")
	}
	if len(derivations) == 1 {
		derivation = derivations[0]
	}
	if err := derivation.Validate(); err != nil {
		return "", err
	}
	if derivation == LegacyDerivationRef() {
		return derivedStableID("operation", string(stage), exportID), nil
	}
	return derivedStableID("operation", string(stage), exportID, derivation.Pipeline, derivation.Policy, derivation.Prompt, derivation.Model), nil
}
