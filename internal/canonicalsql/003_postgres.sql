CREATE TABLE boyan_canonical.restore_source_capability (
    row_id BIGINT GENERATED ALWAYS AS IDENTITY (NO CYCLE) PRIMARY KEY,
    generation BYTEA NOT NULL CHECK (length(generation)=16),
    logical_key BYTEA NOT NULL CHECK (length(logical_key) BETWEEN 1 AND 131100),
    scope_key BYTEA NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
    scope_kind BYTEA NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535)
);
CREATE INDEX restore_source_generation_idx ON boyan_canonical.restore_source_capability(generation,row_id);
CREATE INDEX restore_source_logical_idx ON boyan_canonical.restore_source_capability USING HASH(logical_key);
