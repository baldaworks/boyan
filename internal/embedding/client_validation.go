package embedding

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validOptions(options OpenAIOptions) bool {
	return validEndpoint(options.Endpoint) &&
		validModel(options.Model) &&
		options.Dimensions >= 1 && options.Dimensions <= maxDimensions &&
		len(options.APIKey) <= maxAPIKeyBytes &&
		!strings.ContainsAny(options.APIKey, "\r\n")
}

func validEndpoint(endpoint string) bool {
	if endpoint == "" || len(endpoint) > maxEndpointBytes || !utf8.ValidString(endpoint) {
		return false
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil {
		return false
	}

	return (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}

func validModel(model string) bool {
	if model == "" || len(model) > maxModelBytes || !utf8.ValidString(model) {
		return false
	}

	for _, character := range model {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}
