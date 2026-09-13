# US-004 test evidence

Frozen revision `58870c177f9a1b0e1532507b58d0414ce20321930b3d793a655a507138275d37`.

- AC-001: pure complete-file serialization and copied references; real SQLite
  associations retain exact selected revisions, context digest, execution/check
  IDs, source run/attempt and bytes across close/reopen. Real worktree fixtures
  execute isolated Go tests and retain actual passing output/digest attribution.
- AC-002: wrong context/parent/result/run/attempt/set/revision, missing revision,
  missing proof/check, check digest/attribution, scoped-story relabelling, failed
  and unexecuted checks reject. Current-worktree changes, omitted changed files
  and symlinks reject. Missing/conflicted selected engineering proof rejects.
- AC-003: both workflows recover aggregate-only proof through a fresh source run
  and database handle. Earlier story/aggregate snapshots and effects are equal,
  scoped retained-byte accounting is unchanged and the recovery run has no story
  event. Check-identity conflict rolls back its new snapshot association.
- AC-004: helpers preserve failed/unexecuted facts without running commands;
  actual check execution is explicit in the test fixture. Validate cannot capture
  and can construct/read retained references. Incomplete/blocked exits do not
  fabricate proof. Legacy and separated completed aggregate recovery both pass,
  preserving earlier verified/engineering obligations without new lifecycle paths.

All executed targeted tests passed; zero failures. Logs `US-004-tests.log` and
`US-004-final-tests.log` include the real executed fixture output and final
attribution. `US-004-lint.log` reports 0 issues. No external service or test tag
was required; real temporary SQLite and local git worktrees follow repo conventions.

These tests establish offline helper/storage/worktree behavior. They do not
certify provider execution provenance, production adoption, readiness, acceptance
or terminal save. Synthetic negative facts are identified separately from the
actual isolated Go command output.
