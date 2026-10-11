package memory

import "strings"

// CanonicalForgetOperationID derives a stable exact-scope operation identity
// from the public command. It is deliberately content-free and independent of
// caller timestamps so retries with a different wall clock replay exactly.
func CanonicalForgetOperationID(kind ForgetKind, scope Scope, source SourceRef, requestID string) (string, error) {
	if err := kind.Validate(); err != nil {
		return "", err
	}

	if err := scope.Validate(); err != nil {
		return "", err
	}

	parts := []string{"canonical-forget", string(kind), string(scope.Kind), scope.Key}
	if kind == ForgetKindSource {
		if err := source.Validate(); err != nil {
			return "", err
		}

		if source.Scope != scope {
			return "", PermanentError(CodeScopeViolation, "canonical forget source scope does not match", nil)
		}

		parts = append(parts, source.ExportID, source.ConversationID, source.SourceTurnID)
	} else {
		requestID = strings.TrimSpace(requestID)
		if !isCanonicalID(requestID) {
			return "", invalidDerived("canonical scope forget request id is required")
		}

		parts = append(parts, requestID)
	}

	return reconciliationID(parts[0], parts[1:]...), nil
}

// CanonicalForgetFingerprint binds the exact request shape to the operation
// identity without including mutable timestamps.
func CanonicalForgetFingerprint(
	operationID string,
	scope Scope,
	kind ForgetKind,
	source SourceRef,
	requestID string,
) string {
	parts := []string{
		operationID,
		string(kind),
		scope.Key,
		string(scope.Kind),
		source.ExportID,
		source.ConversationID,
		source.SourceTurnID,
		strings.TrimSpace(requestID),
	}

	return reconciliationID("canonical-forget-fingerprint", parts...)
}
