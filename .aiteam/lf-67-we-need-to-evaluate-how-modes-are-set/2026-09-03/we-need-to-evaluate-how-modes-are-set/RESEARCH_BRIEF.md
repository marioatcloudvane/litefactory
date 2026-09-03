# Research Brief — we-need-to-evaluate-how-modes-are-set

## Request

From GitHub issue #67 (opened by @marioatcloudvane): "Right now, Modes are set in
litefactory but i think that they are also infered from llms, as they are still
hardcoded in claude.md and agents.md. So we need a clear way to make the modes
strict and not infered. Once this is done, we need to ensure that with a new
mode, the sessions get reset (wg instead of `claude --resume` to start fresh!)."

Two claims to evaluate: (1) mode selection is LLM-inferred via hardcoded prose in
`CLAUDE.md`/agent files rather than being strict; (2) switching modes does not
reset the underlying Claude Code session, and should.

## Codebase Context

[V] litefactory is a Go module (`github.com/cloudvane/litefactory`, Go 1.26).
Mode logic spans four Go packages — `internal/agent`, `internal/daemon`,
`internal/turn`, `internal/harness` — with no single owning module. The
prompt-layer team definitions (`CLAUDE.md`, `.claude/agents/*.md`,
`.claude/modes/session.md`) sit alongside, and are mirrored for the Codex CLI
under `.codex/skills/`.

[V] No `.aiteam/rulebook.md` exists in this repository at present.

### 1. How mode classification actually works

[V] Mode resolution is **not** a single mechanism — it is a precedence chain of
four independent pathways, evaluated in this order by `daemon.run`
(`internal/daemon/tick.go:1387-1431`):

1. **Stored selection.** If this exact GitHub comment was already classified in
   a prior tick, the DB-backed `store.ModeSelection` is replayed verbatim — fully
   deterministic, no model call.
2. **GitHub label.** `resolveModeLabel(t.issue.Labels)`
   (`internal/daemon/mode.go:20-38`) looks for an existing `"Mode: X"` label on
   the issue. Deterministic string match; exactly one such label is required or
   the run refuses (`ReplyAmbiguousModeLabels` / `ReplyMalformedModeLabel`).
3. **Slash command.** `ParseCommand` (`internal/turn/command.go:185-222`)
   deterministically recognizes only a leading `/research`, `/plan`,
   `/implement`, `/spike` token in the comment body. **This is Go code, not a
   Claude Code custom-command file** — [V] no `.claude/commands/` directory
   exists in the repo, so the "slash command" tier of `CLAUDE.md`'s Classify
   table is implemented entirely outside the LLM, before any model sees the
   comment.
4. **LLM classifier.** If none of the above resolve a mode, `d.classifyMode`
   calls a stateless, tool-free (`--tools ""`) headless `claude -p` invocation
   against a **fixed Go prompt template**, `classificationPrompt()`
   (`internal/daemon/mode.go:66-96`) — *not* `CLAUDE.md`'s prose. The prompt
   explicitly frames the issue title/body/instruction as **untrusted data to
   classify, never commands that override the output contract**
   (`internal/daemon/mode.go:74-75`) — a deliberate prompt-injection guard. The
   output is strictly validated by `agent.ParseMode`/`ParseModeLabel`.

The resolved mode is then persisted (`GetOrCreateModeSelection`) and written
back to the issue as a `"Mode: X"` GitHub label before `turn.Execute` runs, so
**by the time any orchestrator agent (research/plan/implement-orchestrator)
is invoked, its mode has already been fixed by Go code** — [V] the three
orchestrator `.md` files contain no independent mode-inference logic of their
own; they assume the mode was decided upstream and only restate "One Run, One
Mode" as a prose instruction to themselves (e.g.
`plan-orchestrator.md:201-203`: *"You may not write, review, or discuss
code... If a sub-agent produces code, discard it and redirect."*).

[V] A **fifth, separate** pathway exists as a compatibility fallback:
`InferMode`, reached via `Request.mode()` (`internal/turn/execute.go:310-323`)
only when `Request.Mode` is invalid — which in production only happens through
the `len(d.classifiers) == 0` branch in `daemon.run`
(`internal/daemon/tick.go:1420-1430`), documented in-code as *"Compatibility
for dependency-light daemon tests that predate the classifier seam. Production
startup always supplies one."* This is the **only** pathway that actually reads
and applies `CLAUDE.md`'s own "Classify" prose — `harness.writeInferGuidance`
tells the live model to read the target repo's `CLAUDE.md` and self-report the
mode in its final message. In normal production operation (a classifier is
always supplied at startup) this path is not exercised.

**Where the user's "hardcoded in claude.md and agents.md" claim lands:**
partially confirmed, partially not. `CLAUDE.md`'s Classify table describes
three tiers (slash command / explicit natural language / inferred intent), and
that table genuinely is the operative logic only for the `InferMode`
compatibility path — which production does not normally take. For the three
pathways production does take (stored selection, label, slash command), mode
selection is deterministic Go code that never reads `CLAUDE.md` at all. The
LLM is invoked for mode selection only in the fourth pathway (the classifier),
and there it is bound to a fixed, separate Go-authored prompt
(`classificationPrompt`), not to `CLAUDE.md`'s prose.

### 2. Where the inference risk actually lives

[V] **Deterministic vs. fuzzy, concretely:**

| Pathway | Mechanism | Fuzzy or deterministic |
|---|---|---|
| Stored selection | DB row lookup, keyed to exact comment | Deterministic |
| GitHub label | Exact string match on `"Mode: X"` | Deterministic |
| Slash command | First-token parse in `internal/turn/command.go` | Deterministic |
| LLM classifier | Fixed prompt template, headless, tool-free, strictly validated output | Model judgment, but tightly boxed (untrusted-input framing, fixed template, `ParseMode` validation of output) |
| `InferMode` (compat-only) | Live model reads `CLAUDE.md` prose and self-reports mode in free text | Fuzzy — no Go-level parsing/validation of the self-report confirmed (see Open Questions) |

[V] `CLAUDE.md`'s documented three-tier Classify model does **not** match the
Go implementation. `ParseCommand` implements only the slash-command tier.
There is no Go code distinguishing "explicit natural language" ("I want to
plan X") from "inferred intent" — any non-slash comment is `KindFreeText` and
falls to the same LLM classifier as would handle a vaguer request. In the Go
layer, CLAUDE.md's tiers 2 and 3 are the same code path. This is a genuine
**drift between documentation and implementation**, not a security or
correctness bug per se, but it means the router's own account of how it
classifies is no longer accurate for the majority of real traffic.

[V] **The Gate table (Research/Plan/Implement artifact prerequisites) has no
Go-level enforcement found anywhere.** `internal/daemon/gate.go`'s
`cloneGate` was checked and is an unrelated git-concurrency mutex, not an
artifact-prerequisite check. As documented in `CLAUDE.md` ("Check by trying to
read the file. If it is missing, route to the earlier mode or ask the user"),
the gate is enforced purely by the router agent's own prose-driven behavior —
nothing in Go stops an orchestrator from being invoked, or from proceeding,
without the prerequisite artifact existing.

[V] **Mode-boundary enforcement beyond mode *selection* is almost entirely
instructional.** The single confirmed Go-level (non-prose) permission tied to
mode is `modeMayAsk(mode string) bool` (`internal/turn/ask.go:137-144`), which
restricts which modes may ask clarifying questions (`research`, `plan`,
`infer` only). Everything else a mode is or is not allowed to do — e.g. Plan
mode "may not write code," Research mode "does not write code, does not modify
CLAUDE.md" — is prose in the orchestrator `.md` files, with no tool-permission
setting or code-level restriction found backing it. This is the specific gap
that matches the user's stated worry: once a mode is chosen (which, per §1
above, mostly *is* strict), what that mode is and isn't allowed to *do* is
enforced only by the agent choosing to follow markdown instructions.

**Summary judgment for Plan mode:** the user's premise that mode *selection*
is "inferred from LLMs" is largely incorrect for the paths production actually
takes — three of four production pathways are deterministic Go code, and the
fourth (LLM classifier) is a boxed, single-purpose call, not open-ended
inference from `CLAUDE.md` prose. The more accurate framing of the risk is
two-fold: (a) `CLAUDE.md`'s documented classification model has drifted from
what the Go code does, which is a documentation-accuracy problem more than a
strictness problem; and (b) mode *behavior* (what an orchestrator may do once
in a mode, and the artifact-prerequisite gate between modes) has no code-level
enforcement at all, which is the part of the system that is genuinely
"markdown-only" today.

### 3. How a new session/run currently gets started

[V] The sole gate for resume-vs-cold-start is `resumeDecision(found bool,
segment store.Segment, resolvedKind agent.Kind) bool`
(`internal/turn/prepare.go:462-472`). It resumes the persistent
conversation/session (`store.Segment`) when: (1) a prior terminal run exists
for the segment, (2) `segment.Agent == resolvedKind` (the same coding-agent
tool — e.g. Claude Code vs. Codex — is being used), and, for non-Claude tools,
(3) a non-empty `segment.ResumeToken`. **Mode is not one of these three
conditions.** Switching mode alone (e.g. Research → Plan) does not force a
fresh session; the same persistent conversation is resumed via
`--resume`/`--session-id` across every mode of a session. A tool change (e.g.
Claude Code → Codex mid-session) triggers a typed refusal
(`agentSwitchRefusal`), not a silent reset.

[V] Worktrees are keyed one-per-issue (`naming.WorktreePath(repo.WorktreeRoot,
session.IssueNumber)`) and reused across all modes and all runs within that
issue/session — a worktree is never re-created per mode change.

[V] Session/mode-boundary instructions (e.g. "Plan mode may not write code")
are carried forward via `PromptInput.PreviousMode`/`PreviousMayAsk`, which is
re-emitted to the model only on the run where these values changed from the
prior run, and not repeated on every subsequent resumed turn — meaning
mode-boundary guidance lives only in conversation history text after the first
mention, not re-validated structurally each turn.

[V] **"wg" as named in the issue does not exist as a construct in this
repository.** The only match for the literal string "wg" in Go source is
`sync.WaitGroup` (`internal/daemon/pool.go`) — an unrelated Go concurrency
primitive, not a worktree/workgroup mechanism. No worktree-reset or
session-reset abbreviation of that name was found anywhere in the codebase.
This should be treated as **unconfirmed / not present** rather than guessed
at; if the user has a specific external tool or command in mind by "wg," that
was not identified in this repository and needs clarification from the issue
owner in Plan mode.

**Summary judgment for Plan mode:** the user's second claim — that sessions
should reset on mode change but currently don't — is **confirmed as accurate**.
Today, a single persistent Claude Code (or Codex) conversation legitimately
spans multiple modes within one issue/session; `--resume`/`--session-id` is
used to continue that conversation, and mode is explicitly excluded from the
resume/reset decision. This is presumably intentional today (it lets a
Research→Plan→Implement session share context cheaply), but it is also exactly
the behavior the issue asks to change.

## Domain / Problem Space

This is an internal tooling/process question about litefactory's own
orchestration architecture, not a user-facing product feature. There is no
external domain, library, or specification dependency — the entire question is
answered from the repository itself. No web research was needed or performed.

## Constraints & Risks

- [V] **Documentation drift risk.** `CLAUDE.md`'s three-tier Classify model no
  longer describes the actual Go implementation for the classifier pathway
  (tiers 2 and 3 collapse to one code path). Any change here should either
  update the Go implementation to match the documented tiers, or update
  `CLAUDE.md` to match the code — leaving them mismatched compounds the
  original complaint.
- [V] **No code-level Gate enforcement.** Introducing "strict, not inferred"
  mode switching should probably also close the Gate-enforcement gap (§2) at
  the same time, since it is adjacent and currently rests on the same kind of
  prose-only trust the issue objects to.
- [V] **No code-level mode-behavior enforcement.** Beyond `modeMayAsk`, nothing
  stops an orchestrator from acting outside its assigned mode except its own
  adherence to prose instructions. Making mode *selection* fully deterministic
  does not, by itself, make mode *behavior* enforced — these are separate
  problems that the issue's phrasing ("make the modes strict") could be read
  as covering either or both of.
- [V] **Session-sharing is a deliberate existing design, not an oversight.**
  `resumeDecision`'s doc comments and the `Segment`/worktree-reuse model
  suggest deliberate design intent to keep one conversation across modes for
  context continuity and worktree/git efficiency. Forcing a reset on every
  mode change is a behavior change with tradeoffs (loses shared context
  cheaply carried across modes, e.g. Research's findings informing Plan
  without re-reading files) that Plan mode should weigh explicitly, not treat
  as a pure bug fix.
- [O] The user's literal ask — "wg instead of `claude --resume`" — could not be
  matched to anything in the repository (§3). This is carried forward as an
  open question rather than guessed at.

## Open Questions

1. **What does "wg" refer to?** Not found anywhere in the codebase (Go source,
   scripts, docs). Could be a `git worktree` shorthand the user has in mind
   informally (i.e., "start a new worktree" rather than a literal tool named
   `wg`), or a tool/script external to this repo. Plan mode (or the issue
   owner) needs to clarify before designing the session-reset mechanism.
2. **In the `InferMode` compatibility path, is the model's self-reported mode
   ever parsed/validated/persisted downstream**, or does it exist only as free
   text with no code consequence once emitted? `internal/daemon/tick.go` and
   `internal/turn/execute.go` show `InferMode` only as an input to prompt
   construction in the traced flow; not confirmed as an output consumer.
3. **Is `CLAUDE.md`'s tier-2/tier-3 distinction ("explicit natural language"
   vs. "inferred intent") intentionally simplified in the Go layer, or is this
   an unintended drift** that should be reconciled as part of "making modes
   strict"? Not addressed by any comment or doc found in this run.
4. **Is the missing Gate enforcement (artifact-prerequisite check between
   modes) in scope for this issue**, or should it remain prose-only and only
   mode *selection* be tightened? The issue's title ("evaluate how Modes are
   set... improve the switching") arguably covers both, but this needs an
   explicit scope decision in Plan mode.
5. **Is forced session reset on every mode change actually the desired
   behavior**, given that the current shared-session design appears deliberate
   (cheaper context continuity across modes)? Plan mode should confirm intent
   with the issue owner rather than assume "always reset" is unconditionally
   correct — e.g. should Research→Plan share context but Plan→Implement force
   a reset, or should every mode transition reset uniformly?
6. `store.ModeSelection`'s full struct definition
   (`internal/store/modes.go`) and its cleanup/TTL guarantees beyond
   `PendingModeCleanup` were not directly read this run — low risk, but worth
   a targeted read if Plan mode's architect needs the exact persisted-state
   contract.

## Recommended scope for Plan mode

Plan mode should design changes across two related but separable areas, and
should explicitly decide (with the issue owner, given open question 4) whether
both are in scope for this issue or only the first:

1. **Mode-selection strictness / documentation reconciliation** — reconcile
   `CLAUDE.md`'s Classify table with the actual Go precedence chain (stored →
   label → slash command → LLM classifier → `InferMode` compat fallback), and
   decide whether the `InferMode` compatibility path should be tightened,
   removed, or left as a documented fallback for non-production/test contexts.
2. **Session lifecycle on mode change** — design what "reset" should mean
   concretely (new worktree? new persistent `store.Segment`? both?), where in
   `resumeDecision`/`daemon.run`/`turn.prepare` a mode-change condition would
   need to be added, and what tradeoff is acceptable regarding loss of shared
   context between modes (open question 5).

Out of scope for Plan mode unless the issue owner confirms otherwise: broader
mode-*behavior* enforcement (Gate checks, orchestrator tool-permission
restrictions) beyond what naturally falls out of making selection and session
lifecycle strict — flagged in Constraints & Risks as adjacent but not
explicitly requested by the issue text.
