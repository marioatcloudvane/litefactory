# US-003 executed test evidence

Frozen revision `779ceb3f23625af5e1b71f0f8a51d2efe97b8b854b1df037cbafa1610687c63e`.

Tests use ordinary Go tests and real isolated SQLite files. Repository/frozen
instructions override the integration skill's generic container/tag convention.
No external service, build tag, sleep, daemon or installed-tree action is used.

- AC-001: all five modes bind real running runs without managed generations;
  fresh runs continue the same identity and revoke earlier writers. Changed mode
  or exact frozen revision set creates a new identity. Bad task/run/version/mode
  and forged historical identity reject. Historical access remains read-only.
- AC-002: concurrent updates yield exactly one revision winner and one typed
  expected/current conflict. Injected SQLite failure proves rollback of request,
  snapshot, observations and counter together. Invalid candidates do not write.
  Snapshot bytes and retirement history are copied, attributable and retained.
- AC-003: lost-response replay returns the original application after newer
  revisions and writer revocation; changed bytes collide. Exact receipts/history
  survive database close/reopen, including public reader-only reconnection.
- AC-004: preflight leaves revision history and existing receipts unchanged;
  submit re-resolves real retained v2 evidence and rejects newly unavailable proof.
  Changed frozen scope refuses current writing. Received/input/engineering/
  acceptance/publication states are independently observed with append-only
  histories; rejected input and revoked/read-only writers cannot acquire success.
- AC-005: request association leaves original v2 receipt and pending queue equal.
  Retained v2 snapshot files, checks and frozen revisions remain byte/value equal.
  Service tests compare full run records before/after application writes. Existing
  v2 migration, broker correction, recovery and compatibility tests also pass.

All targeted runs passed with zero failures. Logs:
`US-003-store-tests.log`, `US-003-turn-tests.log`, `US-003-final-tests.log`.
The broader targeted regression runs report 16.2% store and 19.8% turn package
coverage; unrelated package paths were intentionally not exercised here.

Limit: these are offline application/storage tests. Recorded fixture check facts
are synthetic retained test data; authentic provider execution/adoption and
workflow publication are not certified. No acceptance assessment was performed.
