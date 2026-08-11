package mcp

import "time"

// New creates a Boyan MCP service from a defensive copy of its configuration.
func New(cfg Config) *Service {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		enabled:   cfg.Enabled,
		searcher:  cfg.Searcher,
		tracer:    cfg.Tracer,
		forgetter: cfg.Forgetter,
		timeout:   timeout,
		now:       now,
	}
}
