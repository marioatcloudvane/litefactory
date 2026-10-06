-- Implement worker state on the story ledger. attempts counts the failed
-- attempts of a story since it last started or resumed; park_origin says whether
-- the worker or a failed merge parked it; worker_session is the session of the
-- story's own worker run; last_failure is the plain reason of the latest failed
-- attempt; guidance is the owner's reply a resumed story is given.
ALTER TABLE implement_stories ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0 AND attempts<=3);
ALTER TABLE implement_stories ADD COLUMN park_origin TEXT NOT NULL DEFAULT '' CHECK(park_origin IN ('','worker','merge'));
ALTER TABLE implement_stories ADD COLUMN worker_session INTEGER NOT NULL DEFAULT 0 CHECK(worker_session>=0);
ALTER TABLE implement_stories ADD COLUMN last_failure TEXT NOT NULL DEFAULT '';
ALTER TABLE implement_stories ADD COLUMN guidance TEXT NOT NULL DEFAULT '';
CREATE INDEX implement_stories_session ON implement_stories(worker_session) WHERE worker_session>0;
