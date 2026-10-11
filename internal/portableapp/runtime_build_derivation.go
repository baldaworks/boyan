package portableapp

import memory "github.com/baldaworks/boyan/pkg/memory"

func resolveRuntimeDerivation(config RuntimeConfig) (memory.DerivationRef, error) {
	derivation := config.Derivation
	if derivation == (memory.DerivationRef{}) {
		derivation = memory.LegacyDerivationRef()
	}

	if err := derivation.Validate(); err != nil {
		return memory.DerivationRef{}, err
	}

	return derivation, nil
}
