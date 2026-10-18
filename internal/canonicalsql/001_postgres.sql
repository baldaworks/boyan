CREATE SCHEMA IF NOT EXISTS boyan_canonical;
CREATE TABLE boyan_canonical.store_meta (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 store_uuid BYTEA NOT NULL CHECK (length(store_uuid) = 16),
 schema_version INTEGER NOT NULL CHECK (schema_version = 1),
 privacy_protocol INTEGER NOT NULL CHECK (privacy_protocol = 1),
 mutation_clock BYTEA NOT NULL CHECK (length(mutation_clock) = 8)
);
CREATE TABLE boyan_canonical.restore_control (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 control BYTEA NOT NULL CHECK (length(control) BETWEEN 1 AND 4194304)
);
CREATE TABLE boyan_canonical.row_stamp (
 family BYTEA NOT NULL,
 binary_key BYTEA NOT NULL CHECK (length(binary_key) BETWEEN 1 AND 524288),
 last_seq BYTEA NOT NULL CHECK (length(last_seq) = 8),
 deleted BOOLEAN NOT NULL,
 PRIMARY KEY(family,binary_key)
);
CREATE INDEX row_stamp_sequence ON boyan_canonical.row_stamp(last_seq,family,binary_key);
CREATE TABLE boyan_canonical.scope_state (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 version_u64 BYTEA NOT NULL CHECK (length(version_u64) = 8),
 change_seq_u64 BYTEA NOT NULL CHECK (length(change_seq_u64) = 8),
 PRIMARY KEY(scope_key,scope_kind)
);
CREATE TABLE boyan_canonical.operation (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.source (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.message (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 source_id BYTEA NOT NULL CHECK (length(source_id) BETWEEN 1 AND 65535),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE INDEX message_source ON boyan_canonical.message(scope_key,scope_kind,source_id,id);
CREATE TABLE boyan_canonical.item (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.revision (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 item_id BYTEA NOT NULL CHECK (length(item_id) BETWEEN 1 AND 65535),
 revision_u64 BYTEA NOT NULL CHECK (length(revision_u64) = 8),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE INDEX revision_item ON boyan_canonical.revision(scope_key,scope_kind,item_id,revision_u64,id);
CREATE TABLE boyan_canonical.lifecycle (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.head (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.scope_change (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 sequence_u64 BYTEA NOT NULL CHECK (length(sequence_u64) = 8),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,sequence_u64)
);
CREATE TABLE boyan_canonical.delivery (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.delivery_claim (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.payload (
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(id)
);
CREATE TABLE boyan_canonical.provenance_forward (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 from_id BYTEA NOT NULL CHECK (length(from_id) BETWEEN 1 AND 65535),
 to_id BYTEA NOT NULL CHECK (length(to_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,from_id,to_id)
);
CREATE TABLE boyan_canonical.provenance_reverse (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 from_id BYTEA NOT NULL CHECK (length(from_id) BETWEEN 1 AND 65535),
 to_id BYTEA NOT NULL CHECK (length(to_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,from_id,to_id)
);
CREATE TABLE boyan_canonical.source_revision (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 from_id BYTEA NOT NULL CHECK (length(from_id) BETWEEN 1 AND 65535),
 to_id BYTEA NOT NULL CHECK (length(to_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,from_id,to_id)
);
CREATE TABLE boyan_canonical.denied_source (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.denied_revision (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.projection_manifest (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BYTEA NOT NULL CHECK (length(projection_id) BETWEEN 1 AND 65535),
 generation_id BYTEA NOT NULL CHECK (length(generation_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,projection_id,generation_id)
);
CREATE TABLE boyan_canonical.projection_active (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BYTEA NOT NULL CHECK (length(projection_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,projection_id)
);
CREATE TABLE boyan_canonical.projection_retention (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BYTEA NOT NULL CHECK (length(projection_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,projection_id)
);
CREATE TABLE boyan_canonical.projection_checkpoint (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 kind BYTEA NOT NULL CHECK (length(kind) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,kind)
);
CREATE TABLE boyan_canonical.forget_outcome (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 kind BYTEA NOT NULL CHECK (length(kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,kind,id)
);
CREATE TABLE boyan_canonical.ingest_sequence (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 producer_id BYTEA NOT NULL CHECK (length(producer_id) BETWEEN 1 AND 65535),
 sequence_u64 BYTEA NOT NULL CHECK (length(sequence_u64) = 8),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,producer_id,sequence_u64)
);
CREATE TABLE boyan_canonical.ingest_cursor (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 producer_id BYTEA NOT NULL CHECK (length(producer_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,producer_id)
);
CREATE TABLE boyan_canonical.turn_certificate (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.boundary_certificate (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.boundary_capability (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.source_privacy (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.source_privacy_scope (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.erase_journal (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.erase_fence (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.erase_source_identity (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,id)
);
CREATE TABLE boyan_canonical.source_privacy_ready (
 singleton BYTEA NOT NULL CHECK (length(singleton) = 0),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(singleton)
);
CREATE TABLE boyan_canonical.erase_guard (
 singleton BYTEA NOT NULL CHECK (length(singleton) = 0),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(singleton)
);
CREATE TABLE boyan_canonical.erase_source_ref (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 operation_id BYTEA NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,operation_id,id)
);
CREATE TABLE boyan_canonical.erase_lock_ref (
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 operation_id BYTEA NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(scope_key,scope_kind,operation_id,id)
);
CREATE TABLE boyan_canonical.erase_blob_lock (
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(id)
);
CREATE TABLE boyan_canonical.erased_blob (
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 PRIMARY KEY(id)
);
