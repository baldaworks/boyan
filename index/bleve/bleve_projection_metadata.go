package bleve

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type bleveStoredMetadata struct {
	Scope          memory.Scope
	ItemID         string
	RevisionID     string
	Revision       uint64
	ScopeChangeSeq uint64
}

func storedBleveMetadata(fields map[string]interface{}) (bleveStoredMetadata, error) {
	scopeKey, err := requiredBleveString(fields, bleveFieldScopeKey)
	if err != nil {
		return bleveStoredMetadata{}, err
	}
	scopeKind, err := requiredBleveString(fields, bleveFieldScopeKind)
	if err != nil {
		return bleveStoredMetadata{}, err
	}
	itemID, err := requiredBleveString(fields, bleveFieldItemID)
	if err != nil {
		return bleveStoredMetadata{}, err
	}
	revisionID, err := requiredBleveString(fields, bleveFieldRevisionID)
	if err != nil {
		return bleveStoredMetadata{}, err
	}
	revision, err := requiredBleveUint64(fields, bleveFieldRevision, "bleve revision metadata is invalid")
	if err != nil {
		return bleveStoredMetadata{}, err
	}
	changeSeq, err := requiredBleveUint64(fields, bleveFieldChangeSeq, "bleve change metadata is invalid")
	if err != nil {
		return bleveStoredMetadata{}, err
	}
	scope := memory.Scope{Key: scopeKey, Kind: memory.ScopeKind(scopeKind)}
	if err := scope.Validate(); err != nil {
		return bleveStoredMetadata{}, err
	}
	return bleveStoredMetadata{
		Scope:          scope,
		ItemID:         itemID,
		RevisionID:     revisionID,
		Revision:       revision,
		ScopeChangeSeq: changeSeq,
	}, nil
}
