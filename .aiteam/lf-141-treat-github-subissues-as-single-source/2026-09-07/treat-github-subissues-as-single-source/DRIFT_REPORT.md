# Drift Report — treat-github-subissues-as-single-source
# Branch: lf/141-treat-github-subissues-as-single-source | Date: 2026-09-08

## Verification basis
Run 0006 resumed retained, uncommitted implementation instead of rebuilding it. Root verified local HEAD and the remote task branch; implementation was not published. This report distinguishes code already present from this run's acceptance verification and repairs. The acceptance specialist inspected existing tests and ran focused race tests across agent, planbrief, github, harness, report, store, turn and daemon; all passed. Final make check passed: format/vet, pinned golangci-lint v2.13.1 with 0 issues, race tests 27 packages passed and 0 failed. Final go build and whitespace checks passed. No live forge or real coding-agent execution was used.

## Delivered as planned
- US-1 / T-1.1, T-1.3–1.5, T-1.7: retained durable managed identities, complete story publication, current-base updates/recovery and link-only index/transport exclusion; existing publication/stale-base/recovery acceptance fixtures passed this run.
- US-1 / T-1.2, T-1.6: retained strict proposal/acceptance contracts and verified existing parser/rendering tests; this run added unknown-dependency, duplicate-key and duplicate-numeric-identity test cases. Workflow mirror portions remain deferred below.
- US-2 / T-2.1–2.4: retained child provenance, parent-routed discovery/admission and attributable retryable reporting. Existing numeric authorization, routing and delivery fixtures passed.
- US-3 / T-3.1–3.4: retained atomic approved sets, freeze notices, lifecycle-based refusals and explicit quiescent parent replanning. Existing immutable-scope/refusal/recovery fixtures passed.
- US-4 / T-4.1–4.2, T-4.4: retained shared approved bundles for cold/resumed/fallback execution, relationship checks and context isolation. Both fake-runner paths passed; no live agent was launched.
- US-4 / T-4.3: current writable Claude/user workflow documents describe selected revisions and immediate scoped checks. This run's app-context-updater inspected actual code and updated APP_CONTEXT and APP_CONSTRAINTS, including aggregate-only parent recovery. Protected Codex copies remain deferred.
- US-5 / T-5.1–5.2, T-5.4–5.6: retained strict incremental evidence, durable attempts/effects, close/reopen adapter and recovery. Existing live closure with held parent/sibling, replay rejection, invalidation, delivery recovery and complete new-workflow lifecycle tests passed with race detection.
- US-5 / T-5.3: retained runtime evidence validation, immediate reporting/closure and affected-story reopening. The shared application protocol is verified; installation of protected Codex workflow instructions is still outstanding.

## Added scope
- Prior-run narrow lint cleanup remains in the retained tree; it is not a new run 0006 accomplishment.
- Run 0006 repairs one new test-local identifier lint finding and adds three small missing contract rejection cases, without new behavior scope.
- A bounded baseline team lint repair replaces six variable-path reads with a shared repository-rooted helper, opens the fixed repository lock with rooted/O_NOFOLLOW handling and exclusive creation followed by a noncreating open on EEXIST (repairing a concurrent-create regression found by the final race gate), and renames internal TeamEntry to Entry across its limited callers without changing JSON. Parent tested team and added path containment coverage; scoped independent static review found no new blocking issue. This is required to clear the repository-wide pinned check, not a story migration feature.

## Dropped or deferred
- US-6 and all legacy story migration/conversion/backfill/cleanup remain excluded by the owner's run 0004 decision. New embedded SQL migrations support the new workflow and are not legacy story migration.
- Required actual Codex authoring/execution/review/test skill parity is blocked by the read-only `.codex` sandbox. CODEX_PLAN_PARITY.patch and CODEX_IMPLEMENT_PARITY.patch are reviewable intended edits, not installed instructions. Root verified both together with read-only git apply --check this run (exit 0). AC-1.6 and AC-4.5 cannot be declared fully satisfied; applicable portions of T-1.2/T-1.6/T-4.3/T-5.3 remain incomplete.
- Live external integration is skipped because TEST_BASE_URL and TEST_DATABASE_URL are unset. Real SQLite, local git, httptest and fake-runner tests cover repository-supported boundaries.
- No GitHub publication, issue synchronization, closing of real child issues, commit, push or deployment was performed by agents in this run. The factory owns those actions.

## Architectural deviations
- This bootstrap resumption received no factory frozen bundle. The dispatcher explicitly continued the previously approved five-story session inputs to finish the existing implementation; production managed execution remains constrained to supplied frozen bundles. This is a bootstrap assumption, not a local-file fallback in the delivered runtime.
- No new module, worker pool or legacy migration was introduced. Immediate closure remains independent of final aggregate success/save/push; parent-only recovery handles already-completed child scope.

## Verdict
DRIFTED — runtime acceptance and final aggregate checks are green, but protected Codex workflow parity remains a release gate. Apply the two preserved parity patches in an authorized writable environment and verify parity before claiming full delivery; no new planning cycle is needed for the approved scope.
