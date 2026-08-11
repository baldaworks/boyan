package scopelanes

import memory "github.com/baldaworks/boyan/pkg/memory"

const (
	defaultMaxConcurrentScopeLanes = 4
	defaultMaxQueuedPerScopeLane   = 32
	maxConcurrentScopeLanes        = 128
	maxQueuedPerScopeLane          = 1024
)

// Config controls exact-scope FIFO execution with bounded cross-scope
// concurrency.
type Config struct {
	MaxConcurrentScopes int
	MaxQueuedPerScope   int
}

func (c Config) normalized() (Config, error) {
	out := c
	if out.MaxConcurrentScopes <= 0 {
		out.MaxConcurrentScopes = defaultMaxConcurrentScopeLanes
	}
	if out.MaxQueuedPerScope <= 0 {
		out.MaxQueuedPerScope = defaultMaxQueuedPerScopeLane
	}
	if out.MaxConcurrentScopes > maxConcurrentScopeLanes || out.MaxQueuedPerScope > maxQueuedPerScopeLane {
		return Config{}, memory.PermanentError(memory.CodeLimitExceeded, "scope lane limits exceed safe bounds", nil)
	}
	return out, nil
}
