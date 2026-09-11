# Implementation Plan: idle-polling-exhausts-the-github-rate

Status: typed proposal prepared; factory publication, link replacement and save pending.

## Shared context

Reduce idle GitHub quota burn without losing owner-comment discovery. Two stories deliver memory-only conditional caching in `internal/github`, followed by shared discovery admission and request summaries in `internal/daemon`. The second depends on the first. Factory-published GitHub stories are the sole editable acceptance/task source.

Sources: [specification](feature-spec.md), [architecture](ARCH_REVIEW.md), [research](RESEARCH_BRIEF.md). Architecture reviewed source revision `c16fb64a76646f5744b659835238824fe9cbc51d`.

## Decisions and tradeoffs

Keep complete histories, authoritative ID cursors, both relationship checks and sixteen children per parent. Revalidate every requested page; retain complete bodies and pagination metadata in bounded private client memory. Missing validators, restart and eviction use ordinary retrieval; errors never serve stale success.

Proposed defaults reserve 80% of observed core quota and cap background spending at 20%. A shared token bucket has capacity 120 and refills 120 requests/minute from tick time. Reserve one request for scalar operations and the existing maximum 100 for lists; refund unused reservations. This avoids resumable pagination, but can delay short or warm lists near the reserve. Capacity denotes a burst allowance, not a strict rolling-minute ceiling. Every network request consumes traffic, even quota-free 304s.

Keep fair repository/parent rotation and give eligible deferred operations priority before small operations consume their reservation. Unknown/stale quota allows one scalar observation per minute; known exhausted quota waits for reset. Reconcile fresh observations and in-flight work conservatively. Active execution/reporting bypasses local discovery admission. No fixed discovery latency or reserve protection against other token users is promised.

No timestamp/since optimization, persistent cache, migration, new setting, dependency, scheduler service or interval change. Interval remains the operator's decision; no protected instruction-tree changes apply.

## Evidence and completion

Typed stories contain criterion-specific proof inventories. Implement executes and retains isolated tests plus ordinary offline real-client HTTP/migrated SQLite scenarios, cold/warm/changed request summaries and final `make check`. Use injected tick time and channels/Quiesce, not sleeps. Validate reads immutable retained evidence only under the factory's persisted workflow.

Authenticated endpoint support and full production child-discovery traffic remain unmeasured operational evidence. Synthetic fixtures prove local behavior only. No nested coding-agent runtime criterion applies. No owner questions or unresolved standards conflict remain. Publication, Story Index replacement and successful save are factory-owned readiness conditions; this document does not establish readiness.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Revalidate polling responses with a bounded memory cache | [#232](https://github.com/marioatcloudvane/litefactory/issues/232) |
| US-002 | Reserve foreground quota with fair discovery admission and request summaries | [#233](https://github.com/marioatcloudvane/litefactory/issues/233) |

