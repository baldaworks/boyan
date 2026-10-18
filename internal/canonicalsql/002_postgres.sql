CREATE TABLE boyan_canonical.restore_chain (
    singleton INTEGER PRIMARY KEY CHECK (singleton=1),
    control BYTEA NOT NULL CHECK (octet_length(control)<=4096)
);
