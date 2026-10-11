package memory

import "strings"

// NormalizeRecallRequest applies defaults and validates an exact-scope query.
func NormalizeRecallRequest(request RecallRequest) (RecallRequest, error) {
	request.Query = strings.TrimSpace(request.Query)
	if request.SchemaVersion == "" {
		request.SchemaVersion = RecallSchemaVersionV1
	}

	if request.Limit == 0 {
		request.Limit = DefaultSearchLimit
	}

	if err := request.Validate(); err != nil {
		return RecallRequest{}, err
	}

	return request, nil
}
