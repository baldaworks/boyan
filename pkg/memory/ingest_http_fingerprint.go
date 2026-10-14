package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func TurnIngestFingerprint(envelope IngestEnvelope[Turn]) (string, error) {
	if err := envelope.Validate(func(turn Turn) error { return turn.Validate() }); err != nil {
		return "", err
	}

	fingerprint, err := CanonicalTurnFingerprint(envelope.Payload)
	if err != nil {
		return "", err
	}

	return "boyan-ingest-fingerprint:turn:v1:" + fingerprint, nil
}

func BoundaryIngestFingerprint(envelope IngestEnvelope[Boundary]) (string, error) {
	if err := envelope.Validate(func(boundary Boundary) error { return boundary.Validate() }); err != nil {
		return "", err
	}

	normalized, err := NormalizeCanonicalBoundary(envelope.Payload)
	if err != nil {
		return "", err
	}

	return ingestPayloadFingerprint("boundary", normalized)
}

func ingestPayloadFingerprint(kind string, payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", invalidDerived("ingest payload fingerprint is invalid")
	}

	sum := sha256.Sum256(append([]byte(kind+"\x00"), encoded...))

	return "boyan-ingest-fingerprint:" + hex.EncodeToString(sum[:]), nil
}
