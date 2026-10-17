//nolint:lll // Fixed metadata-only SQL keeps full scope/owner and rowid predicates auditable.
package sqlite

import (
	"context"
	"database/sql"

	"github.com/baldaworks/boyan/pkg/memory"
)

type privacyFamily uint8

const (
	privacyLexical privacyFamily = iota
	privacyChunk
	privacyCoverage
)

type privacyRow struct {
	family                    privacyFamily
	scope                     memory.Scope
	rowID, ownerRowID         int64
	owner, revisionID, itemID string
	ordinal                   uint32
	epoch                     uint64
	hasActive                 bool
	estimatedBytes            uint64
}

const (
	privacyIdentityScalarBytes   = 48 // Integer identities, flags and scheduling overhead.
	privacyMembershipScalarBytes = 8  // Encoded owner rowid repeated with an identity.
)

type privacyProof struct {
	row              privacyRow
	deniedRevision   bool
	deniedSourceID   string
	sourceMembership bool
}

// privacyRead closes both the rowset and owned snapshot before returning any
// identities. A failed Close never permits a canonical callback or deletion.
func (store *Store) privacyRead(ctx context.Context, query string, args []any,
	read func(*sql.Rows) error) (result error) {
	snapshot, err := store.read(ctx)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := snapshot.Close()
		if result == nil {
			result = closeErr
		}

		if ctx.Err() != nil {
			result = ctx.Err()
		}
	}()

	rows, err := snapshot.tx.QueryContext(snapshot.ctx, query, args...)
	if err != nil {
		return err
	}

	result = read(rows)
	if result == nil {
		result = rows.Err()
	}

	closeErr := rows.Close()
	if result == nil {
		result = closeErr
	}

	return result
}

func privacyCandidateQuery(family privacyFamily) string {
	if family == privacyLexical {
		return `SELECT r.rowid,b.rowid,r.physical_build_id,r.revision_id,r.item_id,0,NULL,0,` + lexicalScrubBytes + `
 FROM lexical_record r JOIN lexical_build b USING(physical_build_id)
 WHERE b.scope_kind=? AND b.scope_key=? AND r.rowid>? ORDER BY r.rowid LIMIT 128`
	}

	from, owner, ordinal, size, zero := "semantic_chunk", "build_id", "r.ordinal",
		`length(r.embedding)+coalesce((SELECT sum(length(CAST(s.id AS BLOB))+8) FROM semantic_source s WHERE s.chunk_rowid=r.rowid),0)+coalesce((SELECT sum(length(CAST(c.id AS BLOB))+8) FROM semantic_conversation c WHERE c.chunk_rowid=r.rowid),0)`, ""
	if family == privacyCoverage {
		from, ordinal, size = "semantic_coverage", "0", "0"
		zero = ` AND NOT EXISTS(SELECT 1 FROM semantic_chunk c WHERE c.build_id=r.build_id AND c.revision_id=r.revision_id)`
	}

	return `SELECT r.rowid,b.rowid,r.` + owner + `,r.revision_id,r.item_id,` + ordinal + `,b.epoch,
 EXISTS(SELECT 1 FROM semantic_active a WHERE a.build_id=b.build_id),` + size + `
 FROM ` + from + ` r JOIN semantic_build b USING(build_id)
 WHERE b.scope_kind=? AND b.scope_key=? AND r.rowid>?` + zero + ` ORDER BY r.rowid LIMIT 128`
}

func (store *Store) privacyCandidates(ctx context.Context, scope memory.Scope,
	family privacyFamily, cursor int64) ([]privacyRow, error) {
	var (
		page []privacyRow
		size uint64
	)

	err := store.privacyRead(ctx, privacyCandidateQuery(family), []any{scope.Kind, scope.Key, cursor}, func(rows *sql.Rows) error {
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			row, err := scanPrivacyRow(rows, scope, family)
			if err != nil {
				return err
			}

			next, err := checkedInputBytes(size, row.estimatedBytes)
			if err != nil {
				return err
			}

			if len(page) > 0 && next > lexicalPageBytes {
				break
			}

			page = append(page, row)

			size = next
			if size > lexicalPageBytes {
				break
			}
		}

		return nil
	})

	return page, err
}

func scanPrivacyRow(rows *sql.Rows, scope memory.Scope, family privacyFamily) (privacyRow, error) {
	row := privacyRow{family: family, scope: scope}

	var epoch []byte
	if err := rows.Scan(&row.rowID, &row.ownerRowID, &row.owner, &row.revisionID, &row.itemID, &row.ordinal, &epoch, &row.hasActive, &row.estimatedBytes); err != nil {
		return privacyRow{}, err
	}

	if family != privacyLexical {
		var err error

		row.epoch, err = decodeUint64(epoch)
		if err != nil {
			return privacyRow{}, ErrCorrupt
		}
	}

	if row.owner == "" || row.revisionID == "" || row.itemID == "" {
		return privacyRow{}, ErrCorrupt
	}

	identityBytes := uint64(len(row.owner)) + uint64(len(row.revisionID)) + uint64(len(row.itemID)) +
		uint64(len(scope.Kind)) + uint64(len(scope.Key)) + privacyIdentityScalarBytes

	var err error

	row.estimatedBytes, err = checkedInputBytes(row.estimatedBytes, identityBytes)

	return row, err
}

func (store *Store) privacyMemberships(ctx context.Context, row privacyRow, cursor string) ([]string, error) {
	table, owner := "lexical_source", "record_rowid"
	if row.family == privacyChunk {
		table, owner = "semantic_source", "chunk_rowid"
	}

	var (
		page []string
		size uint64
	)

	err := store.privacyRead(ctx, `SELECT id FROM `+table+` WHERE `+owner+`=? AND id>? ORDER BY id LIMIT 128`,
		[]any{row.rowID, cursor}, func(rows *sql.Rows) error {
			for rows.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}

				var id string
				if err := rows.Scan(&id); err != nil {
					return err
				}

				if id == "" {
					return ErrCorrupt
				}

				next, err := checkedInputBytes(size, uint64(len(id))+privacyMembershipScalarBytes)
				if err != nil {
					return err
				}

				if len(page) > 0 && next > lexicalPageBytes {
					break
				}

				page = append(page, id)

				size = next
				if size > lexicalPageBytes {
					break
				}
			}

			return nil
		})

	return page, err
}

func (store *Store) privacyScopes(ctx context.Context, cursor memory.Scope, semantic bool) ([]memory.Scope, error) {
	from := `SELECT scope_kind,scope_key FROM lexical_build`
	if semantic {
		from += ` UNION SELECT scope_kind,scope_key FROM semantic_build`
	}

	query := `SELECT DISTINCT scope_kind,scope_key FROM (` + from + `) WHERE (scope_kind,scope_key)>(?,?) ORDER BY scope_kind,scope_key LIMIT 128`

	var (
		page []memory.Scope
		size uint64
	)

	err := store.privacyRead(ctx, query, []any{cursor.Kind, cursor.Key}, func(rows *sql.Rows) error {
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			var scope memory.Scope
			if err := rows.Scan(&scope.Kind, &scope.Key); err != nil {
				return err
			}

			if err := scope.Validate(); err != nil {
				return ErrCorrupt
			}

			next, err := checkedInputBytes(size, uint64(len(scope.Kind))+uint64(len(scope.Key)))
			if err != nil {
				return err
			}

			if len(page) > 0 && next > lexicalPageBytes {
				break
			}

			page = append(page, scope)

			size = next
			if size > lexicalPageBytes {
				break
			}
		}

		return nil
	})

	return page, err
}
