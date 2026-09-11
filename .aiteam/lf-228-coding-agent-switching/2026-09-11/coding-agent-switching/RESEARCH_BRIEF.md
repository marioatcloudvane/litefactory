# Research Brief — coding-agent-switching

## Request

GitHub issue #228, titled "Coding Agent switching." The issue body is empty;
the only comment on the issue reads "Check this one!" No further description,
acceptance criteria, or context was supplied anywhere in the issue thread. This
run's job was to determine, from the codebase itself, what "coding agent
switching" most plausibly refers to, and to hand Plan mode a scoped starting
point rather than a blank page. `[O]`

## Codebase Context

All findings below are `[V]`, reported by `codebase-tour` this run, unless
marked otherwise.

### litefactory already has a "coding agent" concept, and it already has "switching"

litefactory is a daemon that watches GitHub issues and runs a **coding-agent
tool** — Claude Code or Codex — in a git worktree per issue.
`internal/agent/kind.go` defines `agent.Kind`, described in its own doc comment
as identifying "which coding-agent-tool implementation a `Runner` drives," and
`agent.Runner` is the interface both `internal/harness` (Claude) and
`internal/codex` (Codex) implement. `internal/turn.RunnerSet` is a
`map[agent.Kind]Runner`, built once at daemon startup by
`internal/daemon/runners.go:buildRunners` from `internal/config.Agents`
(`Claude *Claude`, `Codex *Codex`, both optional, at least one required —
`internal/config/config.go:172-257`).

This is the literal referent of "coding agent" in this repository's own
vocabulary: not an AI orchestration sub-agent (`.claude/agents/*.md`), but the
underlying CLI tool — Claude Code vs. Codex — that actually executes a run.

### Today's selection mechanism: a GitHub issue label, resolved per tick

There is no CLI flag and no per-task UI selector. The only way to choose which
coding-agent tool handles an issue is a label on the issue itself:
`Coding Agent: Claude` or `Coding Agent: Codex` (`agentLabelPrefix`,
`internal/daemon/agent.go:11-21`). `Daemon.resolveAgent(labels)`
(`internal/daemon/agent.go:96-144`) re-parses this label every tick (never
cached) and produces one of several outcomes:

- no label → default agent, unchanged behavior
- more than one `Coding Agent:` label → `ReplyAmbiguousAgentLabels`
- wrong-case prefix or malformed value → `ReplyMalformedAgentLabel`
- well-formed label naming an agent with no registered `Runner` →
  `ReplyUnknownAgent`
- well-formed, registered → resolved `agent.Kind`, run proceeds

### Switching mid-conversation is implemented, and implemented as a refusal

If a conversation/segment already exists under one coding-agent tool, and a
follow-up request (via label change) asks to continue it under a different
one, litefactory does not attempt the switch — it refuses. This is a real,
already-shipped code path: `ReplyAgentSwitchRefused`
(`internal/report/reply.go:51-58, 357-364, 796-803`), documented in-repo as
*"A conversation stays with the tool it started with — litefactory does not
hand one tool's session to another."* A segment's `Agent` field
(`AgentClaude`/`AgentCodex`, `internal/store/runs.go:44-57`) is fixed
permanently at creation; there is no schema path today for re-assigning it.

This means "coding agent switching," read literally against this repo's own
terms, is not a green-field feature request — it is an area with substantial
existing, tested, deliberately-designed behavior (a label-driven initial
selection mechanism, plus an explicit, named refusal of mid-conversation
switching).

### The registry is built to add a third coding-agent tool, not to change how switching works

`internal/turn` and `internal/report` never branch on `agent.Kind` with a
`switch` statement — the `Runner` registry is open by design, and doc
comments in `internal/agent/doc.go:34` and elsewhere cite acceptance criteria
(`AC-5.6`, `AC-5.15`) explicitly framing this as room for a third
implementation alongside Claude and Codex. If issue #228 is about *adding*
switching capability rather than *revisiting* the existing refusal, this
registry is very likely where that would land.

### A second, unrelated "pluggable agent" axis exists in the same codebase — do not conflate it

`internal/render` is a separate system: a `Renderer` per "harness"
(`claude`, `codex`, `opencode` — `internal/render/{claude,codex,opencode,
registry}.go`) that converts one harness-neutral agent-definition markdown
file into each harness's native shape. This is about **authoring** agent
definitions portably across tools, not about runtime task routing. Given the
issue's total lack of detail, Plan mode should explicitly confirm which axis
(runtime coding-agent selection, vs. definition-rendering harness support, vs.
both) is actually in scope before work starts.

### Meta-layer note, not part of the runtime feature

`.claude/AGENT_ROSTER.md:23-25` states "Codex maintains `.claude/**`, Claude
maintains `.codex/**`" — i.e., the way litefactory's *own* development
alternates between Claude Code and Codex CLI sessions also touches the words
"Claude" and "Codex." No `.codex/` directory currently exists in this
worktree. This is almost certainly unrelated to issue #228 (which reads as a
product feature, not a contributor-workflow note) but is flagged because the
terminology overlap is real and could cause scope confusion.

### FleetView — not found

The task briefing for this run mentioned a "FleetView" UI described in one
sub-agent's own definition text ("FleetView's default when no agent name is
typed"). `codebase-tour` searched the entire repository, case-insensitive,
across code, docs, and `.claude/` — zero matches. No such component exists in
this codebase today. This detail does not originate from the repo and should
not be treated as evidence of what issue #228 means. `[V]`

## Domain / Problem Space

The plausible problem space, ranked by how directly the evidence in this repo
supports it:

1. **Most likely, given the repo's own vocabulary**: something about how
   litefactory picks which coding-agent tool (Claude vs. Codex, possibly a
   third in future) runs a given issue, and/or whether/how a conversation can
   move from one tool to another once started. This is the only interpretation
   with concrete, already-built code behind it.
2. **Possible**: extending `internal/render`'s harness support (adding a new
   rendering target, or improving how the three existing harnesses —
   claude/codex/opencode — are selected/generated).
3. **Possible but speculative**: a user-facing selector (CLI flag or similar)
   for choosing the coding-agent tool at submission time, since today the
   *only* selection surface is a GitHub label.
4. **Unsupported by any evidence found**: anything resembling a "FleetView" UI
   — this term does not appear anywhere in the repository.

Because the issue body is empty and the comment carries no content beyond
"Check this one!", none of these can be confirmed as the intended scope from
the issue thread alone. `[O]`

## Constraints & Risks

- Switching a segment's coding-agent tool mid-conversation is not just
  unimplemented but **actively refused by name** (`ReplyAgentSwitchRefused`).
  Any work that reopens this needs to address why the refusal exists (session
  state, conversation history format, and tool-specific state are presumably
  not portable between Claude and Codex) before deciding whether to relax it.
  `internal/store` fixes `Segment.Agent` at creation — allowing a genuine swap
  would need a schema and design change, not just a code path change. `[V]`
- Codex's config carries measured, real sandbox behavior differences from
  Claude (`internal/config/config.go` docs: network access breaks
  `httptest`, `--ignore-user-config` also silences `.codex/skills`) — any
  change touching Codex-side agent selection should treat these as load-bearing
  constraints, not incidental config. `[V]`
- The two "pluggable agent" systems in this repo (`internal/agent`/`daemon`
  runtime routing vs. `internal/render` harness rendering) are easy to
  conflate under the single phrase "coding agent switching." Scoping the wrong
  one wastes a full Plan/Implement cycle. `[V]`

## Open Questions

1. **What does issue #228 actually want?** Blocks: all downstream scoping.
   Settles with: the issue owner — the issue body is empty and the comment has
   no content ("Check this one!"). What it would take: a reply on the issue
   naming one of the candidate interpretations above (or a different one this
   run did not surface). `[O]`
2. **Is this a new feature request, or a follow-up/regression against
   already-built agent-switching behavior?** `codebase-tour` observed, without
   reading it (outside this session's scope), that a prior `.aiteam` session
   directory named `lf-120-agent-switch-refused-leaves-the-session`
   (dated 2026-09-06) exists in this repository's history. That name strongly
   suggests "agent switching" — specifically the refusal path — was already
   the subject of substantial prior work. Blocks: whether Plan mode should
   treat this as new scope or as a continuation/regression of session 120's
   work. Settles with: the issue owner, or a deliberate decision by Plan mode
   to consult that prior session's artifacts (which this Research run did not
   do, per this session's boundary — session artifacts are not read across
   session directories). What it would take: owner confirmation, or an
   explicit Plan-mode decision to open that other session. `[R0, new this
   run]`
3. **Which of the two pluggable-agent axes is in scope** — runtime
   Claude/Codex selection and switching (`internal/agent`, `internal/daemon`,
   `internal/store`), or agent-definition rendering across harnesses
   (`internal/render`) — or both? Blocks: which package area Plan mode scopes
   work into. Settles with: the issue owner. What it would take: a one-line
   clarification on the issue. `[O]`
4. **If runtime switching is in scope, should litefactory actually support
   mid-conversation tool switching, or should work instead target the
   selection/labeling UX (e.g., a CLI flag, clearer error messaging on
   `ReplyAmbiguousAgentLabels`/`ReplyMalformedAgentLabel`/`ReplyUnknownAgent`)
   rather than reversing the deliberate refusal?** Blocks: whether Plan mode
   should treat the existing refusal as a design decision to preserve or a
   constraint to relax. Settles with: the issue owner, informed by whatever
   reasoning produced the original refusal (likely session 120, per question
   2). `[R0, new this run]`
5. **Does `.codex/` need to exist in this worktree** (per `AGENT_ROSTER.md`'s
   description of a Claude/Codex parity convention for the meta-layer), or is
   its absence expected/unrelated to issue #228? Low priority — flagged only
   because the terminology overlaps with the main topic. Settles with:
   whoever maintains the `.claude`/`.codex` parity convention, likely not
   blocking for this issue. `[V]`

## Recommended scope for Plan mode

- **Do not start Plan mode against this brief without an owner reply.** The
  issue itself supplies no scope, and this research run's job was to surface
  what's plausible, not to pick for the owner. The single highest-leverage
  next step is getting the issue owner to confirm which of the candidate
  interpretations (see Domain / Problem Space) is intended — in particular,
  whether this is about the existing Claude/Codex runtime-selection-and-
  refusal system, or something else.
- **If runtime coding-agent switching is confirmed as the scope**, Plan mode
  should scope narrowly against the already-existing subsystem: `agent.Kind`
  / `Runner` / `RunnerSet` (`internal/agent`, `internal/daemon/agent.go`,
  `internal/daemon/runners.go`), the label-parsing ladder and its `Reply*`
  outcomes (`internal/report/reply.go`), and the `Segment.Agent` field
  (`internal/store/runs.go`). Plan mode should explicitly decide, with the
  owner, whether "switching" means (a) improving/extending the existing
  label-based initial-selection UX, (b) adding a third coding-agent tool to
  the registry, or (c) relaxing the deliberate `ReplyAgentSwitchRefused`
  behavior to allow genuine mid-conversation handoff — these are three
  different, non-overlapping scopes of work.
- **Before scoping (c) specifically**, Plan mode (or a follow-up Research run)
  should look at the prior `.aiteam` session named
  `lf-120-agent-switch-refused-leaves-the-session` to understand why the
  refusal was built the way it was, rather than re-deriving that reasoning
  from scratch.
- **If the render/harness system (`internal/render`) is confirmed as the
  scope instead**, this brief's coverage of it is shallow (one research pass,
  not a focused one) — a follow-up Research run scoped specifically to
  `internal/render` would be warranted before Plan mode proceeds.
- **Out of scope regardless of which interpretation is chosen**: the
  `.claude/agents/*.md` sub-agent roster used to orchestrate litefactory's own
  development (codebase-tour, go-architect, etc.) — this is a different sense
  of "agent" than the one this repository's runtime code uses, and nothing
  found this run suggests issue #228 refers to it.
