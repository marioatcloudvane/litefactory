# Research Brief — meter-misses-every-descendant-on-codex

## Request

Retry Research for issue #211, “Meter misses every descendant on Codex implement runs, undercounting cost by ~72%.” Investigate descendant collection, an unattributed Claude attempt, and incomplete-subtotal reporting. This is Research only: no implementation, tests, commits, forge access, or automatic transition to Plan.

Evidence tags: **[V]** verified in this run by the repository codebase-tour specialist; **[O]** owner/issue report, not independently verified; **[R0]** statement carried from the supplied earlier failed-run report, not reverified; **[W]** other second-hand evidence. A verified control-flow limitation is not proof that it caused the reported incident.

The explicit headless instruction is interpreted as authorization to complete this research without confirmation. Unknown facts are retained below. [O] Factory says branch synchronization was deferred, with 79 commits behind origin/main and zero ahead. Findings apply to this supplied task base, not current main. The earlier session was not read or modified, and its retained work is not evidence of success.

## Codebase Context

### Ownership and flow

[V] The single Go 1.26 module (`go.mod:1`) routes metering through `internal/agent`, provider adapters `internal/codex` and `internal/harness`, `internal/turn`, `internal/meter`, `internal/store`, and `internal/report`.

1. `agent.UsageCapture` supplies the synchronous persistence seam; invocation, observation and coverage events remain distinct (`internal/agent/usage.go:146`).
2. `turn.runMetered` creates a durable attempt for each runner call before invoking it, and finalizes that attempt independently of subprocess success (`internal/turn/meter.go:17`, `internal/turn/meter.go:23`, `internal/turn/meter.go:37`).
3. Codex inventories session files before launch, consumes stdout, waits for the process, and finalizes rollout collection (`internal/codex/runner.go:320`). `usage.go` owns stream/lifecycle handling and `usage_rollout.go` owns session discovery and consumption.
4. Associated session response records provide exclusive consumption; exec terminal totals supply inclusive reconciliation evidence (`internal/codex/usage.go:150`, `internal/codex/usage_rollout.go:546`). The turn persists normalized evidence and exposes exclusive measurements plus non-additive coverage rows.
5. Run aggregation consumes snapshot rows, and reporting combines their stored prices without repricing (`internal/turn/meter_report.go:16`, `internal/report/meter.go:44`).

[V] Nil counters mean unknown rather than zero (`internal/agent/usage.go:9`). Actual model identity requires runtime provenance; configured model names are not a replacement for missing attribution.

### There is no Implement-specific collector branch

[V] The collector receives usage capture and operates on thread identity, file changes, ancestry and recognized records. It does not branch on Plan versus Implement (`internal/codex/usage.go:31`, `internal/codex/usage_rollout.go:217`). Both modes use the same runner collection lifecycle. Consequently, the observed mode correlation needs explanation through differing attempt history, emitted records, discovery timing or evidence availability; the code does not support assuming a distinct Implement collection implementation.

[V] Ancestry readers support top-level `parent_thread_id`, nested `source.subagent.thread_spawn.parent_thread_id`, and exec `collab_tool_call` spawn events with known sender/receiver identifiers (`internal/codex/usage_rollout.go:63`, `internal/codex/usage_rollout.go:324`, `internal/codex/usage.go:112`).

## Domain / Problem Space

### The incident claim and its unresolved timing conflict

[O] The issue reports seven Codex Implement roots with zero descendants, one Plan root with one descendant, and a Claude Implement root with three descendants. For #195 run 8, it names root `01a0864d-41ce-7bf1-bc89-2d52d796c164` and three files:

- `rollout-2026-09-09T20-16-44-01a08762-f597-70d3-b3e1-a220b2580fc2.jsonl`
- `rollout-2026-09-09T20-17-21-01a08763-8637-7a53-a625-e15a2d20f1e0.jsonl`
- `rollout-2026-09-09T20-17-33-01a08763-b58a-7cf2-aa7f-e968ee2ee839.jsonl`

[O] All three reportedly name that root as parent. The issue attributes $2.632032 to root usage and $6.754276 to the three children, proposing a four-invocation total around $9.39 and approximately 72% missing spend. These quantities and their assignment to run 8 were not independently verified this run.

[R0] The supplied earlier failed-run report instead says those children occurred about two hours before run 8 and belonged to earlier work on the resumed thread. The earlier process failed and produced no completed brief. Its timing conclusion is therefore retained as an unresolved counterclaim, not upgraded to a verified fact.

[V] Code establishes why that distinction matters:

- Prelaunch inventory records existing rollout sizes and identities (`internal/codex/usage_rollout.go:129`).
- Unchanged pre-existing files are skipped (`internal/codex/usage_rollout.go:261`).
- Changed existing files retain a prelaunch byte boundary (`internal/codex/usage_rollout.go:330`).
- Historical records seed deduplication/model state without emitting new billable observations (`internal/codex/usage_rollout.go:451`, `internal/codex/usage_rollout.go:524`).
- Response records with another thread identity are excluded as inherited history (`internal/codex/usage_rollout.go:497`).

Matching ancestry proves a thread relationship, not consumption in a particular metered attempt. Summing whole child-file totals into a later resumed attempt could double charge earlier work. Neither the owner's run-8 allocation nor the earlier timing correction can be accepted without matching response records to persisted attempt intervals and prelaunch boundaries. Zero descendant coverage rows also does not prove no descendants were missed.

### Record contract matters

[V] Rollout billing recognizes `token_usage_record` with valid response/turn identities and matching `turn_context` model evidence. `event_msg` handling recognizes finalization events but does not bill cumulative `token_count` events (`internal/codex/usage_rollout.go:454`). The issue's phrase “final usage records” must be resolved to an exact source shape. A cumulative file total and exclusive attempt response records are different evidence and cannot be substituted without reconciliation.

[V] Compatibility evidence requires authentic version/source provenance (`internal/meter/compatibility.go:5`). The synthetic fixtures inspected here do not establish compatibility with the installed Codex version or the issue's authentic rollouts. `internal/codex/testdata/USAGE_SOURCE.md:23` still says associated sessions are not scanned without a validated contract; that note does not fully describe the current rollout-reading implementation and should be reconciled with authenticated source evidence.

### Concrete collector limitations, not confirmed incident causes

| Finding | Verified mechanism and consequence | Citation |
|---|---|---|
| Changed descendants behind unchanged ancestors | Baseline skipping omits an unchanged intermediate ancestor; association requires a known parent. A changed grandchild cannot associate if that intermediate parent is also absent from the current stream. | `internal/codex/usage_rollout.go:263`, `internal/codex/usage_rollout.go:377` |
| Unassociated files have no final diagnostic | Parsed but unassociated files are ignored by final diagnostics. A missed descendant need not become a coverage row. | `internal/codex/usage_rollout.go:579` |
| Finalization can precede unknown late children | Collection returns when known associated files are terminal. The two-second deadline is a ceiling, not a guaranteed grace period for children not yet discovered. | `internal/codex/usage.go:171`, `internal/codex/usage_rollout.go:549` |
| Bounded discovery | Entries, files, bytes and elapsed discovery time are limited. Incomplete scans have controlled diagnostics; incident evidence must show whether any limit was encountered. | `internal/codex/usage_rollout.go:26`, `internal/codex/usage_rollout.go:145`, `internal/codex/usage_rollout.go:350` |

All table findings are [V] control-flow observations. None is asserted as the cause of the seven reported Implement results.

### Unattributed Claude attempt

[O] The issue names attempt `4110168a8c21fb19bce49ff8693a28a9` in #195 run 12 as an unknown-model root coverage row, alongside a successfully attributed attempt. [R0] The failed prior run described a resume refusal followed by a fresh attempt.

[V] That explanation is plausible in this code: resume refusal and cold fallback invoke `runMetered` separately while sharing a run ordinal/log (`internal/turn/harvest.go:165`, `internal/turn/harvest.go:178`, `internal/turn/harvest.go:216`). Claude registers a root before assistant usage is required (`internal/harness/usage.go:201`), while exclusive model attribution requires assistant-message model evidence (`internal/harness/usage.go:299`). Missing measurements produce a coverage row with unknown pricing (`internal/store/meter_snapshot.go:198`).

This is a mechanism, not conclusive identification of the named attempt. Absence of usage/model evidence does not establish zero usage. Any future classification of a refused launch must distinguish authenticated no-billable-work evidence from merely missing evidence and retain the durable attempt record. Guessing the configured model or suppressing the row would compromise existing unknown-versus-zero semantics.

### Report wording

[V] `meterMetadata` always labels the value “observed priced subtotal,” without “lower bound” (`internal/report/meter.go:165`). Incomplete collection already withholds estimated totals and total token counters (`internal/report/meter.go:98`). This confirms the wording concern independently of the proposed 72% undercount.

[V] Mixed known/unknown pricing rows aggregate to partial, not necessarily unknown (`internal/report/meter.go:94`). The issue's statement that one unknown row makes the entire group's pricing unknown needs checking against the exact report/JSON/CSV surface. Pricing completeness and collection completeness must remain distinct; wording should explain that a subtotal excludes unmeasured work without implying missing consumption has been quantified.

### Existing test evidence

[V] The inspected Codex usage tests are synthetic exec-JSONL tests (`internal/codex/usage_test.go:13`). The specialist found no test references to `prepareRollouts`, `token_usage_record`, or `rolloutsFinalized` under `internal`. Thus the inspected suite does not demonstrate rollout discovery, baseline-skipped ancestry, changed descendants behind unchanged ancestors, delayed discovery, or rollout-model attribution.

[V] Transport tests explicitly disclaim real descendant compatibility (`internal/turn/meter_transport_test.go:18`). Fallback tests verify distinct refusal/fallback attempts and replay deduplication, but both synthetic attempts have model observations and do not cover the reported unattributed shape (`internal/turn/meter_test.go:12`). Fixtures use temporary executables/filesystems/databases. No tests or other build checks were run during this read-only research. `Makefile` defines `check: fmt vet lint test`, including race tests for implementation verification.

## Constraints & Risks

- [V] Research was performed through the mandated codebase-tour specialist. Its repository-only scope and orchestrator restrictions prevented inspection of runtime session/log/database contents. Availability of those paths was not tested. This is a research scope limit, not a claim that evidence is missing on disk.
- [O] Current task base is 79 commits behind main. A synchronized follow-up must check which findings still apply before changing implementation.
- [V] No separate Implement collection branch was found. A mode-specific fix without authentic evidence risks treating correlation as cause.
- [V] Historical ancestry and current-attempt usage are separate. Broadening discovery must preserve prelaunch boundaries, replay deduplication and exclusion of inherited records.
- [V] Unknown unassociated files create an observability limit. Their mere existence also cannot justify charging unrelated sessions; ancestry resolution must remain evidence-based and bounded.
- [O] The issue reports exact pricing, correct resume deduplication, non-additive subsets, faithful zero cache-write counters and unknown-versus-zero behavior as working. Preserve these invariants; this research did not independently audit all their arithmetic or runtime output.
- [V] A missing model must not be repaired by substituting configuration. Failed attempts must not silently disappear merely to improve group pricing status.
- Repository rules reserve SQL to `internal/store`, git operations to `internal/git`, and daemon runtime state to the daemon. Any later authorized evidence extraction must be read-only and must not expose unrelated session contents or credentials.
- No files outside this session were modified. No production code, runtime state, protected harness tree, forge state or git history was changed.

## Open Questions

1. **Attempt attribution:** Which persisted attempt intervals and prelaunch boundaries contain new response records from each cited child? Does #195 run 8 contain any of that usage? An authorized runtime-evidence investigation must resolve the owner/prior-run timing conflict before accepting a four-invocation/$9.39 target.
2. **Source semantics:** Are the issue's totals exclusive response records or cumulative summaries? What authenticated Codex version and record shapes produced the seven Implement attempts and successful Plan attempt?
3. **Reproduction:** Did descendants execute during those exact attempts, or were they only historical relatives of resumed threads? Did unchanged intermediate ancestors, delayed files, discovery limits or malformed/unrecognized records occur?
4. **Claude refusal:** Does attempt `4110168…` have authenticated refusal evidence and evidence of no billable work, or simply insufficient measurement? What explicit classification can truthfully preserve that distinction?
5. **Report surface:** Which aggregation surface yielded the claimed all-unknown group pricing state? How should incomplete collection and partial pricing each qualify the displayed subtotal?
6. **Current main:** Which changes since the supplied task base already affect collector behavior, provenance documentation or test coverage? Resolve after factory-managed synchronization.

These are retained research questions for a subsequent authorized investigation/Plan run, not requests for a headless response. No web research was needed for the code-backed findings; installed-provider compatibility remains unsettled rather than answered from memory.

## Recommended scope for Plan mode

First establish an authenticated, sanitized attempt-level reproduction and reconcile reported children with attempt boundaries. Scope collector changes to failures demonstrated by that evidence, considering the unchanged-ancestor and late-discovery limitations explicitly. Include focused rollout fixtures for fresh/resumed, parallel/nested, delayed and unrecognized-source cases, with exclusive usage, parent identities, replay deduplication and honest coverage outcomes. Preserve bounded discovery and avoid charging unrelated sessions.

Include explicit lower-bound wording for incomplete observed subtotals and truthful classification of the Claude refusal/fallback case if evidence supports it. Preserve unknown values where evidence cannot establish zero or actual model identity. Reconcile provider-source documentation with authentic fixtures and the implemented contract.

Do not accept unconditional inclusion of all historical descendants, the proposed run-8 total, a general pricing rewrite, removal of unknown coverage rows, or an Implement-only collection change as pre-decided requirements. Those conclusions are not established by this research. Reassess the supplied-base findings after synchronization; implementation and acceptance belong to later explicitly selected modes.
