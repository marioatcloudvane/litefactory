-- Story discovery used to walk every registered child of every dispatched
-- parent on every tick — three forge requests each (the child, its parent link,
-- its comments) — whether or not anything had happened on any of them. That is
-- the one cost in the tick that scales with the size of the registry rather
-- than with the work, and at 60 ticks an hour it was the bulk of the daemon's
-- background quota.
--
-- The repository-level comment listing already runs once per tick and already
-- names every issue commented on since the cursor, children included. This is
-- where that answer is kept: a child named by that listing owes a sweep, and
-- nothing else does. The listing's completeness is what makes the narrowing
-- safe — its window reaches back before the cursor, and the cursor only
-- advances on a tick that completed, so a comment cannot fall between two
-- listings the way it could between two rotations of a bounded walk.
--
-- A child with no story_discovery row has never been swept and is owed one
-- regardless; membership here is only the incremental signal, not the whole
-- eligibility rule. See [DB.ListStoriesAwaitingDiscovery].
CREATE TABLE story_discovery_pending (
    session_id   INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    child_number INTEGER NOT NULL,
    PRIMARY KEY (session_id, child_number)
);
