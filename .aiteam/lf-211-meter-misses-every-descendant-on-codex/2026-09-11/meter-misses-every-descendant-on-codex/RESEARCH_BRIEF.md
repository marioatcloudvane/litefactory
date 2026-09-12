# Research Brief — meter-misses-every-descendant-on-codex

## Plan readiness and expected improvement (run 0006)

Run 0006, 2026-09-12: “ok, can we already move that to plan? will we get a better number? we must not necessarily have a real value but it should at least be much closer ... ?!” The clarified objective is a materially closer, useful local cost estimate; invoice-perfect reconciliation is not required. This continuation assesses readiness from the retained investigation, without rescanning runtime records, checking a new source revision, executing checks, or starting Plan.

**Ready for Plan: yes.** The remaining uncertainty does not prevent a bounded plan with measurable improvement. No target accuracy percentage or further user clarification is needed. Prioritize recovering actual observed consumption and proving descendant accounting; presentation alone does not make a number closer.

- **Concrete demonstrated improvement:** historical run 5 can move from $5.148750 to $11.903026 at the recorded rates by recovering $6.754276 of observed child usage. That is coverage of the entire reconciled root-plus-three-child evidence set, rather than its present 43.26%. It is a proposed acceptance target, not an implemented repair or proof of complete real-world spend. Runs 6–8 must retain their existing attribution and amounts.
- **Expected broader benefit, not yet quantified:** the same controlled reconstruction mechanism can recover other retained historical descendants where evidence supports attribution. The size of that backlog is unmeasured. Authentic-envelope regression cases for parallel/nested descendants and resumed attempts, including unchanged ancestors and late discovery, should establish future accounting behavior and identify any live hardening needed. There is no defensible promised fleet-wide uplift or accuracy percentage; the cited live attempts did not demonstrate missing active descendants.
- **Useful estimates despite incomplete evidence:** keep the observed priced subtotal visible and label it as a lower bound at recorded rates when coverage is incomplete; retain an unknown complete total. Recover supported usage rather than applying a guessed multiplier. Explicit zero-turn/zero-cost refusal classification removes demonstrated misleading uncertainty without inventing a model or turning other unknown failures into zero. These changes improve interpretation, while descendant recovery supplies the numerical improvement.

Recommended Plan scope: (1) previewable, idempotent historical descendant recovery with correct attempt/model provenance and resume deduplication; (2) executed collector regression coverage and fixes for failures those cases demonstrate; (3) evidence-backed refusal classification; (4) honest incomplete-subtotal presentation on the surviving CLI. The implementation can be judged against retained observed usage, duplicate-free repeated reconstruction, unchanged later-attempt charges, and explicit uncertainty for unsupported records. Invoice reconciliation and a fleet-wide accuracy certification are outside this initial scope.

The prior broad product-choice question is sufficiently narrowed by this clarified objective: pursue the combined bounded scope above, with accounting coverage first and wording secondary. Exact historical group-output reconstruction remains unresolved but does not block planning. This run remains Research under its authoritative mode header; no Plan artifact or stories are created. The next factory-selected Plan run can consume this brief directly.

## Request and completion

Run 0005, 2026-09-12: “Ok, so what is the reality now? How accurate are we and what do you propose to fix it?” Completed a bounded accuracy assessment using the retained run-0004 evidence and focused current-source checks. Current HEAD is **c02be04e2605d49ee1a8350ec4c3f5e31a846bbd**. A diff against the cited run-0004 revision confirms no changes in `internal/codex/usage.go`, `internal/codex/usage_rollout.go`, `internal/report/meter.go`, `internal/report/report_test.go`, `internal/harness/runner.go` or `internal/cli/cmd_meter.go`. Runtime records were not rescanned; historical numerical conclusions below reuse the retained snapshot. No code, runtime state or test changes, no checks executed, and no automatic mode transition.

## Current accuracy assessment (run 0005)

**The meter's checked arithmetic is accurate; its completeness is not certified. There is one quantified historical omission, and the reported live Implement failure was a mistaken attribution.** No defensible single accuracy percentage exists across all runs or relative to provider invoices.

| Dimension | What the evidence establishes | Limit |
|---|---|---|
| Price arithmetic | The investigated counters reconcile exactly at the issue's historical rates; runs 6–8 sum to $2.632032, and run 8 alone is $0.558352. | This verifies these calculations, not all models, tariff revisions, invoice adjustments or every run. |
| Live collection in this incident | None of the seven cited Implement attempts has observed child activity in the inspected records. The active Plan child was captured. | Zero missing children in attempts with no observed children is not a test of Implement descendant capture. No universal 100% claim is justified. |
| Historical reconstruction | Run 5 retains $5.148750 of $11.903026 reconciled observed usage: **43.26% captured, 56.74% omitted**, or $6.754276 missing across three children. | This ratio applies only to that historical run and that observed set, not to all usage or all historical reconstruction. |
| Claude failed resume | Explicit zero turns and provider-reported zero cost establish a non-work refusal, followed by a separately recorded successful attempt. | Model remains unknown; provider evidence must remain distinguishable from token-derived pricing and from ordinary missing measurement. |
| Reporting honesty | Incomplete collection/pricing suppresses `estimated_usd`; the available priced subtotal survives. Current mixed known/unknown pricing becomes partial. | A partial subtotal is not the complete cost or a reconciled invoice. CLI wording still lacks an explicit lower-bound qualifier. |

Current source support: exact stored-amount aggregation and coverage behavior at `internal/report/meter.go:41`, `:89`–`:104`, `:117`; surviving human-readable totals/details at `internal/cli/cmd_meter.go:220`, `:238`; structural refusal at `internal/harness/runner.go:860`, `:884`; shared collector and attempt baselines at `internal/codex/usage.go:31`, `internal/codex/usage_rollout.go:129`, `:217`, `:263`. These citations are rechecked at the run-0005 HEAD; the detailed runtime provenance and earlier line references remain below.

## Proposed bounded fix scope (run 0005)

1. **Repair the demonstrated historical omission.** Provide a controlled, previewable, idempotent reconstruction/correction path that reconciles retained descendant records to the original attempt, with parent links, actual-model evidence and existing measurements. Start with run 5; quantify any broader backlog before generalizing. Preserve provenance and uncertainty where evidence is incomplete. Expected verification: run 5 has one root and three children totaling $11.903026 observed priced usage at the retained rates; reapplying makes no duplicate charges; runs 6–8 retain their existing amounts. Do not simply attach every child of a resumed thread to its latest attempt. No repair is executed in this Research run.
2. **Classify the demonstrated refused attempt explicitly.** Reuse the runner's structural refusal outcome plus affirmative terminal zero-turn/zero-cost evidence. Retain the durable attempt, fallback relationship and unknown model. Present this as a provider-reported zero-cost refusal rather than unexplained missing model/usage; keep that evidence separate from calculated token cost. Verification must include the actual refusal shape plus negative cases with missing terminal cost, nonzero cost, interruption or real work, so other failures remain uncertain. Do not blanket-force the fallback run's collection to complete: independent compatibility/descendant diagnostics still apply.
3. **Clarify the surviving CLI subtotal.** When collection or pricing is incomplete, label a known priced subtotal “observed lower bound at recorded rates; total unknown,” preserving `estimated_usd: null` and structured status fields. This qualifies modeled usage cost, not the provider invoice. Keep unknown distinct from zero and do not restore the removed run-comment cost block. Verify complete, partial, wholly unknown and confirmed-refusal presentations. This is a recommended small presentation improvement, not proof of another collection defect.
4. **Establish live descendant assurance with executable cases, then harden only demonstrated failures.** Use minimal sanitized Codex 0.153.4 envelopes grounded in retained metadata/counters to exercise the actual collector: parallel and nested descendants, resumed attempt deltas, foreign/inherited records, unchanged ancestors with active grandchildren, delayed children and bounded-scan diagnostics. Include a mode-labelled Implement case while preserving the shared collection mechanism. Required result: each attempt's own exclusive usage is counted once, unrelated/historical usage is excluded, and unresolved evidence cannot claim completeness. These tests are proposed validation, not executed checks. The source risks below justify the cases but are not established causes of this incident.

Priority rationale: historical correction fixes the quantified $6.754276 loss; refusal classification removes misleading uncertainty for the demonstrated zero-cost outcome; CLI wording makes present limits understandable. Collector validation is necessary before claiming broader live accuracy, with implementation hardening conditional on reproduced failures. Do not rewrite pricing arithmetic or resume deduplication on the evidence available.

Residual limits: no fleet-wide coverage audit, no provider-invoice reconciliation, no executed authentic collector regression suite, no rescan of runtime after run 0004, and no exact reconstruction of the original whole-group unknown-pricing output. None blocks this recommendation. There is no remaining user decision needed to explain the incident; implementation scope can be chosen in a subsequent explicitly requested mode.


Run 0004, 2026-09-12: “Ok, run the investigation now.” Investigate retained Q-01–Q-06 directly using repository and read-only local runtime evidence. Research investigation is complete for the bounded incident: the claimed live Implement undercount is not supported, the historical omission is quantified, and the Claude refusal is identified. No implementation, backfill, provider invocation, daemon mutation, forge access, commit or push occurred. No build/tests were run because this is research; record parsing, arithmetic reconciliation and source inspection were performed.

Run 0002 established repository mechanisms but did not reconcile runtime records; run 0003 inventoried questions without renewed investigation. Their historical uncertainty is superseded below by new observations, not treated as successful prior verification. The failed run-0001 timing counterclaim is now independently verified.

## Main finding

**The issue combines costs from different runs. The three children belong to #195 run 5, not run 8.** Their filenames encode local wall-clock times two hours ahead of the UTC timestamps inside their records. All three children's exclusive response usage occurred 18:16–18:25 UTC; run 8 occurred 20:16–20:17 UTC. Shared parentage comes from a resumed root thread and does not change that attribution.

The quoted $2.632032 is exactly the sum of root usage for runs **6, 7 and 8**, not run 8 alone. The $6.754276 child amount is arithmetically correct at the issue's astra rates but belongs to run 5. Therefore the proposed ~$9.39/run-8 total and ~72% live undercount are invalid incident targets.

There is a real historical coverage gap: current storage represents run 5 with `historical-log-reconstruction-v1`, root-only usage and `historical_reconstruction` diagnostics. Its root's $5.148750 plus these children's $6.754276 gives **$11.903026 observed priced usage**, of which the reconstruction omits 56.7442%. This is an observed subtotal at the supplied rates, not a provider invoice or complete-cost certification. Backfilling that earlier run is separate from fixing live descendant collection.

## Evidence and provenance

Repository citations below refer to **13c265cf276efea6b973d96e822df850c9cc9e9e** unless stated otherwise. Local `origin/main` is `3e55019712df03bcae8176a3c774855e66013861` (2026-09-12); `HEAD...origin/main` is 4 ahead / 0 behind. No fetch or synchronization was performed. The previous 79-behind warning is historical and no longer describes this supplied checkout.

[RUNTIME_EVIDENCE.json](RUNTIME_EVIDENCE.json) retains sanitized metadata, per-response counters and line numbers, model matching, source hashes, selected run metadata, relevant database rows, and the Claude refusal record. It intentionally excludes prompts, assistant/tool transcript bodies and credentials. Database rows reflect the current read-only snapshot, which includes later historical reconstruction; they are not asserted to be the exact database state when the issue was opened. Original JSONL/meta paths below are local retained evidence, not upstream compatibility certification.

### Timing and cost reconciliation

All times are UTC on 2026-09-09. Attempt times are from `~/.litefactory/litefactory.db`, and run windows from `~/.litefactory/logs/runs/github/marioatcloudvane/litefactory/195/NNNN.meta.json`.

| Run | Run window | Recorded root uncached / cache-read / output | Root subtotal |
|---|---|---|---|
| 5 | 18:15:40–18:28:32 | 55,156 / 4,189,440 / 8,155 | $5.148750 |
| 6 | 20:09:48–20:10:49 | 127,491 / 417,536 / 1,085 | $1.746696 |
| 7 | 20:13:16–20:13:29 | 18,248 / 136,704 / 156 | $0.326984 |
| 8 | 20:16:23–20:17:15 | 17,869 / 323,712 / 1,119 | $0.558352 |
| 6+7+8 | Three separate attempts | 163,608 / 877,952 / 2,360 | $2.632032 |

Run 8 is DB run 276, attempt `f82cf1ffee1e6599cf033396cd5ad557`. Its stdout terminal usage in `195/0008.jsonl:7` independently matches the recorded measurement (inclusive input 341,581 minus cached input 323,712 = 17,869 uncached). Run 5 terminal stdout at `195/0005.jsonl:44` matches its current reconstructed root. Summing root-thread response records during run 5 independently gives the same counters.

The three cited files are under `~/.codex/sessions/2026/09/09/`. First-line metadata identifies the child and parent, Codex `0.153.4`, and UTC creation time. Line 2 contains inherited root metadata; it must not replace the first-line child identity when analyzing the file.

| Child ID suffix / filename time | Exclusive response interval | Records | Uncached / cache-read / output | Subtotal |
|---|---|---|---|---|
| `01a08762-f597-70d3-b3e1-a220b2580fc2` / 20:16:44 | 18:16:53.064–18:20:30.554 | 18 | 77,992 / 1,410,048 / 5,258 | $2.452868 |
| `01a08763-8637-7a53-a625-e15a2d20f1e0` / 20:17:21 | 18:17:28.482–18:25:50.525 | 22 | 41,941 / 1,774,080 / 2,416 | $2.314290 |
| `01a08763-b58a-7cf2-aa7f-e968ee2ee839` / 20:17:33 | 18:17:45.907–18:25:16.675 | 23 | 21,283 / 1,522,688 / 5,032 | $1.987118 |
| Total | Within run 5 | 63 | 141,216 / 4,706,816 / 12,706 | $6.754276 |

These amounts use the issue's $10/$1/$50 per million uncached/read/output rates, not current web pricing. Each `token_usage_record.usage` is counted once for its own `thread_id`; all 63 records have matching `turn_context.turn_id` and actual model `gpt-6-astra`. Per-file sums exactly match the final cumulative `token_count.info.total_token_usage`. Reasoning is a subset and is not charged again; cache-write counters are explicitly zero. This resolves cumulative-versus-incremental ambiguity for these files without relying on filename times or inherited parent content.

### Why seven Implement roots had no children

The Sep-9 session directory's first-line ancestry graph has 21 descendants under #195's root and 19 under #170's root. All their own-thread response records were inspected for the interval covering the cited live attempts. No descendant usage appears during #195 runs 6–11 or #170 run 9. Root usage appears in those windows. The only descendant active in the combined 20:09–20:43 interval is #170 Plan run 10's child `01a087e5-117d-7f60-82a8-606774564a49`, with four response records at 20:38:58–20:41:52, and it is present in storage with its parent and $1.272924.

This establishes that the supplied on-disk evidence does **not** demonstrate missing live Implement descendants. It does not prove that no deleted, unavailable or differently stored descendant could ever have existed. The bounded Sep-9 graph inspection and the absence of unresolved-source diagnostics in those seven attempts are positive evidence against the proposed mode-specific diagnosis; they are not universal collector completeness certification.

Plan and Implement use the same collection path (`internal/codex/usage.go:31`, `internal/codex/usage_rollout.go:217`). The initial baseline at `usage_rollout.go:129` and unchanged-file skip at `:263` correctly exclude the earlier child files from later attempts. Historical records do not emit usage (`:524`); foreign-thread inherited responses are excluded (`:497`). Root ancestry alone is therefore an inappropriate criterion for charging a child to a resumed attempt.

### Claude unattributed attempt is a refused resume

Confirmed attempt `4110168a8c21fb19bce49ff8693a28a9` is #195 run 12's failed first call, 20:47:57.210–20:48:00.327 UTC. The first line of `195/0012.jsonl` is a terminal `result`, subtype `error_during_execution`, `is_error: true`, error “No conversation found with session ID: 3e8167ca-dfb7-553f-be1f-e5bafdde7b7b”, `num_turns: 0`, and provider `total_cost_usd: 0`. Initialization of the fallback follows on line 2. The second durable attempt is `994465510c48f708eba5e1d2b5f7081a`, starts 20:48:06.616, succeeds, and has actual Claude version `2.1.267` plus root and three descendant measurements.

This is affirmative provider no-turn/zero-cost evidence for the refusal, not merely absent usage. It supports an explicit “resume refused before conversation initialization; provider reported zero cost” classification. It does not provide an actual model, and no model should be invented. Current metering retains the first attempt as unknown-model coverage with `capability_evidence_missing`, `resume_baseline_unverified`, and `session_source_unavailable` diagnostics. A later change can preserve that factual record while explaining the non-work attempt distinctly.

The existing runner recognizes resume refusal structurally (`internal/harness/runner.go:860`, `:884`) and turn orchestration reruns cold (`internal/turn/harvest.go:186`, `:217`). A classification should reuse that owned result rather than matching provider error prose or treating every failed attempt as zero cost.

### Report surface has changed

The old run-comment cost/coverage metadata was deleted by commit `9ea7fdd`; it is not rendered on this checkout. `internal/report/report_test.go:440`–`:471` explicitly checks that populated meter values do not appear in comments. Thus the proposed lower-bound change to that comment line is obsolete on the current base. Do not reintroduce the deleted cost block solely to satisfy this old issue description.

The underlying meter/CLI remains relevant. `internal/report/meter.go:89`–`:104` makes mixed known/unknown pricing **partial** and suppresses complete estimates for incomplete collection/pricing. The failed attempt by itself has unknown pricing; the run containing both unknown coverage and priced measurements aggregates to partial. The exact historical external output behind the owner's whole-group “unknown” assertion was not retained here, so that quoted surface cannot be independently reconstructed. Current aggregation does not support the blanket claim. An explanation/lower-bound label for surviving CLI output is a possible separate presentation scope, not an automatically required replacement for the deleted comment.

## Remaining collector risks

These source mechanisms remain unchanged between the incident's task revision `1d5186d8e7fee7bae43464812775473ced7668f7` and current HEAD for `internal/codex/usage.go` and `usage_rollout.go`. They remain legitimate hardening candidates, but none explains this incident:

- An unchanged intermediate ancestor can be baseline-skipped (`usage_rollout.go:263`), leaving a changed deeper descendant unassociated unless its parent is already known (`:377`).
- Finalization requires terminal known files (`:549`); it need not wait for undiscovered late children once all known files are terminal.
- Final diagnostics skip unassociated files (`:579`). They must not indiscriminately charge unrelated sessions, but unresolved relationships can remain invisible.
- Discovery is bounded by time, entries, files and bytes (`:26`, `:145`, `:350`). The cited live attempts show only `capability_evidence_missing`, not scan-limit failures.

A focused search still finds no direct `prepareRollouts`, `token_usage_record` or `rolloutsFinalized` references in Codex test files. Authentic records inspected here demonstrate the provider shapes and accounting relationships; they are not an executed collector regression suite or complete supported-version capability matrix. The collector's compatibility diagnostic remains honest. No paid reproduction was commissioned.

## Question status and decisions

- **Q-01 resolved:** Children incurred usage during #195 run 5. Run 8 attribution is rejected; current historical reconstruction omits run-5 children.
- **Q-02 resolved for the cited evidence:** Codex 0.153.4; 63 own-thread incremental records sum to final cumulative totals, with matching actual-model context. Rates used are those supplied in the issue. No claim about all future versions.
- **Q-03 resolved for the reported incident:** No observed descendants executed during the seven live Implement attempts. Their absence is consistent with correct baseline behavior; no mode-specific live collection bug has been demonstrated. General collector risks above remain independent candidates.
- **Q-04 resolved:** Explicit refused resume before initialization, zero turns and provider-reported zero cost, then fresh successful attempt. Preserve the durable attempt and unknown model; classify the known outcome explicitly if later authorized.
- **Q-05 partly superseded, bounded residual:** The requested comment surface no longer exists; current mixed pricing aggregates partial. Exact historical owner-cited group output is unavailable. Whether to clarify surviving CLI subtotals is an optional later product scope decision, not a factual blocker.
- **Q-06 resolved for supplied base:** Current checkout is 4 ahead/0 behind local origin/main; collector code remains materially identical, report metadata was removed. No remote freshness claim.
- **Q-07 retained/reverified:** No separate Implement collector path.
- **Q-08 retained/reverified:** Parentage is not attempt ownership.
- **Q-09 updated:** Old lower-bound wording concern was independent of the percentage, but its run-comment surface has now been deleted.
- **Q-10 resolved beyond prior plausibility:** This actual Claude incident is refusal plus fallback.
- **Q-11 updated:** Authentic local source and per-response evidence now inspected; executable collector compatibility coverage remains absent from this investigation.
- **Q-12 retained:** Missing evidence alone cannot establish zero/model; this Claude case additionally has affirmative provider zero-turn/zero-cost evidence, still without a model.

## Recommendation and options

Correct the incident premise first: remove the run-8/~72% target and separate **historical reconstruction coverage**, **explicit refused-attempt classification**, and optional **collector hardening**. The strongest demonstrated remaining work is associating/reconciling historical descendants for run 5 without double billing later resumed attempts, and clearly classifying the Claude refusal while preserving factual evidence. Do not broaden live collection to charge every descendant ever seen under a resumed thread.

A later implementation scope can use the sanitized real counters and identities retained here as regression evidence, with synthetic minimal record envelopes rather than full sensitive transcript fixtures. Historical correction should be previewable and idempotent, reconcile existing reconstruction rows first, and remain outside this Research run. Collector hardening should get focused cases for unchanged ancestors, delayed discovery, and unsupported ancestry, with honest diagnostics; it should not claim to repair a reproduced seven-run failure.

Historical run-0004 product choice (narrowed by run 0006 above): whether to pursue historical repair, refusal classification, independent collector hardening, or some combination. No question or approval is required to finish this investigation, and no automatic transition to Plan is made. Exact historic report text and deleted/unavailable session evidence remain explicit limits, not reasons to guess or block the established conclusions.
