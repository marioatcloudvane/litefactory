-- submission_checks was keyed by (run_id,reference) alone. Since "reference" is
-- the agent-supplied check identity (e.g. "build"), two different stories in the
-- same run whose snapshots both retain a check named "build" collided on this
-- key: the second retention was treated as an immutable conflict against the
-- first story's check instead of being accepted as separate, snapshot-scoped
-- evidence. Recreate the table keyed by (run_id,snapshot_ref,reference) so check
-- identity is scoped to the snapshot that produced it, matching the existing
-- immutable-evidence model used by submission_snapshots. Existing rows are
-- unique on the old (run_id,reference) key, which is a subset of the new key, so
-- every row carries over without conflict.
ALTER TABLE submission_checks RENAME TO submission_checks_before_snapshot_scope;
CREATE TABLE submission_checks (
 run_id INTEGER NOT NULL, reference TEXT NOT NULL, attempt TEXT NOT NULL, snapshot_ref TEXT NOT NULL, name TEXT NOT NULL,
 executed INTEGER NOT NULL CHECK(executed IN (0,1)), passed INTEGER NOT NULL CHECK(passed IN (0,1)),
 provenance BLOB NOT NULL, result BLOB NOT NULL,
 PRIMARY KEY(run_id,snapshot_ref,reference), FOREIGN KEY(run_id,snapshot_ref) REFERENCES submission_snapshots(run_id,reference)
);
INSERT INTO submission_checks(run_id,reference,attempt,snapshot_ref,name,executed,passed,provenance,result)
 SELECT run_id,reference,attempt,snapshot_ref,name,executed,passed,provenance,result FROM submission_checks_before_snapshot_scope;
DROP TABLE submission_checks_before_snapshot_scope;
