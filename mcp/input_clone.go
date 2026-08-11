package mcp

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func cloneMemoryKind(value *memory.MemoryKind) *memory.MemoryKind {
	if value == nil {
		return nil
	}
	copyOfValue := *value
	return &copyOfValue
}

func cloneCategory(value *memory.AtomCategory) *memory.AtomCategory {
	if value == nil {
		return nil
	}
	copyOfValue := *value
	return &copyOfValue
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyOfValue := *value
	return &copyOfValue
}
