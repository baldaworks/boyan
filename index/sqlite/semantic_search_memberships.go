package sqlite

import (
	"context"
	"database/sql"

	semantic "github.com/baldaworks/boyan/internal/semantic"
)

// Each cursor retains one lookahead identity; callers retain only the current
// chunk's bounded memberships. Both streams share the search transaction.
type searchMemberships struct {
	rows     *sql.Rows
	revision string
	ordinal  uint32
	id       string
	ready    bool
}

func openSearchMemberships(ctx context.Context, tx *sql.Tx, build string, source bool) (*searchMemberships, error) {
	table := "semantic_conversation"
	if source {
		table = "semantic_source"
	}

	rows, err := tx.QueryContext(ctx, `SELECT c.revision_id,c.ordinal,m.id
 FROM semantic_chunk c JOIN `+table+` m ON m.chunk_rowid=c.rowid
 WHERE c.build_id=? ORDER BY c.revision_id,c.ordinal,m.id`, build)
	if err != nil {
		return nil, err
	}

	return &searchMemberships{rows: rows}, nil
}

func (members *searchMemberships) advance() error {
	members.ready = members.rows.Next()
	if !members.ready {
		return members.rows.Err()
	}

	return members.rows.Scan(&members.revision, &members.ordinal, &members.id)
}

//nolint:cyclop // Streaming lookahead, bound and ordering failures must all fail closed.
func (members *searchMemberships) read(revision string, ordinal uint32) ([]string, error) {
	if !members.ready {
		if err := members.advance(); err != nil {
			return nil, err
		}
	}

	var ids []string

	for members.ready {
		if members.revision != revision || members.ordinal != ordinal {
			if members.revision < revision || (members.revision == revision && members.ordinal < ordinal) {
				return nil, semantic.ErrCorrupt
			}

			break
		}

		ids = append(ids, members.id)
		if len(ids) > semantic.MaxMetadataIDs {
			return nil, semantic.ErrCorrupt
		}

		if err := members.advance(); err != nil {
			return nil, err
		}
	}

	if !semantic.ValidSortedIdentities(ids) {
		return nil, semantic.ErrCorrupt
	}

	return ids, nil
}
