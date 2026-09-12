# Research Brief: Fresh coding agent per mode

- **Feature:** fresh-coding-agent-per-mode
- **Branch:** lf/253-fresh-coding-agent-per-mode
- **Issue:** #253 ("Fresh coding agent per mode"), supersedes #227
- **Date:** 2026-09-12
- **Citation base commit:** `576208a2e541f9f2b44ce6a812ecddf2dd87c957`
- **Triggering instruction:** owner comment "Do it" (minimal/non-specific)

## Context for this run

This session already has a completed Plan run (`feature-spec.md`, `ARCH_REVIEW.md`,
`plan_brief.md`, one typed story with 3 tasks / 15 acceptance criteria submitted;
`submission receipt` last showed `PlanState: pending`, publication not yet
confirmed). That Plan proceeded with Research recorded as skipped. This run's
header selects Research mode explicitly, so this brief is written on Research's
own terms: it independently investigates the mechanism, re-derives the same
evidence at the current commit, and records where it converges with or adds to
the Plan artifacts already in the session. It does not reopen or re-litigate the
Plan's decisions.

## Request

Investigate how litefactory currently tracks the admitted run mode and the
conversation (segment) a run resumes, and how a coding-agent prompt could be told
where a task's existing session artifacts live — the two mechanisms the goal
("start a fresh conversation on a mode change; stay resumable within a mode;
point a reset run at existing artifacts") depends on.

## Current behavior (evidence)

**Mode is admitted once and recorded on every run.**
`internal/daemon/mode.go` admits the mode and hands it into
`turn.Request.Mode`. `Request.mode()` (`internal/turn/execute.go:438`) is the
single reader of that field, and it is the exact string `allocateRun` writes to
`runs.mode` (`internal/turn/prepare.go:1507`, `Mode: req.mode()` at line 1534,
and again at lines 352/493 when constructing prompt/report inputs). There is no
other recorded mode value; `infer` is an ordinary value in this space, not a
special case.

**Segment selection is one function, and it is where continuity lives.**
`prepareDeliverySegment` (`internal/turn/delivery.go:295-382`) is the only place
a conversation segment (`store.Segment`) is chosen or created for a run. Read
directly at the cited commit:
- A continuation (`req.Continuation != nil`, lines 296-317) returns the recorded
  segment unconditionally and refuses only when the instruction-bundle identity
  changed — mode is never consulted on this path.
- With no instruction bundle (`p == nil`, lines 318-327), a session with no prior
  delivery short-circuits straight to `GetOrCreateSegment` on
  `store.FirstSegmentOrdinal` and **never rotates thereafter for any reason** —
  this is the asymmetry the feature-spec calls "the bug": a repo with no
  external instruction bundle gets no reset at all today, while a bundle-carrying
  repo gets one by accident because a mode change also changes `bootstrap.md`
  (line 104: `"The daemon has selected mode " + req.mode() + ...`), and that
  changes the bundle's `identity` hash (line 153), which the general path below
  already rotates on.
- The general path (lines 328-358) lists existing segments, and when the last
  segment's stored `DeliveryIdentity` differs from the newly computed one, it
  rotates: `ordinal = last.Ordinal + 1`, a new `session_ref` via
  `naming.SessionRef(ref, ordinal)` (`internal/naming/session.go:62`, a
  recomputable UUIDv5), and (implicitly, since `GetOrCreateSegment` is called
  fresh) an empty resume token. A vendor mismatch (`last.Agent != req.Agent`,
  line 343) returns the unchanged segment so the caller raises
  `agentSwitchRefusal` — this check runs before any rotation logic and does not
  consult mode.
- **Mode is not compared anywhere in this function today.** The only reset
  trigger that exists is instruction-bundle identity change. This confirms the
  feature-spec's core claim: mode-driven reset is not implemented; a repo
  without a bundle never resets, and a repo with a bundle resets only as a side
  effect of the mode label reaching `bootstrap.md`.

**"The last run that actually used this conversation" already has a name and an
evidence source.** `previousRun` (`internal/turn/prepare.go:1024-1045`) walks
`ListRuns` for the session, ordinal order, and returns the last run whose
`SegmentID` matches the given segment, whose status is terminal
(`terminalRunStatus`), and whose `SemanticOutcome != "refused"`. Its own doc
comment (lines 990-1023) argues carefully why a running or refused row is not
evidence of a usable conversation, and why a sibling worker's rows cannot leak
in (session-scoped `ListRuns`, one worker per session, cross-process store
lock). `resumeDecision` (line 1064) then requires `found`, agent match and a
non-empty `ResumeToken` before returning `agent.PathResume`; any reset segment
naturally fails all three (no terminal run yet, empty token), so it lands on
`agent.PathCold` with no new branch needed in either runner. This is the
existing seam a mode-vs-previous-run comparison would plug into, and it is
already the input `resumeDecision` consumes for the unrelated cold/resume
question — i.e., mode-change detection and cold/resume selection are two
different comparisons over the same underlying data (`previousRun` against the
segment), not two separate pieces of state to invent.

**The prompt has exactly one construction point for both agents.**
`harness.BuildPrompt` (`internal/harness/prompt.go:378-510`) is the single
function both `internal/harness/runner.go` (Claude) and
`internal/codex/runner.go` (Codex) go through. It takes a `PromptInput` struct
(defined lines 183-371) with no per-agent branching in its body; `in.Resume`
(line 427) is the only fork, selecting `writeHeader` vs `writeResumeHeader`.
Optional sections already follow a fixed discipline: `writeAttachments`
(line 527) writes nothing when `strings.TrimSpace(in.Attachments) == ""` — no
heading, no blank line, no trailing newline (documented explicitly at
lines 512-526 as the byte-identity property a new field must preserve). Any new
"where your artifacts are" section would be a same-shaped optional field/writer
pair, populated by `internal/turn` (which owns the worktree and slug
derivation) and rendered by `internal/harness` (which owns prompt wording) —
`PromptInput.Attachments`'s own doc comment (lines 331-352) states this
boundary explicitly: harness renders finished strings, it does not know what an
attachment or, by the same reasoning, a session artifact "is."

**A rooted, non-scanning locator for the session directory already exists.**
`findSessionDir(worktreeDir, branch string, issue int, day time.Time, marker
string, log *slog.Logger)` (`internal/turn/sessiondir.go:38-50`) derives the
slug from the branch (`naming.SlugFromBranch`), tries the run's own day first
(`sessionDirOn`, line 54, via a capability-rooted `Lstat` + `IsRegular` check —
no bare `os.Stat`), and falls back to one bounded, one-level read of
`.aiteam/<branchDir>/` newest-date-first (`scanSessionDates`, line 81) if the
run's own day does not hold the named `marker`. It takes exactly one marker
per call and returns `(string, bool)` — never an error for "not found," which
matches the edge-case shape the feature needs (an unlocatable directory is a
value, not a failure). It already has two callers today,
`internal/turn/report.go:827` and `internal/turn/subissues.go:343`, both keyed
on a single marker (`plan_brief.md` for the sub-issue lookup per the comment at
`subissues.go:338`) — so a caller wanting "first of several markers, in order"
composes multiple calls to the same function rather than needing a new one.

**The brief names are already named constants, not string literals to
invent.** `naming.ResearchBriefName = "RESEARCH_BRIEF.md"`
(`internal/naming/aiteam.go:19`) and `planbrief.FileName = "plan_brief.md"`
(`internal/planbrief/index.go:11`) are the only two brief names with Go
constants today. `MANIFEST.md` is required in every non-Validate session by
`docs/workflow/session.md` ("scaffold MANIFEST.md before delegation ... Reuse an
existing manifest") but has no existing Go constant — confirmed by grep: no
`MANIFEST.md` occurrence in `internal/naming/*.go` today.

**Evidence-availability constraint on any live-behavior claim.**
`docs/decisions/nested-agent-evidence.md` (adopted decision, read in full)
establishes that a managed run cannot promise live nested-agent observation
under the current sandbox boundary (citing issue #183, nested Codex failing to
initialize its in-process app-server inside a managed run though the same
invocation succeeds in an ordinary shell). It requires any criterion resting on
"a real coding agent actually reset/continued its conversation" to be recorded
as an unsatisfied obligation rather than treated as proven by an offline test
suite, and forbids expanding sandbox permissions or making a live capture a
CI requirement to manufacture that proof. This bears directly on US-1/US-2's
strongest form ("the coding agent's own conversation state actually resets or
continues") and on US-3's comprehension half ("the agent actually reads the
named brief"): both are observable only as litefactory's own invocation inputs
(session id passed, resume token present/absent, prompt bytes), not as
verified agent-side behavior.

## What this confirms about the mechanism (no new decision required)

Independently reading the current source at this commit reproduces, without
contradiction, the mechanism the Plan artifacts in this session already
describe:

1. Mode-change reset has exactly one correct location: the predicate ladder
   inside `prepareDeliverySegment`, compared using `previousRun`'s result
   against the *candidate* segment (existing evidence, no new store column).
2. The two "delivery configurations" the goal asks to treat consistently are
   `prepareDeliverySegment`'s `p != nil` (bundle) and `p == nil` (no bundle)
   branches — the `p == nil && !hadDelivery` early return is the one place that
   never re-checks anything today, which is the asymmetry named in the spec's
   Problem section.
3. The artifact pointer has one natural construction: reuse `findSessionDir`
   (already rooted, already non-scanning, already has two callers depending on
   its exact behavior) tried against an ordered list of existing markers
   (`MANIFEST.md`, `planbrief.FileName`, `naming.ResearchBriefName`), and thread
   the result into `harness.BuildPrompt` as one or two new optional
   `PromptInput` string fields following the `Attachments` field's
   empty-means-absent discipline, on the cold path only (a resumed conversation
   has already been told).
4. Nothing about mode admission, the mode label/journal machinery
   (`internal/daemon/mode.go`, `internal/store/modes.go`), bundle construction,
   or `writeResumeHeader`'s now-relevant `PreviousMode` re-emission needs to
   change for any of the above — they are read-only inputs or unaffected
   neighbors.

This is the same mechanism `feature-spec.md`, `ARCH_REVIEW.md` and
`plan_brief.md` in this session already commit to (BR-1 through BR-18 and
D-1 through D-10). Research does not find a materially different design open
here, and does not reopen those decisions.

## Options considered

Only one design axis had more than one plausible reading, and it is already
closed by evidence rather than by preference:

- **Where to compare mode:** inside `prepareDeliverySegment` (one seam, shares
  the existing `previousRun` read) vs. a new check in `prepare` after segment
  selection. The former is correct: segment selection has to *decide* the
  rotation, not merely observe it after the fact, and `prepareDeliverySegment`
  is already the sole writer of new segment rows. A post-hoc check in `prepare`
  would need its own read-then-rotate path, duplicating `GetOrCreateSegment`
  and ordinal derivation that already exists here.
- **Where the brief-name policy lives:** `internal/turn` (owns branch/slug/
  filesystem) vs. `internal/harness` (owns prompt wording). `harness` already
  treats mode only as a header string with no mode-to-filename table; keeping
  the mapping in `turn` avoids adding that vocabulary to a package whose
  boundary discipline (`PromptInput.Attachments`'s doc comment) explicitly
  keeps such vocabularies out.

No option surfaced that would change what data is compared (mode vs.
delivery identity) or where continuity is recorded (segment rows); the
existing schema already carries both facts.

## Recommendation

Proceed with the mechanism already reflected in this session's Plan artifacts:
compare `req.mode()` against the mode of `previousRun`'s result for the
candidate segment inside `prepareDeliverySegment`, rotate on mode change OR
delivery-identity change (one rotation, not two), and add an optional
session-artifact pointer to `PromptInput` populated by a small `turn`-side
locator built on the existing `findSessionDir`, rendered only on the cold path.
No new schema, no new package boundary crossing, no change to mode admission.
This recommendation is not a new decision; it is Research independently
reaching the same conclusion the Plan run already froze into a submitted story.

## Risks / unresolved evidence obligations

These are carried forward, not newly discovered, and Research adds no way to
close them:

- **Live conversation-lifecycle behavior** (that a real Claude or Codex
  process actually starts a new conversation on reset and continues the old
  one on a same-mode reply) is unverifiable under the current sandbox per
  `docs/decisions/nested-agent-evidence.md`. Any acceptance evidence here is
  limited to litefactory's own invocation inputs (session id, resume token
  presence/absence, argv/stdin shape) — never an observed agent-side reset.
- **Agent comprehension of the artifact pointer** (that a reset run actually
  opens and uses the named brief) is unverifiable for the same reason. The
  criterion this mechanism can support is "the prompt correctly names the
  directory and brief," not "the agent read it."
- Both risks are already named as explicit unsatisfied obligations in
  `feature-spec.md`'s evidence table and `ARCH_REVIEW.md`'s carried-forward
  section; this brief does not weaken or attempt to resolve them.

## Open questions for the owner

None material. The instruction ("Do it") was minimal but not ambiguous about
scope: the issue body supplies the goal and this session's own Plan artifacts
already supply a fully reasoned, evidence-backed design for it, which
independent reading of the current source (same commit, same day) reproduces
without contradiction. No plausible alternative reading of "fresh coding agent
per mode" changed the recommendation above, so there is nothing to ask before
proceeding.

One process note worth the owner's attention, not a question: `plan_brief.md`'s
history records `PlanState: pending` with no story issue link yet confirmed as
of the last check in this session. That is a Plan-run publication concern, not
a Research finding, and is restated here only so it is not lost between runs.
