package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

// All encoded columns excluding physical build identity and rowid. Keeping
// their encoded form makes copy accounting identical to ordinary apply.
const lexicalInputColumns = `item_id,revision_id,revision,change_seq,kind,category,memory_key,
 created_at,observed_at,valid_from,valid_until,expires_at,sensitivity,retention,raw_tokens,ru_tokens,en_tokens`
const lexicalInputCount = 17
const lexicalTokenOffset = 14

type lexicalInput struct {
	revision               string
	values                 []any
	sources, conversations []string
}

func canonicalLexicalInput(r memory.RecallRecord) (lexicalInput, error) {
	document := memory.RecallProjectionDocument{Scope: r.Scope, ItemID: r.ItemID,
		RevisionID: r.RevisionID, Revision: r.Revision, Kind: r.Kind, Category: r.Category,
		MemoryKey: r.MemoryKey, Text: r.Text, CreatedAt: r.CreatedAt, Temporal: r.Temporal,
		Sensitivity: r.Sensitivity, Retention: r.Retention, SourceIDs: r.SourceIDs,
		ConversationIDs: r.ConversationIDs, ScopeChangeSeq: r.ScopeChangeSeq}
	if err := document.Validate(); err != nil {
		return lexicalInput{}, err
	}

	for _, source := range r.SourceRefs {
		if err := source.Validate(); err != nil {
			return lexicalInput{}, err
		}

		if source.Scope != r.Scope {
			return lexicalInput{}, memory.PermanentError(memory.CodeScopeViolation, "sqlite source scope mismatch", nil)
		}
	}

	if r.State != memory.RevisionStateActive {
		return lexicalInput{}, memory.PermanentError(memory.CodeForgotten, "sqlite canonical record is inactive", nil)
	}

	fields := analyzeLexicalText(r.Text)

	category := ""
	if r.Category != nil {
		category = string(*r.Category)
	}

	return lexicalInput{revision: r.RevisionID, values: []any{
		r.ItemID, r.RevisionID, encodeUint64(r.Revision), encodeUint64(r.ScopeChangeSeq),
		string(r.Kind), category, string(r.MemoryKey), encodeTime(r.CreatedAt), encodeTime(r.Temporal.ObservedAt),
		nullableTime(r.Temporal.ValidFrom), nullableTime(r.Temporal.ValidUntil), nullableTime(r.Temporal.ExpiresAt),
		string(r.Sensitivity), string(r.Retention), fields.raw, fields.ru, fields.en,
	}, sources: r.SourceIDs, conversations: r.ConversationIDs}, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}

	return encodeTime(*t)
}

// encodedBytes counts deterministic bound inputs, including repeated lookup
// identities, FTS rowid/owner lookup, both copies of tokens, and every supplied
// membership (duplicates included). SQL syntax/pages/WAL are not input bytes.
func (input lexicalInput) encodedBytes(build string) (uint64, error) {
	var counter inputByteCounter
	counter.add(len(build))
	counter.add(len(input.revision))
	// Existing-row delete binds: two rowids; count conservatively even for insert.
	counter.add(unsignedBytes)
	counter.add(unsignedBytes)

	if len(input.values) == 0 {
		return counter.bytes, counter.err
	}

	counter.add(len(build))
	counter.add(unsignedBytes)
	counter.add(unsignedBytes)

	for _, value := range input.values {
		switch v := value.(type) {
		case string:
			counter.add(len(v))
		case []byte:
			counter.add(len(v))
		}
	}
	// Token fields occur in both the relational row and FTS input.
	if len(input.values) > 0 {
		for _, value := range input.values[lexicalTokenOffset:] {
			counter.add(len(value.(string)))
		}
	}

	for _, id := range input.sources {
		counter.add(unsignedBytes)
		counter.add(len(id))
	}

	for _, id := range input.conversations {
		counter.add(unsignedBytes)
		counter.add(len(id))
	}

	return counter.bytes, counter.err
}

//nolint:cyclop // Every dependent row mutation must propagate errors before commit.
func (input lexicalInput) write(ctx context.Context, tx *sql.Tx, build string) error {
	var row int64

	const lookup = `SELECT rowid FROM lexical_record WHERE physical_build_id=? AND revision_id=?`

	err := tx.QueryRowContext(ctx, lookup, build, input.revision).Scan(&row)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if err == nil {
		if err = deleteLexicalRow(ctx, tx, row); err != nil {
			return err
		}
	}

	if len(input.values) == 0 {
		return nil
	}

	args := append([]any{build}, input.values...)

	const insert = `INSERT INTO lexical_record(physical_build_id,` + lexicalInputColumns + `)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

	result, err := tx.ExecContext(ctx, insert, args...)
	if err != nil {
		return err
	}

	row, err = result.LastInsertId()
	if err != nil {
		return err
	}

	if err = insertLexicalFTS(ctx, tx, row,
		input.values[14].(string), input.values[15].(string), input.values[16].(string)); err != nil {
		return err
	}

	for _, id := range input.sources {
		const source = `INSERT INTO lexical_source VALUES(?,?) ON CONFLICT(record_rowid,id) DO NOTHING`
		if _, err = tx.ExecContext(ctx, source, row, id); err != nil {
			return err
		}
	}

	for _, id := range input.conversations {
		const conversation = `INSERT INTO lexical_conversation VALUES(?,?) ON CONFLICT(record_rowid,id) DO NOTHING`
		if _, err = tx.ExecContext(ctx, conversation, row, id); err != nil {
			return err
		}
	}

	return nil
}

func deleteLexicalRow(ctx context.Context, tx *sql.Tx, row int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM lexical_fts WHERE rowid=?`, row); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `DELETE FROM lexical_record WHERE rowid=?`, row)

	return err
}

//nolint:cyclop,funlen // The bounded row scan, memberships and required Close errors share one owned snapshot.
func (store *Store) loadLexicalPage(ctx context.Context, build string,
	after int64) (result []lexicalInput, last int64, resultErr error) {
	snapshot, err := store.read(ctx)
	if err != nil {
		return nil, after, err
	}
	defer func() {
		if err := snapshot.Close(); resultErr == nil && err != nil {
			result, resultErr = nil, err
		}
	}()

	const page = `SELECT rowid,` + lexicalInputColumns + ` FROM lexical_record
 WHERE physical_build_id=? AND rowid>? ORDER BY rowid LIMIT ?`

	rows, err := snapshot.tx.QueryContext(snapshot.ctx, page, build, after, 1)
	if err != nil {
		return nil, after, err
	}

	var (
		inputs []lexicalInput
		rowIDs []int64
	)

	for rows.Next() {
		input := lexicalInput{values: make([]any, lexicalInputCount)}

		var row int64

		dest := []any{&row}
		for i := range input.values {
			dest = append(dest, &input.values[i])
		}

		if err = rows.Scan(dest...); err != nil {
			_ = rows.Close()

			return nil, after, err
		}

		input.revision = input.values[1].(string)
		inputs = append(inputs, input)
		rowIDs = append(rowIDs, row)
		after = row
	}

	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return nil, after, err
	}

	for i, row := range rowIDs {
		const sources = `SELECT id FROM lexical_source WHERE record_rowid=? ORDER BY id`

		inputs[i].sources, err = loadMembership(snapshot, sources, row)
		if err != nil {
			return nil, after, err
		}

		const conversations = `SELECT id FROM lexical_conversation WHERE record_rowid=? ORDER BY id`

		inputs[i].conversations, err = loadMembership(snapshot, conversations, row)
		if err != nil {
			return nil, after, err
		}
	}

	return inputs, after, nil
}

func loadMembership(snapshot *readSnapshot, query string, row int64) (result []string, resultErr error) {
	rows, err := snapshot.tx.QueryContext(snapshot.ctx, query, row)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); resultErr == nil {
			resultErr = closeErr
		}
	}()

	var ids []string

	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}
