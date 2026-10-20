package sqlite

import (
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

var lexicalTestTime = time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)

var lexicalTestScope = memory.Scope{Kind: memory.ScopeKindGroup, Key: "baseline:v1"}
