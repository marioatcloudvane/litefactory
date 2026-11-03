# Authorization upgrade and rollback rehearsal

The pinned previous build and changed build passed the same persisted-authorization journey through actual OAuth and MCP HTTP handlers. This is engineering evidence for AC-005; acceptance remains with Validate.

Run from the repository with `python3 <this-directory>/run.py`. The runner archives the pinned revision into a temporary directory, builds a small executable against each revision, and runs eight separate processes sequentially against one synthetic temporary database. It never restores a database snapshot. Every process closes its store before the next starts; the runner deletes the temporary database and synthetic credentials after success. Builds have 15-minute timeouts; phases have 2-minute timeouts.

## Build identities

- Previous source: `5a08f9e4e99640ef56592c90094f1dae75babe87`.
- Previous executable SHA256: `b40ca6141bcff3301dc91d048db044618bc663721f1933af3982c1403a718c3b`.
- Changed source manifest identity: `source-sha256-57fbcaf66e4d11757cfceab4872e5d133cb7a48c828e4344430340a5067897b3`.
- Changed executable SHA256: `74b1cecb889d7e8ee4eb4ff47c946e38f9817caab6dfc65c24442d31056ebb5e`.

The complete changed source snapshot manifest is retained in [changed-source-manifest.json](changed-source-manifest.json). Executable identities include identical synthetic harness and temporary store-owned schema inspector source. Neither helper remains in a production package. The current snapshot copies tracked and untracked source files into isolated scratch before building; temporary helpers never touch the current worktree. This final run includes the completed staticcheck fix and lifecycle/boundary fixtures.

## Observed journey

| Process | Build | Observations |
|---|---|---|
| Seed | Previous | Registered client; real durable consent/code exchange created four independent grant families; protected overview reads returned HTTP 200. |
| Restart | Previous | All four persisted access credentials remained usable. |
| Upgrade | Changed | Existing access remained usable; current refresh rotated; explicit grant revocation withheld protected data. |
| Restart | Changed | Valid and rotated credentials remained usable; revoked access and refresh remained refused. |
| Rollback | Previous | Valid access and changed-build successor refresh remained usable; revoked credentials remained refused; spent ancestor replay revoked its successor; unrelated grant remained usable. |
| Restart after rollback | Previous | Valid grant remained usable; explicit and replay revocation remained effective for access and refresh. |
| Owner change | Changed | Pin-away reconciliation refused the absent alternate owner while durably advancing generation from 2 to 3; pin-back did not revive any grant. |
| Restart after owner change | Previous | Generation 3 persisted; every invalidated access and refresh credential remained refused. |

Protected reads returned HTTP 200 and the synthetic evidence marker only with valid credentials. Invalid access returned HTTP 401 without the marker; invalid refresh returned HTTP 400 without replacement credentials. Each phase produced the identical complete SQLite schema fingerprint: `8f694680d9a657dbcde855c37cadcf8d5666146cad93934d466281098226a677`. Actual cross-build code exchange, refresh rotation, replay and authorization reads prove compatibility of the exercised persisted encodings, rather than merely comparing rows.

Full observed statuses and command identities are retained in [observed.log](observed.log). Authorization used deterministic synthetic identity verification and a bounded synthetic overview provider, with a clock beginning at 2026-11-03T12:00:00Z, advancing by one second per request and 30 seconds per phase. This avoids transport rate limits without approaching token expiry. A preliminary draft kept time frozen and encountered the existing per-source request burst limit; the fixture was corrected before the clean successful sequence retained here.

## Limits and rollback route

This proves restart and stop/select-previous-build/restart against latest state for the exercised authorization paths. It does not certify a live provider/client, network listener behavior, abrupt power-loss durability, or restoring obsolete authorization snapshots. It does not retract bytes already returned. No migrations or serialized authorization changes were introduced by the rehearsal.

Operational rollback must stop and drain the changed process, keep its latest SQLite state, select the compatible pinned previous executable, then restart against that same state. Do not restore an older authorization backup: doing so can discard spent-token and revocation history. SQLite `synchronous=NORMAL` retains its existing power-loss limits.
