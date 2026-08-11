package forgetops

import memory "github.com/baldaworks/boyan/pkg/memory"

func sourceForgetOperationID(command memory.ForgetSourceCommand) (string, error) {
	return memory.CanonicalForgetOperationID(memory.ForgetKindSource, command.Source.Scope, command.Source, "")
}
