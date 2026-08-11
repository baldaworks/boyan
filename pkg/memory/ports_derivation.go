package memory

import "context"

// AtomExtractionRequest is bounded input for atom extraction.
type AtomExtractionRequest struct {
	SchemaVersion string        `json:"schema_version"`
	Derivation    DerivationRef `json:"derivation"`
	Turn          Turn          `json:"turn"`
	View          ScopeView     `json:"view"`
}

// ScenarioSynthesisRequest is bounded input for scenario synthesis.
type ScenarioSynthesisRequest struct {
	SchemaVersion string        `json:"schema_version"`
	Derivation    DerivationRef `json:"derivation"`
	Boundary      Boundary      `json:"boundary"`
	View          ScopeView     `json:"view"`
}

// ProfileSynthesisRequest is bounded input for profile synthesis.
type ProfileSynthesisRequest struct {
	SchemaVersion string        `json:"schema_version"`
	Derivation    DerivationRef `json:"derivation"`
	Boundary      Boundary      `json:"boundary"`
	View          ScopeView     `json:"view"`
}

// AtomExtractor converts one completed turn into untrusted atom candidates.
type AtomExtractor interface {
	ExtractAtoms(ctx context.Context, request AtomExtractionRequest) ([]AtomCandidate, error)
}

// ScenarioSynthesizer converts a boundary view into untrusted scenarios.
type ScenarioSynthesizer interface {
	SynthesizeScenarios(ctx context.Context, request ScenarioSynthesisRequest) ([]ScenarioCandidate, error)
}

// ProfileSynthesizer converts a boundary view into one untrusted profile.
type ProfileSynthesizer interface {
	SynthesizeProfile(ctx context.Context, request ProfileSynthesisRequest) (*ProfileCandidate, error)
}
