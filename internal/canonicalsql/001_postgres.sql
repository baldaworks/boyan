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
 stamp_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 family BYTEA NOT NULL CHECK (family IN ('scope'::bytea,'operation'::bytea,'source'::bytea,'message'::bytea,'item'::bytea,'revision'::bytea,'lifecycle'::bytea,'head'::bytea,'change'::bytea,'delivery'::bytea,'delivery-claim'::bytea,'payload'::bytea,'provenance-forward'::bytea,'provenance-reverse'::bytea,'source-revision'::bytea,'denied-source'::bytea,'denied-revision'::bytea,'projection-manifest'::bytea,'projection-active'::bytea,'projection-retention'::bytea,'projection-checkpoint'::bytea,'forget-outcome'::bytea,'ingest-sequence'::bytea,'ingest-cursor'::bytea,'turn-certificate'::bytea,'boundary-certificate'::bytea,'boundary-capability'::bytea,'source-privacy'::bytea,'source-privacy-scope'::bytea,'erase-journal'::bytea,'erase-fence'::bytea,'erase-source-identity'::bytea,'source-privacy-ready'::bytea,'erase-guard'::bytea,'erase-source-ref'::bytea,'erase-lock-ref'::bytea,'erase-blob-lock'::bytea,'erased-blob'::bytea)),
 binary_key BYTEA NOT NULL CHECK (length(binary_key) BETWEEN 1 AND 524288),
 last_seq BYTEA NOT NULL CHECK (length(last_seq) = 8),
 deleted BOOLEAN NOT NULL
);
CREATE INDEX row_stamp_key_idx ON boyan_canonical.row_stamp USING HASH(binary_key);
CREATE INDEX row_stamp_sequence_idx ON boyan_canonical.row_stamp(last_seq,family);
CREATE TABLE boyan_canonical.scope_state (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 version_u64 BYTEA NOT NULL CHECK (length(version_u64) = 8),
 change_seq_u64 BYTEA NOT NULL CHECK (length(change_seq_u64) = 8)
);
CREATE INDEX scope_state_logical_idx ON boyan_canonical.scope_state USING HASH(logical_key);
CREATE INDEX scope_state_scope_idx ON boyan_canonical.scope_state USING HASH(scope_partition);
CREATE TABLE boyan_canonical.operation (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX operation_logical_idx ON boyan_canonical.operation USING HASH(logical_key);
CREATE INDEX operation_scope_idx ON boyan_canonical.operation USING HASH(scope_partition);
CREATE INDEX operation_legacy_namespace_idx ON boyan_canonical.operation USING HASH(scope_partition)
 WHERE length(id)=91 AND substring(id from 1 for 27)='boyan-derived:v1:operation:'::bytea;
CREATE TABLE boyan_canonical.source (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX source_logical_idx ON boyan_canonical.source USING HASH(logical_key);
CREATE INDEX source_scope_idx ON boyan_canonical.source USING HASH(scope_partition);
CREATE TABLE boyan_canonical.message (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 source_partition BYTEA NOT NULL CHECK (length(source_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 source_id BYTEA NOT NULL CHECK (length(source_id) BETWEEN 1 AND 65535)
);
CREATE INDEX message_logical_idx ON boyan_canonical.message USING HASH(logical_key);
CREATE INDEX message_scope_idx ON boyan_canonical.message USING HASH(scope_partition);
CREATE INDEX message_source_idx ON boyan_canonical.message USING HASH(source_partition);
CREATE TABLE boyan_canonical.item (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX item_logical_idx ON boyan_canonical.item USING HASH(logical_key);
CREATE INDEX item_scope_idx ON boyan_canonical.item USING HASH(scope_partition);
CREATE TABLE boyan_canonical.revision (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 item_partition BYTEA NOT NULL CHECK (length(item_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304),
 item_id BYTEA NOT NULL CHECK (length(item_id) BETWEEN 1 AND 65535),
 revision_u64 BYTEA NOT NULL CHECK (length(revision_u64) = 8)
);
CREATE INDEX revision_logical_idx ON boyan_canonical.revision USING HASH(logical_key);
CREATE INDEX revision_scope_idx ON boyan_canonical.revision USING HASH(scope_partition);
CREATE INDEX revision_item_idx ON boyan_canonical.revision USING HASH(item_partition);
CREATE TABLE boyan_canonical.lifecycle (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX lifecycle_logical_idx ON boyan_canonical.lifecycle USING HASH(logical_key);
CREATE INDEX lifecycle_scope_idx ON boyan_canonical.lifecycle USING HASH(scope_partition);
CREATE TABLE boyan_canonical.head (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX head_logical_idx ON boyan_canonical.head USING HASH(logical_key);
CREATE INDEX head_scope_idx ON boyan_canonical.head USING HASH(scope_partition);
CREATE TABLE boyan_canonical.scope_change (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 sequence_u64 BYTEA NOT NULL CHECK (length(sequence_u64) = 8),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX scope_change_logical_idx ON boyan_canonical.scope_change USING HASH(logical_key);
CREATE INDEX scope_change_scope_idx ON boyan_canonical.scope_change USING HASH(scope_partition);
CREATE TABLE boyan_canonical.delivery (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX delivery_logical_idx ON boyan_canonical.delivery USING HASH(logical_key);
CREATE INDEX delivery_scope_idx ON boyan_canonical.delivery USING HASH(scope_partition);
CREATE TABLE boyan_canonical.delivery_claim (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX delivery_claim_logical_idx ON boyan_canonical.delivery_claim USING HASH(logical_key);
CREATE INDEX delivery_claim_scope_idx ON boyan_canonical.delivery_claim USING HASH(scope_partition);
CREATE TABLE boyan_canonical.payload (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX payload_logical_idx ON boyan_canonical.payload USING HASH(logical_key);
CREATE TABLE boyan_canonical.provenance_forward (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 from_id BYTEA NOT NULL CHECK (length(from_id) BETWEEN 1 AND 65535),
 to_id BYTEA NOT NULL CHECK (length(to_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX provenance_forward_logical_idx ON boyan_canonical.provenance_forward USING HASH(logical_key);
CREATE INDEX provenance_forward_scope_idx ON boyan_canonical.provenance_forward USING HASH(scope_partition);
CREATE INDEX provenance_forward_prefix_idx ON boyan_canonical.provenance_forward USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.provenance_reverse (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 from_id BYTEA NOT NULL CHECK (length(from_id) BETWEEN 1 AND 65535),
 to_id BYTEA NOT NULL CHECK (length(to_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX provenance_reverse_logical_idx ON boyan_canonical.provenance_reverse USING HASH(logical_key);
CREATE INDEX provenance_reverse_scope_idx ON boyan_canonical.provenance_reverse USING HASH(scope_partition);
CREATE INDEX provenance_reverse_prefix_idx ON boyan_canonical.provenance_reverse USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.source_revision (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 from_id BYTEA NOT NULL CHECK (length(from_id) BETWEEN 1 AND 65535),
 to_id BYTEA NOT NULL CHECK (length(to_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX source_revision_logical_idx ON boyan_canonical.source_revision USING HASH(logical_key);
CREATE INDEX source_revision_scope_idx ON boyan_canonical.source_revision USING HASH(scope_partition);
CREATE INDEX source_revision_prefix_idx ON boyan_canonical.source_revision USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.denied_source (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX denied_source_logical_idx ON boyan_canonical.denied_source USING HASH(logical_key);
CREATE INDEX denied_source_scope_idx ON boyan_canonical.denied_source USING HASH(scope_partition);
CREATE TABLE boyan_canonical.denied_revision (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX denied_revision_logical_idx ON boyan_canonical.denied_revision USING HASH(logical_key);
CREATE INDEX denied_revision_scope_idx ON boyan_canonical.denied_revision USING HASH(scope_partition);
CREATE TABLE boyan_canonical.projection_manifest (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BYTEA NOT NULL CHECK (length(projection_id) BETWEEN 1 AND 65535),
 generation_id BYTEA NOT NULL CHECK (length(generation_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX projection_manifest_logical_idx ON boyan_canonical.projection_manifest USING HASH(logical_key);
CREATE INDEX projection_manifest_scope_idx ON boyan_canonical.projection_manifest USING HASH(scope_partition);
CREATE INDEX projection_manifest_prefix_idx ON boyan_canonical.projection_manifest USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.projection_active (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BYTEA NOT NULL CHECK (length(projection_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX projection_active_logical_idx ON boyan_canonical.projection_active USING HASH(logical_key);
CREATE INDEX projection_active_scope_idx ON boyan_canonical.projection_active USING HASH(scope_partition);
CREATE TABLE boyan_canonical.projection_retention (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 projection_id BYTEA NOT NULL CHECK (length(projection_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX projection_retention_logical_idx ON boyan_canonical.projection_retention USING HASH(logical_key);
CREATE INDEX projection_retention_scope_idx ON boyan_canonical.projection_retention USING HASH(scope_partition);
CREATE TABLE boyan_canonical.projection_checkpoint (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 kind BYTEA NOT NULL CHECK (length(kind) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX projection_checkpoint_logical_idx ON boyan_canonical.projection_checkpoint USING HASH(logical_key);
CREATE INDEX projection_checkpoint_scope_idx ON boyan_canonical.projection_checkpoint USING HASH(scope_partition);
CREATE TABLE boyan_canonical.forget_outcome (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 kind BYTEA NOT NULL CHECK (length(kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX forget_outcome_logical_idx ON boyan_canonical.forget_outcome USING HASH(logical_key);
CREATE INDEX forget_outcome_scope_idx ON boyan_canonical.forget_outcome USING HASH(scope_partition);
CREATE INDEX forget_outcome_prefix_idx ON boyan_canonical.forget_outcome USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.ingest_sequence (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 producer_id BYTEA NOT NULL CHECK (length(producer_id) BETWEEN 1 AND 65535),
 sequence_u64 BYTEA NOT NULL CHECK (length(sequence_u64) = 8),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX ingest_sequence_logical_idx ON boyan_canonical.ingest_sequence USING HASH(logical_key);
CREATE INDEX ingest_sequence_scope_idx ON boyan_canonical.ingest_sequence USING HASH(scope_partition);
CREATE INDEX ingest_sequence_prefix_idx ON boyan_canonical.ingest_sequence USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.ingest_cursor (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 producer_id BYTEA NOT NULL CHECK (length(producer_id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX ingest_cursor_logical_idx ON boyan_canonical.ingest_cursor USING HASH(logical_key);
CREATE INDEX ingest_cursor_scope_idx ON boyan_canonical.ingest_cursor USING HASH(scope_partition);
CREATE TABLE boyan_canonical.turn_certificate (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX turn_certificate_logical_idx ON boyan_canonical.turn_certificate USING HASH(logical_key);
CREATE INDEX turn_certificate_scope_idx ON boyan_canonical.turn_certificate USING HASH(scope_partition);
CREATE TABLE boyan_canonical.boundary_certificate (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX boundary_certificate_logical_idx ON boyan_canonical.boundary_certificate USING HASH(logical_key);
CREATE INDEX boundary_certificate_scope_idx ON boyan_canonical.boundary_certificate USING HASH(scope_partition);
CREATE TABLE boyan_canonical.boundary_capability (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX boundary_capability_logical_idx ON boyan_canonical.boundary_capability USING HASH(logical_key);
CREATE INDEX boundary_capability_scope_idx ON boyan_canonical.boundary_capability USING HASH(scope_partition);
CREATE TABLE boyan_canonical.source_privacy (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX source_privacy_logical_idx ON boyan_canonical.source_privacy USING HASH(logical_key);
CREATE INDEX source_privacy_scope_idx ON boyan_canonical.source_privacy USING HASH(scope_partition);
CREATE TABLE boyan_canonical.source_privacy_scope (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX source_privacy_scope_logical_idx ON boyan_canonical.source_privacy_scope USING HASH(logical_key);
CREATE INDEX source_privacy_scope_scope_idx ON boyan_canonical.source_privacy_scope USING HASH(scope_partition);
CREATE TABLE boyan_canonical.erase_journal (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erase_journal_logical_idx ON boyan_canonical.erase_journal USING HASH(logical_key);
CREATE INDEX erase_journal_scope_idx ON boyan_canonical.erase_journal USING HASH(scope_partition);
CREATE TABLE boyan_canonical.erase_fence (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erase_fence_logical_idx ON boyan_canonical.erase_fence USING HASH(logical_key);
CREATE INDEX erase_fence_scope_idx ON boyan_canonical.erase_fence USING HASH(scope_partition);
CREATE TABLE boyan_canonical.erase_source_identity (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erase_source_identity_logical_idx ON boyan_canonical.erase_source_identity USING HASH(logical_key);
CREATE INDEX erase_source_identity_scope_idx ON boyan_canonical.erase_source_identity USING HASH(scope_partition);
CREATE TABLE boyan_canonical.source_privacy_ready (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 singleton BYTEA NOT NULL CHECK (length(singleton) = 0),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX source_privacy_ready_logical_idx ON boyan_canonical.source_privacy_ready USING HASH(logical_key);
CREATE TABLE boyan_canonical.erase_guard (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 singleton BYTEA NOT NULL CHECK (length(singleton) = 0),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erase_guard_logical_idx ON boyan_canonical.erase_guard USING HASH(logical_key);
CREATE TABLE boyan_canonical.erase_source_ref (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 operation_id BYTEA NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erase_source_ref_logical_idx ON boyan_canonical.erase_source_ref USING HASH(logical_key);
CREATE INDEX erase_source_ref_scope_idx ON boyan_canonical.erase_source_ref USING HASH(scope_partition);
CREATE INDEX erase_source_ref_prefix_idx ON boyan_canonical.erase_source_ref USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.erase_lock_ref (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 scope_partition BYTEA NOT NULL CHECK (length(scope_partition) BETWEEN 1 AND 131080),
 prefix_partition BYTEA NOT NULL CHECK (length(prefix_partition) BETWEEN 1 AND 196620),
 scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
 scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
 operation_id BYTEA NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 65535),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erase_lock_ref_logical_idx ON boyan_canonical.erase_lock_ref USING HASH(logical_key);
CREATE INDEX erase_lock_ref_scope_idx ON boyan_canonical.erase_lock_ref USING HASH(scope_partition);
CREATE INDEX erase_lock_ref_prefix_idx ON boyan_canonical.erase_lock_ref USING HASH(prefix_partition);
CREATE TABLE boyan_canonical.erase_blob_lock (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erase_blob_lock_logical_idx ON boyan_canonical.erase_blob_lock USING HASH(logical_key);
CREATE TABLE boyan_canonical.erased_blob (
 row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
 logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 524288),
 id BYTEA NOT NULL CHECK (length(id) BETWEEN 1 AND 65535),
 envelope BYTEA NOT NULL CHECK (length(envelope) BETWEEN 1 AND 4194304)
);
CREATE INDEX erased_blob_logical_idx ON boyan_canonical.erased_blob USING HASH(logical_key);
