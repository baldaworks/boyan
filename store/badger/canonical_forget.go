package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

const maxCanonicalScrubPayloads = 4096

type badgerCanonicalForgetOutcome struct {
	Fingerprint string               `json:"fingerprint"`
	Outcome     memory.ForgetOutcome `json:"outcome"`
}

var _ memory.CanonicalForgetEnumerator = (*CanonicalStore)(nil)
var _ memory.CanonicalSourceIdentityResolver = (*CanonicalStore)(nil)
var _ memory.CanonicalForgetOutcomeStore = (*CanonicalStore)(nil)
