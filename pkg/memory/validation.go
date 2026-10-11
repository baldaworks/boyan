package memory

import "strings"

// Validate verifies that scope is an exact canonical locator-shaped key.
func (s Scope) Validate() error {
	if strings.TrimSpace(s.Key) != s.Key || s.Key == "" {
		return PermanentError(CodeInvalidScope, "canonical scope key is required", nil)
	}

	if err := validateScopeLocator(s.Key); err != nil {
		return err
	}

	switch s.Kind {
	case ScopeKindPersonal, ScopeKindGroup:
		return nil
	default:
		return PermanentError(CodeUnsupportedScope, "scope kind must be personal or group", nil)
	}
}

// Validate verifies stable standalone conversation and runtime identity.
func (s ConversationRef) Validate() error {
	if !isCanonicalID(s.ConversationID) {
		return PermanentError(CodeInvalidConversation, "conversation id is required", nil)
	}

	if !isCanonicalID(s.AgentConversationID) {
		return PermanentError(CodeInvalidConversation, "agent conversation id is required", nil)
	}

	if s.LineageID != "" && !isCanonicalID(s.LineageID) {
		return PermanentError(CodeInvalidConversation, "lineage id must be canonical", nil)
	}

	if s.PreviousConversationID != "" && !isCanonicalID(s.PreviousConversationID) {
		return PermanentError(CodeInvalidConversation, "previous conversation id must be canonical", nil)
	}

	return nil
}

func isCanonicalID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\r\n\t")
}

func validateScopeLocator(key string) error {
	channel, address, ok := strings.Cut(key, ":")
	if !ok || channel == "" || address == "" || address != strings.TrimSpace(address) ||
		channel != strings.ToLower(channel) || strings.ContainsAny(channel, " \t\r\n") {
		return PermanentError(CodeInvalidScope, "scope key must be canonical <channel_type>:<address_key>", nil)
	}

	return nil
}
