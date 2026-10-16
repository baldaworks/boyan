package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

const applicationID = 0x42595351 // BYSQ
const schemaVersion = 1

// Version 1 is an unreleased, rebuildable format. Canonical identities are
// byte-nonempty: SQLite length(TEXT) stops at NUL, which canonical lexical IDs
// and scope keys permit. These corrected create-time constraints do not migrate
// earlier development databases; recreate those disposable fixtures. Reopening
// a fresh database preserves the corrected constraints. Semantic projection
// identities retain their stricter portable admission (including NUL rejection).

const lexicalSchema = `
CREATE TABLE lexical_build (
 physical_build_id TEXT PRIMARY KEY CHECK(length(physical_build_id)>0),
 scope_kind TEXT NOT NULL CHECK(length(scope_kind)>0), scope_key TEXT NOT NULL CHECK(length(CAST(scope_key AS BLOB))>0),
 generation_id TEXT NOT NULL CHECK(length(CAST(generation_id AS BLOB))>0),
 projection_id TEXT NOT NULL DEFAULT 'sqlite' CHECK(projection_id='sqlite'),
 schema_version INTEGER NOT NULL DEFAULT 1 CHECK(schema_version=1),
 preprocessing_version INTEGER NOT NULL DEFAULT 1 CHECK(preprocessing_version=1),
 watermark BLOB NOT NULL CHECK(typeof(watermark)='blob' AND length(watermark)=8),
 state TEXT NOT NULL DEFAULT 'building' CHECK(state IN ('building','committed')),
 record_count INTEGER NOT NULL DEFAULT 0 CHECK(record_count>=0),
 source_count INTEGER NOT NULL DEFAULT 0 CHECK(source_count>=0),
 conversation_count INTEGER NOT NULL DEFAULT 0 CHECK(conversation_count>=0),
 revoked INTEGER NOT NULL DEFAULT 0 CHECK(revoked IN (0,1)),
 UNIQUE(scope_kind,scope_key,physical_build_id)
) STRICT;
CREATE TABLE lexical_active (
 scope_kind TEXT NOT NULL, scope_key TEXT NOT NULL, physical_build_id TEXT NOT NULL,
 PRIMARY KEY(scope_kind,scope_key),
 FOREIGN KEY(scope_kind,scope_key,physical_build_id)
 REFERENCES lexical_build(scope_kind,scope_key,physical_build_id) ON DELETE CASCADE
) STRICT;
CREATE TABLE lexical_previous (
 scope_kind TEXT NOT NULL, scope_key TEXT NOT NULL, physical_build_id TEXT NOT NULL,
 PRIMARY KEY(scope_kind,scope_key),
 FOREIGN KEY(scope_kind,scope_key,physical_build_id)
 REFERENCES lexical_build(scope_kind,scope_key,physical_build_id) ON DELETE CASCADE
) STRICT;
CREATE TABLE lexical_record (
 rowid INTEGER PRIMARY KEY,
 physical_build_id TEXT NOT NULL REFERENCES lexical_build(physical_build_id) ON DELETE CASCADE,
 item_id TEXT NOT NULL CHECK(length(CAST(item_id AS BLOB))>0),
 revision_id TEXT NOT NULL CHECK(length(CAST(revision_id AS BLOB))>0),
 revision BLOB NOT NULL CHECK(typeof(revision)='blob' AND length(revision)=8),
 change_seq BLOB NOT NULL CHECK(typeof(change_seq)='blob' AND length(change_seq)=8),
 kind TEXT NOT NULL CHECK(length(kind)>0), category TEXT NOT NULL DEFAULT '', memory_key TEXT NOT NULL DEFAULT '',
 created_at BLOB NOT NULL CHECK(length(created_at)=17), observed_at BLOB NOT NULL CHECK(length(observed_at)=17),
 valid_from BLOB CHECK(valid_from IS NULL OR length(valid_from)=17),
 valid_until BLOB CHECK(valid_until IS NULL OR length(valid_until)=17),
 expires_at BLOB CHECK(expires_at IS NULL OR length(expires_at)=17),
 sensitivity TEXT NOT NULL DEFAULT '', retention TEXT NOT NULL DEFAULT '',
 raw_tokens TEXT NOT NULL DEFAULT '', ru_tokens TEXT NOT NULL DEFAULT '', en_tokens TEXT NOT NULL DEFAULT '',
 UNIQUE(physical_build_id,revision_id)
) STRICT;
CREATE TABLE lexical_source (
 record_rowid INTEGER NOT NULL REFERENCES lexical_record(rowid) ON DELETE CASCADE,
 id TEXT NOT NULL CHECK(length(CAST(id AS BLOB))>0), PRIMARY KEY(record_rowid,id)
) STRICT;
CREATE INDEX lexical_source_id ON lexical_source(id,record_rowid);
CREATE TABLE lexical_conversation (
 record_rowid INTEGER NOT NULL REFERENCES lexical_record(rowid) ON DELETE CASCADE,
 id TEXT NOT NULL CHECK(length(CAST(id AS BLOB))>0), PRIMARY KEY(record_rowid,id)
) STRICT;
CREATE INDEX lexical_conversation_id ON lexical_conversation(id,record_rowid);
CREATE VIRTUAL TABLE lexical_fts USING fts5(raw_tokens,ru_tokens,en_tokens, tokenize='unicode61 remove_diacritics 0');
`

const semanticSchema = `
CREATE TABLE semantic_build (
 build_id TEXT PRIMARY KEY CHECK(length(build_id)>0),
 scope_kind TEXT NOT NULL CHECK(length(scope_kind)>0), scope_key TEXT NOT NULL CHECK(length(CAST(scope_key AS BLOB))>0),
 generation_id TEXT NOT NULL CHECK(length(generation_id)>0),
 physical_build_id TEXT NOT NULL CHECK(length(physical_build_id)>0),
 projection_id TEXT NOT NULL DEFAULT 'sqlite' CHECK(projection_id='sqlite'),
 watermark BLOB NOT NULL CHECK(length(watermark)=8), model_space TEXT NOT NULL CHECK(length(model_space)>0),
 dimensions INTEGER NOT NULL CHECK(dimensions BETWEEN 1 AND 4096),
 epoch BLOB NOT NULL CHECK(length(epoch)=8), complete INTEGER NOT NULL DEFAULT 0 CHECK(complete IN (0,1)),
 status TEXT NOT NULL CHECK(status IN ('ready','degraded','building')), reason TEXT NOT NULL DEFAULT '',
 coverage_hash TEXT NOT NULL DEFAULT '',
 revision_count INTEGER NOT NULL DEFAULT 0 CHECK(revision_count BETWEEN 0 AND 8192),
 chunk_count INTEGER NOT NULL DEFAULT 0 CHECK(chunk_count BETWEEN 0 AND 8192),
 encoded_bytes INTEGER NOT NULL DEFAULT 0 CHECK(encoded_bytes BETWEEN 0 AND 67108864),
 omitted_ineligible INTEGER NOT NULL DEFAULT 0 CHECK(omitted_ineligible>=0),
 omitted_expired INTEGER NOT NULL DEFAULT 0 CHECK(omitted_expired>=0),
 omitted_sensitive INTEGER NOT NULL DEFAULT 0 CHECK(omitted_sensitive>=0),
 ready_at BLOB CHECK(ready_at IS NULL OR length(ready_at)=17),
 UNIQUE(scope_kind,scope_key,build_id,epoch), UNIQUE(build_id,dimensions)
) STRICT;
CREATE TABLE semantic_active (
 scope_kind TEXT NOT NULL, scope_key TEXT NOT NULL, build_id TEXT NOT NULL, epoch BLOB NOT NULL CHECK(length(epoch)=8),
 PRIMARY KEY(scope_kind,scope_key),
 FOREIGN KEY(scope_kind,scope_key,build_id,epoch)
 REFERENCES semantic_build(scope_kind,scope_key,build_id,epoch) ON DELETE CASCADE
) STRICT;
CREATE TABLE semantic_coverage (
 build_id TEXT NOT NULL REFERENCES semantic_build(build_id) ON DELETE CASCADE,
 item_id TEXT NOT NULL CHECK(length(item_id)>0), revision_id TEXT NOT NULL CHECK(length(revision_id)>0),
 revision BLOB NOT NULL CHECK(length(revision)=8), content_hash TEXT NOT NULL CHECK(length(content_hash)>0),
 chunk_count INTEGER NOT NULL CHECK(chunk_count BETWEEN 0 AND 8192),
 PRIMARY KEY(build_id,revision_id), UNIQUE(build_id,revision_id,item_id,revision,content_hash)
) STRICT;
CREATE TABLE semantic_chunk (
 rowid INTEGER PRIMARY KEY,
 build_id TEXT NOT NULL, item_id TEXT NOT NULL, revision_id TEXT NOT NULL,
 revision BLOB NOT NULL CHECK(length(revision)=8),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 8191),
 content_hash TEXT NOT NULL CHECK(length(content_hash)>0), input_hash TEXT NOT NULL CHECK(length(input_hash)>0),
 change_seq BLOB NOT NULL CHECK(length(change_seq)=8),
 dimensions INTEGER NOT NULL CHECK(dimensions BETWEEN 1 AND 4096),
 embedding BLOB NOT NULL CHECK(length(embedding)=4*dimensions),
 kind TEXT NOT NULL CHECK(length(kind)>0), category TEXT NOT NULL DEFAULT '', memory_key TEXT NOT NULL DEFAULT '',
 created_at BLOB NOT NULL CHECK(length(created_at)=17), observed_at BLOB NOT NULL CHECK(length(observed_at)=17),
 valid_from BLOB CHECK(valid_from IS NULL OR length(valid_from)=17),
 valid_until BLOB CHECK(valid_until IS NULL OR length(valid_until)=17),
 expires_at BLOB CHECK(expires_at IS NULL OR length(expires_at)=17),
 sensitivity TEXT NOT NULL DEFAULT '', retention TEXT NOT NULL DEFAULT '',
 UNIQUE(build_id,revision_id,ordinal),
 FOREIGN KEY(build_id,dimensions) REFERENCES semantic_build(build_id,dimensions) ON DELETE CASCADE,
 FOREIGN KEY(build_id,revision_id,item_id,revision,content_hash)
 REFERENCES semantic_coverage(build_id,revision_id,item_id,revision,content_hash) ON DELETE CASCADE
) STRICT;
CREATE TABLE semantic_source (
 chunk_rowid INTEGER NOT NULL REFERENCES semantic_chunk(rowid) ON DELETE CASCADE,
 id TEXT NOT NULL CHECK(length(id)>0), PRIMARY KEY(chunk_rowid,id)
) STRICT;
CREATE INDEX semantic_source_id ON semantic_source(id,chunk_rowid);
CREATE TABLE semantic_conversation (
 chunk_rowid INTEGER NOT NULL REFERENCES semantic_chunk(rowid) ON DELETE CASCADE,
 id TEXT NOT NULL CHECK(length(id)>0), PRIMARY KEY(chunk_rowid,id)
) STRICT;
CREATE INDEX semantic_conversation_id ON semantic_conversation(id,chunk_rowid);
`

func initializeSchema(ctx context.Context, db *sql.DB, fresh, embeddings bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if fresh {
		if _, err := tx.ExecContext(ctx, lexicalSchema); err != nil {
			return err
		}

		identity := fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion)
		if _, err := tx.ExecContext(ctx, identity); err != nil {
			return err
		}
	}

	if embeddings {
		var present int

		const probe = "SELECT count(*) FROM sqlite_schema WHERE name='semantic_build'"
		if err := tx.QueryRowContext(ctx, probe).Scan(&present); err != nil {
			return err
		}

		if present == 0 {
			if _, err := tx.ExecContext(ctx, semanticSchema); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// FTS5 cannot declare foreign keys. All FTS mutation stays inside app-owned
// writer transactions with the corresponding relational rows.
func insertLexicalFTS(ctx context.Context, tx *sql.Tx, rowid int64, raw, ru, en string) error {
	var present int

	const owner = "SELECT count(*) FROM lexical_record WHERE rowid=?"
	if err := tx.QueryRowContext(ctx, owner, rowid).Scan(&present); err != nil {
		return err
	}

	if present != 1 {
		return fmt.Errorf("%w: FTS owner", ErrCorrupt)
	}

	const insert = "INSERT INTO lexical_fts(rowid,raw_tokens,ru_tokens,en_tokens) VALUES(?,?,?,?)"

	_, err := tx.ExecContext(ctx, insert, rowid, raw, ru, en)

	return err
}

func deleteLexicalBuild(ctx context.Context, tx *sql.Tx, buildID string) error {
	const removeFTS = `DELETE FROM lexical_fts WHERE rowid IN
	 (SELECT rowid FROM lexical_record WHERE physical_build_id=?)`
	if _, err := tx.ExecContext(ctx, removeFTS, buildID); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, "DELETE FROM lexical_build WHERE physical_build_id=?", buildID)

	return err
}
