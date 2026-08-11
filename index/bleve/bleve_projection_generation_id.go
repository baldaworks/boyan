package bleve

import "strings"

func validBleveGenerationID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "/\\\r\n\t")
}
