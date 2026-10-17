//nolint:lll // Fixed ownership and proof predicates remain auditable with their exact bound arguments.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"github.com/baldaworks/boyan/pkg/memory"
)

func privacyStorageError(err error) error {
	if errors.Is(err, ErrCorrupt) {
		return memory.PermanentError(memory.CodeStoreFailure, "sqlite search storage ownership is corrupt; stop and rebuild disposable search state", ErrCorrupt)
	}

	return lexicalStorageError(err)
}

// ReconcileAllCanonicalDenials reconstructs retirement from persisted scope
// tuples, including retained-only history. It never enumerates receipts or
// initializes semantic tables in a disabled store.
//
//nolint:cyclop // Validate dependencies, acquire lifetime ownership and propagate each closed-page failure.
func (store *Store) ReconcileAllCanonicalDenials(ctx context.Context) (result error) {
	defer func() { result = privacyStorageError(result) }()

	if ctx == nil {
		return invalidCanonicalPrivacy()
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if _, err := store.canonicalPrivacy(); err != nil {
		return err
	}

	operation, done, err := store.acquire(ctx)
	if err != nil {
		return err
	}
	defer done()
	defer func() {
		if operation.Err() != nil {
			result = operation.Err()
		}
	}()

	if err := store.checkPrivacyOwnership(operation); err != nil {
		return err
	}

	var semantic bool
	if err := store.reader.QueryRowContext(operation, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='semantic_build')`).Scan(&semantic); err != nil {
		return err
	}

	var cursor memory.Scope
	for {
		page, err := store.privacyScopes(operation, cursor, semantic)
		if err != nil {
			return err
		}

		if len(page) == 0 {
			return operation.Err()
		}

		for _, scope := range page {
			if err := store.reconcileCanonicalDenials(operation, scope, false); err != nil {
				return err
			}
		}

		cursor = page[len(page)-1]
	}
}

// ReconcileCanonicalDenials retires permanently denied identities from every
// retained build in scope. Canonical metadata is read only between closed SQL
// pages, while the full-scope lease excludes late material producers.
func (store *Store) ReconcileCanonicalDenials(ctx context.Context, scope memory.Scope) (result error) {
	return store.reconcileCanonicalDenials(ctx, scope, true)
}

// The all-scopes pass establishes ownership once before enumeration. Its
// delete-only transactions and other supported writers preserve FK/FTS
// ownership, so each scope need not rescan the database. An independent public
// scope operation must establish its own proof, including unknown-owner rows.
//
//nolint:cyclop // Preserve validation, ownership and each required family failure boundary.
func (store *Store) reconcileCanonicalDenials(ctx context.Context, scope memory.Scope,
	checkOwnership bool) (result error) {
	defer func() { result = privacyStorageError(result) }()

	if err := validateScrub(ctx, scope, nil, nil); err != nil {
		return err
	}

	privacy, err := store.canonicalPrivacy()
	if err != nil {
		return err
	}

	operation, done, err := store.semanticScopeLease(ctx, scope)
	if err != nil {
		return err
	}
	defer done()
	defer func() {
		if operation.Err() != nil {
			result = operation.Err()
		}
	}()

	if checkOwnership {
		if err := store.checkPrivacyOwnership(operation); err != nil {
			return err
		}
	}

	var semantic bool
	if err := store.reader.QueryRowContext(operation, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='semantic_build')`).Scan(&semantic); err != nil {
		return err
	}

	families := []privacyFamily{privacyLexical}
	if semantic {
		families = []privacyFamily{privacyCoverage, privacyChunk, privacyLexical, privacyCoverage}
	}

	for _, family := range families {
		if err := store.reconcilePrivacyFamily(operation, privacy, scope, family); err != nil {
			return err
		}
	}

	return operation.Err()
}

func (store *Store) checkPrivacyOwnership(ctx context.Context) error {
	var invalid bool

	const query = `SELECT EXISTS(SELECT 1 FROM pragma_foreign_key_check) OR
 EXISTS(SELECT 1 FROM lexical_fts f LEFT JOIN lexical_record r ON r.rowid=f.rowid WHERE r.rowid IS NULL) OR
 EXISTS(SELECT 1 FROM lexical_record r LEFT JOIN lexical_fts f ON f.rowid=r.rowid WHERE f.rowid IS NULL)`
	if err := store.reader.QueryRowContext(ctx, query).Scan(&invalid); err != nil {
		return err
	}

	if invalid {
		return ErrCorrupt
	}

	return ctx.Err()
}

func (store *Store) reconcilePrivacyFamily(ctx context.Context, privacy canonicalPrivacyReader,
	scope memory.Scope, family privacyFamily) error {
	var cursor int64
	for {
		page, err := store.privacyCandidates(ctx, scope, family, cursor)
		if err != nil {
			return err
		}

		if len(page) == 0 {
			return ctx.Err()
		}

		if err := store.reconcilePrivacyRows(ctx, privacy, page); err != nil {
			return err
		}

		cursor = page[len(page)-1].rowID
	}
}

// SQL estimates cannot include a source witness found only in immutable
// Evidence. Account for that retained proof before grouping writer inputs.
func (store *Store) reconcilePrivacyRows(ctx context.Context, privacy canonicalPrivacyReader, rows []privacyRow) error {
	var (
		proofs []privacyProof
		size   uint64
	)

	for _, row := range rows {
		proof, denied, err := store.provePrivacyRow(ctx, privacy, row)
		if err != nil {
			return err
		}

		if !denied {
			continue
		}

		cost, err := checkedInputBytes(row.estimatedBytes, uint64(len(proof.deniedSourceID)))
		if err != nil {
			return err
		}

		next, err := checkedInputBytes(size, cost)
		if err != nil {
			return err
		}

		if len(proofs) > 0 && next > lexicalPageBytes {
			if err := store.deletePrivacyProofs(ctx, proofs); err != nil {
				return err
			}

			proofs = nil
			next = cost
		}

		proofs = append(proofs, proof)
		size = next
	}

	if len(proofs) == 0 {
		return ctx.Err()
	}

	return store.deletePrivacyProofs(ctx, proofs)
}

func (store *Store) provePrivacyRow(ctx context.Context, privacy canonicalPrivacyReader,
	row privacyRow) (privacyProof, bool, error) {
	denial, err := deniedCanonicalRevision(ctx, privacy, row.scope, row.revisionID)

	proof := privacyProof{row: row, deniedRevision: denial.revision, deniedSourceID: denial.sourceID}
	if err != nil || denial.denied() {
		return proof, denial.denied(), err
	}

	if row.family == privacyCoverage {
		return proof, false, nil
	}

	var cursor string
	for {
		page, err := store.privacyMemberships(ctx, row, cursor)
		if err != nil {
			return proof, false, err
		}

		if len(page) == 0 {
			return proof, false, ctx.Err()
		}

		for _, source := range page {
			denied, err := deniedCanonicalSource(ctx, privacy, row.scope, source)
			if err != nil {
				return proof, false, err
			}

			if denied {
				proof.deniedSourceID, proof.sourceMembership = source, true

				return proof, true, nil
			}
		}

		cursor = page[len(page)-1]
	}
}

//nolint:cyclop,gocognit // Atomic certificate revocation precedes each independently owned row family.
func (store *Store) deletePrivacyProofs(ctx context.Context, proofs []privacyProof) error {
	return store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, proof := range proofs {
			if err := ctx.Err(); err != nil {
				return err
			}

			matches, err := proof.matches(ctx, tx)
			if err != nil {
				return err
			}

			if !matches {
				continue
			}

			row := proof.row
			if row.family == privacyLexical {
				if _, err := tx.ExecContext(ctx, `UPDATE lexical_build SET revoked=1,state='building' WHERE rowid=? AND physical_build_id=? AND scope_kind=? AND scope_key=?`, row.ownerRowID, row.owner, row.scope.Kind, row.scope.Key); err != nil {
					return err
				}

				if err := deleteLexicalRow(ctx, tx, row.rowID); err != nil {
					return err
				}

				continue
			}

			if active, err := retirePrivacySemantic(ctx, tx, row); err != nil {
				return err
			} else if active {
				continue
			}

			if _, err := tx.ExecContext(ctx, `UPDATE semantic_build SET complete=0,status='degraded',reason='canonical_forget' WHERE build_id=?`, row.owner); err != nil {
				return err
			}

			if row.family == privacyChunk {
				if _, err := tx.ExecContext(ctx, `DELETE FROM semantic_chunk WHERE rowid=?`, row.rowID); err != nil {
					return err
				}

				if _, err := tx.ExecContext(ctx, `DELETE FROM semantic_coverage WHERE build_id=? AND revision_id=? AND NOT EXISTS(SELECT 1 FROM semantic_chunk WHERE build_id=? AND revision_id=?)`, row.owner, row.revisionID, row.owner, row.revisionID); err != nil {
					return err
				}
			} else if _, err := tx.ExecContext(ctx, `DELETE FROM semantic_coverage WHERE rowid=?`, row.rowID); err != nil {
				return err
			}
		}

		return nil
	})
}

func (proof privacyProof) matches(ctx context.Context, tx *sql.Tx) (bool, error) {
	r := proof.row
	table, owner := "lexical_record", "physical_build_id"
	build, membership, membershipOwner := "lexical_build", "lexical_source", "record_rowid"
	extra := ""

	if r.family != privacyLexical {
		table, owner, build, membership, membershipOwner = "semantic_chunk", "build_id", "semantic_build", "semantic_source", "chunk_rowid"
		if r.family == privacyCoverage {
			table = "semantic_coverage"
			extra = ` AND NOT EXISTS(SELECT 1 FROM semantic_chunk c WHERE c.build_id=r.build_id AND c.revision_id=r.revision_id)`
		}
	}

	args := []any{r.rowID, r.owner, r.revisionID, r.itemID, r.ownerRowID, r.scope.Kind, r.scope.Key}
	if r.family == privacyChunk {
		extra += ` AND r.ordinal=?`

		args = append(args, r.ordinal)
	}

	if r.family != privacyLexical {
		extra += ` AND b.epoch=? AND EXISTS(SELECT 1 FROM semantic_active a WHERE a.build_id=b.build_id)=?`

		args = append(args, encodeUint64(r.epoch), r.hasActive)
	}

	if proof.sourceMembership {
		extra += ` AND EXISTS(SELECT 1 FROM ` + membership + ` s WHERE s.` + membershipOwner + `=r.rowid AND s.id=?)`

		args = append(args, proof.deniedSourceID)
	}

	var matches bool

	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` r JOIN `+build+` b USING(`+owner+`)
 WHERE r.rowid=? AND r.`+owner+`=? AND r.revision_id=? AND r.item_id=? AND b.rowid=? AND b.scope_kind=? AND b.scope_key=?`+extra+`)`, args...).Scan(&matches)

	return matches, err
}
