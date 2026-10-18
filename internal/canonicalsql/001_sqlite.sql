CREATE TABLE store_meta (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 store_uuid BLOB NOT NULL CHECK (typeof(store_uuid) = 'blob' AND length(store_uuid) = 16),
 schema_version INTEGER NOT NULL CHECK (schema_version = 1),
 privacy_protocol INTEGER NOT NULL CHECK (privacy_protocol = 1),
 mutation_clock BLOB NOT NULL CHECK (typeof(mutation_clock) = 'blob' AND length(mutation_clock) = 8)
);
CREATE TABLE restore_control (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 control BLOB NOT NULL CHECK (typeof(control) = 'blob' AND length(control) BETWEEN 1 AND 4194304)
);
CREATE TABLE row_stamp (
 family BLOB NOT NULL,
 binary_key BLOB NOT NULL CHECK (typeof(binary_key) = 'blob' AND length(binary_key) BETWEEN 1 AND 524288),
 last_seq BLOB NOT NULL CHECK (typeof(last_seq) = 'blob' AND length(last_seq) = 8),
 deleted BOOLEAN NOT NULL CHECK (deleted IN (0,1)),
 PRIMARY KEY(family,binary_key)
);
CREATE INDEX row_stamp_sequence ON row_stamp(last_seq,family,binary_key);
CREATE TABLE scope_state (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 version_u64 BLOB NOT NULL CHECK (typeof(version_u64) = 'blob' AND length(version_u64) = 8),
 change_seq_u64 BLOB NOT NULL CHECK (typeof(change_seq_u64) = 'blob' AND length(change_seq_u64) = 8),
 PRIMARY KEY(scope_key,scope_kind)
) WITHOUT ROWID;
CREATE TABLE operation (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE source (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE message (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 source_id BLOB NOT NULL CHECK (typeof(source_id) = 'blob' AND length(source_id) BETWEEN 1 AND 65535),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE INDEX message_source ON message(scope_key,scope_kind,source_id,id);
CREATE TABLE item (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE revision (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 item_id BLOB NOT NULL CHECK (typeof(item_id) = 'blob' AND length(item_id) BETWEEN 1 AND 65535),
 revision_u64 BLOB NOT NULL CHECK (typeof(revision_u64) = 'blob' AND length(revision_u64) = 8),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE INDEX revision_item ON revision(scope_key,scope_kind,item_id,revision_u64,id);
CREATE TABLE lifecycle (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE head (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE scope_change (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 sequence_u64 BLOB NOT NULL CHECK (typeof(sequence_u64) = 'blob' AND length(sequence_u64) = 8),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,sequence_u64)
) WITHOUT ROWID;
CREATE TABLE delivery (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE delivery_claim (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE payload (
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(id)
) WITHOUT ROWID;
CREATE TABLE provenance_forward (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 from_id BLOB NOT NULL CHECK (typeof(from_id) = 'blob' AND length(from_id) BETWEEN 1 AND 65535),
 to_id BLOB NOT NULL CHECK (typeof(to_id) = 'blob' AND length(to_id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,from_id,to_id)
) WITHOUT ROWID;
CREATE TABLE provenance_reverse (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 from_id BLOB NOT NULL CHECK (typeof(from_id) = 'blob' AND length(from_id) BETWEEN 1 AND 65535),
 to_id BLOB NOT NULL CHECK (typeof(to_id) = 'blob' AND length(to_id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,from_id,to_id)
) WITHOUT ROWID;
CREATE TABLE source_revision (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 from_id BLOB NOT NULL CHECK (typeof(from_id) = 'blob' AND length(from_id) BETWEEN 1 AND 65535),
 to_id BLOB NOT NULL CHECK (typeof(to_id) = 'blob' AND length(to_id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,from_id,to_id)
) WITHOUT ROWID;
CREATE TABLE denied_source (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE denied_revision (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE projection_manifest (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BLOB NOT NULL CHECK (typeof(projection_id) = 'blob' AND length(projection_id) BETWEEN 1 AND 65535),
 generation_id BLOB NOT NULL CHECK (typeof(generation_id) = 'blob' AND length(generation_id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,projection_id,generation_id)
) WITHOUT ROWID;
CREATE TABLE projection_active (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BLOB NOT NULL CHECK (typeof(projection_id) = 'blob' AND length(projection_id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,projection_id)
) WITHOUT ROWID;
CREATE TABLE projection_retention (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BLOB NOT NULL CHECK (typeof(projection_id) = 'blob' AND length(projection_id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,projection_id)
) WITHOUT ROWID;
CREATE TABLE projection_checkpoint (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 kind BLOB NOT NULL CHECK (typeof(kind) = 'blob' AND length(kind) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,kind)
) WITHOUT ROWID;
CREATE TABLE forget_outcome (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 kind BLOB NOT NULL CHECK (typeof(kind) = 'blob' AND length(kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,kind,id)
) WITHOUT ROWID;
CREATE TABLE ingest_sequence (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 producer_id BLOB NOT NULL CHECK (typeof(producer_id) = 'blob' AND length(producer_id) BETWEEN 1 AND 65535),
 sequence_u64 BLOB NOT NULL CHECK (typeof(sequence_u64) = 'blob' AND length(sequence_u64) = 8),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,producer_id,sequence_u64)
) WITHOUT ROWID;
CREATE TABLE ingest_cursor (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 producer_id BLOB NOT NULL CHECK (typeof(producer_id) = 'blob' AND length(producer_id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,producer_id)
) WITHOUT ROWID;
CREATE TABLE turn_certificate (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE boundary_certificate (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE boundary_capability (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE source_privacy (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE source_privacy_scope (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE erase_journal (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE erase_fence (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE erase_source_identity (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
) WITHOUT ROWID;
CREATE TABLE source_privacy_ready (
 singleton BLOB NOT NULL CHECK (typeof(singleton) = 'blob' AND length(singleton) = 0),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(singleton)
) WITHOUT ROWID;
CREATE TABLE erase_guard (
 singleton BLOB NOT NULL CHECK (typeof(singleton) = 'blob' AND length(singleton) = 0),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(singleton)
) WITHOUT ROWID;
CREATE TABLE erase_source_ref (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 operation_id BLOB NOT NULL CHECK (typeof(operation_id) = 'blob' AND length(operation_id) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,operation_id,id)
) WITHOUT ROWID;
CREATE TABLE erase_lock_ref (
 scope_key BLOB NOT NULL CHECK (typeof(scope_key) = 'blob' AND length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BLOB NOT NULL CHECK (typeof(scope_kind) = 'blob' AND length(scope_kind) BETWEEN 1 AND 65535),
 operation_id BLOB NOT NULL CHECK (typeof(operation_id) = 'blob' AND length(operation_id) BETWEEN 1 AND 65535),
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,operation_id,id)
) WITHOUT ROWID;
CREATE TABLE erase_blob_lock (
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(id)
) WITHOUT ROWID;
CREATE TABLE erased_blob (
 id BLOB NOT NULL CHECK (typeof(id) = 'blob' AND length(id) BETWEEN 1 AND 65535),
 envelope BLOB NOT NULL CHECK (typeof(envelope) = 'blob' AND length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(id)
) WITHOUT ROWID;
