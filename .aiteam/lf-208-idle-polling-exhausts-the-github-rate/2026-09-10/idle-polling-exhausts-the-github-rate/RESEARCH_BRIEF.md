# Research Brief — idle-polling-exhausts-the-github-rate

## Request

Research issue `github:marioatcloudvane/litefactory#208`: idle GitHub polling reportedly exhausts the primary API quota because story discovery repeats identity/parent checks and rereads entire comment histories. Assess the diagnosis, conditional requests, incremental comment retrieval, identity-check frequency, and a quota guard. This is Research run 0001, dated 2026-09-10; no implementation or operational change is authorized by this artifact.

Provenance: **[V]** verified this run from repository inspection or explicitly linked primary documentation; **[O]** owner-reported evidence supplied in the issue, not independently measured; **[W]** unresolved external claim or research inference, explicitly qualified; **[R0]** earlier-run evidence (none used). Repository citations are paths relative to this worktree. External documentation was retrieved 2026-09-10; library source is pinned to go-github v90.0.0. No live forge requests, credentials, daemon-state access, test execution, commits, or pushes were used. The codebase-tour and web-researcher specialists supplied the findings synthesized below.

## Codebase Context

### Ownership and existing seams

- [V] This is one Go module. The pinned GitHub dependency is `github.com/google/go-github/v90 v90.0.0` (`go.mod:1`, `go.mod:7`). The daemon owns scheduling/backoff, the GitHub package owns SDK/HTTP adaptation, the store owns durable SQL/cursors, and turn execution consumes the resulting work (`internal/daemon/tick.go:391`, `internal/github/client.go:207`, `internal/store/story_refinements.go:139`).
- [V] Consumer interfaces expose an ID-based `ListCommentsAfter(..., afterID int64)` contract (`internal/daemon/daemon.go:83`, `internal/turn/story_progress.go:29`). The concrete GitHub client is concurrency safe and already accepts an injected HTTP transport/client (`internal/github/client.go:48`, `:64`, `:143`). Its central `do` wrapper observes responses and classifies failures (`:207`).
- [V] The current client has no ETag cache, conditional-request headers, or 304 handling. SDK quota checks are deliberately disabled because daemon backoff owns the policy (`internal/github/client.go:136`). This matters when interpreting upstream SDK behavior: upstream can reject an exhausted quota before transport access, but this application disables that mechanism.

### Idle dispatch is broader than `needs_poll`

[V] Each discovery unions forge sightings, all managed-feature parents, and backlog. Managed-parent discovery is independent of `needs_poll`; `ListManagedFeatures` reads all registered feature rows without a state filter (`internal/daemon/tick.go:391`, `internal/daemon/story_refinements.go:26`, `internal/store/story_refinements.go:110`). Clearing a backlog flag therefore does not remove a registered managed parent from discovery.

[V] Materialising a parent can itself issue `GetIssue`. Explicitly closed parents clear backlog and skip child work, but remain registry candidates on subsequent discovery (`internal/daemon/tick.go:984`). Every dispatched open parent enters refinement discovery (`:1033`). Its sorted child set is scanned using a durable rotation position, with a limit of `min(16, child count)` **per parent worker**, not per whole daemon tick (`internal/daemon/story_refinements.go:74`). Closed/completed children are still inspected; completed-story handling happens later (`:132`).

[V] A successfully scanned child makes three logical reads: child issue, parent relationship, and one or more comment pages (`internal/daemon/story_refinements.go:163`). With 16 children scanned every 30 seconds and one page per comment listing, the issue's arithmetic is valid:

```
16 children × 3 requests × 120 dispatch rounds/hour = 5,760 requests/hour
16 children × 3 requests ×  60 dispatch rounds/hour = 2,880 requests/hour
```

[W] These are conditional lower-bound scenarios, not measured whole-daemon rates. They assume those parents actually dispatch each round and all child reads succeed. Parent materialisation, parent comment scans, repository discovery, recovery, and multi-page histories add requests; worker capacity/failures/backoff can reduce completed rounds. All managed parents, rather than just two `needs_poll` parents, must be included in a workload inventory.

[O] The reported 403, bot account's 5,000/hour quota, two flagged parents with 9 and 7 children, ten registered parents, and operational change from 30 to 60 seconds were supplied by the issue. Their live values and exact contribution to exhaustion were not independently observed. The mechanism is verified; the claim that the calculation exactly explains the observed incident remains unmeasured.

### Comment pagination and cursors

[V] `ListCommentsAfter` sets `PerPage=100`, fetches the complete history, then filters by ID and returns ascending IDs. It never sets the SDK `Since` option (`internal/github/comments.go:72`). The shared paginator follows response pagination, caps at 100 pages, and returns no partial result on an error (`internal/github/pagination.go:59`). Thus an unchanged issue costs one request per historical page even when callers hold precise discovery cursors.

[V] `story_discovery` stores `(session_id, child_number, comment_id)` without a source timestamp (`internal/store/migrations/0012_story_refinements.sql:15`, `internal/store/story_refinements.go:139`). Parent `Watermark.UpdatedAt` records local cursor movement, not the GitHub comment's creation/update time (`internal/store/watermarks.go:13`, `:117`). It is not a safe substitute for a remote `since` bound. GitHub comments already expose both source timestamps, and the existing ID contract deliberately handles ordering and same-second arrivals (`internal/github/comments.go:36`, `:46`).

[V] Admission accepts numeric-owner identity while excluding bot/report comments. Every examined comment can advance discovery ID after any owner admission; discovery, durable admission, and consumption are separate operations so delivery can retry (`internal/daemon/story_refinements.go:184`). Parent draining and continuation-intent checks also use nonzero cursors (`internal/daemon/tick.go:1261`, `internal/daemon/continuation_dispatch.go:131`).

[V] A zero cursor has intentional complete-history meaning. Current consumers exceed the three listed in the issue: bootstrap (`internal/daemon/tick.go:1102`), progress reconciliation (`internal/turn/story_progress.go:491`), scope notices (`internal/turn/managed_scope.go:225`), refinement delivery (`internal/turn/story_refinement.go:128`), feature delivery (`internal/turn/feature_delivery.go:454`), owner exceptions (`internal/turn/owner_exceptions.go:309`), follow-ups (`internal/turn/followups.go:213`), and acceptance (`internal/turn/acceptance.go:161`). These searches recover markers/provenance. A global recent-only replacement would change their behavior.

### Identity and failure semantics

[V] Child and parent checks are independent requests. The parent endpoint validates positive identity and rejects pull-request payloads (`internal/github/issues.go:225`, `internal/github/managed_issues.go:21`, `:27`). Discovery checks gate admission into the pending queue. Execution re-fetches child and parent and rejects identity/relationship mismatch (`internal/daemon/story_refinements.go:110`). Removing discovery checks would therefore change when detached/reparented comments can enter durable state, even with execution checks retained.

[V] The first child error terminates that worker's scan after advancing its rotation position (`internal/daemon/story_refinements.go:94`). Earlier successfully processed children can already have advanced discovery cursors. The refusal log's wording about all watermarks staying unchanged (`internal/daemon/tick.go:2012`) is not evidence of a global rollback. A failed individual multi-page comment fetch does return no partial results. This nuance belongs in correctness tests; it does not justify expanding this task into a backoff rewrite.

## Domain / Problem Space

### Conditional requests offer the largest primary-quota saving

[V] GitHub explicitly exempts a conditional request returning 304 from primary quota when it is correctly authenticated using `Authorization`. ETag/`If-None-Match` and Last-Modified/`If-Modified-Since` are documented mechanisms. GitHub says most endpoints expose ETag and many expose Last-Modified; stable query parameters/page size/order help validator reuse. This verifies the issue's central optimization claim, subject to endpoint support. [GitHub REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api#use-conditional-requests)

[V] Endpoint documentation explicitly lists 304 for issue GET, but lists only 200/301/404/410 for parent GET and 200/404/410 for issue-specific comment listing. [Get issue](https://docs.github.com/en/rest/issues/issues#get-an-issue), [Get parent issue](https://docs.github.com/en/rest/issues/sub-issues#get-parent-issue), [List issue comments](https://docs.github.com/en/rest/issues/comments#list-issue-comments)

[W] Those omissions do not prove lack of support. They also do not establish that all three endpoints can become primary-quota-free. Exact authenticated validator behavior, including relationship changes, needs focused factory-owned observation before making that guarantee. No authenticated probe was performed. Current docs showed REST version 2026-03-10 examples; requesting a historical-version documentation view still returned current examples, so historical documentation fidelity is uncertain.

[V] Primary savings do not remove secondary limits. GitHub documents 100 concurrent requests across REST/GraphQL, 900 REST points/minute, compute limits, and undisclosed safeguards; most GETs cost one secondary point. There is no documented blanket 304 exemption from secondary limits and no API exposing secondary headroom. [GitHub rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)

### A validator alone is not a usable cache

[V] go-github v90.0.0 does not supply conditional caching itself; its README recommends a caching HTTP transport. Raw 304 is handled as `*ErrorResponse` by `CheckResponse`, and `Do` exits before normal body decoding. A response cache must reconstruct a usable successful representation rather than pass raw 304 to existing client calls. `newResponse` parses pagination/rate data; `bareDo` normally updates quota state before error checking, but skips that when rate checking is disabled or `X-From-Cache` is set. [Pinned README](https://raw.githubusercontent.com/google/go-github/v90.0.0/README.md), [Pinned SDK source](https://raw.githubusercontent.com/google/go-github/v90.0.0/github/github.go)

[V] HTTP cache keys include method/target URI and matching `Vary` fields. Validation updates stored response metadata while retaining applicable stored fields absent from the validation response; authenticated/shared caching has additional rules. [RFC 9111, sections 3.5, 4.1 and 4.3.4](https://www.rfc-editor.org/rfc/rfc9111.html)

[W] Design implications inferred from these contracts: cache complete response bodies and pagination metadata, isolate API base/authorization/representation contexts, bound retention, and keep fresh network quota metadata distinct from old cached metadata. Never put raw credentials in logs or persisted keys. An in-memory cache avoids new durable storage but pays cold-start costs; a persistent cache reduces those costs while retaining sensitive authenticated issue/comment content and requiring a storage/lifecycle policy.

[W] Pagination is a correctness boundary. A page-one 304 only validates page one. Later pages may have changed, and their bodies/Link relationships must still be considered. Treating page-one 304 as “no new comments anywhere” risks skipping tail additions. Conversely, replaying complete historical pages on every poll may save primary quota while preserving network, CPU, and secondary-limit cost. Changing `since` changes the representation URL and therefore cold cache behavior; continually advancing a timestamp with wall time would undermine validator reuse.

### Incremental retrieval must retain the ID contract

[V] The issue-specific comments endpoint returns ascending comment ID and accepts `since` for comments **last updated after** a timestamp formatted `YYYY-MM-DDTHH:MM:SSZ`. It supports up to 100 results/page and has no sort/direction parameters; repository-wide listing is a different endpoint. The documentation does not establish timestamp equality/precision edge behavior or snapshot consistency during pagination. [List issue comments](https://docs.github.com/en/rest/issues/comments#list-issue-comments)

[W] `since` can narrow the transfer, but cannot safely replace ID filtering. For example, resolving an old consumed comment to its newly edited update timestamp could skip higher-ID comments created before that edit. Resolving by fetching a cursor comment also adds requests and fails if it was deleted. Existing IDs may be edited and reappear under `since`; the unchanged ID contract should continue to prevent replay unless the product deliberately adds edit-trigger semantics.

[W] A conservative source-time lower bound captured with successful discovery, overlapping the timestamp boundary and still filtering by ID, is a candidate worth evaluating. It is not proven by this research. Plan must define bootstrap/deletion fallback, source timestamp choice, same-second behavior, monotonic progress, pagination failure handling, and durable migration. Local cursor-write time is specifically unsuitable. Older installations lacking a source timestamp need a safe complete-history fallback.

### Quota observability and budget policy

[V] Successful responses currently feed debug `rate_limit_remaining`, but there is no daemon-facing successful-quota interface (`internal/github/client.go:230`). Refusal errors expose limit, remaining, resource, reset, and retry delay (`internal/github/errors.go:336`). Existing debug request records include status/duration/remaining quota; comment summaries include request count and returned count (`internal/github/client.go:234`, `internal/github/comments.go:109`). They are useful measurement seams, not measurements from this run.

[V] GitHub supplies `x-ratelimit-limit`, `remaining`, `used`, `reset` and `resource`. It recommends reading response headers instead of polling `/rate_limit`, which is primary-exempt but may consume secondary allowance. Authenticated-user quota is normally 5,000/hour, with account/app exceptions and other token limits. A guard should use actual headers rather than universally assume 5,000. [GitHub rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)

[W] A useful guard needs a reserve for foreground work, resource/credential attribution, concurrent-worker accounting, and stale/absent-header behavior. The GitHub package can expose observations while the daemon remains the owner of time and policy. A conditional cache may reconstruct successful responses with old headers, so the distinction between a fresh network observation and cached metadata is essential.

## Constraints & Risks

1. [V] Preserve package ownership: SQL and durable persistence belong to `internal/store`; scheduling/backoff belong to daemon. The existing central HTTP seam is reusable; direct SQL from the GitHub client would introduce the wrong dependency (`internal/github/client.go:207`, `internal/store/story_refinements.go:139`; project `AGENTS.md`, `.aiteam/APP_CONSTRAINTS.md`).
2. [V] Preserve numeric identity checks, retryable pending delivery, complete-history reconciliation, no-partial-results pagination failures, and the ID consumption contract. These are observable behavior, not incidental request overhead (`internal/daemon/story_refinements.go:110`, `:184`, `internal/github/pagination.go:59`).
3. [W] Validate cached representation freshness after mutations, reparenting, deletion, permission changes, and auth changes. An ordinary issue ETag must not be assumed to certify its separate parent relationship. No evidence establishes that the two identity requests can be collapsed without losing the relationship check.
4. [W] Keep fair scanning bounded: primary-free polling still spends time, bandwidth, secondary allowance, and worker capacity. The existing bound is per parent, so aggregate demand grows with active managed parents. Increasing/removing sixteen without a global policy is not justified by 304 support alone.
5. [V] Wire-level fixtures capture method/path/query/headers/body (`internal/github/github_test.go:20`, `:45`). Existing tests cover ordering, zero-cursor history, pagination, timestamps, bounds, SDK refusal, and daemon backoff (`internal/github/comments_test.go:21`, `:55`, `:75`, `:102`; `internal/github/pagination_test.go:130`; `internal/github/sdk_test.go:177`; `internal/daemon/tick_test.go:974`).
6. [V] Refinement tests cover independent parent watermarks, closed children, owner rename, failed-post retry, and post-promotion refusal (`internal/daemon/story_refinements_test.go:27`, `:45`, `:74`, `:97`). Their parent fixture substitutes issue lookup for the actual relationship endpoint (`:23`), so it cannot verify relationship validators. No existing cache/304 tests or dedicated sixteen-child rotation regression were found in the scoped tour.
7. [W] Future validation should exercise cold 200/warm 304/changed 200; absent validators; cached pagination with tail changes; authenticated key isolation; mutation/delete/reparent invalidation; same-second and edited comments; legacy/zero cursor fallback; later-page refusal without cursor advancement; restart behavior; and concurrent budget observations. These are research-derived evidence needs, not executed checks or a story plan.

## Open Questions

| Question | Evidence needed / decision owner |
|---|---|
| What is the actual idle request distribution across endpoints, status codes, pages, and dispatched open parents? | Factory/operator-owned observation using existing debug seams or purpose-built instrumentation; no daemon configuration changed in this run. |
| Do comment and parent endpoints return usable validators, and does reparenting invalidate the relationship representation? | Focused authenticated external verification owned by the factory/implementation environment; public docs alone are insufficient. |
| Should cache state survive restarts, and with what capacity, lifetime, auth isolation, and content retention? | Plan/architecture decision informed by cold-start frequency and acceptable storage cost. |
| What exact timestamp bound and overlap preserve discovery under edits, same-second arrivals, deleted cursor comments, and migration? | Plan/architecture contract, then focused boundary tests and external semantics verification where docs are silent. |
| May detached/reparented comments be admitted temporarily, or must discovery retain both identity gates? | Explicit product/architecture decision before removing checks; execution-time rejection alone does not preserve admission semantics. |
| Is sixteen still the desired per-parent latency/fairness bound, and is an aggregate discovery budget needed? | Plan decision based on measured managed-parent inventory and secondary/request-time cost. |
| What foreground quota reserve and concurrent-worker policy should a guard enforce? | Plan/product decision using response-header limits, not a universal 5,000 constant. |
| When can the reported 60-second mitigation be reversed? | Deliberate operator decision after measured savings and correctness evidence; research makes no runtime change. |

## Recommended scope for Plan mode

[W] The evidence supports prioritizing conditional revalidation on the polling reads, with a correct bounded response cache and explicit endpoint-support fallback. Evaluate timestamp narrowing as a complementary optimization for growing histories while keeping ID cursors and complete-history consumers intact. Include measurement sufficient to compare cold, unchanged, and changed workload and determine whether a daemon quota reserve is needed. Decide persistence, relationship-check policy, and fairness explicitly rather than leaving them implicit in an HTTP change.

[W] Preserve the existing refusal/backoff behavior and execution identity gates. Do not assume all unchanged polling will be free, remove relationship checks on the strength of issue validators, globally reinterpret zero cursors, or automatically restore the polling interval. Plan should resolve the open design choices into an approved scope; this research does not select dependencies, create stories, or authorize implementation.

[O] Headless assumption: the request is investigation of the reported efficiency defect, not permission to inspect account credentials, edit operational configuration, or run a live measurement. Unresolved facts are retained above instead of asking questions or waiting for confirmation.
