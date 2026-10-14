package boyan_test

import boyan "github.com/baldaworks/boyan/pkg/boyan"

import (
	"testing"
	"time"
)

func TestDefaultConfigValidate(t *testing.T) {
	requireNoError(t, boyan.DefaultConfig().Validate(), "DefaultConfig().Validate() error = %v")
}

func TestValidateRejectsNegativeOperationalBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*boyan.Config)
	}{
		{"request timeout", func(c *boyan.Config) { c.HTTP.RequestTimeout = -time.Second }},
		{"shutdown timeout", func(c *boyan.Config) { c.HTTP.ShutdownTimeout = -time.Second }},
		{"body bytes", func(c *boyan.Config) { c.HTTP.MaxBodyBytes = -1 }},
		{"derivation timeout", func(c *boyan.Config) { c.Derivation.Timeout = -time.Second }},
		{"output bytes", func(c *boyan.Config) { c.Derivation.MaxOutputBytes = -1 }},
		{"gc interval", func(c *boyan.Config) { c.Maintenance.ValueLogGCInterval = -time.Second }},
		{"discard ratio", func(c *boyan.Config) { c.Maintenance.DiscardRatio = -0.1 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := boyan.DefaultConfig()
			tc.mutate(&config)

			if err := config.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want negative-bound rejection")
			}
		})
	}
}

func TestValidateRejectsNonLoopbackListener(t *testing.T) {
	config := boyan.DefaultConfig()

	config.HTTP.Listen = "0.0.0.0:8686"
	if err := config.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want loopback validation failure")
	}
}
