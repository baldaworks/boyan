CREATE TABLE restore_source_capability (
    row_id INTEGER PRIMARY KEY AUTOINCREMENT,
    generation BLOB NOT NULL CHECK (length(generation)=16),
    scope_key BLOB NOT NULL CHECK (length(scope_key) BETWEEN 1 AND 65535),
    scope_kind BLOB NOT NULL CHECK (length(scope_kind) BETWEEN 1 AND 65535),
    UNIQUE (generation,scope_key,scope_kind)
);
CREATE INDEX restore_source_generation_idx ON restore_source_capability(generation,row_id);
