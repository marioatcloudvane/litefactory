-- 0001_init — the whole M0 schema (feature-spec S-2), in one migration.
--
-- All eight tables land together because M0 is a walking skeleton: shipping
-- half the schema first would only buy a second migration.
--
-- Every timestamp column is TEXT holding UTC RFC 3339 with millisecond
-- precision (see store.FormatTime). Fixed width and fixed zone are what make
-- ORDER BY and MAX() on these columns chronological.
--
-- Enumerated columns (session state, run status, comment classification) carry
-- no CHECK constraint on purpose: the values are enforced in Go by typed
-- constants, and every write goes through this package. Encoding them in the
-- schema would turn a later state — LF-11's `blocked` — into a migration for
-- no extra safety.

-- identities — one row per (forge, role); `init` upserts both.
CREATE TABLE identities (
    forge      TEXT    NOT NULL,
    role       TEXT    NOT NULL,   -- owner | bot
    handle     TEXT    NOT NULL,   -- display only: handles are renameable
    user_id    INTEGER,            -- numeric forge id; the identity that comparisons use (D-I)
    source     TEXT    NOT NULL,   -- gh for owner, forge-api for bot
    derived_at TEXT    NOT NULL,
    PRIMARY KEY (forge, role)
);

-- repos — the registered clones. `path` is unique, which is what makes
-- `repo add` on an already-registered path an update rather than a duplicate
-- (AC-1.8).
CREATE TABLE repos (
    id                 INTEGER PRIMARY KEY,
    path               TEXT    NOT NULL UNIQUE,
    forge              TEXT    NOT NULL,
    owner              TEXT    NOT NULL,
    name               TEXT    NOT NULL,
    remote_url         TEXT    NOT NULL,
    base_branch        TEXT    NOT NULL,
    feature_prefix     TEXT    NOT NULL,
    worktree_root      TEXT    NOT NULL,
    aiteam_team        TEXT,        -- NULL when undetectable (AC-1.5)
    aiteam_verified_at TEXT,
    last_fetch_at      TEXT,        -- NULL until the first fetch; never touched by an upsert
    created_at         TEXT    NOT NULL,
    updated_at         TEXT    NOT NULL
);

CREATE INDEX idx_repos_forge_owner_name ON repos (forge, owner, name);

-- sessions — one row per task litefactory has ever touched.
CREATE TABLE sessions (
    id               INTEGER PRIMARY KEY,
    repo_id          INTEGER NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    issue_number     INTEGER NOT NULL,
    task_ref         TEXT    NOT NULL UNIQUE,   -- canonical github:owner/name#123
    feature_slug     TEXT,                      -- frozen on first run (AC-3.6)
    branch           TEXT,
    worktree_path    TEXT,                      -- NULL until the first run, cleared by `task clean`
    state            TEXT    NOT NULL,          -- idle | running | failed
    lock_owner       TEXT,                      -- daemon instance id, NULL when free
    lock_acquired_at TEXT,
    created_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL,
    UNIQUE (repo_id, issue_number)
);

-- Bare issue_number, without repo_id: `litefactory logs 123` searches across
-- every registered repo and reports ambiguity (AC-5.2).
CREATE INDEX idx_sessions_issue_number ON sessions (issue_number);

-- segments — M0 always creates exactly one per session (ordinal 1). The table
-- exists so the claude session id is recorded and LF-8/LF-17 need no
-- migration.
CREATE TABLE segments (
    id                INTEGER PRIMARY KEY,
    session_id        INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    ordinal           INTEGER NOT NULL,
    claude_session_id TEXT    NOT NULL,
    created_at        TEXT    NOT NULL,
    UNIQUE (session_id, ordinal)
);

-- runs — one row per claude invocation, inserted before the subprocess starts
-- so a crashed run still owns its ordinal (S-1).
CREATE TABLE runs (
    id                 INTEGER PRIMARY KEY,
    session_id         INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    segment_id         INTEGER NOT NULL REFERENCES segments (id) ON DELETE CASCADE,
    ordinal            INTEGER NOT NULL,   -- per session; the NNNN in the log filename
    mode               TEXT    NOT NULL,   -- spike at M0
    trigger_comment_id INTEGER NOT NULL,
    status             TEXT    NOT NULL,   -- running | succeeded | failed | reported
    exit_code          INTEGER,            -- NULL while running; -1 for a signal or timeout (S-9)
    error              TEXT,               -- kill/abandon reason accompanying exit_code -1
    started_at         TEXT    NOT NULL,
    finished_at        TEXT,
    duration_ms        INTEGER,
    cost_usd           REAL,               -- from the terminal result event
    log_path           TEXT    NOT NULL,
    commit_sha         TEXT,
    result_comment_id  INTEGER,            -- NULL until the report is posted
    -- One comment can cause at most one run, ever. This is the constraint the
    -- whole crash-replay story rests on (AC-3.4, AC-2.21, AC-4.10).
    UNIQUE (session_id, trigger_comment_id),
    UNIQUE (session_id, ordinal)
);

-- Startup reconciliation scans for runs left `running` by a killed daemon.
CREATE INDEX idx_runs_status ON runs (status);

-- comments — every comment litefactory has classified, third-party ones
-- included: LF-2 mandates that they be recorded even though nothing consumes
-- them until /include at M1.
CREATE TABLE comments (
    id               INTEGER PRIMARY KEY,
    session_id       INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    forge_comment_id INTEGER NOT NULL,
    author_handle    TEXT    NOT NULL,
    author_user_id   INTEGER,            -- durable identity behind the renameable handle (D-I)
    author_is_bot    INTEGER NOT NULL,   -- 0/1, as the forge reported it
    body             TEXT    NOT NULL,   -- verbatim, unredacted
    created_at       TEXT    NOT NULL,   -- the forge's timestamp, not ours
    classification   TEXT    NOT NULL,   -- instruction | third_party | self
    tag              TEXT,               -- c1, c2, … assigned to third_party only
    recorded_at      TEXT    NOT NULL,
    UNIQUE (session_id, forge_comment_id)
);

CREATE INDEX idx_comments_session_tag ON comments (session_id, tag);

-- watermarks — the highest comment id consumed per session.
CREATE TABLE watermarks (
    session_id      INTEGER PRIMARY KEY REFERENCES sessions (id) ON DELETE CASCADE,
    last_comment_id INTEGER NOT NULL,
    updated_at      TEXT    NOT NULL
);

-- poll_cursor — a single row, repo-set-wide. The CHECK is what makes "single
-- row" a property of the schema rather than a convention the accessors happen
-- to follow.
CREATE TABLE poll_cursor (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    tasks_updated_since TEXT,   -- NULL until the first-ever tick initialises it (AC-2.9)
    last_tick_at        TEXT
);
