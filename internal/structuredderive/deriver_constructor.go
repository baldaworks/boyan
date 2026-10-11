package structuredderive

import (
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// NewDeriver constructs a typed model adapter over a neutral invoker.
func NewDeriver(invoker StructuredInvoker) (*Deriver, error) {
	return NewDeriverWithConfig(DeriverConfig{Invoker: invoker})
}

// NewDeriverWithConfig constructs a bounded typed model adapter.
func NewDeriverWithConfig(config DeriverConfig) (*Deriver, error) {
	if config.Invoker == nil {
		return nil, fmt.Errorf("structured derivation invoker is required")
	}

	maxInput := config.MaxInputBytes
	if maxInput == 0 {
		maxInput = memory.MaxDerivedTurnTextBytes
	}

	if maxInput < 1 || maxInput > memory.MaxDerivedTurnTextBytes {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "derivation input bound is invalid", nil)
	}

	maxOutput := config.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = DefaultDerivationMaxOutputBytes
	}

	if maxOutput < 1 || maxOutput > DefaultDerivationMaxOutputBytes {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "derivation output bound is invalid", nil)
	}

	return &Deriver{invoker: config.Invoker, maxInputBytes: maxInput, maxOutputBytes: maxOutput}, nil
}
