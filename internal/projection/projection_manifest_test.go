package projection //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"testing"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func TestProjectionManifestValidation(t *testing.T) {
	manifest := ProjectionManifest{
		Scope:        Scope{Key: "telegram:projection", Kind: ScopeKindPersonal},
		ProjectionID: "bleve",
		GenerationID: "generation-1",
		Status:       ProjectionGenerationBuilding,
		UpdatedAt:    time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC),
	}
	requireNoError(t, manifest.Validate(), "Validate() error = %v")

	manifest.Status = "unknown"
	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate() error = nil for unknown status")
	}
}
