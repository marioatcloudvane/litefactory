# Research Brief — idle-polling-exhausts-the-github-rate

## Request

Research run 0003, issue `github:marioatcloudvane/litefactory#208`, 2026-09-10. Continue the existing research session. The owner asks for actual request measurement; further conditional-endpoint research; nonpersistent versus persistent cache tradeoffs; recommendations for timestamp cursors, relationship checks and fair polling; a significant quota reserve; and removal of the polling interval as a feature decision.

**Provenance:** [V] verified this run through the delegated repository/log tour or cited primary documentation; [O] owner statement; [W] research recommendation or inference, not approved requirements; [R0] retained earlier research, not reverified unless explicitly stated in the current findings. Recommendations below are options for Plan, not implementation authorization or an approved plan.

[O] The owner leans against cache persistence, wants significant quota reserved, and retains the interval decision. [V] This run used read-only source inspection, a narrowly scoped read of the known daemon log, and public documentation. No credentials, configuration contents, database, live forge APIs, runtime changes, tests, commits or pushes were used. Source inspection, observed runtime versions, and historic incident evidence are distinct.

## Codebase Context

[V] Current ownership remains daemon scheduling, GitHub HTTP/SDK adaptation, and store persistence. go-github remains v90.0.0. Discovery includes registered managed parents independently of `needs_poll`, then open state, worker availability and inflight gates determine whether a parent actually dispatches (`internal/daemon/story_refinements.go:23`, `internal/daemon/tick.go:391`, `:984`).

[V] A selected child costs `2 + comment listing pages` logical API requests. The limit is sixteen children per parent worker opportunity, not globally per tick; durable rotation advances even after a failed child, and that worker then stops (`internal/daemon/story_refinements.go:75`, `:94`, `:163`). The existing comment contract remains ID-based, with zero meaning complete history; no `since` is sent (`internal/github/comments.go:41`, `:70`). Both remote timestamps already exist on returned comments (`:36`).

[V] The wrapper observes all SDK endpoint responses at DEBUG, while repository issue/comment listing request summaries are INFO. Child-specific comment listing summaries are DEBUG. Thus the earlier claim that all request counters require DEBUG was too broad (`internal/github/client.go:207`–`:240`, `internal/github/issues.go:202`, `internal/github/comments.go:101`, `:182`).

## Domain / Problem Space

### 1. Can actual idle traffic be measured?

**Yes, partially from retained logs; the requested whole-daemon total cannot be recovered from this capture.**

[V] The tour derived the default log location from `internal/config/paths.go:45`, `:101` and read only the relevant log, `/Users/mariomh/.litefactory/logs/daemon.log`. No configuration or state database was read. The log is documented as unredacted (`:104`–`:106`); this artifact retains aggregate observations and minimal incident metadata, not raw records.

| Observation | Actual evidence |
|---|---|
| Window | 2026-09-10 09:10:00 inclusive to 09:30:00 exclusive, +02:00 |
| Completed ticks | 20, approximately 60 seconds apart |
| Worker fields | All twenty report zero inflight and zero workers dispatched |
| Runtime version | `3955ecd` |
| Repository issue listings | 20 records, each reporting one request |
| Repository comment listings | 20 records, each reporting one request |
| Measured request lower bound | **40 requests in 20 minutes** from those two endpoint families |
| Hourly extrapolation | **120 requests/hour for those families only**; extrapolated, not an hour-long measurement |

[V] Tick records occur at log lines 99656–99713. Two independent pattern counts returned 40 listing records: all matching listing records in the window, and the same records additionally requiring `requests:1`. There were no matches anywhere in the retained log for the all-endpoint `github: api request` debug message or child-specific `github: listed issue comments` summaries.

[V] Zero workers does not establish zero other calls: parent materialisation can fetch an issue before rejecting a closed parent and skipping dispatch (`internal/daemon/tick.go:984`–`:1005`). The retained log therefore cannot reveal total parent/child identity reads, child comment pages, per-response quota deltas, or status distribution. It also cannot establish the current or historical managed-parent population.

[V] A real historical refusal is independently retained at log line 97360: 2026-09-09 21:51:27 +02:00, runtime `03d0481`, parent #73, child #172 comment listing, HTTP 403, primary core limit (`secondary=false`), retry 16m19.720603s. Another refusal occurred at 21:58:27 on repository issue listing. This confirms actual exhaustion, not which requests caused it. The source-reported #196 incident, observed #172 refusal, and present zero-worker sample must not be treated as the same trace.

[W] The earlier **5,760/hour** remains a valid model for sixteen one-page child scans every thirty seconds, not a measured result. The new 120/hour partial observation does not disprove it: this sample dispatched no parent workers and omits endpoint families.

[W] To finish measurement, a factory/operator-controlled capture must include all endpoint responses over an idle window in which managed parents actually dispatch, plus a cold-start and changed-comment sample. Record endpoint class, time, status, actual transport attempt count, comment pages, quota limit/remaining/used/reset/resource, cache outcome, and worker/tick correlation. Existing DEBUG records provide method/URL/status/duration/remaining, but lack several of those fields, omit response-less transport failures, and count SDK responses rather than necessarily every redirected network exchange. Workers can outlive their tick (`internal/daemon/tick.go:1020`); grouping only between tick-complete lines is unsound. This run did not toggle log level or restart the daemon because repository instructions prohibit those operational changes.

### 2. What further research establishes conditional support?

[V] Current official documentation explicitly lists **304 for issue GET**. The issue-specific comment list documents 200/404/410; parent GET documents 200/301/404/410. Neither of the latter two endpoint pages explicitly promises 304 or describes validator invalidation on reparenting. Missing documentation is neither proof of support nor proof of absence. [Get an issue](https://docs.github.com/en/rest/issues/issues?apiVersion=2022-11-28#get-an-issue), [List issue comments](https://docs.github.com/en/rest/issues/comments?apiVersion=2022-11-28#list-issue-comments), [Get parent issue](https://docs.github.com/en/rest/issues/sub-issues?apiVersion=2022-11-28#get-parent-issue).

[V] GitHub's general guarantee remains useful: correctly authorized conditional requests returning 304 do not spend primary quota; most endpoints expose ETag and many expose Last-Modified. It recommends stable query parameters and ordering. This does not guarantee all GET endpoints support the mechanism. [REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api#use-conditional-requests).

[V] Documentation was fetched on 2026-09-10. Requested historical-version URLs rendered examples using 2026-03-10. These findings describe the fetched documentation, not verified historical behavior or the running daemon's API version/auth combination.

[W] Recommendation: use validators opportunistically, with ordinary complete 200 responses as the fallback. Advertise measured savings per endpoint, not “all idle polls are free.” Retain a specific verification gate for comments/parent validators, unchanged responses, reparenting, detachment, permission loss and pagination under the actual configured API/auth context. No authenticated probe was available within this agent's forge boundary; more public-documentation reading cannot substitute for that evidence.

[V] HTTP 304 has no body; Link is not a required 304 field. Cache validation merges applicable metadata into the stored response. [RFC 9110 §15.4.5](https://www.rfc-editor.org/rfc/rfc9110.html#section-15.4.5), [RFC 9111 §4.3.4](https://www.rfc-editor.org/rfc/rfc9111.html#section-4.3.4).

[W] Consequently preserve stored body and applicable Link on 304, but discard obsolete links when a fresh 200 replaces a response and has no Link. A first-page 304 does not establish that later pages are unchanged; each relevant page must be considered. Caching must respect no-store, Vary, authorization isolation and representation/API-version differences. These are correctness conditions, not merely performance details.

### 3. Cache persistence: pros and cons

[W] **Recommendation: start with a bounded in-memory response cache, matching the owner's preference.** Persist correctness cursors separately; that is independent of whether response bodies survive restarts.

| Choice | Advantages | Costs and risks |
|---|---|---|
| Memory only — recommended | Simple lifecycle; no durable copy of private response bodies; no cache schema/recovery/storage maintenance; process/auth replacement naturally abandons old entries | Cold restart and eviction require paid 200s; bytes and bandwidth return on startup; repeated restarts can consume quota; startup must obey the same guard |
| Persistent response cache | Reuses validators and bodies after restart; reduces cold-start bandwidth and quota for stable histories; useful if restarts are frequent or many pages are retained | Sensitive body retention, auth/account partitioning, disk limits/eviction and crash recovery; additional store ownership/migrations and invalidation policy; stale or unauthorized cache reuse must be prevented |

[W] Missing cache entries must always mean “fetch authoritative data,” never “nothing changed.” Even in memory, revalidate each polling representation instead of serving an unvalidated response for a long freshness period. Bound retained bytes, evict by recency, respect HTTP storage controls, avoid raw-token keys/logging, and discard entries when credential context changes. Exact capacity should follow measured working-set size rather than an unsupported number.

[W] Revisit persistence only if measured restart/eviction costs are material despite the quota guard. Model cost as cold responses per restart times restart frequency, including each historical page. A single startup sweep is not by itself a reason to add durable response storage.

### 4. Recommended timestamp strategy

[V] The API filters by **last update after** `since`, orders issue comments by ascending ID, and exposes creation/update timestamps. It does not promise inclusive boundaries, fractional-second comparisons, bounded propagation delay, ID/creation monotonicity or a cross-page snapshot. [Issue comments reference](https://docs.github.com/en/rest/issues/comments?apiVersion=2022-11-28#list-issue-comments).

[W] **Recommendation: keep the durable ID cursor authoritative; treat `since` as a conservative transfer optimization with a full-history fallback.** Prefer deploying conditional retrieval first if timestamp safety cannot yet be demonstrated.

- Persist the source **creation time associated with the advanced ID cursor**, obtained from the successfully read response. Keep the ID/time pair and durable admission crash-consistent. Do not use local poll time, cursor-write time, or the largest update timestamp.
- As an initial proposal, use that creation time minus **60 seconds** for `since`, and continue filtering/deduplicating by ID. The overlap is a conservative tuning proposal, not an API guarantee or proof against arbitrary delayed visibility. An old comment edited today must not move the scan boundary to today.
- Keep the same query bound while no new ID is observed, preserving cache reuse. Advance only after a complete successful pagination/admission sequence; a failed page must not advance that fetch's discovery cursor.
- Legacy cursors with no source timestamp, missing/invalid timestamps or inconsistent source ordering fall back to full history. Obtain a trustworthy ID/time association from a complete scan; do not add a cursor-comment GET that can fail on deletion or use its changed update time.
- Cursor zero must continue to mean complete history for reconciliation/marker/provenance consumers. Existing edits to consumed IDs remain nontriggers under today's contract; this feature should not silently add edit replay.
- Require boundary evidence for same-second comments, edited old comments, deletion, page changes, restart/migration and partial failures. A periodic complete-ID-filtered reconciliation can expose higher-ID comments missed by a time window, but adds cost and does not create a mathematical completeness guarantee under unspecified visibility delays.

[W] This approach avoids the concrete edit-induced timestamp jump and handles legacy state conservatively. It still relies on source-time/visibility behavior not fully guaranteed by public docs. If the required correctness standard cannot tolerate that remaining uncertainty, retain conditional full-history discovery and defer `since`; the highest-value primary-quota improvement does not depend on shipping both together.

### 5. Recommended identity and relationship checks

[V] Discovery validates child numeric identity and direct parent identity before admitting comments; execution repeats both checks. The parent endpoint returns a parent object, not the immutable child ID, so that response cannot replace both checks by itself (`internal/daemon/story_refinements.go:109`–`:120`, `:164`–`:190`; `internal/github/managed_issues.go:14`–`:30`).

[W] **Recommendation: keep both discovery checks and both execution checks.** Apply conditional revalidation to the independent child and parent representations where supported. Do not infer an unchanged relationship from an unchanged child issue ETag. Handle deletion/access loss as fresh authoritative failures; never replay an old success after one.

[W] Removing checks or verifying only every few minutes cuts requests when parent validators are absent, but admits comments from moved/detached children into durable pending state and delays diagnosis. Execution rejection protects processing but does not preserve admission behavior. “A tick later” is not a bound when dispatch is deferred or quota-limited. The safe default changes efficiency while retaining the current workflow; periodic verification is a separate explicit behavior choice if measurements later justify it. The two requests are not an atomic relationship snapshot even today; retaining execution-time validation remains necessary.

### 6. Fairness and the sixteen-child bound

[W] **Recommendation: retain sixteen as a per-parent ceiling, and add a shared request scheduler across background discovery.** Rotate parents and children fairly, charge each actual request/page, and pause/resume at budget exhaustion without losing queue or cursor progress. A global cap must include parent materialisation and repository listings as well as child reads.

| Alternative | Comparison |
|---|---|
| Sixteen per parent alone | Preserves current behavior and simple rotation, but demand grows with parent count and histories; concurrency alone does not bound requests |
| Increase/remove the ceiling after adding 304 | Lower latency for a large parent when idle, but 304 still consumes network/secondary resources; cold or unsupported endpoints can spend primary quota rapidly |
| Shared request budget plus retained ceiling — recommended | Bounds aggregate work and keeps small parents from waiting behind one large parent; increases sweep latency when constrained and requires explicit pause/resume fairness |
| Fixed sixteen children globally | Simple aggregate child bound, but a child may have many pages; child count does not actually bound requests, and careless parent ordering can starve others |

[W] Without throttling/failure/deferred dispatch, a parent needs `ceil(children/16)` **worker opportunities** for a sweep; wall-clock time is not guaranteed. At a proposed 120 discovery network requests/minute, sixteen one-page children need at least 24 seconds of network budget, excluding other calls. Ten fully scanned sixteen-child parents need at least four minutes at that rate, again excluding other calls. These are scheduling illustrations, not workload measurements or latency promises.

[V] GitHub documents secondary constraints including 100 concurrent REST/GraphQL requests and 900 REST points/minute **to an endpoint**, plus compute and undisclosed limits; most GETs cost one point. There is no documented universal secondary exemption for 304. [Rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).

[W] A conservative starting ceiling of **120 total background network requests/minute**, paced with a small burst and at most **four** in-flight background reads, is an application proposal below those documented ceilings, not a guarantee against secondary throttling or a replacement for existing backoff. Lower concurrency must win if existing configuration is more restrictive. Tune from measurements; no runtime change is made here.

### 7. Significant quota reserved

[O] The owner requires “significant quota” for useful work but has not approved an exact percentage.

[W] **Proposal: protect 80% of the actual primary allowance for foreground/other work; permit background discovery to consume at most 20% per server reset window.** With a 5,000 allowance this is a **4,000-request reserve and at most 1,000 charged discovery requests/hour**. Use observed limit/resource/reset, not a hard-coded 5,000. These are proposed defaults for Plan.

[W] The guard would stop potentially charged discovery whenever it would cross the reserve, even if discovery has not spent its own 20%. Foreground work may use the reserve; this is a background spending restriction, not a promise that useful work or another process cannot exhaust quota. At 1,000/hour, a 48-request cold/fully changed scan alone fits about twenty times/hour before other discovery costs. Primary-free 304 revalidation does not spend the charged allowance, but still obeys the separate network pacing in question 6.

[W] Reserve each outgoing request pessimistically as a potential charge; release that provisional charge only on a confirmed primary-free 304. A cached validator is not permission to send a request below the reserve because the server may return a paid 200. Budget each page, with fair deferral and existing retry semantics. Shared atomic accounting must cover workers using the same quota owner/resource; fresh server observations should be reconciled conservatively against outstanding reservations, delayed responses and reset windows. Cached old headers must not replenish allowance.

[W] On missing/stale header knowledge, prefer learning from foreground traffic; allow at most a single serialized bootstrap/background probe every thirty seconds until trustworthy headers arrive, while obeying known refusals/reset timing. This is a proposed bounded fallback, not a quota guarantee when other consumers spend invisibly. Persisting a small budget counter/reset record, if necessary to prevent restart-based allowance resets, is distinct from persisting response bodies. Restart must not blindly reset a locally enforced 20% allowance while retaining the same server window.

[V] User-scoped API allowance can be shared by tokens/apps, whereas installation-based quota differs. Response headers are the preferred observation source; `GET /rate_limit` is primary-free but may spend secondary quota, and no endpoint reports secondary headroom. Other processes may consume the same budget, so a daemon-local reserve cannot control them. [Rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).

### 8. Polling interval

[O] **Resolved: the user decides.** Remove “restore thirty seconds” from the feature's unresolved questions and acceptance scope. Keep the current configuration untouched; evaluate efficiency at the configured interval.

## Constraints & Risks

- [V] No production or runtime changes belong to this Research run. Store owns SQL/durable cursors, daemon owns scheduling/time/budget policy, and GitHub owns response adaptation. The current central request wrapper is an existing observation seam.
- [W] Endpoint 304 behavior and timestamp consistency remain external evidence gaps. The design must work with absent validators and preserve correctness when time narrowing cannot be trusted.
- [W] Memory-only caching trades startup cost for simpler retention. Primary savings and request count are separate: revalidating every old page may save primary quota while retaining secondary/network cost.
- [W] Global throttling can delay comments. Reserve policy must surface deferral and sweep age so “quiet” does not disguise starvation. Both known quotas and aggregate fairness must be considered.
- [V] The present runtime sample had no parent workers, and lacks all-endpoint diagnostics. It is not a measurement of the original sixteen-child scenario.
- [W] Neither ID/time overlap nor HTTP caching should alter zero-cursor/full-history callers, numeric-owner admission, durable retry delivery or execution identity validation.
- [R0] Existing fixture/test coverage and SDK 304 adaptation details are retained below, but were not re-audited this run. No tests were executed because only research artifacts changed.

## Open Questions

The owner has received a recommendation for each design question; the remaining items are evidence or concrete choices for Plan, rather than a repeat of the original eight questions.

| Remaining item | Who resolves it / proposed default |
|---|---|
| Complete request/status/page/quota measurement while managed parent workers actually dispatch | Factory/operator supplies permitted all-endpoint capture; this run's measured lower bound is 40 requests/20 minutes |
| Actual comment/parent validators, changed relationship and permission behavior, pagination metadata under real auth/API version | Factory-controlled verification; retain ordinary 200 fallback and relationship gates until established |
| Timestamp source-time/visibility assumptions sufficient for the required no-loss standard | Architecture/validation; proposed ID + source creation time with overlap and full-history fallback, or defer narrowing |
| Cache working-set capacity and eviction effects | Plan using measured bodies/pages; memory-only response cache is recommended |
| Exact reserve/pacing values and fair pause/resume mechanics, including restart accounting | Plan evaluates proposed 80% reserve, 20% charged ceiling, 120 background network requests/minute and four in-flight requests |
| Maximum acceptable deferred-discovery latency under quota pressure | Plan states the tradeoff using measured population; no latency promise follows from sixteen alone |

[O] Poll interval is not an open feature question. [W] Cache persistence and removing identity gates need not block progress: take the recommended memory-only cache and retained gates unless Plan selects a different explicit behavior.

## Recommended scope for Plan mode

[W] Prioritize measurable, bounded conditional revalidation with correct bodies/pagination/auth handling, global discovery pacing and a substantial foreground reserve. Keep the existing relationship checks, complete-history consumers and refusal/backoff semantics. Treat timestamp narrowing as a complementary optimization with a demonstrated safe fallback; do not make primary-quota improvement depend on an unproven timestamp assumption. Use memory-only response caching unless cold-start evidence changes the tradeoff. Include observable request/quota/deferral outcomes sufficient to verify the intended savings. The polling interval stays outside scope.

[O] This run answers the requested research follow-up. It does not select another mode, create stories, change settings or seek confirmation.

## Retained technical evidence from run 0001

The following detail is preserved for continuity. All earlier verified assertions are now marked [R0]; original inferences and owner reports are also explicitly labeled. Current findings above supersede earlier wording where instrumentation or evidence has been refined.

### Codebase Context

#### Ownership and existing seams

- [R0] This is one Go module. The pinned GitHub dependency is `github.com/google/go-github/v90 v90.0.0` (`go.mod:1`, `go.mod:7`). The daemon owns scheduling/backoff, the GitHub package owns SDK/HTTP adaptation, the store owns durable SQL/cursors, and turn execution consumes the resulting work (`internal/daemon/tick.go:391`, `internal/github/client.go:207`, `internal/store/story_refinements.go:139`).
- [R0] Consumer interfaces expose an ID-based `ListCommentsAfter(..., afterID int64)` contract (`internal/daemon/daemon.go:83`, `internal/turn/story_progress.go:29`). The concrete GitHub client is concurrency safe and already accepts an injected HTTP transport/client (`internal/github/client.go:48`, `:64`, `:143`). Its central `do` wrapper observes responses and classifies failures (`:207`).
- [R0] The current client has no ETag cache, conditional-request headers, or 304 handling. SDK quota checks are deliberately disabled because daemon backoff owns the policy (`internal/github/client.go:136`). This matters when interpreting upstream SDK behavior: upstream can reject an exhausted quota before transport access, but this application disables that mechanism.

#### Idle dispatch is broader than `needs_poll`

[R0] Each discovery unions forge sightings, all managed-feature parents, and backlog. Managed-parent discovery is independent of `needs_poll`; `ListManagedFeatures` reads all registered feature rows without a state filter (`internal/daemon/tick.go:391`, `internal/daemon/story_refinements.go:26`, `internal/store/story_refinements.go:110`). Clearing a backlog flag therefore does not remove a registered managed parent from discovery.

[R0] Materialising a parent can itself issue `GetIssue`. Explicitly closed parents clear backlog and skip child work, but remain registry candidates on subsequent discovery (`internal/daemon/tick.go:984`). Every dispatched open parent enters refinement discovery (`:1033`). Its sorted child set is scanned using a durable rotation position, with a limit of `min(16, child count)` **per parent worker**, not per whole daemon tick (`internal/daemon/story_refinements.go:74`). Closed/completed children are still inspected; completed-story handling happens later (`:132`).

[R0] A successfully scanned child makes three logical reads: child issue, parent relationship, and one or more comment pages (`internal/daemon/story_refinements.go:163`). With 16 children scanned every 30 seconds and one page per comment listing, the issue's arithmetic is valid:

```
16 children × 3 requests × 120 dispatch rounds/hour = 5,760 requests/hour
16 children × 3 requests ×  60 dispatch rounds/hour = 2,880 requests/hour
```

[R0; inference] These are conditional lower-bound scenarios, not measured whole-daemon rates. They assume those parents actually dispatch each round and all child reads succeed. Parent materialisation, parent comment scans, repository discovery, recovery, and multi-page histories add requests; worker capacity/failures/backoff can reduce completed rounds. All managed parents, rather than just two `needs_poll` parents, must be included in a workload inventory.

[R0; owner report] The reported 403, bot account's 5,000/hour quota, two flagged parents with 9 and 7 children, ten registered parents, and operational change from 30 to 60 seconds were supplied by the issue. Their live values and exact contribution to exhaustion were not independently observed. The mechanism is verified; the claim that the calculation exactly explains the observed incident remains unmeasured.

#### Comment pagination and cursors

[R0] `ListCommentsAfter` sets `PerPage=100`, fetches the complete history, then filters by ID and returns ascending IDs. It never sets the SDK `Since` option (`internal/github/comments.go:72`). The shared paginator follows response pagination, caps at 100 pages, and returns no partial result on an error (`internal/github/pagination.go:59`). Thus an unchanged issue costs one request per historical page even when callers hold precise discovery cursors.

[R0] `story_discovery` stores `(session_id, child_number, comment_id)` without a source timestamp (`internal/store/migrations/0012_story_refinements.sql:15`, `internal/store/story_refinements.go:139`). Parent `Watermark.UpdatedAt` records local cursor movement, not the GitHub comment's creation/update time (`internal/store/watermarks.go:13`, `:117`). It is not a safe substitute for a remote `since` bound. GitHub comments already expose both source timestamps, and the existing ID contract deliberately handles ordering and same-second arrivals (`internal/github/comments.go:36`, `:46`).

[R0] Admission accepts numeric-owner identity while excluding bot/report comments. Every examined comment can advance discovery ID after any owner admission; discovery, durable admission, and consumption are separate operations so delivery can retry (`internal/daemon/story_refinements.go:184`). Parent draining and continuation-intent checks also use nonzero cursors (`internal/daemon/tick.go:1261`, `internal/daemon/continuation_dispatch.go:131`).

[R0] A zero cursor has intentional complete-history meaning. Current consumers exceed the three listed in the issue: bootstrap (`internal/daemon/tick.go:1102`), progress reconciliation (`internal/turn/story_progress.go:491`), scope notices (`internal/turn/managed_scope.go:225`), refinement delivery (`internal/turn/story_refinement.go:128`), feature delivery (`internal/turn/feature_delivery.go:454`), owner exceptions (`internal/turn/owner_exceptions.go:309`), follow-ups (`internal/turn/followups.go:213`), and acceptance (`internal/turn/acceptance.go:161`). These searches recover markers/provenance. A global recent-only replacement would change their behavior.

#### Identity and failure semantics

[R0] Child and parent checks are independent requests. The parent endpoint validates positive identity and rejects pull-request payloads (`internal/github/issues.go:225`, `internal/github/managed_issues.go:21`, `:27`). Discovery checks gate admission into the pending queue. Execution re-fetches child and parent and rejects identity/relationship mismatch (`internal/daemon/story_refinements.go:110`). Removing discovery checks would therefore change when detached/reparented comments can enter durable state, even with execution checks retained.

[R0] The first child error terminates that worker's scan after advancing its rotation position (`internal/daemon/story_refinements.go:94`). Earlier successfully processed children can already have advanced discovery cursors. The refusal log's wording about all watermarks staying unchanged (`internal/daemon/tick.go:2012`) is not evidence of a global rollback. A failed individual multi-page comment fetch does return no partial results. This nuance belongs in correctness tests; it does not justify expanding this task into a backoff rewrite.

### Domain / Problem Space

#### Conditional requests offer the largest primary-quota saving

[R0] GitHub explicitly exempts a conditional request returning 304 from primary quota when it is correctly authenticated using `Authorization`. ETag/`If-None-Match` and Last-Modified/`If-Modified-Since` are documented mechanisms. GitHub says most endpoints expose ETag and many expose Last-Modified; stable query parameters/page size/order help validator reuse. This verifies the issue's central optimization claim, subject to endpoint support. [GitHub REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api#use-conditional-requests)

[R0] Endpoint documentation explicitly lists 304 for issue GET, but lists only 200/301/404/410 for parent GET and 200/404/410 for issue-specific comment listing. [Get issue](https://docs.github.com/en/rest/issues/issues#get-an-issue), [Get parent issue](https://docs.github.com/en/rest/issues/sub-issues#get-parent-issue), [List issue comments](https://docs.github.com/en/rest/issues/comments#list-issue-comments)

[R0; inference] Those omissions do not prove lack of support. They also do not establish that all three endpoints can become primary-quota-free. Exact authenticated validator behavior, including relationship changes, needs focused factory-owned observation before making that guarantee. No authenticated probe was performed. Current docs showed REST version 2026-03-10 examples; requesting a historical-version documentation view still returned current examples, so historical documentation fidelity is uncertain.

[R0] Primary savings do not remove secondary limits. GitHub documents 100 concurrent requests across REST/GraphQL, 900 REST points/minute, compute limits, and undisclosed safeguards; most GETs cost one secondary point. There is no documented blanket 304 exemption from secondary limits and no API exposing secondary headroom. [GitHub rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)

#### A validator alone is not a usable cache

[R0] go-github v90.0.0 does not supply conditional caching itself; its README recommends a caching HTTP transport. Raw 304 is handled as `*ErrorResponse` by `CheckResponse`, and `Do` exits before normal body decoding. A response cache must reconstruct a usable successful representation rather than pass raw 304 to existing client calls. `newResponse` parses pagination/rate data; `bareDo` normally updates quota state before error checking, but skips that when rate checking is disabled or `X-From-Cache` is set. [Pinned README](https://raw.githubusercontent.com/google/go-github/v90.0.0/README.md), [Pinned SDK source](https://raw.githubusercontent.com/google/go-github/v90.0.0/github/github.go)

[R0] HTTP cache keys include method/target URI and matching `Vary` fields. Validation updates stored response metadata while retaining applicable stored fields absent from the validation response; authenticated/shared caching has additional rules. [RFC 9111, sections 3.5, 4.1 and 4.3.4](https://www.rfc-editor.org/rfc/rfc9111.html)

[R0; inference] Design implications inferred from these contracts: cache complete response bodies and pagination metadata, isolate API base/authorization/representation contexts, bound retention, and keep fresh network quota metadata distinct from old cached metadata. Never put raw credentials in logs or persisted keys. An in-memory cache avoids new durable storage but pays cold-start costs; a persistent cache reduces those costs while retaining sensitive authenticated issue/comment content and requiring a storage/lifecycle policy.

[R0; inference] Pagination is a correctness boundary. A page-one 304 only validates page one. Later pages may have changed, and their bodies/Link relationships must still be considered. Treating page-one 304 as “no new comments anywhere” risks skipping tail additions. Conversely, replaying complete historical pages on every poll may save primary quota while preserving network, CPU, and secondary-limit cost. Changing `since` changes the representation URL and therefore cold cache behavior; continually advancing a timestamp with wall time would undermine validator reuse.

#### Incremental retrieval must retain the ID contract

[R0] The issue-specific comments endpoint returns ascending comment ID and accepts `since` for comments **last updated after** a timestamp formatted `YYYY-MM-DDTHH:MM:SSZ`. It supports up to 100 results/page and has no sort/direction parameters; repository-wide listing is a different endpoint. The documentation does not establish timestamp equality/precision edge behavior or snapshot consistency during pagination. [List issue comments](https://docs.github.com/en/rest/issues/comments#list-issue-comments)

[R0; inference] `since` can narrow the transfer, but cannot safely replace ID filtering. For example, resolving an old consumed comment to its newly edited update timestamp could skip higher-ID comments created before that edit. Resolving by fetching a cursor comment also adds requests and fails if it was deleted. Existing IDs may be edited and reappear under `since`; the unchanged ID contract should continue to prevent replay unless the product deliberately adds edit-trigger semantics.

[R0; inference] A conservative source-time lower bound captured with successful discovery, overlapping the timestamp boundary and still filtering by ID, is a candidate worth evaluating. It is not proven by this research. Plan must define bootstrap/deletion fallback, source timestamp choice, same-second behavior, monotonic progress, pagination failure handling, and durable migration. Local cursor-write time is specifically unsuitable. Older installations lacking a source timestamp need a safe complete-history fallback.

#### Quota observability and budget policy

[R0] Successful responses currently feed debug `rate_limit_remaining`, but there is no daemon-facing successful-quota interface (`internal/github/client.go:230`). Refusal errors expose limit, remaining, resource, reset, and retry delay (`internal/github/errors.go:336`). Existing debug request records include status/duration/remaining quota; comment summaries include request count and returned count (`internal/github/client.go:234`, `internal/github/comments.go:109`). They are useful measurement seams, not measurements from this run.

[R0] GitHub supplies `x-ratelimit-limit`, `remaining`, `used`, `reset` and `resource`. It recommends reading response headers instead of polling `/rate_limit`, which is primary-exempt but may consume secondary allowance. Authenticated-user quota is normally 5,000/hour, with account/app exceptions and other token limits. A guard should use actual headers rather than universally assume 5,000. [GitHub rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)

[R0; inference] A useful guard needs a reserve for foreground work, resource/credential attribution, concurrent-worker accounting, and stale/absent-header behavior. The GitHub package can expose observations while the daemon remains the owner of time and policy. A conditional cache may reconstruct successful responses with old headers, so the distinction between a fresh network observation and cached metadata is essential.

### Constraints & Risks

1. [R0] Preserve package ownership: SQL and durable persistence belong to `internal/store`; scheduling/backoff belong to daemon. The existing central HTTP seam is reusable; direct SQL from the GitHub client would introduce the wrong dependency (`internal/github/client.go:207`, `internal/store/story_refinements.go:139`; project `AGENTS.md`, `.aiteam/APP_CONSTRAINTS.md`).
2. [R0] Preserve numeric identity checks, retryable pending delivery, complete-history reconciliation, no-partial-results pagination failures, and the ID consumption contract. These are observable behavior, not incidental request overhead (`internal/daemon/story_refinements.go:110`, `:184`, `internal/github/pagination.go:59`).
3. [R0; inference] Validate cached representation freshness after mutations, reparenting, deletion, permission changes, and auth changes. An ordinary issue ETag must not be assumed to certify its separate parent relationship. No evidence establishes that the two identity requests can be collapsed without losing the relationship check.
4. [R0; inference] Keep fair scanning bounded: primary-free polling still spends time, bandwidth, secondary allowance, and worker capacity. The existing bound is per parent, so aggregate demand grows with active managed parents. Increasing/removing sixteen without a global policy is not justified by 304 support alone.
5. [R0] Wire-level fixtures capture method/path/query/headers/body (`internal/github/github_test.go:20`, `:45`). Existing tests cover ordering, zero-cursor history, pagination, timestamps, bounds, SDK refusal, and daemon backoff (`internal/github/comments_test.go:21`, `:55`, `:75`, `:102`; `internal/github/pagination_test.go:130`; `internal/github/sdk_test.go:177`; `internal/daemon/tick_test.go:974`).
6. [R0] Refinement tests cover independent parent watermarks, closed children, owner rename, failed-post retry, and post-promotion refusal (`internal/daemon/story_refinements_test.go:27`, `:45`, `:74`, `:97`). Their parent fixture substitutes issue lookup for the actual relationship endpoint (`:23`), so it cannot verify relationship validators. No existing cache/304 tests or dedicated sixteen-child rotation regression were found in the scoped tour.
7. [R0; inference] Future validation should exercise cold 200/warm 304/changed 200; absent validators; cached pagination with tail changes; authenticated key isolation; mutation/delete/reparent invalidation; same-second and edited comments; legacy/zero cursor fallback; later-page refusal without cursor advancement; restart behavior; and concurrent budget observations. These are research-derived evidence needs, not executed checks or a story plan.
