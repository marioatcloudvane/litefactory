-- Only durable references live here. Content stays with its original owner.
CREATE TABLE retained_evidence_manifests (
    scope_kind TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    repository TEXT NOT NULL,
    task TEXT NOT NULL,
    revision TEXT NOT NULL,
    manifest BLOB NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (scope_kind, scope_id, repository, task, revision)
);
CREATE TABLE retained_evidence_sources (
    scope_kind TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    repository TEXT NOT NULL,
    task TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    source_revision TEXT NOT NULL,
    channel TEXT NOT NULL,
    digest TEXT NOT NULL,
    byte_extent INTEGER NOT NULL CHECK (byte_extent >= -1),
    PRIMARY KEY (scope_kind, scope_id, repository, task, source_kind, owner_id, source_revision, channel)
);
