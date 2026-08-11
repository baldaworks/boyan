package mcp

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func forgetSuccess(outcome memory.ForgetOutcome, forgottenAt time.Time) ForgetOutput {
	scope := outcome.Scope
	return ForgetOutput{
		ToolOutcome:  ToolOutcome{OK: true},
		Scope:        &scope,
		OperationID:  outcome.OperationID,
		Kind:         outcome.Kind,
		ScopeVersion: outcome.ScopeVersion,
		ForgottenAt:  &forgottenAt,
		Sources:      append([]memory.SourceRef(nil), outcome.Sources...),
		Revisions:    append([]memory.RevisionRef(nil), outcome.Revisions...),
		Notice:       "Forget outcomes are content-free durable acknowledgements. No recalled content is returned.",
	}
}
