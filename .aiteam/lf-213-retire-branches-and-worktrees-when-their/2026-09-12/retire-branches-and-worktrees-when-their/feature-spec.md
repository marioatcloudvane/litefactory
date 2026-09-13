# Feature Spec: retire-branches-and-worktrees-when-their
# Status: APPROVED
# Date: 2026-09-12

## Goal
- Reclaim completed tasks' checkout space and merged branches automatically, with inspectable decisions and no deletion of local-only content.

## Scope and Decisions
- Owner run 0003: proceed to Plan; keep the implementation small and reuse existing facilities.
- Owner run 0002: daily sweep accepted; issue closure checks and cleanup must be logged.
- Owner run 0003 agreement adopts fresh individual GitHub issue verification and visible logging described in run 0002.
- Research recommendations adopted: strict commit ancestry, retention from last completed run, conservative ignored-file protection, preserve unmerged branches, shared parent ownership, and resumable cleanup.
- Plan artifacts only in this run; no implementation, cleanup, automatic mode switch, or approval to merge.

## User Stories
| ID | User need |
|---|---|
| NEED-1 | As an operator, inspect the same retirement decisions the daily sweep will use before resources disappear. |
| NEED-2 | As an operator, have closed tasks retired daily when their content is safely preserved. |
| NEED-3 | As an operator, see why closure was accepted and what was removed, retained, or failed, including after restart. |
- NEED identifiers describe needs, not factory story keys; the orchestrator sizes and authors the typed stories.

## Existing Features to Reuse
| Need | Existing owner |
|---|---|
| Task/resource inventory, run timestamps and durable task locks | `internal/store` sessions, repositories and runs |
| Confirm issue state | `internal/github.Client.GetIssue`, final request with `WithoutConditionalCache` |
| Git fetch, worktree identity, status, removal and recreation | `internal/git`; extend its pinned surface for missing preservation/deletion operations |
| Shared clone coordination and scheduling | `internal/daemon` clone gate and tick lifecycle |
| Offline real Git verification | `internal/gittest` local bare-origin fixtures |
| Operator evidence | Injected structured logger, daemon.log and existing `litefactory logs --all --grep retirement` |
| Retention input | Existing `settings.worktree_retain_days`, default 14, valid integer range 1–3650 |

## Data Model
| Entity | Required data | Constraints |
|---|---|---|
| Registered resource owner | Existing repository, task/session identity, branch, canonical worktree ownership, configured base | Parent resources are evaluated against the parent issue; no ownership inferred from an `lf/*` prefix alone |
| Sweep progress | Due time and bounded-pass progress | Survives daemon restart; repeated restarts do not initiate repeated full scans; progress must not starve later owners |
| Retirement attempt | Sweep/task identity, resource path/ref, observed SHAs, decision/reason, intent and confirmed/uncertain outcome | Minimal durable record before deletion; retained independently of the checkout; retry reconciles observed absence |
- Retain session identity, run reports, acceptance and delivery records after retirement.
- Clear recorded worktree path only after confirmed removal or confirmed absence; retain sufficient ownership to repair stale Git registration.
- Do not create a second workflow-completion model or general job framework.

## API / Interface Changes
| Surface | Behavior | Auth / validation |
|---|---|---|
| `litefactory task retire` | Preview registered resources using the same evaluator as automation; list exact resource, action and reason | Existing local operator and configured repository credentials; no arbitrary filesystem paths or refs |
| Daily daemon sweep | Apply fresh verified decisions after startup recovery and when due; resume bounded batches | Existing daemon credentials, task ownership and clone coordination |
| Existing logs command | Find default-visible closure, decision, result and summary records containing `retirement` | Existing logs access; no new history command required |
| Existing configuration/help/docs | Describe active retention, preview, skips, ancestry limitations and logging | Preserve existing retention bounds/default; no new policy matrix or per-task overrides |
- Preview never deletes, clears resource ownership, records successful retirement, or advances the automatic due marker; refreshing tracking refs and reading GitHub are allowed.
- No manual apply command in this version: daemon is the sole new retirement mutation entry point, avoiding a second cross-process mutation path.
- Existing `task clean` remains an explicit operator command; correct its informational-retention text without expanding its behavior in this feature.

## Business Rules
- Enumerate registered owners even if their issues no longer appear in open-issue polling or local workflow state is unfinished.
- Only a successful individual issue response matching expected identity and exactly `state: closed` permits cleanup; closure reason is context, never preservation proof.
- Under task/clone coordination, re-read run/effect state and verify closure again without conditional caching immediately before mutations; reopen before that check cancels cleanup.
- Skip active/prelaunch runs, concurrent ownership, and pending operations needing the checkout.
- Fetch/prune the expected remote successfully; verify configured remote base and local/remote task tips from fresh refs.
- A worktree is expired when the last completed run, successful or failed, finished at least `worktree_retain_days` ago. With no run, use valid session creation time only with certain ownership. Never use lock-mutated session UpdatedAt.
- Remove an expired owned worktree only if tracked/staged, ordinary untracked and ignored content are all absent, and its exact local tip is reachable from the fresh remote task tip or remote base.
- Use normal unforced worktree removal after final checks. Dirty or locally unpreserved tasks defer all retirement for that task.
- Delete each branch only if its own observed tip is an ancestor of the fresh remote base tip. Local and remote tips require independent proof.
- Remote branch deletion need not wait for worktree retention. Local branch deletion waits until no worktree uses it.
- Guard branch mutation against changes to the observed head; changed refs cause a skip/retry, never unconditional deletion.
- Never delete the configured base/default branch, uncertain resources or another worktree's local branch.
- Record durable intent before mutation; a failed intent write prohibits deletion. Confirm outcomes; uncertain results remain retryable and must not be reported as success.
- Daily scheduling respects existing request budgets/backoff, shutdown and clone/task lock ordering; errors on one owner do not discard remaining progress.

## Lifecycle and Error Handling
| Path | Required outcome |
|---|---|
| Validate passes or PR merges while issue remains open | Keep all resources |
| Factory or another person merges and closes issue | Apply identical closure, preservation and retention rules; do not invent acceptance |
| Won't-do, duplicate, abandoned or otherwise closed with unmerged commits | Expired backed-up clean worktree may go; retain unmerged branches |
| Squash/rebase/cherry-pick integration without exact ancestry | Retain original branches; equivalent content and historical PR merge flags are insufficient |
| Remote task branch absent | Use fresh base preservation proof; if original local commits are absent from base, keep worktree and local branch |
| Remote-only registered branch | Evaluate without requiring a checkout |
| Child closes while parent owns shared worktree | Keep parent resources unless parent independently qualifies |
| Dirty, ignored or local-only content; wrong branch/path; detached or locked worktree | Keep and report a specific reason |
| 404, timeout, rate limit, malformed/null/unknown/mismatched issue response, missing base or failed Git probe | Skip as unknown; never infer closure or preservation |
| Crash, removed resource, stale owned Git registration or partial failure | Reconcile exact owned state and retry; preserve other task records; never recursively remove an arbitrary directory |
| Reopen after cleanup | Next authorized run recreates checkout from surviving local branch, remote branch, or fresh base through normal setup |
- Document residual cross-system race: an issue may reopen immediately after the final GitHub read; task locks do not serialize arbitrary external filesystem changes.

## Logging
- Default-visible structured closure records: sweep ID, repository, issue/session, observation time, source, initial/final phase, returned state/reason or error category, decision.
- Resource decisions/results: path or exact ref, preview/apply, retention evidence when relevant, observed branch/base SHAs, reason, intended action and actual outcome.
- Outcomes distinguish `would_remove`, `removed`, `kept`, `already_absent`, `failed`, and `unknown`; preview never claims removal.
- Sweep summary includes zero-candidate passes, duration, counts and deferred/incomplete work; interrupted passes are not labelled completed.
- Ordinary decisions are Info; unavailable verification is Warn; mutation/storage failures are Error. Never log credentials, issue bodies or file contents.

## Assumptions and Sizing
- Daily means once per elapsed 24 hours when the daemon can run, not a configurable wall-clock appointment.
- First due pass may run after startup recovery; persisted scheduling prevents a fresh full sweep on every restart.
- An old checkout can qualify immediately after issue closure; retention is not a post-closure grace period.
- Skip ignored content rather than classify caches versus secrets; operators may remove disposable generated files themselves.
- Preview plus existing log viewer is sufficient operator surface; one minimal durable retirement record supports recovery without a dashboard or event framework.
- No material owner-only questions remain; architect may choose bounded batching and minimal storage representation without adding product options.

## Out of Scope
- Automatic archival tags, deletion of unmerged history, squash/content-equivalence heuristics, history rewriting, base-branch changes or GitHub delete-on-merge settings.
- Closing issues, changing acceptance/delivery decisions, recurring issue comments, scanning arbitrary worktree roots, or altering manual `task clean` safety semantics.
- Manual apply/history UI, configurable schedules, per-task policy overrides, instruction-tree maintenance or live coding-agent capability changes.

## Acceptance Candidates
- [ ] AC-C01: Preview evaluates registered owners, including externally closed and remote-only owners, with exact-resource decisions; preserves resource contents/refs and retirement scheduling/state apart from permitted fetch updates.
- [ ] AC-C02: Only explicit matching closed issue responses permit cleanup; open, 404, failed, null, malformed and unknown responses skip; final uncached reopen cancels mutation; closure checks are logged.
- [ ] AC-C03: Worktree retention uses last completed run or valid no-run creation fallback, honors configured bounds/default, and never expires running/prelaunch work or uses UpdatedAt.
- [ ] AC-C04: Real Git cases prove dirty/staged/untracked/ignored content and local-only commits are kept; clean expired worktrees are removed only with fresh remote-task/base preservation proof.
- [ ] AC-C05: Local and remote branches require independent exact-tip ancestry; unmerged and squash/rebase-equivalent tips remain; absent upstream is not proof; expected-head changes reject deletion.
- [ ] AC-C06: Ownership checks protect base/default branches, manual resources, shared parent resources and other worktrees; task/clone coordination excludes active work and required pending effects.
- [ ] AC-C07: A bounded daily pass survives restart without duplicate full scans, progresses fairly through owners, respects request backoff/shutdown, and continues past individual failures.
- [ ] AC-C08: Durable intent precedes deletion; simulated interruption/outcome-storage failure yields uncertain retryable records; reconciliation handles exact-resource absence/stale registration without deleting unrelated data.
- [ ] AC-C09: Reopening after full or partial cleanup can recreate the worktree on the next authorized run; original run/acceptance/delivery records remain unchanged.
- [ ] AC-C10: Default-visible logs include closure evidence, per-resource reason/result and sweep totals; preview, skipped, uncertain, interrupted and removed outcomes remain distinct and sensitive contents are excluded.
- [ ] AC-C11: CLI help/configuration documentation explains daily automation, preview invocation, active last-run retention, ignored-file skips, strict ancestry and residual reopen race; no informational-only retention claims remain.
- [ ] AC-C12: Lifecycle regression fixtures cover successful/external closure, won't-do/duplicate abandonment, merge while open, deleted upstream, remote-only resources, and shared children; required repository `make check` passes during Implement.

## Candidate Evidence Obligations
| Criteria | Proof method | Source availability | Version/context bounds | Missing prerequisite |
|---|---|---|---|---|
| AC-C01–C03, C06–C10, C12 | Controlled forge responses, fake clock/lock/error injection, temporary real store, captured structured logs and lifecycle assertions | Existing GitHub HTTP contract tests, daemon/store test patterns; feature tests to be written | Current repository implementation; synthetic GitHub responses prove handling, not production credentials or GitHub availability | Implement must execute and retain relevant passing checks |
| AC-C04–C06, C08–C09, C12 | Real local bare-origin Git fixtures, ref-change races and temporary worktree/filesystem assertions | `internal/gittest` available; feature fixture cases to be written | Installed Git used by checks; offline origin proves local Git safety/deletion semantics, not hosted service policy | Implement must execute and retain real Git evidence |
| AC-C11 | Source/help/documentation review plus relevant CLI rendering assertions | Existing config/template and task CLI files available | Repository behavior described by this feature | Updated documentation and review required |
- No nested agent, paid service, production deletion or authentic dual-harness evidence obligation applies to this feature.
