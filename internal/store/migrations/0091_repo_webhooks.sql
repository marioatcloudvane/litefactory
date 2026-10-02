-- repo_webhooks — the GitHub webhook litefactory registered on a repo: its hook
-- id and the delivery URL it was registered with. Written only after GitHub
-- confirmed the hook; cleared when GitHub says the hook is gone.
CREATE TABLE repo_webhooks (
    repo_id    INTEGER PRIMARY KEY REFERENCES repos (id) ON DELETE CASCADE,
    hook_id    INTEGER NOT NULL,
    url        TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
