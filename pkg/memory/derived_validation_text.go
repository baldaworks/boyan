package memory

import "strings"

func validateDerivedKey(name, value string) (string, error) {
	if len(value) > MaxSearchQueryBytes {
		return "", limitExceeded(name + " exceeds the size limit")
	}
	normalized := normalizeDerivedText(value)
	if normalized == "" {
		return "", invalidDerived(name + " is required")
	}
	return normalized, nil
}

func validateDerivedText(name, value string) (string, error) {
	if len(value) > MaxDerivedTextBytes {
		return "", limitExceeded(name + " exceeds the size limit")
	}
	normalized := normalizeDerivedText(value)
	if normalized == "" {
		return "", invalidDerived(name + " is required")
	}
	return normalized, nil
}

func normalizeDerivedText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
