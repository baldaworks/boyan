package memory

const (
	// DefaultSearchLimit is used when a search request omits its limit.
	DefaultSearchLimit = 10
	// MaxSearchLimit bounds the number of results requested from a provider.
	MaxSearchLimit = 100
	// MaxSearchQueryBytes bounds a UTF-8 search query at the provider boundary.
	MaxSearchQueryBytes = 4096
)
