# Implement run 0007 test report

All five selected stories are engineered and final aggregate checks pass. No acceptance review or issue closure was performed.

## Final gates

- `go build ./...`: passed after final source fix, no output.
- `gofmt -l .`: passed; no files reported.
- `GOFLAGS=-p=1 GOLANGCI_LINT_CACHE=/private/tmp/lf272-golangci-cache make check`: exit 0. Format, vet, pinned golangci-lint v2.13.1 (0 issues), and the full repository race suite passed (33 package results). Package execution was serialized; no tests were omitted or timeouts increased. The existing repository target uses a 20-minute per-package timeout. Final turn package completed in 1166.347s.
- Focused protocol/profile/authority/grant/recovery suites passed normally and with race detection; final deadline fix also passed its architecture gate and exact-expiry/admission race regressions with an 8-minute timeout.

The final immutable snapshot contains all 44 changed source/test/documentation files. Digest: `7085fcec10e13f442575ea7daae737aed961556b73964ab5adf3329d8c8c1331`. Every story's final capture binds its build, format and full make-check outcomes to these exact bytes.

## Scoped evidence

| Story | Evidence established |
| --- | --- |
| US-001 | Real SQLite/private-socket operation refusal matrix, exact expiry, scope/instance isolation, fresh bindings, admission/revocation races and retained internal history. |
| US-002 | Catalog/profile/default-deny and additive permissions; real dispatcher/socket exact bytes/call counts, durable receipts, Validate retained-only communication and unsafe Plan-publication refusal across recovery. |
| US-003 | Strict handshake/framing/state/depth/size/IDs and worker bounds; cancellation/EOF/blocked-output joins; strict config and immutable neutral values; real explicit CLI and bound producer with active/stale authority. |
| US-004 | Canonical proposal/command/gate/provenance and secret serialization; real persisted intent/grant TTL/clamp/replay, concurrent single dispatch, quota windows/unresolved charges, release restrictions and physical reopen. |
| US-005 | Combined real pipes/socket/SQLite fault barriers before/after reservation/dispatch/simulated effects; partial interruption, lost reply/reconnect, retained one operation/owner/quota and fresh scoped receipt authority. |

Detailed executed commands/outcomes are retained in the US001–US005 specialist reports, AGGREGATE_FIX_001.md, AGGREGATE_FIX_002.md and AGGREGATE_GATES_RUN0007.json.

## Failure and correction history

The first aggregate attempt stopped at 46 lint findings. API comments, naming, equivalent control flow and test assertions were corrected without exclusions. Unwritable default lint-cache warnings were resolved with a temporary cache in the existing sandbox. The next attempt found the turn deadline-ownership regression and eventually timed out in the full turn race suite. `WithDeadline` was removed from turn; caller cancellation and store-enforced lease expiry remain. Focused regressions passed. The final serialized-package attempt passed all checks. Initial failures and engineering invalidations remain retained; no failure is relabelled as a pass.

## Final factory receipts

| Story | Outcome | Request |
| --- | --- | --- |
| [US-001](https://github.com/marioatcloudvane/litefactory/issues/275) | Recorded; effects delivered | `05b93fd76eebd158698967efc9b9da36` |
| [US-002](https://github.com/marioatcloudvane/litefactory/issues/276) | Recorded; effects delivered | `8f223b8a5e4586568f7cc4ddcd395a15` |
| [US-003](https://github.com/marioatcloudvane/litefactory/issues/277) | Recorded; effects delivered | `b44ea6c686f67096351a6b18416f26ce` |
| [US-004](https://github.com/marioatcloudvane/litefactory/issues/278) | Recorded; effects delivered | `92ed75a57a7f984240de71efdade4fcb` |
| [US-005](https://github.com/marioatcloudvane/litefactory/issues/279) | Recorded; effects delivered | `d41c9522453ed086effbc56560c21621` |

All final receipts report input valid, engineering recorded, acceptance not_applicable and effects delivered. Factory save/commit/push is separate and was not performed by the agent. The final preflight refresh required explicit invalidation of previously recorded unaccepted US002–US004 engineering; this was completed before replacement captures. No historical acceptance was changed.

## Evidence limits and consumer boundaries

The owner explicitly makes #272 precede #264. Reusable groundwork is implemented over delivered #263; #264 owns production control/launch wiring. #273 owns real creation handlers/publication/reconciliation. #274 owns native host startup/teardown, inventory and isolation; host consumption remains unavailable. Synthetic trusted GitHub metadata and fake creation prove local behavior, not live retrieval or remote exactly-once delivery. Completed/story-bearing MCP Plan proposals are safely refused until a guarded publication route exists; ordinary helper publication is unchanged. No daemon/runtime-state edits, live provider probes, new modules, integration tags, dist writes, commits or pushes were used.
