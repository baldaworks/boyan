package embedding

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type embeddingResponseEntry struct {
	index  *int
	vector []float32
}

type decodedEmbeddingResponse struct {
	model    string
	vectors  [][]float32
	seenData bool
}

func (provider *openAIProvider) decodeResponse(encoded []byte, inputCount int) ([][]float32, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if !readDelimiter(decoder, '{') {
		return nil, newError(CodeInvalidResponse)
	}

	result, err := provider.decodeResponseObject(decoder, inputCount)
	if err != nil {
		return nil, err
	}

	if !readDelimiter(decoder, '}') || !decoderAtEOF(decoder) {
		return nil, newError(CodeInvalidResponse)
	}

	if result.model != provider.model {
		return nil, newError(CodeModelMismatch)
	}

	if !result.seenData {
		return nil, newError(CodeInvalidResponse)
	}

	return result.vectors, nil
}

func (provider *openAIProvider) decodeResponseObject(
	decoder *json.Decoder,
	inputCount int,
) (decodedEmbeddingResponse, error) {
	var result decodedEmbeddingResponse

	for decoder.More() {
		key, ok := readObjectKey(decoder)
		if !ok {
			return decodedEmbeddingResponse{}, newError(CodeInvalidResponse)
		}

		if err := provider.decodeResponseField(decoder, key, inputCount, &result); err != nil {
			return decodedEmbeddingResponse{}, err
		}
	}

	return result, nil
}

func (provider *openAIProvider) decodeResponseField(
	decoder *json.Decoder,
	key string,
	inputCount int,
	result *decodedEmbeddingResponse,
) error {
	switch key {
	case "model":
		if err := decoder.Decode(&result.model); err != nil {
			return newError(CodeInvalidResponse)
		}
	case "data":
		if result.seenData {
			return newError(CodeInvalidResponse)
		}

		vectors, err := provider.decodeData(decoder, inputCount)
		if err != nil {
			return err
		}

		result.vectors = vectors
		result.seenData = true
	default:
		if err := skipJSONValue(decoder); err != nil {
			return newError(CodeInvalidResponse)
		}
	}

	return nil
}

func (provider *openAIProvider) decodeData(decoder *json.Decoder, inputCount int) ([][]float32, error) {
	if !readDelimiter(decoder, '[') {
		return nil, newError(CodeInvalidResponse)
	}

	vectors := make([][]float32, inputCount)
	entryCount := 0

	for decoder.More() {
		if entryCount >= inputCount {
			return nil, newError(CodeInvalidResponse)
		}

		entry, err := provider.decodeEntry(decoder)
		if err != nil {
			return nil, err
		}

		if invalidIndex(entry.index, vectors) {
			return nil, newError(CodeInvalidResponse)
		}

		vectors[*entry.index] = entry.vector
		entryCount++
	}

	if !readDelimiter(decoder, ']') || entryCount != inputCount {
		return nil, newError(CodeInvalidResponse)
	}

	return vectors, nil
}

func (provider *openAIProvider) decodeEntry(decoder *json.Decoder) (embeddingResponseEntry, error) {
	if !readDelimiter(decoder, '{') {
		return embeddingResponseEntry{}, newError(CodeInvalidResponse)
	}

	var entry embeddingResponseEntry

	seenVector := false

	for decoder.More() {
		key, ok := readObjectKey(decoder)
		if !ok {
			return embeddingResponseEntry{}, newError(CodeInvalidResponse)
		}

		if err := provider.decodeEntryField(decoder, key, &entry, &seenVector); err != nil {
			return embeddingResponseEntry{}, err
		}
	}

	if !readDelimiter(decoder, '}') {
		return embeddingResponseEntry{}, newError(CodeInvalidResponse)
	}

	if !seenVector {
		return embeddingResponseEntry{}, newError(CodeDimensionMismatch)
	}

	return entry, nil
}

func (provider *openAIProvider) decodeEntryField(
	decoder *json.Decoder,
	key string,
	entry *embeddingResponseEntry,
	seenVector *bool,
) error {
	switch key {
	case "index":
		if err := decoder.Decode(&entry.index); err != nil {
			return newError(CodeInvalidResponse)
		}
	case "embedding":
		if *seenVector {
			return newError(CodeInvalidResponse)
		}

		vector, err := provider.decodeVector(decoder)
		if err != nil {
			return err
		}

		entry.vector = vector
		*seenVector = true
	default:
		if err := skipJSONValue(decoder); err != nil {
			return newError(CodeInvalidResponse)
		}
	}

	return nil
}

func (provider *openAIProvider) decodeVector(decoder *json.Decoder) ([]float32, error) {
	if err := requireVectorStart(decoder); err != nil {
		return nil, err
	}

	components := make([]*float64, 0, provider.dimensions)
	for decoder.More() {
		if len(components) >= provider.dimensions {
			return nil, newError(CodeDimensionMismatch)
		}

		var component *float64
		if err := decoder.Decode(&component); err != nil || component == nil {
			return nil, newError(CodeInvalidResponse)
		}

		components = append(components, component)
	}

	if !readDelimiter(decoder, ']') || len(components) != provider.dimensions {
		return nil, newError(CodeDimensionMismatch)
	}

	return normalize(components)
}

func requireVectorStart(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return newError(CodeInvalidResponse)
	}

	if token == nil {
		return newError(CodeDimensionMismatch)
	}

	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '[' {
		return newError(CodeInvalidResponse)
	}

	return nil
}

func invalidIndex(index *int, vectors [][]float32) bool {
	return index == nil || *index < 0 || *index >= len(vectors) || vectors[*index] != nil
}

func readDelimiter(decoder *json.Decoder, want json.Delim) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}

	delimiter, ok := token.(json.Delim)

	return ok && delimiter == want
}

func readObjectKey(decoder *json.Decoder) (string, bool) {
	token, err := decoder.Token()
	if err != nil {
		return "", false
	}

	key, ok := token.(string)

	return key, ok
}

func decoderAtEOF(decoder *json.Decoder) bool {
	_, err := decoder.Token()

	return err == io.EOF
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		return skipJSONObject(decoder)
	case '[':
		return skipJSONArray(decoder)
	default:
		return newError(CodeInvalidResponse)
	}
}

func skipJSONObject(decoder *json.Decoder) error {
	for decoder.More() {
		if _, ok := readObjectKey(decoder); !ok {
			return newError(CodeInvalidResponse)
		}

		if err := skipJSONValue(decoder); err != nil {
			return err
		}
	}

	if !readDelimiter(decoder, '}') {
		return newError(CodeInvalidResponse)
	}

	return nil
}

func skipJSONArray(decoder *json.Decoder) error {
	for decoder.More() {
		if err := skipJSONValue(decoder); err != nil {
			return err
		}
	}

	if !readDelimiter(decoder, ']') {
		return newError(CodeInvalidResponse)
	}

	return nil
}

func statusError(status int, body []byte) error {
	if status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge {
		return newError(CodeInputLimit)
	}

	if status == http.StatusUnprocessableEntity && isTEIValidationFailure(body) {
		return newError(CodeInputLimit)
	}

	return newError(CodeUnavailable)
}

func isTEIValidationFailure(body []byte) bool {
	var failure struct {
		Code *int   `json:"code"`
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &failure) != nil || failure.Code == nil {
		return false
	}

	return *failure.Code == http.StatusUnprocessableEntity && failure.Type == "Validation"
}
