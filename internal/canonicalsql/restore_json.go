package canonicalsql

import (
	"bytes"
	"encoding/json"
	"io"
)

func decodeRestoreJSON(data []byte, target any) error {
	if err := validateRestoreJSON(data); err != nil {
		return err
	}

	return decodeCanonicalProofJSON(data, target)
}

func validateRestoreJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()

	if err := restoreJSONValue(d, 0); err != nil {
		return invalid("invalid or duplicate canonical archive JSON")
	}

	if _, err := d.Token(); err != io.EOF {
		return invalid("trailing canonical archive JSON")
	}

	return nil
}

//nolint:cyclop // Traverse objects and arrays while rejecting duplicate keys at every depth.
func restoreJSONValue(d *json.Decoder, depth int) error {
	const maxRestoreJSONDepth = 64
	if depth > maxRestoreJSONDepth {
		return limitError()
	}

	token, err := d.Token()
	if err != nil {
		return err
	}

	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := map[string]bool{}

		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}

			key, ok := token.(string)
			if !ok || seen[key] {
				return invalid("duplicate canonical JSON field")
			}

			seen[key] = true

			if err := restoreJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := restoreJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return invalid("invalid canonical JSON delimiter")
	}

	_, err = d.Token()

	return err
}
