package httpingest

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func decodeEnvelope[T any](body []byte, dst *T) error {
	if len(body) == 0 {
		return memory.PermanentError(memory.CodeInvalidDerived, "request body is required", nil)
	}

	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "request body is invalid", err)
	}

	if err := decoder.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return memory.PermanentError(memory.CodeInvalidDerived, "request body must contain exactly one JSON object", err)
	}

	return nil
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)

	return err == nil && mediaType == "application/json"
}
