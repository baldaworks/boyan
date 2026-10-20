package canonicalsqltest //nolint:testpackage // Exercises private cleanup identity guards with real connections.

import (
	"strings"
	"testing"
)

// Missing required PostgreSQL must fail the matrix rather than produce a green skip.
func TestRequiredPostgresConfiguration(t *testing.T) {
	t.Setenv("BOYAN_REQUIRE_POSTGRES", "1")
	t.Setenv("BOYAN_TEST_POSTGRES_DSN", "")

	if _, err := configuredPostgres(); err == nil || !strings.Contains(err.Error(), "BOYAN_TEST_POSTGRES_DSN") {
		t.Fatalf("missing required DSN: %v", err)
	}
}

func TestPostgresConfigurationRejectsInvalidRequiredMode(t *testing.T) {
	t.Setenv("BOYAN_REQUIRE_POSTGRES", "sometimes")

	if _, err := configuredPostgres(); err == nil {
		t.Fatal("invalid required mode was accepted")
	}
}
