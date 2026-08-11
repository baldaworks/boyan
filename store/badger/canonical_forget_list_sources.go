package badger

import (
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) ListCanonicalSourceRefs(ctx context.Context, scope memory.Scope, afterSourceID string, limit uint32) ([]memory.SourceRef, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}
	if err := scope.Validate(); err != nil {
		return nil, "", err
	}
	if limit == 0 || limit > 512 || (afterSourceID != "" && strings.TrimSpace(afterSourceID) != afterSourceID) {
		return nil, "", memory.PermanentError(memory.CodeLimitExceeded, "canonical source enumeration limit is invalid", nil)
	}
	if s == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	prefix, err := canonicalPrefix(scope, badgerRecordSource)
	if err != nil {
		return nil, "", err
	}
	var refs []memory.SourceRef
	next := ""
	err = s.db.View(func(txn *badger.Txn) error {
		page, cursor, err := listCanonicalSourceRefsPage(txn, scope, prefix, afterSourceID, limit)
		refs = page
		next = cursor
		return err
	})
	if err != nil {
		return nil, "", canonicalStoreError("list canonical source refs", err)
	}
	if len(refs) < int(limit) {
		next = ""
	}
	return refs, next, nil
}
