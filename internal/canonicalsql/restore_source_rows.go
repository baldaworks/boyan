package canonicalsql

import (
	"bytes"
	"context"
	"encoding/hex"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type restoreSourceRow struct {
	id         int64
	generation []byte
	scope      memory.Scope
}

func sourceGenerationBytes(generation string) ([]byte, error) {
	if !validRestoreGeneration(generation) {
		return nil, invalid("missing canonical restore source provenance")
	}

	return hex.DecodeString(generation)
}

func (tx *Tx) hasRestoreSource(ctx context.Context, generation string, scope memory.Scope) (bool, error) {
	gen, err := sourceGenerationBytes(generation)
	if err != nil {
		return false, err
	}

	where := "generation=$1 AND scope_key=$2 AND scope_kind=$3"
	args := []any{gen, []byte(scope.Key), []byte(scope.Kind)}

	if tx.connection.pg != nil {
		where += " AND logical_key=$4"

		args = append(args, stampKey([][]byte{gen, []byte(scope.Key), []byte(scope.Kind)}))
	}

	var count int

	err = scanOne(ctx, tx.q, "SELECT count(*) FROM "+tx.connection.table("restore_source_capability")+
		" WHERE "+where, args, &count)
	if err != nil {
		return false, databaseError(ctx, "read restore source capability", err)
	}

	if count > 1 {
		return false, corruptRow()
	}

	return count == 1, nil
}

func (tx *Tx) putRestoreSource(ctx context.Context, generation string, scope memory.Scope, copied bool) error {
	found, err := tx.hasRestoreSource(ctx, generation, scope)
	if err != nil {
		return err
	}

	if found {
		if copied {
			return corruptRow()
		}

		return nil
	}

	gen, err := sourceGenerationBytes(generation)
	if err != nil {
		return err
	}

	columns, values := "generation,scope_key,scope_kind", "$1,$2,$3"
	args := []any{gen, []byte(scope.Key), []byte(scope.Kind)}

	if tx.connection.pg != nil {
		columns += ",logical_key"
		values += ",$4"

		args = append(args, stampKey([][]byte{gen, []byte(scope.Key), []byte(scope.Kind)}))
	}

	_, err = tx.q.exec(ctx, "INSERT INTO "+tx.connection.table("restore_source_capability")+
		" ("+columns+") VALUES ("+values+")", args...)

	return databaseError(ctx, "write restore source capability", err)
}

func (tx *Tx) deleteRestoreSource(ctx context.Context, generation string, scope memory.Scope) error {
	if _, err := tx.hasRestoreSource(ctx, generation, scope); err != nil {
		return err
	}

	gen, err := sourceGenerationBytes(generation)
	if err != nil {
		return err
	}

	where := "generation=$1 AND scope_key=$2 AND scope_kind=$3"
	args := []any{gen, []byte(scope.Key), []byte(scope.Kind)}

	if tx.connection.pg != nil {
		where += " AND logical_key=$4"

		args = append(args, stampKey([][]byte{gen, []byte(scope.Key), []byte(scope.Kind)}))
	}

	_, err = tx.q.exec(ctx, "DELETE FROM "+tx.connection.table("restore_source_capability")+
		" WHERE "+where, args...)

	return databaseError(ctx, "delete restore source capability", err)
}

// Fixed row IDs page both engines without ordering or indexing unbounded scopes.
//
//nolint:cyclop // Validate native rows and PostgreSQL framing before using any copied source evidence.
func (tx *Tx) restoreSourcePage(ctx context.Context, where string, args []any) ([]restoreSourceRow, error) {
	columns := "row_id,generation,scope_key,scope_kind"
	if tx.connection.pg != nil {
		columns += ",logical_key"
	}

	r, err := tx.q.query(ctx, "SELECT "+columns+" FROM "+tx.connection.table("restore_source_capability")+
		" WHERE "+where+" ORDER BY row_id LIMIT 128", args...)
	if err != nil {
		return nil, databaseError(ctx, "page restore source capability", err)
	}
	defer r.Close()

	var page []restoreSourceRow

	for r.Next() {
		var row restoreSourceRow

		var key, kind, logical []byte

		values := []any{&row.id, &row.generation, &key, &kind}
		if tx.connection.pg != nil {
			values = append(values, &logical)
		}

		if err := r.Scan(values...); err != nil {
			return nil, databaseError(ctx, "read restore source row", err)
		}

		row.scope = memory.Scope{Key: string(key), Kind: memory.ScopeKind(kind)}
		if len(row.generation) != storeIdentityBytes || row.id <= 0 || row.scope.Validate() != nil ||
			len(key) > 65535 || len(kind) > 65535 ||
			(tx.connection.pg != nil && !bytes.Equal(logical, stampKey([][]byte{row.generation, key, kind}))) {
			return nil, corruptRow()
		}

		page = append(page, row)
	}

	if err := r.Err(); err != nil {
		return nil, databaseError(ctx, "page restore source capability", err)
	}

	return page, nil
}

func (tx *Tx) deleteRestoreSourceRows(ctx context.Context, page []restoreSourceRow) error {
	for _, row := range page {
		if _, err := tx.q.exec(ctx, "DELETE FROM "+tx.connection.table("restore_source_capability")+
			" WHERE row_id=$1", row.id); err != nil {
			return databaseError(ctx, "clear restore source generation", err)
		}
	}

	return nil
}
