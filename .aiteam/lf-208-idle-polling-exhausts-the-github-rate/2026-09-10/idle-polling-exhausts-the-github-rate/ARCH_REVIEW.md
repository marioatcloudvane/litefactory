# Architect: Go Architect
# Created: 2026-09-10

## Verdict

APPROVED for a two-story plan with the decisions below incorporated. No owner question or standards deviation is needed. Review source revision: `c16fb64a76646f5744b659835238824fe9cbc51d`. This is architectural review of the feature specification, not implementation acceptance or proof of live GitHub behavior.

## System Overview

The existing `internal/github.Client` owns the single authenticated SDK stack and full-history pagination. Add a bounded, private in-memory conditional-response cache there. The daemon continues to own admission, time, rotation and backoff. A small request-observation/admission seam connects them without importing daemon into github or introducing a new scheduler package.

Discovery goes through admission, then the existing client, then conditional network validation. A complete 200 representation can replace a cache entry; a valid 304 restores the matching complete representation and pagination headers for SDK decoding. Actual network observations feed counters and the daemon-owned quota policy. Existing ID filtering, numeric identity checks, durable admission and independent consumption remain unchanged.

New components are private cache state in `internal/github` and shared discovery allowance/accounting in `internal/daemon`. There is no new database schema, persistent cache, configuration key, server, dependency or background goroutine.

### Boundaries and data flow

- Cache entries belong to one client/authenticated identity and exact API representation: method, base/URL/query, Accept and API-version headers. Never share entries across clients or reuse content after an unsuccessful validation.
- Clone requests, headers and cached bytes at ownership boundaries (CONC-1). Keep cache metadata/body limits explicit and finite; skip caching oversized responses without truncating the normal response.
- Preserve the SDK's same-origin page-number traversal, existing `maxPages=100`, error taxonomy, and no-partial-results behavior (`internal/github/pagination.go`). A first-page 304 never suppresses later-page requests.
- `internal/daemon` labels background discovery work and owns allowance state. Existing active execution, reporting and mutations bypass local discovery admission but their response quota observations inform the remaining-quota estimate.
- Time/refill decisions originate at Tick using its existing timestamp; workers consume shared grants, not fresh daemon clocks. No retry or sleep below the daemon (CONC-6); no new goroutine (CONC-2).

## Cross-story decisions

1. Memory-only cache, conditional validation on every use. Cache successful GET representations for issue retrieval, direct parent lookup and issue comment pages; missing validators, restart and eviction use ordinary GET. Follow applicable response cache restrictions; do not store a representation when it cannot safely be retained. Preserve current denial/deletion/reparenting behavior and never serve stale content on errors. HTTP conditional support is opportunistic, not assumed.
2. Keep full histories and authoritative ID filtering, both discovery/execution relationship checks, and sixteen children per parent. `since`, durable response content, interval changes and new operator settings are deferred.
3. Background primary-quota floor is `ceil(0.8 * observed_limit)`. Before admission subtract outstanding pessimistic reservations; normal discovery cannot cross that floor. Charged background spending within the observed reset window is also bounded to 20% of the observed limit. Refund proven correctly authenticated 304 charges, but count every network request against the separate traffic allowance. Active work and other token users can consume quota; the policy cannot promise they preserve the reserve.
4. Traffic allowance is a shared token bucket refilled at 120 requests/minute, capacity 120, initially full. This is explicitly a burst allowance of 120, not a strict rolling-minute maximum. Refill from daemon tick time, not sleeps. Reserve one request for scalar operations and the existing maximum 100 for paginated operations; refund unused reservations when the operation ends. Once admitted, a supported full-history fetch has enough allowance to finish without repeatedly restarting at page one. Actual errors/cancellation still return no partial result. This conservative reservation avoids a new persistent or resumable pagination system.
5. All background network discovery participates: repository issue/comment listings, candidate/managed-parent materialization, child checks, parent comment discovery and every pagination page. Request classification ends before active execution/recovery/report delivery. A denied operation returns a distinguishable local deferral, never a GitHub RateLimitError or successful empty list; keep relevant cursors/retry intent unchanged. Preserve pending active work even when fresh background discovery is deferred.
6. Preserve fair parent/repository rotation and give a deferred operation its next eligible admission opportunity before repeated small operations consume its needed reservation. Retain only bounded in-memory deferral ordering; no new scheduler service. Test a 100-page supported history alongside short histories and multiple parents/repositories. Quota exhaustion may defer until reset; no fixed detection-latency guarantee is claimed. Do not count loop iterations as network requests.

### Quota observations and recovery defaults

Use valid Limit/Remaining/Reset observations for the core bucket, including active responses. Apply same-window observations conservatively under out-of-order concurrent completion and outstanding requests; a delayed response cannot increase already-consumed allowance. Reset transitions require fresh response evidence rather than assuming a full bucket at wall-clock rollover.

A quota observation is stale after two minutes of daemon tick time or its reset time, whichever comes first. Unknown/stale state allows only one globally shared scalar observation request per minute, charged to the traffic allowance; it never permits a speculative 100-page operation. Reuse an ordinary issue GET as that observation, rather than add a new rate-limit endpoint. If it supplies no usable headers, discovery remains deferred until a later bounded observation or active response yields usable data. The observation exception can cost up to 60 primary requests/hour when metadata is unavailable; it does not claim the 80% reserve can be proven in that state. Known exhausted quota waits for reset before observation. Missing/invalid metadata does not erase previously known tighter constraints.

The principal tradeoff is deliberate: pessimistic 100-request pagination reservation can delay a one-page or warm cached list near the quota floor. It prevents starvation of long supported histories and removes the need for partial-history continuation. The network allowance bounds warm 304 traffic as well as charged 200 traffic. Unsupported validators continue to return correct full results within the same allowance.

## Task Hints for the Proposed Two-Story Split

No frozen task IDs exist yet; the product owner should map the following hints to its typed tasks without creating local story files.

### Story 1: Conditional responses — implementation task

Agent: go-implementation-engineer.

Package placement: existing `internal/github` construction, transport and observation paths. Reuse `Options.HTTPClient`, the single credential reveal path, existing SDK decoding and pagination. Preserve caller-owned transport/client configuration by wrapping a copy. Bound retained body bytes, entry count and metadata. Reconcile 304 entity/pagination headers while preserving actual response status/quota metadata separately for accounting. Never cache auth failures or return stale success. No new credential path, external HTTP dependency, endpoint or scheduler. Expose only the narrow seam needed by Story 2 and existing test fakes (TEST-5, TEST-10, SEC-8).

### Story 1: Correctness tests

Agent: go-unit-test-engineer for isolated cache behavior; go-integration-test-engineer for offline real-client httptest flows.

Prove cold/warm/changed behavior, exact query/representation isolation, complete-body/metadata bounds, oversized bypass, eviction/restart, denial/deletion/reparenting, cached page one plus changed tail, ID-zero and nonzero filtering, later-page failure, cancellation and concurrent use. Use existing stdlib fixtures (TEST-1 through TEST-4); no tags or paid/live calls. An HTTP fixture demonstrates local support for supplied validators, not live endpoint validator support.

### Story 2: Shared allowance and summaries — implementation task

Agent: go-implementation-engineer.

Place policy and fair deferral handling in `internal/daemon`; reuse tick timestamps, parent/repository queues, rotation, `needs_poll`, and existing cursor semantics. Client supplies actual network outcomes, page counts and quota observations. Wire operation reservations so simultaneous workers cannot reserve the same quota. Release unused allowance on all exits. Distinguish local scheduling deferrals without arming GitHub backoff or starving already-pending active work. Keep the existing real GitHub refusal path intact. Extend `slog` tick/worker summaries with actual 200/304/other-error requests, pages, local deferrals and useful quota state; do not introduce metrics infrastructure or log cached bodies/tokens.

### Story 2: Policy and regression tests

Agent: go-unit-test-engineer for deterministic accounting; go-integration-test-engineer for daemon plus real httptest client/store.

Cover concurrent reservations, refunds, out-of-order observations, 80% floor and 20% spending cap, initial 120 burst/refill, unknown/stale/reset observations, real refusal/backoff, active bypass, unchanged watermarks, queued work delivery, fair repos/parents/children and a 100-page list completing after admission. Retain a cold/warm/changed multi-parent fixture request summary as measurement evidence. Use channels/Quiesce and real subsequent ticks, never time.Sleep (TEST-8, TEST-9). Store promises use migrated real SQLite (TEST-6). Run applicable focused checks and final `make check`; Validate later reads retained evidence only (SEC-21, CONC-11).

## Platform Notes

Platform: project layout/interfaces — preserve existing github/daemon direction and consumer-owned seams; no generic cache or rate-limiter package.
Platform: error handling — typed local deferral distinct from real API refusal; preserve wrapping and current outcomes (STYLE-5, CONC-3).
Platform: concurrency — synchronize shared cache/accounting, clone mutable data, honor cancellation, no added workers (CONC-1, CONC-2).
Platform: dependencies — stdlib and existing SDK only (TEST-2).
Platform: HTTP — outbound client only; inbound router/server/graceful shutdown additions not applicable.
Platform: database/configuration — no new persistence or setting; existing durable cursor APIs remain authoritative (CFG-8, CONC-5).
Platform: tests/observability/lint — existing offline fixtures, structured slog and make check; no metrics stack or integration build tags.

## Evidence and Remaining Operational Limits

Feature proof is available from source inspection, deterministic unit tests and real local HTTP/store fixtures. Read-only acceptance uses the immutable implementation snapshot and retained checks; no tests were executed for this architectural review. Full production request traffic and authenticated comment/parent conditional support remain unmeasured. They are operational verification, not a prerequisite for correctness of the opportunistic implementation or its typed Plan submission.

The nested-agent evidence decision applies as a planning inventory requirement (EXT-4), but no acceptance behavior here requires a nested coding-agent runtime. Do not add paid harness launches or promote synthetic HTTP observations to authentic GitHub evidence. No protected instruction tree is in feature scope and no instruction-parity story is needed.
