package memory

import (
	"testing"
	"time"
)

func TestNewBoundaryBuildsStableExport(t *testing.T) {
	t.Parallel()

	scope, conversation := testIdentity()
	occurredAt := time.Date(2026, time.August, 3, 4, 5, 6, 0, time.UTC)
	boundary, err := NewBoundary(scope, conversation, "reset-42", BoundaryReasonReset, occurredAt)
	if err != nil {
		t.Fatalf("NewBoundary() error = %v", err)
	}
	wantID, err := BoundaryExportID(scope, conversation, "reset-42")
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
		scope    Scope
		wantCode ErrorCode
	}{
		{name: "personal", scope: Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}},
		{name: "personal topic", scope: Scope{Key: "telegram:1:77", Kind: ScopeKindPersonal}},
		{name: "group topic", scope: Scope{Key: "telegram:-100:42", Kind: ScopeKindGroup}},
		{name: "leading whitespace", scope: Scope{Key: " telegram:1:0", Kind: ScopeKindPersonal}, wantCode: CodeInvalidScope},
		{name: "uppercase channel", scope: Scope{Key: "Telegram:1:0", Kind: ScopeKindPersonal}, wantCode: CodeInvalidScope},
		{name: "non-canonical address", scope: Scope{Key: "telegram: 1:0", Kind: ScopeKindPersonal}, wantCode: CodeInvalidScope},
		{name: "missing address", scope: Scope{Key: "telegram:", Kind: ScopeKindPersonal}, wantCode: CodeInvalidScope},
		{name: "unknown kind", scope: Scope{Key: "telegram:1:0", Kind: "shared"}, wantCode: CodeUnsupportedScope},
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
			code, class, ok := ClassifyError(err)
			if !ok || code != tt.wantCode || class != ErrorClassPermanent {
				t.Fatalf("ClassifyError() = %q, %q, %v; want %q, permanent, true", code, class, ok, tt.wantCode)
			}
		})
	}
}
