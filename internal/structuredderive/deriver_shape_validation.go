package structuredderive

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxWireDepth = 32

func validatedOutput(raw []byte, stage string) ([]byte, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, ok := readWireValue(decoder, 0)

	if !ok {
		return nil, false
	}

	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}

	if !stageShape(stage).accepts(value) {
		return nil, false
	}
	// JSON Schema integers include 1.0 and 1e0; Go's uint32 decoder only
	// accepts digit lexemes. Preserve their exact value through canonical JSON.
	normalized, err := json.Marshal(value)

	return normalized, err == nil
}

// Read member names before storing values: maps and ordinary struct decoding
// would erase duplicates, including distinct escape spellings of the same name.
func readWireValue(decoder *json.Decoder, depth int) (any, bool) {
	if depth > maxWireDepth {
		return nil, false
	}

	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}

	delimiter, container := token.(json.Delim)

	if !container {
		if number, ok := token.(json.Number); ok {
			if integer, ok := uint32Number(string(number)); ok {
				token = json.Number(strconv.FormatUint(integer, 10))
			}
		}

		return token, true
	}

	switch delimiter {
	case '{':
		return readWireObject(decoder, depth)
	case '[':
		return readWireArray(decoder, depth)
	default:
		return nil, false
	}
}

func readWireObject(decoder *json.Decoder, depth int) (any, bool) {
	object := make(map[string]any)

	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, false
		}

		key, ok := keyToken.(string)

		if !ok {
			return nil, false
		}

		if _, exists := object[key]; exists {
			return nil, false
		}

		value, ok := readWireValue(decoder, depth+1)

		if !ok {
			return nil, false
		}

		object[key] = value
	}

	end, err := decoder.Token()

	return object, err == nil && end == json.Delim('}')
}

func readWireArray(decoder *json.Decoder, depth int) (any, bool) {
	array := make([]any, 0)

	for decoder.More() {
		value, ok := readWireValue(decoder, depth+1)

		if !ok {
			return nil, false
		}

		array = append(array, value)
	}

	end, err := decoder.Token()

	return array, err == nil && end == json.Delim(']')
}

func (s wireShape) accepts(value any) bool {
	if len(s.AnyOf) > 0 {
		return s.acceptsAlternative(value)
	}

	if len(s.Enum) > 0 && !slices.Contains(s.Enum, value) {
		return false
	}

	switch s.Type {
	case "null":
		return value == nil
	case "object":
		return s.acceptsObject(value)
	case "array":
		return s.acceptsArray(value)
	case "string":
		return s.acceptsString(value)
	case "integer":
		return s.acceptsInteger(value)
	default:
		return false
	}
}

// uint32Number checks a decimal JSON number exactly, without rounding or
// allocating according to an untrusted exponent.
func uint32Number(number string) (uint64, bool) {
	negative := strings.HasPrefix(number, "-")
	number = strings.TrimPrefix(number, "-")
	mantissa, exponentText, hasExponent := strings.Cut(number, "e")

	if !hasExponent {
		mantissa, exponentText, hasExponent = strings.Cut(number, "E")
	}

	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")

	if digits == "" {
		return 0, true
	}

	if negative {
		return 0, false
	}

	exponent := 0

	if hasExponent {
		var err error

		exponent, err = strconv.Atoi(exponentText)

		if err != nil || exponent > len(number)+10 || exponent < -len(number)-10 {
			return 0, false
		}
	}

	scale := exponent - len(fraction)
	trimmed := strings.TrimRight(digits, "0")
	scale += len(digits) - len(trimmed)

	if scale < 0 || len(trimmed)+scale > 10 {
		return 0, false
	}

	integer, err := strconv.ParseUint(trimmed+strings.Repeat("0", scale), 10, 32)

	return integer, err == nil
}

func (s wireShape) acceptsObject(value any) bool {
	object, ok := value.(map[string]any)

	if !ok {
		return false
	}

	for _, key := range s.Required {
		if _, exists := object[key]; !exists {
			return false
		}
	}

	for key, value := range object {
		field, exists := s.Properties[key]

		if !exists || !field.accepts(value) {
			return false
		}
	}

	return true
}

func (s wireShape) acceptsArray(value any) bool {
	array, ok := value.([]any)

	if !ok {
		return false
	}

	if s.MinItems != nil && len(array) < *s.MinItems || s.MaxItems != nil && len(array) > *s.MaxItems {
		return false
	}

	for _, item := range array {
		if !s.Items.accepts(item) {
			return false
		}
	}

	return true
}

func (s wireShape) acceptsString(value any) bool {
	text, ok := value.(string)

	if !ok {
		return false
	}

	if s.MinLength != nil && utf8.RuneCountInString(text) < *s.MinLength {
		return false
	}

	if s.Pattern != "" {
		matched, err := regexp.MatchString(s.Pattern, text)

		if err != nil || !matched {
			return false
		}
	}

	if s.Format == "date-time" {
		_, err := time.Parse(time.RFC3339Nano, text)

		return err == nil
	}

	return true
}

func (s wireShape) acceptsInteger(value any) bool {
	number, ok := value.(json.Number)

	if !ok {
		return false
	}

	integer, err := strconv.ParseUint(string(number), 10, 32)

	return err == nil && (s.Minimum == nil || integer >= *s.Minimum) && (s.Maximum == nil || integer <= *s.Maximum)
}

func (s wireShape) acceptsAlternative(value any) bool {
	for _, branch := range s.AnyOf {
		if branch.accepts(value) {
			return true
		}
	}

	return false
}
