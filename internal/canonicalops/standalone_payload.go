package canonicalops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func compatibilityPayloadRef(kind, identity string, payload memory.CanonicalCompatibilityPayload) (memory.CanonicalPayload, memory.PayloadRef, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return memory.CanonicalPayload{}, memory.PayloadRef{}, fmt.Errorf("encode %s compatibility payload: %w", kind, err)
	}
	return canonicalPayload("compatibility:"+kind+":"+identity, data)
}

func canonicalPayload(identity string, data []byte) (memory.CanonicalPayload, memory.PayloadRef, error) {
	if len(data) == 0 {
		return memory.CanonicalPayload{}, memory.PayloadRef{}, fmt.Errorf("canonical payload data is empty")
	}
	digest := sha256.Sum256(data)
	ref := memory.PayloadRef{
		ID:       stableCanonicalID("payload", identity),
		Digest:   hex.EncodeToString(digest[:]),
		ByteSize: uint32(len(data)),
	}
	payload := memory.CanonicalPayload{Ref: ref, Data: append([]byte(nil), data...)}
	if err := payload.Validate(); err != nil {
		return memory.CanonicalPayload{}, memory.PayloadRef{}, err
	}
	return payload, ref, nil
}
