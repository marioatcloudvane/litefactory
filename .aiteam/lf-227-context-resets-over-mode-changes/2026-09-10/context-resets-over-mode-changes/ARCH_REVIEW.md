# Architecture Review: context-resets-over-mode-changes

Architect: Go Architect
Created: 2026-09-11
Verdict: APPROVED for typed story authoring with the evidence prerequisite below retained. No standards deviation required.

## System overview

The feature belongs in existing `turn` preparation, `store` conversation/session persistence and the shared runner input consumed by `harness` and `codex`. `daemon` remains responsible for admitting and confirming mode selection. A confirmed selection is not itself proof that a subprocess ran. `turn` must distinguish admission, conversation allocation and launch so a failed preparation cannot resurrect the outgoing conversation or allocate a new segment repeatedly on retry.

Existing `prepareDeliverySegment` in `internal/turn/delivery.go` rotates when the effective delivery identity changes, but its no-delivery fast path selects ordinal one. Its identity includes mode only when a delivery bundle exists. Replace that conditional policy with a mode-aware conversation policy independent of bundle presence. Reuse segment ordinals, `naming.SessionRef`, adapter resume tokens, delivery verification and the task lock. Preserve agent-switch refusals; supporting both agents does not authorize switching a task's agent.

Artifact identity is separate from native conversation identity. `findSessionDir` currently chooses a marker-dependent date (today, then newest); `findPlanningContext` already rejects multiple matching contexts. Neither is a general durable task-session binding. Bind one worktree-relative artifact session to the task in `store`, adopt a unique safe legacy candidate, and supply it across receiving modes. The factory may select a path for a new task without creating artifacts in Validate. Existing Plan working-context identity checks remain authoritative.

No new public package, service, dependency, CLI or configuration key is needed. Add small neutral context values to `internal/agent` where they cross adapters; keep rooted file I/O and orchestration in `turn`, SQL and migrations in `store`, and pure rendering in the existing prompt builder. Do not create a second story or evidence store.

### Boundaries and data flow

Confirmed daemon mode and request → turn validates existing admission/frozen-scope conditions → selects or reuses durable mode-bound segment → resolves task artifact binding → reads bounded safe artifacts and retained factory records → builds explicitly framed receiving-mode input → existing adapter launches fresh/resumed request → existing harvest/save/evidence paths retain results.

Receiving-mode handoff contains a validated artifact-session reference, available bounded background documents with provenance, omissions and receiving instructions. Frozen stories and retained validation context retain their existing separate typed authority. They must never be reconstructed from Markdown, comments or handoff summaries. Corrective Implement receives relevant retained blocking findings with assessment run, story/revision and snapshot/check attribution; old completed stories stay completed.

### Concurrency, errors and side effects

Use the existing task lock; no new goroutines, sleeps or clocks below daemon. Store changes use context-first accessors and small transactions, never transactions spanning filesystem or process I/O. Persist segment mode and task artifact binding through additive embedded migrations if existing state cannot express them; use the single SQLite connection unchanged. Make allocation/retry semantics atomic or idempotent. `SetSessionState(...Running)` remains the final successful preparation statement (CONC-7).

An absent optional brief is a reportable omission. An unsafe path is never read; ambiguous session identity is diagnosed and cannot silently select a replacement. Independent work may continue without that artifact, but Plan working-context ambiguity and missing approved implementation scope retain existing refusals. Existing frozen input and evidence limits remain hard safety boundaries; do not silently truncate an authoritative contract. Any new handoff budget reserves omission/provenance reporting and references oversized background documents explicitly.

## Cross-story decisions

1. Persist the segment's mode independently of delivery identity; select the latest segment even without a bundle. Rotate on admitted mode change or existing independent delivery-reset reasons, and never find an older matching-mode segment. Legacy segments need a deterministic mode backfill from attributable run records; unknown legacy mode must cause a conservative fresh boundary before reuse, not a guessed resume.
2. Persist one task artifact-session binding independently of the conversation. Reuse it across dates, restart and fallback. Legacy adoption must consider all relevant known session markers, validate task/branch/feature and reject ambiguity; do not rely on newest-date wins. Root all reads in the supplied worktree and preserve pinned descriptor/content checks (SEC-21).
3. Deliver one bounded handoff contract on cold, resume and fallback paths, with explicit consumption instructions and authority distinctions. Reuse current bounded recorded-comment policy. Do not serialize or summarize provider transcripts, and do not create a completion ceremony for outgoing phases.
4. Reuse frozen story bundles, immutable engineering evidence and retained validation assessments. Add only a narrowly scoped store query if needed to select applicable assessments. Validate stays read-only; failed/missing evidence stays failed/unverified; corrective findings do not reopen historical acceptance (CONC-11/12).
5. Shared source/root documentation precedes independently ready installed-tree stories. Codex edits `.claude/**`; Claude edits `.codex/**`. Scoped checks must not depend on installed sibling parity; final aggregate parity is separate.
6. Offline tests establish factory selection/delivery behavior only. Spec AC-006 remains an external feature-specific observation obligation; unavailable capture cannot block coding independent slices but prevents claiming that obligation passed. No unrelated historical evidence recovery or paid nested execution is introduced.

## Recommended story slices and task hints

These are proposed decomposition hints for the proxy product owner, not editable story sources. Final task/criterion IDs belong in typed stories.

| Slice | Dependencies | Proposed task | Owner role | Architectural hint |
|---|---|---|---|---|
| S1 Mode-bound conversations | none | Persist and select mode-aware conversation segments | go-implementation-engineer | Extend `internal/store/segments.go`, additive `internal/store/migrations/NNNN_*.sql`, and `internal/turn/delivery.go`/`prepare.go`. Mode identity must survive allocation-before-launch failure; preserve vendor refusal and bundle verification. Scope excludes artifacts and native CLI redesign. |
| S1 | none | Exercise transitions and recovery | go-unit-test-engineer | Extend existing turn delivery/prepare and adapter request tests. Cover all ordered distinct mode pairs, same-mode replies, no/external delivery, reverse transitions, legacy records, delivery changes, denied transitions, restart and cold fallback. Assert segment/token selection and unchanged task identity. |
| S1 | none | Verify durable allocation/retry behavior | go-integration-test-engineer | Use existing migrated SQLite, local git and fake runner fixtures under `internal/turn`/`store`; test failure boundaries and continuation binding. Ordinary Go tests, no integration tags or live agents. |
| S2 Task artifact binding | none | Resolve, validate and persist one artifact session | go-implementation-engineer | `internal/turn/sessiondir.go`, `plan_context.go`, `plan_attempt.go`, `internal/store/sessions.go` and additive migration; reuse `naming` and rooted artifact readers. All markers must resolve to the same binding; updating diagnostic/link consumers must not revive per-marker date guessing. Preserve Validate's no-artifact prerequisite. |
| S2 | none | Verify legacy discovery and security failures | go-unit-test-engineer | Fixtures for same session across dates, unique legacy adoption, multiple candidates, cross-task context, symlinked components, replacement during reads and size limits. Exercise typed omissions vs existing authoritative-context refusals. |
| S2 | none | Verify persisted binding survives reopen | go-integration-test-engineer | Real SQLite reopen and existing worktree fixtures; prove binding remains identical through new segment/restart/fallback and no arbitrary external path is adopted. |
| S3 Explicit receiving handoffs | S1, S2 | Assemble and render handoff context | go-implementation-engineer | `internal/agent/request.go` or new neutral context file, `internal/turn/prepare.go`, `approved_context.go`, `plan_attempt.go`, `validation.go`, `internal/harness/prompt.go`, relevant Codex request preparation. Reuse shared builder, deep-clone nested values (CONC-1), frame data (SEC-19/20), preserve stdin (SEC-2). |
| S3 | S1, S2 | Supply corrective validation findings | go-implementation-engineer | Read `internal/store/validation.go` retained assessments through a narrow attributed query. Filter by current parent/generation/story revision and relevant engineering snapshot; preserve diagnostic history without presenting stale findings as current blockers. Do not change acceptance state. |
| S3 | S1, S2 | Verify mode matrix and incomplete inputs | go-unit-test-engineer | Prompt tests cover Research→Plan, Plan→Implement, Implement→Validate, Validate→Implement, Spike and reverse paths, cold/resume/fallback parity, bounded comments, framing attacks, oversized optional artifacts and unchanged complete frozen bundles. |
| S3 | S1, S2 | Verify retained assessment and gate behavior | go-integration-test-engineer | Existing store/turn fixtures prove evidence attribution, missing/stale/failed outcomes and readiness gates. No fresh checks commissioned by Validate and no external forge beyond existing fake surfaces. |
| S4 Shared handoff instructions | S3 | Document shared contract and source role obligations | go-implementation-engineer | Update the exact shared paths below. Keep absence handling, input authority and artifact reuse aligned with S2/S3. Record known evidence limits and an operator observation recipe under `docs/`; no release publication. |
| S4 | S3 | Verify shared rendering contract | go-unit-test-engineer | Temporary/in-memory rendering in existing `internal/render`/`teamsource` tests verifies required handoff behavior without writing either installed tree. |
| S5 Claude installed instructions (Codex execution) | S4 | Apply contract to `.claude/**` only | go-implementation-engineer | Exact protected scope below; preserve harness-specific references and mode routing. Never run a two-tree generator. |
| S5 | S4 | Retain scoped installed contract checks | go-unit-test-engineer | Compare this tree to applicable shared content in memory/temp output; do not require `.codex/**` completion. |
| S6 Codex installed instructions (Claude execution) | S4 | Apply contract to `.codex/**` only | go-implementation-engineer | Exact protected scope below; unavailable Claude requires explicit manual opposite-harness handoff, not widened sandbox access. |
| S6 | S4 | Retain scoped installed contract checks | go-unit-test-engineer | Compare this tree only; sibling drift is recorded pending aggregate. |
| S7 Aggregate evidence and external observation | S1–S6 | Record aggregate parity/checks and observation inventory | go-implementation-engineer | Final `make check` plus scoped source/installed parity evidence; preserve artifact and source provenance. Supply operator procedure for authentic mode sequence and handoff observations for both adapters. Do not perform live nested runs. |
| S7 | S1–S6 | Verify accepted external evidence or retain missing status | go-unit-test-engineer | If separately authorized sanitized capture is supplied, checks validate provenance/identity sequence and documented context bounds. Without it AC-006 remains unverified; never manufacture a fixture as authentic. |

### Exact instruction ownership scope

Shared source/root S4: `teams/source/skills/workflow/SKILL.md`; `teams/source/skills/planning-contracts/SKILL.md`; `teams/source/agents/shared/research-orchestrator.md`; `teams/source/agents/shared/plan-orchestrator.md`; `teams/source/agents/shared/implement-orchestrator.md`; `teams/source/agents/go-team/go-code-reviewer.md`; root `AGENTS.md` and `CLAUDE.md` only where routing/session pointers require consistency; `.aiteam/APP_CONTEXT.md` and `.aiteam/APP_CONSTRAINTS.md` for the implemented contract; a feature-specific `docs/context-resets.md` documenting supported behavior and bounded external observation procedure. Broader specialist rewrites are unnecessary.

Codex-owned S5: `.claude/skills/shared/workflow.md`; `.claude/agents/research-orchestrator.md`; `.claude/agents/plan-orchestrator.md`; `.claude/agents/implement-orchestrator.md`; `.claude/agents/go-code-reviewer.md`; `.claude/modes/session.md`; `.claude/modes/spike.md`; `.claude/modes/validate.md`. The historical modes directory binds both adapters, but remains protected Claude-tree maintenance.

Claude-owned S6: `.codex/skills/workflow/SKILL.md`; `.codex/skills/research-orchestrator/SKILL.md`; `.codex/skills/plan-orchestrator/SKILL.md`; `.codex/skills/implement-orchestrator/SKILL.md`; `.codex/skills/go-code-reviewer/SKILL.md`.

## Platform and security notes

- Platform: layout/interfaces — reuse existing internal packages and consumer-owned seams (TEST-5); no new service/API or third-party dependency (TEST-2).
- Platform: database — all SQL stays in store, context-first, parameterized, single connection, embedded additive migrations; transaction promises require real SQLite tests (TEST-6).
- Platform: errors — distinguish optional omission from authoritative refusal with typed/domain results, wrap causal errors, never add panic/recover (SEC-11, CONC-3).
- Platform: concurrency — no new topology needed; copy nested shared contexts, preserve task lock and preparation ordering (CONC-1/7), no timer below daemon (CONC-6).
- Platform: logging — existing structured logger only; bounded sanitized outbound diagnostics (SEC-6). Never log submission descriptor secrets.
- Platform: tests — stdlib/table-driven existing fixtures, deterministic ordering and rendezvous rather than sleeps (TEST-1/3/7/8), race tests and `make check` retained for implementation. No tests executed in this architecture review.
- Platform: HTTP/new infrastructure — not applicable; existing forge authority remains factory-owned.
- Security: preserve tool/environment/sandbox boundaries and no provider-transcript reads; framing background does not grant authority. Rooted reads must reject unsafe components and detect replacement (SEC-20/21).
- Lifecycle: preserve managed admission, durable confirmed modes, active workflow/content versions, explicit owner control and fresh per-attempt Plan submission binding (CONC-9–14/21/22). No automatic phase chaining.

## Evidence and unresolved prerequisites

Carry the feature-spec inventory into story-local criterion IDs. S1–S3 prove offline repository behavior using executed retained checks; S4–S6 prove documented and rendered instructions only. S7 carries feature-specific authentic forward/reverse lifecycle and consumed-handoff observations with exact provider version, host/sandbox, code revision, mode sequence, native conversation identities and supplied artifacts. Those observations are currently unavailable. This is a known external prerequisite, not an architecture deviation or permission request; explicit owner exception can preserve unverified facts but cannot produce a pass.

No design decision needs an owner answer. Instruction maintenance has a required manual Claude handoff for S6. No implementation files were changed by this review.
