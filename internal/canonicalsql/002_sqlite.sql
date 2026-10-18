CREATE TABLE restore_chain (
    singleton INTEGER PRIMARY KEY CHECK (singleton=1),
    control BLOB NOT NULL CHECK (length(control)<=4096)
);
