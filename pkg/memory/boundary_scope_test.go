package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"testing"
	"time"
)

func TestNewBoundaryBuildsStableExport(t *testing.T) {
	t.Parallel()

	scope, conversation := testIdentity()
	occurredAt := time.Date(2026, time.August, 3, 4, 5, 6, 0, time.UTC)

	boundary, err := memory.NewBoundary(scope, conversation, "reset-42", memory.BoundaryReasonReset, occurredAt)
	if err != nil {
		t.Fatalf("NewBoundary() error = %v", err)
	}

	wantID, err := memory.BoundaryExportID(scope, conversation, "reset-42")
	if err != nil {
		t.Fatalf("BoundaryExportID() error = %v", err)
	}

	if boundary.ExportID != wantID {
		t.Fatalf("ExportID = %q, want %q", boundary.ExportID, wantID)
	}

	if err := boundary.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestScopeValidateUsesExactLocatorAndKnownKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		scope    memory.Scope
		wantCode memory.ErrorCode
	}{
		{name: "personal", scope: memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}},
		{name: "personal topic", scope: memory.Scope{Key: "telegram:1:77", Kind: memory.ScopeKindPersonal}},
		{name: "group topic", scope: memory.Scope{Key: "telegram:-100:42", Kind: memory.ScopeKindGroup}},
		{
			name:     "leading whitespace",
			scope:    memory.Scope{Key: " telegram:1:0", Kind: memory.ScopeKindPersonal},
			wantCode: memory.CodeInvalidScope,
		},
		{
			name:     "uppercase channel",
			scope:    memory.Scope{Key: "Telegram:1:0", Kind: memory.ScopeKindPersonal},
			wantCode: memory.CodeInvalidScope,
		},
		{
			name:     "non-canonical address",
			scope:    memory.Scope{Key: "telegram: 1:0", Kind: memory.ScopeKindPersonal},
			wantCode: memory.CodeInvalidScope,
		},
		{
			name:     "missing address",
			scope:    memory.Scope{Key: "telegram:", Kind: memory.ScopeKindPersonal},
			wantCode: memory.CodeInvalidScope,
		},
		{
			name:     "unknown kind",
			scope:    memory.Scope{Key: "telegram:1:0", Kind: "shared"},
			wantCode: memory.CodeUnsupportedScope,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.scope.Validate()
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}

			code, class, ok := memory.ClassifyError(err)
			if !ok || code != tt.wantCode || class != memory.ErrorClassPermanent {
				t.Fatalf(
					"ClassifyError() = %q, %q, %v; want %q, permanent, true",
					code,
					class,
					ok,
					tt.wantCode,
				)
			}
		})
	}
}
