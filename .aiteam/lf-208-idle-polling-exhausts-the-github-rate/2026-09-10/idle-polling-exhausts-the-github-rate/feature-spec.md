# Feature Spec: idle-polling-exhausts-the-github-rate
# Status: SPECIFICATION COMPLETE — factory publication/readiness pending
# Date: 2026-09-10

## Goal

- Reduce idle GitHub quota consumption while preserving reliable owner-comment discovery and leaving significant quota for active work.

## User Stories

| ID | Story |
|---|---|
| US-1 | As the operator, I want polling reads to revalidate cached responses so unchanged issues, relationships and comment pages avoid unnecessary primary-quota charges where GitHub supports validators. |
| US-2 | As the operator, I want discovery to yield fairly before consuming the foreground reserve, with request and deferral summaries that let me verify its actual cost. |

## Existing Features to Reuse

| Concern | Existing owner |
|---|---|
| Authenticated GitHub access, injectable transport and request observations | `internal/github/client.go` and endpoint adapters |
| Complete comment-history pagination and authoritative comment IDs | `internal/github/comments.go`, `internal/github/pagination.go` |
| Scheduling, backoff and worker admission | `internal/daemon/tick.go`, `internal/daemon/worker.go` |
| Sixteen-child per-parent rotation and durable child discovery cursors | `internal/daemon/story_refinements.go`, `internal/store/story_refinements.go` |

## Data Model

| Entity | Fields | Lifetime | Constraints |
|---|---|---|---|
| Cached polling representation | Request identity, validator, complete response body, relevant response metadata | Bounded client memory | Separate authenticated/API/representation contexts; no persistent response content; no credentials in diagnostics |
| Quota observation | Resource, limit, remaining, reset, observation freshness | Current daemon/client lifetime | Fresh network metadata only; cached historical metadata must not replenish quota |
| Discovery allowance | Shared available request capacity and reserved/in-flight work | Daemon lifetime | Shared across parent workers and pages; bounded startup and unknown-header behavior |
| Existing discovery state | Parent/child identity, comment ID and rotation | Existing store persistence | Preserve current ID-based contracts; no timestamp migration |

## API / Interface Changes

| Action | Auth | Scope |
|---|---|---|
| Conditional polling GETs for issues, parent relationships and comment pages | Existing configured authorization | Internal HTTP/client adaptation; fall back to ordinary retrieval without a usable validator |
| Quota observations and discovery request admission | Existing internal daemon/client interfaces | Internal interface only; no new public CLI, endpoint or configuration surface required |
| Request/quota/deferral summaries | Existing structured logger | Aggregate endpoint-family/status/page counts, reserve state and deferred work; exclude private response contents and credentials |

## Decisions

- Use memory-only response caching, following the owner's stated preference and request for simplicity.
- Preserve child existence and parent relationship checks during both discovery and execution.
- Preserve sixteen children per parent and the existing rotation; add a shared discovery allowance instead of replacing the scheduler.
- Interpret “significant quota” as an initial 80% reserve of the observed primary limit: 4,000 of a 5,000 allowance; pause discretionary discovery when another potentially charged request would cross that floor.
- Budget actual network requests, including later pages and unchanged responses for traffic pacing; a 304 may be primary-free but is not free network activity.
- Start with 120 background network requests/minute as a conservative proposed traffic ceiling; architecture must define bounded burst, concurrency, restart, stale-header and pagination behavior without a general-purpose scheduling framework.
- Exempt active execution/reporting from discretionary discovery admission while preserving existing GitHub refusal/backoff handling.
- Defer `since` timestamp narrowing because its correctness and migration requirements are unnecessary for the first quota-saving change.
- Polling interval remains exclusively the operator's decision.

## Business Rules and Edge Cases

- Revalidate each requested page independently; an unchanged first page does not prove later pages unchanged.
- A 304 restores only a matching complete cached representation and valid pagination metadata; it must not become an empty comment list or an SDK error.
- Missing validators, eviction and restart cause normal retrieval, subject to the discovery allowance.
- Changed/deleted/reparented/permission-denied responses retain existing error and identity behavior; cached content never masks a failed network validation.
- Mid-pagination refusal, cancellation or local budget deferral returns no partial comment result and does not advance the failed fetch's discovery cursor; prior successful children retain existing progress semantics.
- Local budget deferral is resumable scheduling state, not a fabricated GitHub refusal or successful empty discovery result.
- Multiple workers share accounting so concurrent admissions cannot each spend the same remaining allowance; the design must avoid starving later parents or repeatedly abandoning a long comment history without progress.
- A cold cache or missing/stale quota headers permits only bounded discovery observation; neither case authorizes an unlimited allowance or an assumption that all quota is available.
- Repository polling, managed-parent materialization, child discovery and pagination costs must be included or explicitly accounted for in the background budget boundary; limiting child loops alone does not bound aggregate polling.

## Assumptions

- Existing research recommendations are proposed planning decisions, not previously approved implementation requirements.
- The request for a simple fix favors two cohesive stories, no database migration, no persistent cache and no new tuning surface.
- The 80% reserve and 120 requests/minute values are initial implementation defaults subject to architecture review; no maximum discovery latency is promised while quota is constrained.
- External consumers sharing the token may consume the reserve; this feature limits litefactory background spending and cannot guarantee other clients preserve quota.
- Actual endpoint validator support remains environment-dependent; unsupported endpoints retain correctness through ordinary responses and remain constrained by budgeting.

## Out of Scope

- Timestamp cursors, `since` queries and comment-edit-trigger semantics.
- Persistent HTTP cache, cache database migrations and quota-accounting database migrations.
- Removing relationship checks, webhooks, a new scheduler framework or changing polling interval.
- Live daemon reconfiguration/restart, credentials or agent-performed authenticated forge probes.
- Claiming a measured production reduction or universal comment/parent 304 support from synthetic tests.

## Candidate Acceptance Obligations

| Candidate | Required behavior | Proof method and limits |
|---|---|---|
| CACHE-1 | Cold 200, warm conditional 304 and changed 200 deliver equivalent usable issue, parent and comment representations. | Offline HTTP fixtures through the real client; exercises local implementation, not live GitHub support. |
| CACHE-2 | Cached multi-page histories discover changed tails, preserve zero/nonzero-ID filtering and never return partial results after a later-page failure. | Offline client and discovery regression tests, including cached page-one with changed later page. |
| CACHE-3 | Missing validators, eviction, restart, authorization/representation separation and denied/deleted/reparented responses preserve correctness with bounded memory. | Offline client tests; inspect complete cache-key/body/header handling and retained checks. |
| BUDGET-1 | Concurrent background requests respect the shared 80% reserve and bounded traffic allowance, including pages, startup, stale/missing observations and reset transitions. | Deterministic daemon/client tests with injected time and quota responses; actual numeric policy finalized by architecture. |
| BUDGET-2 | Deferred work resumes fairly without lost/replayed owner admission, skipped cursor progress or starvation of later parents and long histories; active work and refusal/backoff contracts remain intact. | Deterministic daemon tests with multiple parents, pagination, exhausted allowance and later replenishment. |
| MEASURE-1 | Structured summaries distinguish network 200/304/error requests, pages and local deferrals sufficiently to compare cold/warm/changed multi-parent fixture traffic. | Retained offline scenario output plus source review; production capture remains operator-owned operational verification. |

## Evidence Boundaries

- Final stories must assign canonical AC IDs and carry criterion-specific proof inventories; candidate names above are specification references only.
- Source available: retained research brief, current client/daemon/store code and existing offline HTTP test seams.
- Version/context bounds: repository dependency and code revision exercised by retained implementation checks; no live API, runtime or nested-agent assertions.
- Missing operational evidence: authenticated endpoint validator behavior and a complete production sample with actual managed-parent workers dispatching; do not report either as measured or proven by offline fixtures.
- No nested-agent runtime criterion is affected by this feature; `docs/decisions/nested-agent-evidence.md` was reviewed.
- Questions for the owner: none.
