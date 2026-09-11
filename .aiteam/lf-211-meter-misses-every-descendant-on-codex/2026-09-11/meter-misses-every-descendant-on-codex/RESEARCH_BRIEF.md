# Research Brief — meter-misses-every-descendant-on-codex

## Request

Run 0003 asks: “ok, what open questions do we have here?” This continuation inventories the existing research; it does not renew the investigation or start Plan. The brief already existed at run start. No specialists were invoked because this request requires synthesis of retained findings only. [V]

Run 0002 requested a Research retry for issue #211, “Meter misses every descendant on Codex implement runs, undercounting cost by ~72%,” covering descendant collection, an unattributed Claude attempt, and incomplete-subtotal reporting. [R0]

Evidence tags: **[V]** verified in this run (artifact continuity only); **[O]** owner/issue report, not independently verified; **[R0]** carried forward from earlier research or supplied history, not reverified this run; **[W]** other second-hand evidence. Code findings below were reported as verified by the run-0002 codebase-tour specialist; their citations describe that inspected base. The failed-run timing counterclaim remains explicitly unverified. No historical claim has been upgraded to current verification. [V]

The earlier factory prompt reported a base 79 commits behind main and zero ahead. That is historical context, not a newly checked current branch status. This run does not inspect source, runtime evidence, git state or other sessions, execute checks, or change implementation. The request is interpreted as an inventory request, not permission to settle factual gaps by assumption. [V]

## Codebase Context

### Ownership and flow

[R0] The single Go 1.26 module (`go.mod:1`) routes metering through `internal/agent`, provider adapters `internal/codex` and `internal/harness`, `internal/turn`, `internal/meter`, `internal/store`, and `internal/report`.

1. `agent.UsageCapture` supplies the synchronous persistence seam; invocation, observation and coverage events remain distinct (`internal/agent/usage.go:146`).
2. `turn.runMetered` creates a durable attempt for each runner call before invoking it, and finalizes that attempt independently of subprocess success (`internal/turn/meter.go:17`, `internal/turn/meter.go:23`, `internal/turn/meter.go:37`).
3. Codex inventories session files before launch, consumes stdout, waits for the process, and finalizes rollout collection (`internal/codex/runner.go:320`). `usage.go` owns stream/lifecycle handling and `usage_rollout.go` owns session discovery and consumption.
4. Associated session response records provide exclusive consumption; exec terminal totals supply inclusive reconciliation evidence (`internal/codex/usage.go:150`, `internal/codex/usage_rollout.go:546`). The turn persists normalized evidence and exposes exclusive measurements plus non-additive coverage rows.
5. Run aggregation consumes snapshot rows, and reporting combines their stored prices without repricing (`internal/turn/meter_report.go:16`, `internal/report/meter.go:44`).

[R0] Nil counters mean unknown rather than zero (`internal/agent/usage.go:9`). Actual model identity requires runtime provenance; configured model names are not a replacement for missing attribution.

### There is no Implement-specific collector branch

[R0] The collector receives usage capture and operates on thread identity, file changes, ancestry and recognized records. It does not branch on Plan versus Implement (`internal/codex/usage.go:31`, `internal/codex/usage_rollout.go:217`). Both modes use the same runner collection lifecycle. Consequently, the observed mode correlation needs explanation through differing attempt history, emitted records, discovery timing or evidence availability; the code does not support assuming a distinct Implement collection implementation.

[R0] Ancestry readers support top-level `parent_thread_id`, nested `source.subagent.thread_spawn.parent_thread_id`, and exec `collab_tool_call` spawn events with known sender/receiver identifiers (`internal/codex/usage_rollout.go:63`, `internal/codex/usage_rollout.go:324`, `internal/codex/usage.go:112`).

## Domain / Problem Space

### The incident claim and its unresolved timing conflict

[O] The issue reports seven Codex Implement roots with zero descendants, one Plan root with one descendant, and a Claude Implement root with three descendants. For #195 run 8, it names root `01a0864d-41ce-7bf1-bc89-2d52d796c164` and three files:

- `rollout-2026-09-09T20-16-44-01a08762-f597-70d3-b3e1-a220b2580fc2.jsonl`
- `rollout-2026-09-09T20-17-21-01a08763-8637-7a53-a625-e15a2d20f1e0.jsonl`
- `rollout-2026-09-09T20-17-33-01a08763-b58a-7cf2-aa7f-e968ee2ee839.jsonl`

[O] All three reportedly name that root as parent. The issue attributes $2.632032 to root usage and $6.754276 to the three children, proposing a four-invocation total around $9.39 and approximately 72% missing spend. These quantities and their assignment to run 8 were not independently verified this run.

[R0] The supplied earlier failed-run report instead says those children occurred about two hours before run 8 and belonged to earlier work on the resumed thread. The earlier process failed and produced no completed brief. Its timing conclusion is therefore retained as an unresolved counterclaim, not upgraded to a verified fact.

[R0] Code establishes why that distinction matters:

- Prelaunch inventory records existing rollout sizes and identities (`internal/codex/usage_rollout.go:129`).
- Unchanged pre-existing files are skipped (`internal/codex/usage_rollout.go:261`).
- Changed existing files retain a prelaunch byte boundary (`internal/codex/usage_rollout.go:330`).
- Historical records seed deduplication/model state without emitting new billable observations (`internal/codex/usage_rollout.go:451`, `internal/codex/usage_rollout.go:524`).
- Response records with another thread identity are excluded as inherited history (`internal/codex/usage_rollout.go:497`).

Matching ancestry proves a thread relationship, not consumption in a particular metered attempt. Summing whole child-file totals into a later resumed attempt could double charge earlier work. Neither the owner's run-8 allocation nor the earlier timing correction can be accepted without matching response records to persisted attempt intervals and prelaunch boundaries. Zero descendant coverage rows also does not prove no descendants were missed.

### Record contract matters

[R0] Rollout billing recognizes `token_usage_record` with valid response/turn identities and matching `turn_context` model evidence. `event_msg` handling recognizes finalization events but does not bill cumulative `token_count` events (`internal/codex/usage_rollout.go:454`). The issue's phrase “final usage records” must be resolved to an exact source shape. A cumulative file total and exclusive attempt response records are different evidence and cannot be substituted without reconciliation.

[R0] Compatibility evidence requires authentic version/source provenance (`internal/meter/compatibility.go:5`). The synthetic fixtures inspected here do not establish compatibility with the installed Codex version or the issue's authentic rollouts. `internal/codex/testdata/USAGE_SOURCE.md:23` still says associated sessions are not scanned without a validated contract; that note does not fully describe the current rollout-reading implementation and should be reconciled with authenticated source evidence.

### Concrete collector limitations, not confirmed incident causes

| Finding | Verified mechanism and consequence | Citation |
|---|---|---|
| Changed descendants behind unchanged ancestors | Baseline skipping omits an unchanged intermediate ancestor; association requires a known parent. A changed grandchild cannot associate if that intermediate parent is also absent from the current stream. | `internal/codex/usage_rollout.go:263`, `internal/codex/usage_rollout.go:377` |
| Unassociated files have no final diagnostic | Parsed but unassociated files are ignored by final diagnostics. A missed descendant need not become a coverage row. | `internal/codex/usage_rollout.go:579` |
| Finalization can precede unknown late children | Collection returns when known associated files are terminal. The two-second deadline is a ceiling, not a guaranteed grace period for children not yet discovered. | `internal/codex/usage.go:171`, `internal/codex/usage_rollout.go:549` |
| Bounded discovery | Entries, files, bytes and elapsed discovery time are limited. Incomplete scans have controlled diagnostics; incident evidence must show whether any limit was encountered. | `internal/codex/usage_rollout.go:26`, `internal/codex/usage_rollout.go:145`, `internal/codex/usage_rollout.go:350` |

All table findings are [R0] control-flow observations. None is asserted as the cause of the seven reported Implement results.

### Unattributed Claude attempt

[O] The issue names attempt `4110168a8c21fb19bce49ff8693a28a9` in #195 run 12 as an unknown-model root coverage row, alongside a successfully attributed attempt. [R0] The failed prior run described a resume refusal followed by a fresh attempt.

[R0] That explanation is plausible in this code: resume refusal and cold fallback invoke `runMetered` separately while sharing a run ordinal/log (`internal/turn/harvest.go:165`, `internal/turn/harvest.go:178`, `internal/turn/harvest.go:216`). Claude registers a root before assistant usage is required (`internal/harness/usage.go:201`), while exclusive model attribution requires assistant-message model evidence (`internal/harness/usage.go:299`). Missing measurements produce a coverage row with unknown pricing (`internal/store/meter_snapshot.go:198`).

This is a mechanism, not conclusive identification of the named attempt. Absence of usage/model evidence does not establish zero usage. Any future classification of a refused launch must distinguish authenticated no-billable-work evidence from merely missing evidence and retain the durable attempt record. Guessing the configured model or suppressing the row would compromise existing unknown-versus-zero semantics.

### Report wording

[R0] `meterMetadata` always labels the value “observed priced subtotal,” without “lower bound” (`internal/report/meter.go:165`). Incomplete collection already withholds estimated totals and total token counters (`internal/report/meter.go:98`). This confirms the wording concern independently of the proposed 72% undercount.

[R0] Mixed known/unknown pricing rows aggregate to partial, not necessarily unknown (`internal/report/meter.go:94`). The issue's statement that one unknown row makes the entire group's pricing unknown needs checking against the exact report/JSON/CSV surface. Pricing completeness and collection completeness must remain distinct; wording should explain that a subtotal excludes unmeasured work without implying missing consumption has been quantified.

### Existing test evidence

[R0] The inspected Codex usage tests are synthetic exec-JSONL tests (`internal/codex/usage_test.go:13`). The specialist found no test references to `prepareRollouts`, `token_usage_record`, or `rolloutsFinalized` under `internal`. Thus the inspected suite does not demonstrate rollout discovery, baseline-skipped ancestry, changed descendants behind unchanged ancestors, delayed discovery, or rollout-model attribution.

[R0] Transport tests explicitly disclaim real descendant compatibility (`internal/turn/meter_transport_test.go:18`). Fallback tests verify distinct refusal/fallback attempts and replay deduplication, but both synthetic attempts have model observations and do not cover the reported unattributed shape (`internal/turn/meter_test.go:12`). Fixtures use temporary executables/filesystems/databases. No tests or other build checks were run during this read-only research. `Makefile` defines `check: fmt vet lint test`, including race tests for implementation verification.

## Constraints & Risks

- [R0] Research was performed through the mandated codebase-tour specialist. Its repository-only scope and orchestrator restrictions prevented inspection of runtime session/log/database contents. Availability of those paths was not tested. This is a research scope limit, not a claim that evidence is missing on disk.
- [R0] Run 0002 reported its task base as 79 commits behind main. Current divergence was not checked; a later investigation must establish which findings still apply before changing implementation.
- [R0] No separate Implement collection branch was found. A mode-specific fix without authentic evidence risks treating correlation as cause.
- [R0] Historical ancestry and current-attempt usage are separate. Broadening discovery must preserve prelaunch boundaries, replay deduplication and exclusion of inherited records.
- [R0] Unknown unassociated files create an observability limit. Their mere existence also cannot justify charging unrelated sessions; ancestry resolution must remain evidence-based and bounded.
- [O] The issue reports exact pricing, correct resume deduplication, non-additive subsets, faithful zero cache-write counters and unknown-versus-zero behavior as working. Preserve these invariants; this research did not independently audit all their arithmetic or runtime output.
- [R0] A missing model must not be repaired by substituting configuration. Failed attempts must not silently disappear merely to improve group pricing status.
- Repository rules reserve SQL to `internal/store`, git operations to `internal/git`, and daemon runtime state to the daemon. Any later authorized evidence extraction must be read-only and must not expose unrelated session contents or credentials.
- No files outside this session were modified. No production code, runtime state, protected harness tree, forge state or git history was changed.

## Open Questions

[V] Six questions remain open. IDs Q-01 through Q-06 preserve the order and identity of the prior six numbered items. They are chiefly factual investigations; Q-05 also records a presentation choice for later planning. Tables describe ways to resolve or handle uncertainty, not evidence that a solution has been implemented or authorized. No live answers are requested.

### Q-01 — Which attempt actually incurred the cited child usage?

[R0] Prior inventory item 1, attempt attribution. The issue attributes three children and ~$6.75 to #195 run 8; the failed-run report places them earlier. Neither allocation was independently reconciled. Compare persisted attempt intervals, prelaunch boundaries and new response records before accepting the four-invocation/~$9.39 target. Historical parentage alone cannot establish attempt ownership (`internal/codex/usage_rollout.go:129`, `:261`, `:330`, `:451`).

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Reconcile the original evidence — Recommended | Match each cited child response to an attempt and account for already billed history. | Settles the incident and avoids double charging. | Requires sanitized, read-only runtime evidence. |
| Use a fresh reproduction | Demonstrate current behavior with known attempt boundaries; retain the original claim as unresolved. | Can establish a reproducible current defect. | Does not settle the historical amount. |

### Q-02 — What do the reported usage totals actually measure?

[R0] Prior item 2, source semantics. The exact authenticated Codex version and record shapes behind the seven Implement attempts and successful Plan attempt remain unknown. Determine whether totals are exclusive response records or cumulative summaries. The inspected collector bills `token_usage_record`, not cumulative `token_count`, and requires runtime model provenance (`internal/codex/usage_rollout.go:454`, `internal/meter/compatibility.go:5`).

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Authenticate the original records — Recommended | Record provider version, source shape and counter meaning from sanitized incident evidence. | Makes reconciliation and compatibility claims reliable. | Requires original provenance and record inspection. |
| Validate a current controlled sample | Establish supported shapes for a known current version. | Gives a defensible present-day contract. | Historical compatibility remains unsettled. |

### Q-03 — What caused descendants to disappear during the affected attempts?

[R0] Prior item 3, reproduction. It remains unproven that children executed during each cited attempt and which mechanism explains missing measurements. Candidate mechanisms include unchanged intermediate ancestors, late discovery, scan limits and unrecognized records; none was tied to the incident (`internal/codex/usage_rollout.go:263`, `:377`, `:579`, `internal/codex/usage.go:171`).

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Reproduce from reconciled incident evidence — Recommended | Trace discovery and association using actual attempt boundaries and sanitized records. | Targets the cause and provides a regression case. | Depends on the attribution and source questions. |
| Test the known failure candidates independently | Exercise ancestry, timing, limits and unsupported records with controlled fixtures. | Can improve resilience even if incident evidence is unavailable. | Cannot prove which candidate caused the original incident. |

### Q-04 — Was the unattributed Claude attempt refused before billable work?

[R0] Prior item 4, Claude refusal. Attempt `4110168a8c21fb19bce49ff8693a28a9` has not been conclusively matched to authenticated refusal/no-billable-work evidence. Separate refusal and cold-fallback attempts are supported by the inspected code, but missing model/usage does not prove zero (`internal/turn/harvest.go:165`, `:178`, `:216`, `internal/harness/usage.go:201`, `:299`).

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Classify using authenticated attempt evidence — Recommended | Confirm refusal and distinguish proven no-work from missing measurements. | Explains the row without concealing uncertain spend. | May still leave the amount unknown. |
| Retain the unknown classification | Keep the durable attempt and missing-evidence status. | Preserves honest accounting without guessing. | Leaves the incident unexplained and the subtotal incomplete. |

### Q-05 — Which report showed wholly unknown pricing, and how should incomplete subtotals read?

[R0] Prior item 5 combines a factual report-surface gap and a presentation decision. The reported all-unknown group is not reconciled to the exact comment/JSON/CSV surface; inspected aggregation mixes known/unknown pricing as partial (`internal/report/meter.go:94`). Missing lower-bound wording is already established for the inspected version (`:165`); collection and pricing completeness must stay distinct (`:98`). No owner wording preference has been selected.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Trace the exact output and use explicit lower-bound wording — Recommended | Reconcile the affected surface; label incomplete observed subtotals as a lower bound and explain excluded unmeasured work. | Keeps useful measured spend while making uncertainty visible. | Requires wording and coverage cases for each affected surface. |
| Trace the output and withhold incomplete subtotals | Display unknown total and coverage without a numerical subtotal. | Reduces the chance readers mistake partial spend for total cost. | Removes useful measured-cost information; this is a material product alternative, not selected scope. |

### Q-06 — Which findings still apply to the version that will be fixed?

[R0] Prior item 6, current-main applicability. The earlier factory prompt reported a base 79 commits behind main. Current divergence and intervening collector, documentation and test changes were not checked. Historical citations must be reassessed on the factory-selected implementation base; this inventory run does not synchronize it.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Reassess on the factory-selected current base — Recommended | Check collector, reporting, provenance documentation and tests after factory-managed synchronization when applicable. | Avoids duplicate or obsolete fixes. | Requires a later scoped comparison. |
| Investigate the retained historical base first | Use the previously inspected revision to reproduce the original behavior before comparing later changes. | Preserves a stable historical target. | Findings still need reassessment before implementation. |

## Answered Questions

[V] The previous brief had no numbered answered inventory. The following IDs make its already-established answers explicit without resolving any of its six factual gaps. Answers are carried forward from run 0002, not independently reverified on the current base.

- **Q-07 — Is there a separate Implement collector?** [R0] No separate mode branch was found: Plan and Implement share the runner/collector lifecycle (`internal/codex/usage.go:31`, `internal/codex/usage_rollout.go:217`). Mode correlation alone does not identify a mode-specific defect.
- **Q-08 — Does shared parentage prove that child spend belongs to run 8?** [R0] No. Baselines and historical-record handling deliberately exclude earlier consumption (`internal/codex/usage_rollout.go:129`, `:261`, `:330`, `:451`, `:524`). Ancestry establishes a relationship, while new usage must be assigned to an attempt. Which attempt incurred it remains Q-01.
- **Q-09 — Is the lower-bound wording concern independent of the 72% claim?** [R0] Yes. The inspected report says “observed priced subtotal” without “lower bound,” while withholding estimated totals on incomplete collection (`internal/report/meter.go:165`, `:98`). Improving clarity does not depend on proving the incident's percentage; precise surface and presentation scope remain Q-05.
- **Q-10 — Could resume refusal and fallback produce two attempts?** [R0] Yes. Separate metered calls support that sequence, and an early root can lack assistant model evidence (`internal/turn/harvest.go:165`, `:178`, `:216`, `internal/harness/usage.go:201`, `:299`). This is a plausible mechanism, not confirmed identification of the named Claude attempt; Q-04 remains open.
- **Q-11 — Does the retained test evidence demonstrate authentic descendant compatibility?** [R0] No. The reported tests were synthetic and transport tests disclaim real descendant compatibility (`internal/codex/usage_test.go:13`, `internal/turn/meter_transport_test.go:18`); source provenance also requires reconciliation (`internal/codex/testdata/USAGE_SOURCE.md:23`). This limits prior evidence and is not a freshly checked claim about today's suite.
- **Q-12 — Can missing attribution be repaired by guessing a model or declaring zero?** [R0] No. Unknown counters and actual runtime model provenance must be preserved (`internal/agent/usage.go:9`, `internal/harness/usage.go:299`, `internal/store/meter_snapshot.go:198`). Missing evidence cannot establish no billable work, and removing the attempt would hide uncertainty.

## Recommended scope for Plan mode

First establish an authenticated, sanitized attempt-level reproduction and reconcile reported children with attempt boundaries. Scope collector changes to failures demonstrated by that evidence, considering the unchanged-ancestor and late-discovery limitations explicitly. Include focused rollout fixtures for fresh/resumed, parallel/nested, delayed and unrecognized-source cases, with exclusive usage, parent identities, replay deduplication and honest coverage outcomes. Preserve bounded discovery and avoid charging unrelated sessions.

Include explicit lower-bound wording for incomplete observed subtotals and truthful classification of the Claude refusal/fallback case if evidence supports it. Preserve unknown values where evidence cannot establish zero or actual model identity. Reconcile provider-source documentation with authentic fixtures and the implemented contract.

Do not accept unconditional inclusion of all historical descendants, the proposed run-8 total, a general pricing rewrite, removal of unknown coverage rows, or an Implement-only collection change as pre-decided requirements. Those conclusions are not established by this research. Reassess the supplied-base findings after synchronization; implementation and acceptance belong to later explicitly selected modes.
