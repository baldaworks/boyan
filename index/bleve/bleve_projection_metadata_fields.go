package bleve

import (
	"fmt"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func requiredBleveString(fields map[string]interface{}, name string) (string, error) {
	field, ok := fields[name]
	if !ok {
		return "", memory.PermanentError(memory.CodeStoreFailure, "bleve hit metadata is incomplete", nil)
	}
	text, ok := field.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", memory.PermanentError(memory.CodeStoreFailure, "bleve hit metadata has an invalid value", nil)
	}
	return text, nil
}

func requiredBleveUint64(fields map[string]interface{}, name, message string) (uint64, error) {
	text, err := requiredBleveString(fields, name)
	if err != nil {
		return 0, err
	}
	var value uint64
	if _, err := fmt.Sscan(text, &value); err != nil || value == 0 {
		return 0, memory.PermanentError(memory.CodeStoreFailure, message, err)
	}
	return value, nil
}
