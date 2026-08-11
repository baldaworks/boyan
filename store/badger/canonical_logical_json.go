package badger

import (
	"encoding/json"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func writeCanonicalLogicalJSON(writer io.Writer, value any) error {
	line, err := marshalCanonicalLogicalJSON(value)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = writer.Write(line)
	return err
}

func marshalCanonicalLogicalJSON(value any) ([]byte, error) {
	line, err := json.Marshal(value)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "encode canonical logical export record", err)
	}
	return line, nil
}
