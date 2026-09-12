# Feature Spec: Fresh coding agent per mode
# Status: APPROVED
# Date: 2026-09-12

Supersedes issue #227 and its seven-story plan. Nothing from that plan is
imported; this is an independent, from-scratch requirement.

## Goal

When the admitted mode changes, the next run starts a fresh coding-agent
conversation instead of continuing the previous mode's transcript, and it is
pointed at the task's existing artifact directory so nothing durable is lost.

## Problem

A task is answered by one conversation per segment. A Research run, a Plan run
and an Implement run on the same issue all resume that same conversation, so a
Plan run starts inside a transcript full of Research reasoning and a role that
has already been told it is doing something else. Repeated mode switches
accumulate: the context that mattered is buried, and the role instructions the
new mode carries compete with the old mode's instructions still in the history.

A repository that supplies an external team already gets this right by accident:
the instruction bundle's `bootstrap.md` names the selected mode, so a mode change
changes the bundle identity and `prepareDeliverySegment` rotates the segment. A
repository with no external team gets no reset at all — its segment is always
`store.FirstSegmentOrdinal`. The behavior therefore differs between the two
delivery configurations for no reason anyone chose.

Replies inside one mode must stay resumable. That is the whole value of the
conversation mechanism and it is not being traded away here.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an owner switching a task from one mode to the next, I want the run to start a fresh conversation so that the new role is not reasoning inside the previous role's transcript. |
| US-2 | As an owner replying inside the same mode, I want the run to continue the existing conversation so that follow-up replies stay cheap and keep their context. |
| US-3 | As an owner, I want a mode-change run to be told where the task's existing session artifacts are and which brief to read, so that the reset conversation recovers the written context it needs. |
| US-4 | As an operator, I want the reset to behave the same for Claude and Codex and for repositories with and without an external instruction bundle, so that behavior does not depend on configuration. |

## Scope of change

One story, three tasks, as instructed:

1. Consistent mode-change resets.
2. Existing-artifact pointer in the prompt.
3. Focused regression tests.

No protected-tree split applies. This change is entirely Go source under
`internal/`; it edits neither `.claude/**` nor `.codex/**`, so it needs no
Codex-owned or Claude-owned companion story.

## Data Model

No new schema, no migration, no new table or column. The behavior is derived
from rows that already exist.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `segments` (`internal/store/segments.go`) | `ordinal` | int | yes | existing; rotation writes `last.Ordinal + 1` |
| `segments` | `session_ref` | string | yes | existing; `naming.SessionRef(ref, ordinal)`, UUIDv5, recomputable |
| `segments` | `agent` | string | yes | existing; `claude` or `codex`; a change here stays an `agentSwitchRefusal` |
| `segments` | `resume_token` | string | no | existing; empty on a new segment, which is what keeps a Codex reset cold |
| `segments` | delivery identity/root | string | no | existing, via `store.SegmentDelivery` / `SetSegmentDelivery` |
| `runs` (`internal/store/runs.go`) | `mode` | string | yes | existing; written as `req.mode()` by `allocateRun` |
| `runs` | `segment_id` | int64 | yes | existing; ties a run to the conversation it used |
| `runs` | `status`, `semantic_outcome` | string | yes | existing; the terminal/non-refused filter `previousRun` already applies |

### Relationships

- `sessions` → `segments` via `session_id`, ordered by `ordinal`; the last row is the current conversation.
- `segments` → `runs` via `segment_id`; the last terminal, non-refused run of a segment is "the last run that actually used this conversation".
- `runs.mode` is the recorded admitted mode; `agent.Mode` labels plus `harness.InferMode` are its value space.

## API / Interface Changes

No CLI command, forge API, database schema or configuration key changes. Two
internal contracts move.

| Surface | Change | Notes |
|--------|--------|-------|
| `turn.prepareDeliverySegment` (`internal/turn/delivery.go`) | also rotates the segment when the admitted mode differs from the last run that used the candidate segment | the one place segment selection happens; already rotates on delivery-identity change |
| `harness.PromptInput` (`internal/harness/prompt.go`) | new optional session-artifact field(s) carrying the repository-relative session directory and the brief to read | empty means no section is written, the same discipline `Attachments` follows |

Unchanged by contract: `agent.Request`, `agent.SessionRef`, `agent.RunPath`,
`naming.SessionRef`, `store.GetOrCreateSegment`, both `agent.Runner`
implementations' cold/resume invocation.

## Business Rules

**Reset**

- BR-1 Compare the mode this run was admitted in (`req.mode()`, the same string `allocateRun` writes to `runs.mode`) with the mode of the last terminal, non-refused run recorded against the candidate segment — the evidence `turn.previousRun` already uses. Different means reset.
- BR-2 A reset is a new segment: `ordinal = last.Ordinal + 1`, `session_ref = naming.SessionRef(ref, ordinal)`, empty resume token. It is not a flag on the existing segment and no existing row is rewritten.
- BR-3 Equal modes, including `infer` following `infer`, reuse the current segment and resume exactly as today.
- BR-4 A candidate segment with no terminal, non-refused run has no previous mode and never resets: a first run and a re-prepared prelaunch attempt both stay on the segment they are on.
- BR-5 A mode change and a delivery-identity change occurring together produce exactly one new segment, not two.
- BR-6 A vendor change keeps priority: when `last.Agent` differs from the requested agent, `prepareDeliverySegment` still returns the stored segment unchanged so `prepare` raises `agentSwitchRefusal`. Mode is never compared on that path.
- BR-7 The rule applies identically with an instruction bundle (`deliveryPlan != nil`) and without one (`deliveryPlan == nil`), and identically for `claude` and `codex`. With a bundle, the mode-driven reset must not change bundle retention: a new segment gets its own written bundle, retained bytes of older bundles are never deleted or rewritten.
- BR-8 Automatic recovery is untouched. `req.Continuation != nil` reuses the recorded segment, resumes it and still refuses when the instruction bundle changed. A continuation carries its own recorded mode, so it is same-mode by construction.
- BR-9 A reset changes no other turn machinery: worktree, branch, base sync, attachments, comment history, approved/frozen story context, plan handoff and every workflow gate run exactly as they do for any other cold run.
- BR-10 After the reset, cold-vs-resume is still decided by the existing `resumeDecision`; a fresh segment has no terminal run and no resume token, so both agents take `agent.PathCold` without any new branch in either runner.

**Artifact pointer**

- BR-11 When a session artifact directory for this task can be located, the prompt names it — repository-relative, forward slashes — and tells the run to read the brief relevant to the current mode before starting.
- BR-12 The directory is located with the existing locator (`turn.findSessionDir`), which tries the run's own day and then reads `.aiteam/<branch>/` one level, newest date first. No new scan, no walk.
- BR-13 The markers considered are the ones the tree already names: `MANIFEST.md` (scaffolded in every non-Validate session per `docs/workflow/session.md`), `planbrief.FileName` (`plan_brief.md`) and `naming.ResearchBriefName` (`RESEARCH_BRIEF.md`). First marker that resolves a directory wins.
- BR-14 The brief named to the run follows the mode: `research` → `RESEARCH_BRIEF.md`; `plan` → `RESEARCH_BRIEF.md` when present, otherwise `plan_brief.md`; `implement` and `validate` → `plan_brief.md`; `spike` and `infer` → the directory alone, with no brief named. Only files actually present are named.
- BR-15 The section is written on the cold path only. A resumed conversation has already been told, and a resumed prompt stays the delta it is today.
- BR-16 With nothing located, no section, no heading and no trailing newline are written: the prompt is byte-identical to today's.
- BR-17 The section is written by `harness.BuildPrompt`, which both runners already go through, so it reaches Claude and Codex and both delivery configurations without a second code path.
- BR-18 The pointer is a location, not authority: it names files for the run to read, and does not grant the run permission it does not otherwise have.

## Edge Cases & Error Handling

| Case | Expected behavior |
|------|-------------------|
| First run of a task | No previous run on the segment; no reset; no artifact section (nothing exists yet). |
| Reply in the same mode | Same segment, resume, prompt unchanged from today. |
| `research` → `plan` → `implement` | A new segment per change; ordinals 1, 2, 3; each `session_ref` distinct and recomputable. |
| Mode changes back to a mode used earlier | A new segment again. Old conversations are never revived; the reset is forward-only. |
| Mode change on a repo with an external team | One new segment, a bundle written for it, its identity recorded; older retained bundles untouched. |
| Mode change plus vendor change | `agentSwitchRefusal`, exactly as today; no segment created, no run allocated. |
| Continuation / automatic recovery | Recorded segment reused and resumed; a changed bundle still refuses with "resume manually". |
| Prelaunch row re-prepared after a failure | The prelaunch run is not terminal, so it is not the previous run; the second attempt selects the same segment and `allocateRun`'s prelaunch match still holds. |
| Previous run refused or still running | Not evidence of a conversation; no mode comparison against it. |
| Claude reset | Cold with `--session-id <new session_ref>`; the previous UUID is untouched on disk. |
| Codex reset | New segment has an empty resume token, so no `resume <id>` argument; a fresh thread. |
| Resume refused by the tool | Unchanged `PathColdAfterResumeRefused` rerun; this feature adds no new fallback. |
| Session directory cannot be located (unmanaged branch, artifacts not yet written, non-derivable slug) | No artifact section; run proceeds. Never an error, never a refusal. |
| Session directory located but holds no known brief | The directory is named; no brief is named. |
| Marker is a symlink or a non-regular file | Rejected by the locator's existing component checks; treated as not found. |
| `PreviousMode` handling in the resume header | Left in place. A resumed run now always has an equal previous mode, so the existing mode-change re-emission at `writeResumeHeader` becomes unreachable; it is not deleted in this story. The ask-permission change note still fires, because permission can change within one mode. |

## Out of Scope

- New database schema, migration, table or column.
- An artifact-binding subsystem: no recorded association between a segment and the artifacts it read, no verification of artifact contents.
- A generalized handoff or context-transfer system between modes.
- Instruction-tree rollout: no change to `.claude/**`, `.codex/**`, `teams/source/**` or root project instructions.
- A separate live-agent evidence project, sandbox permission changes, or making a live agent capture a CI requirement.
- Carrying unwritten conversation context across the reset. Context that was never written to an artifact or a comment is lost at a mode change; this limitation is accepted and is not mitigated here.
- Changing which mode is admitted, how it is admitted, or the mode label/journal machinery in `internal/daemon/mode.go` and `internal/store/modes.go`.
- Changing bundle construction, `planDelivery`'s identity inputs, or removing the incidental mode component of `bootstrap.md`.
- Removing the now-unreachable `PreviousMode` branch in `writeResumeHeader`.
- Reviving or reading an earlier segment's transcript.

## Acceptance Criteria

- [ ] AC-1 A run admitted in a mode different from the last terminal, non-refused run of the current segment is prepared against a newly created segment with the next ordinal, a distinct `session_ref` and an empty resume token.
- [ ] AC-2 A run admitted in the same mode as that last run is prepared against the existing segment and takes `agent.PathResume` whenever it does today.
- [ ] AC-3 AC-1 holds with no instruction bundle configured (`deliveryPlan == nil`) — the case that has no reset today.
- [ ] AC-4 AC-1 holds with an instruction bundle configured, producing exactly one new segment when mode and bundle identity change together, and leaving previously retained bundle bytes unmodified.
- [ ] AC-5 AC-1 and AC-2 hold for `claude` and for `codex`, with the resulting run taking `agent.PathCold` on reset and `agent.PathResume` on same-mode continuation for both.
- [ ] AC-6 A requested agent differing from the segment's agent still produces `agentSwitchRefusal` with no segment created and no run allocated, regardless of whether the mode also changed.
- [ ] AC-7 A segment with no terminal, non-refused run is never reset, including the first run of a task and a re-prepared prelaunch allocation.
- [ ] AC-8 A continuation run reuses its recorded segment, resumes it, and still refuses with the existing message when the instruction bundle identity changed.
- [ ] AC-9 A reset run's prompt names the located session artifact directory, repository-relative with forward slashes, and instructs the run to read the mode-relevant brief named in BR-14.
- [ ] AC-10 When no session directory can be located, `harness.BuildPrompt` returns bytes identical to those it returns today for the same input.
- [ ] AC-11 A resumed prompt is unchanged: no artifact section is written on the resume path.
- [ ] AC-12 The artifact section reaches both runners through `harness.BuildPrompt`, with no runner-specific construction of it.
- [ ] AC-13 The reset leaves worktree, branch, base-sync outcome, attachments, recorded comment history, approved/frozen story context and every existing workflow gate behaving as they do for any other cold run.
- [ ] AC-14 No migration is added and no `store` schema changes; `store.Migrate` output is unchanged.
- [ ] AC-15 Focused regression tests cover AC-1 through AC-13 offline, with no network and no coding-agent subprocess, and `make check` passes.

## Evidence obligations

Read against `docs/decisions/nested-agent-evidence.md` before criteria freeze.

| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-1, AC-2, AC-3, AC-4, AC-6, AC-7, AC-8 | Offline Go assertions over `internal/turn` and `internal/store` with the existing in-repo SQLite fixtures; segment rows, ordinals, session refs and `agent.RunPath` read back directly | Available in repository (`internal/turn/prepare_test.go`, `resume_test.go`, `internal/store/store_test.go` patterns) | Go 1.26, offline, no subprocess | None |
| AC-5 (routing half) | Offline assertions that each agent kind produces the expected `agent.RunPath` and argv shape via the existing runner fixtures | Available in repository (`internal/harness/runner_test.go`, `internal/codex/runner_test.go`) | Established as argv/flag construction only | None for the routing claim |
| AC-5 (live behavior half): that real `claude` and real Codex actually begin a new conversation on the reset and continue the old one on same-mode replies | Would require live nested observation of both agents inside a managed run | **Unavailable.** Unsupported under the current sandbox per the nested-agent evidence decision | Claude and Codex versions unknown; no authorized external capture supplied | **Unsatisfied obligation.** Proof is limited to litefactory's own invocation inputs. Do not claim end-to-end conversation-lifecycle verification. An externally captured, sanitized, provenance-reviewed fixture would prove only its documented historical observation. |
| AC-9, AC-10, AC-11, AC-12 | Offline byte assertions over `harness.BuildPrompt`, including the empty-input byte-identity case | Available in repository (`internal/harness/prompt_test.go`) | Prompt construction only | None |
| AC-9 (comprehension half): that a reset agent in fact opens and uses the named brief | Would require live nested agent observation | **Unavailable** under the current sandbox | Unknown agent versions | **Unsatisfied obligation.** The criterion is met by the prompt naming the directory and brief, not by observed agent behavior. Retained, not weakened. |
| AC-13 | Offline assertions that a reset turn's prepared values (sync outcome, attachments, history, approved context) match those of an equivalent cold turn | Available in repository | Offline, synthetic git fixtures via `internal/gittest` | None |
| AC-14 | Offline assertion over `store.Migrate` and the existing migration inventory test | Available in repository (`internal/store/migrate_test.go`) | None | None |
| AC-15 | `make check` output (fmt, vet, golangci-lint v2, race tests) | Available; runs offline | Pinned linter version | None |

Synthetic fixtures here establish source behavior only. No criterion above may be
marked proven by a successful synthetic run standing in for the two unavailable
live observations; both stay recorded as unsatisfied obligations rather than
dropped scope.

## Assumptions

| # | Assumption | Based on |
|---|---|---|
| A-1 | "The last run that actually used the current conversation" is `turn.previousRun`: the last run on the segment with a terminal status and a semantic outcome other than `refused`. | Its own doc comment in `internal/turn/prepare.go` states exactly that, and it is already the input to `resumeDecision`. |
| A-2 | "Both delivery configurations" means with and without an external instruction bundle — `prepareDeliverySegment`'s `deliveryPlan != nil` and `== nil` paths — not comment delivery (`report_delivery`). | `prepareDeliverySegment` is the only segment-selection site and is the only place today where a configuration difference produces a different reset behavior. |
| A-3 | The admitted mode to compare is `req.mode()`, the string `allocateRun` records in `runs.mode`, so `infer` participates as an ordinary value. | `internal/turn/execute.go:438` and `allocateRun`; `runs.mode` is the only recorded run mode. |
| A-4 | The reset rotates the segment ordinal rather than adding a new reset mechanism. | `prepareDeliverySegment` already rotates on bundle identity change; `naming.SessionRef` was designed for ordinals above 1 with no migration. |
| A-5 | The artifact pointer is written on the cold path only. | Resumed prompts are deliberately the delta (`harness` prompt tests); every mode-change reset is cold, so cold-only covers the case that motivates the pointer. |
| A-6 | The mode→brief mapping in BR-14 is the "relevant brief". | `docs/workflow/session.md` states what each mode reads and writes; `naming.ResearchBriefName` and `planbrief.FileName` are the only brief names with Go constants. |
| A-7 | `MANIFEST.md` is an acceptable directory marker even though it has no Go constant today; a constant may be added beside the existing ones. | `docs/workflow/session.md` requires it in every non-Validate session, making it the mode-independent marker. |
| A-8 | The existing incidental reset for bundle-configured repositories stays as it is; this story adds the explicit rule alongside it rather than removing the mode component from `bootstrap.md`. | Removing it would change bundle identity for every existing task and is outside the stated scope. |
| A-9 | The now-unreachable `PreviousMode` branch in `writeResumeHeader` is left in place. | Deleting it is tidy-up beyond the three named tasks. |
