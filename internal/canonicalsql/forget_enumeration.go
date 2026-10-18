package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strings"
)

func validateForgetPage(scope memory.Scope, after string, limit uint32) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	if limit == 0 || limit > 512 || strings.TrimSpace(after) != after {
		return limitError()
	}

	return nil
}
func forgetScan(scope memory.Scope, family Family, after string) Range {
	scan := Range{Family: family, Scope: &scope, Limit: scopeEraseBatchSize}
	if after != "" {
		scan.After = &Key{Family: family, Scope: scope, Parts: [][]byte{[]byte(after)}}
	}

	return scan
}

// ListCanonicalSourceRefs returns representable, non-denied exact-scope identities.
func (o *Owner) ListCanonicalSourceRefs(ctx context.Context, scope memory.Scope, after string,
	limit uint32) ([]memory.SourceRef, string, error) {
	if err := validateForgetPage(scope, after, limit); err != nil {
		return nil, "", err
	}

	var refs []memory.SourceRef

	next := ""

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		return walkForgetRefs(ctx, tx, forgetScan(scope, Source, after), int(limit), func(r eraseRawRecord) (bool, error) {
			denied, err := privacyDenied(ctx, tx, scope, DeniedSource, r.ID)
			if err != nil || denied {
				return false, err
			}

			data, err := listedCanonicalSourcePayload(ctx, tx, r.Payload.ID)
			if err != nil {
				return false, err
			}

			if !validErasePayload(data, r.Payload) {
				return false, corruptRow()
			}

			ref, _, ok := canonicalSourceRef(data, scope, r.ID)
			if !ok {
				return false, memory.PermanentError(memory.CodeInvalidDerived,
					"canonical source identity is not representable", nil)
			}

			refs = append(refs, ref)
			next = r.ID

			return true, nil
		})
	})
	if err != nil {
		return nil, "", err
	}

	if len(refs) < int(limit) {
		next = ""
	}

	return refs, next, nil
}

// ListCanonicalRevisionRefs returns non-denied immutable revision identities.
func (o *Owner) ListCanonicalRevisionRefs(ctx context.Context, scope memory.Scope, after string,
	limit uint32) ([]memory.RevisionRef, string, error) {
	if err := validateForgetPage(scope, after, limit); err != nil {
		return nil, "", err
	}

	var refs []memory.RevisionRef

	next := ""

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		return walkForgetRefs(ctx, tx, forgetScan(scope, Revision, after), int(limit), func(r eraseRawRecord) (bool, error) {
			denied, err := privacyDenied(ctx, tx, scope, DeniedRevision, r.ID)
			if err != nil || denied {
				return false, err
			}

			refs = append(refs, memory.RevisionRef{ItemID: r.Revision.ItemID, RevisionID: r.ID})
			next = r.ID

			return true, nil
		})
	})
	if err != nil {
		return nil, "", err
	}

	if len(refs) < int(limit) {
		next = ""
	}

	return refs, next, nil
}
func walkForgetRefs(ctx context.Context, tx *Tx, scan Range, limit int, visit func(eraseRawRecord) (bool,
	error)) error {
	count := 0
	for count < limit {
		rows, err := tx.Scan(ctx, scan)
		if err != nil {
			return err
		}

		for _, row := range rows {
			record, err := eraseRecord(ctx, tx, row.Key, scan.Family)
			if err != nil {
				return err
			}

			included, err := visit(record)
			if err != nil {
				return err
			}

			if included {
				count++
			}

			if count == limit {
				return nil
			}

			key := row.Key
			scan.After = &key
		}

		if len(rows) < scan.Limit {
			return nil
		}
	}

	return nil
}

// CanonicalSourceID resolves native identities first, then bounded structural pages.
func (o *Owner) CanonicalSourceID(ctx context.Context, scope memory.Scope, ref memory.SourceRef) (string, error) {
	if err := ref.Validate(); err != nil {
		return "", err
	}

	if ref.Scope != scope {
		return "", memory.PermanentError(memory.CodeScopeViolation, "canonical source scope does not match", nil)
	}

	var resolved string

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		id := memory.TurnSourceID(ref.ExportID)

		key, err := privacyKey(scope, Source, id)
		if err != nil {
			return err
		}

		_, err = eraseRecord(ctx, tx, key, Source)
		if err == nil {
			resolved = id

			return nil
		}

		if !privacyMissing(err) {
			return err
		}

		resolved, err = resolveForgetSource(ctx, tx, scope, ref)

		return err
	})
	if err != nil {
		return "", err
	}

	if resolved == "" {
		return "", memory.PermanentError(memory.CodeNotFound, "canonical source does not exist", nil)
	}

	return resolved, nil
}

func resolveForgetSource(ctx context.Context, tx *Tx, scope memory.Scope, ref memory.SourceRef) (string, error) {
	var resolved string

	err := walkForgetRefs(ctx, tx, forgetScan(scope, Source, ""), 1, func(r eraseRawRecord) (bool, error) {
		data, err := listedCanonicalSourcePayload(ctx, tx, r.Payload.ID)
		if err != nil {
			return false, err
		}

		candidate, _, ok := canonicalSourceRef(data, scope, r.ID)
		if ok && candidate == ref {
			resolved = r.ID

			return true, nil
		}

		return false, nil
	})

	return resolved, err
}
