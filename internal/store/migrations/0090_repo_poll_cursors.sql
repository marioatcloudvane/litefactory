-- repo_poll_cursors — the forge listing's place in time, one row per repo. A
-- repo's row advances only when that repo's own listing finished, so a repo that
-- is listed later or less often than its neighbours is never skipped past.
-- poll_cursor keeps last_tick_at; its tasks_updated_since column is no longer read.
CREATE TABLE repo_poll_cursors (
    repo_id      INTEGER PRIMARY KEY REFERENCES repos (id) ON DELETE CASCADE,
    listed_up_to TEXT NOT NULL
);

-- Every existing repo starts from the old global cursor, so nothing is replayed
-- and nothing is skipped by the switch. A database that never ticked has no
-- global cursor and so seeds nothing: those repos list from their creation time.
INSERT INTO repo_poll_cursors (repo_id, listed_up_to)
SELECT r.id, p.tasks_updated_since
FROM repos r, poll_cursor p
WHERE p.id = 1 AND p.tasks_updated_since IS NOT NULL;
