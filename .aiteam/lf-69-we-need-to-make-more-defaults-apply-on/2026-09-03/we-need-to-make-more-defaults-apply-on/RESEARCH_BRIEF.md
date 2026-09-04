# Research Brief — lf-69-we-need-to-make-more-defaults-apply-on

## Request

> Right now, we always expect issues to be correctly labeled. This is a bit odd when e.g. we
> only have one coding agent. To improve that, we should default to the standard agent when no
> label is present. The same applies to the mode — but here it is a bit more complicated. How can
> we solve this? Or is this already solved? Also, check where else we can ease the usage here!

Headless run; no clarification available. Ambiguity in "standard agent" (coding-agent tool vs.
sub-agent persona) is resolved below under Constraints & Risks, with the assumption stated
explicitly.

### Follow-up (2026-09-04) — author clarifications on the run-0001 open questions

The issue author (@marioatcloudvane) replied to the first research pass with direct answers to
four of the five open questions below, plus a new, more specific research question of their own.
Their comment, verbatim:

> some comments:
>
> Part 2: as I understand, claude.md and agents.md will always force the agent to infer the mode,
> independnt if there is a mode set by litefactory. is this true? how can we avoid it?
>
> Part 3: this is part of the design and ok that way!
>
> as of your open questions:
>
> 1: yes, coding agent tool
> 2: mode defaulting: if no mode is set, it should infer the mode from the llm
> 3: no, litefactory should not know that at the moment
> 4: unassigned is no problem; if no assignment is done it is fine to not react, this is what a
> human must resolve
> 5: skip this

This resolves Open Questions 1, 3, 4 and 5 from run 0001 outright (see **Open Questions**, marked
closed below), confirms Question 2's premise as intended behavior rather than a gap, and raises one
new, concrete question — whether `CLAUDE.md`/`AGENTS.md`'s mode-classification instructions
actually ignore a mode the daemon has already resolved. That question is investigated in full below
as **Part 4**.

## Codebase Context

litefactory is a standalone Go polling daemon (`internal/daemon/`), not a GitHub Action/webhook
handler. It polls issues/comments, classifies them, and dispatches a coding-agent tool (Claude,
Codex, …) in one of four modes (Research/Plan/Implement/Spike). Three independent label "ladders"
resolve untrusted forge input, all following the same philosophy — **refuse loudly on
ambiguity/malformation, never silently misinterpret**:

- **Agent** — label `"Coding Agent: <Name>"`, prefix at `internal/daemon/agent.go:21`, name shape
  `^[A-Z][A-Za-z0-9_-]{0,39}$` (`agent.go:27`), resolution ladder `resolveAgent`
  (`agent.go:98-146`). [V]
- **Mode** — label `"Mode: <Name>"`, prefix at `internal/daemon/mode.go:14`, resolution ladder
  `resolveModeLabel` (`mode.go:20-38`), values via `agent.ParseModeLabel`
  (`internal/agent/mode.go:47-55`). [V]
- **Tools profile** — label `"lf:tools=<profile>"`, prefix at `internal/daemon/tools.go:30`,
  profile shape `^[a-z0-9][a-z0-9_-]{0,39}$` (`tools.go:41`), resolution ladder `resolveTools`
  (`tools.go:133-212`). [V]

These are the **only three** label-driven inputs in the daemon package. No `lf:agent=` or
`lf:mode=` labels exist anywhere in code — the issue's own framing of the label names was
inaccurate; the real labels are `"Coding Agent: "`, `"Mode: "`, and `"lf:tools="`. [V]

A fourth, distinct mechanism — the `lf: ask` **comment tag** (not a label) — controls whether a
run may pause and ask a clarifying question. It is parsed by `HasAskTag`
(`internal/turn/ask.go:38-76`), stripped before the body reaches the agent by `stripAskTag`
(`ask.go:84-109`), and gated by mode: `modeMayAsk` (`ask.go:111-144`) allows Research/Plan/`infer`,
denies Implement/Spike. Permission is computed as `mayAsk := req.Command.Tagged &&
modeMayAsk(req.mode())` (`internal/turn/prepare.go:220`). [V]

The full per-tick decision path is `Daemon.run` (`internal/daemon/tick.go:1380` onward): mode
resolution (stored selection → label → slash command → LLM classification fallback,
`tick.go:1387-1440`) → agent-label resolution (stage 1, `tick.go:1457-1467`) → tools-label
resolution (`tick.go:1475-1490`) → profile-vs-agent compatibility check (stage 3,
`tick.go:1492-1505`) → `turn.Execute` (`tick.go:1507-1535`). [V]

## Domain / Problem Space

### Part 1 — Agent defaulting: already solved

When the `"Coding Agent: "` label is absent, `tick.go:1465-1466` falls back to
`d.defaultAgent`, a value fixed once at daemon construction:
`daemon.go:328-331` — `defaultAgent := opts.DefaultAgent; if defaultAgent == "" { defaultAgent =
agent.Kind(store.AgentClaude) }`, validated at startup (`daemon.go:332-339`) against the registered
agent/classifier set. [V]

This is backed by a real config knob: `internal/config/config.go:48-50`
(`DefaultAgent string `yaml:"default_agent"``), package-level default `"Claude"`
(`config.go:449`), written into every fresh `config.yaml` by `litefactory init`
(`internal/config/template.go:49-51`) with an explanatory comment: *"Coding-agent implementation
used when no Coding Agent label is present and for classifying requests with no Mode label."* The
effective default is echoed at daemon startup by `internal/cli/cmd_daemon.go:209`. [V]

**Conclusion: the literal ask — "default to the standard agent when no label is present" — is
already fully implemented, both at the label-resolution layer and the config layer.** What is
*not* implemented is anything smarter than a fixed string: `init` never detects which agent
tool(s) are actually installed/registered, and never adjusts `default_agent` accordingly (see
Constraints & Risks).

When the agent label **is** present but malformed, ambiguous (multiple conflicting labels), or
names an unregistered agent, the ladder refuses rather than defaulting (`agent.go:126-145`,
reply kinds `ReplyMalformedAgentLabel` / `ReplyAmbiguousAgentLabels` / `ReplyUnknownAgent`,
`internal/report/reply.go:25-165`). This refuse-on-malformed / default-on-absent split is
intentional and consistent across all three ladders (`agent.go:73-77` calls out that its ladder
"reuses `resolveTools`'s ladder byte for byte"). [V]

The tools-profile label follows the identical default-on-absent pattern: absence is a silent
no-op that leaves the harness's default tool surface untouched (`tools.go:104-108`, doc tag
AC-2.3). [V]

### Part 2 — Mode defaulting: partially solved, and the "complicated" part is real

**Resolved 2026-09-04:** the author confirmed the LLM-classification fallback described below *is*
the intended mode-defaulting behavior ("if no mode is set, it should infer the mode from the llm")
— this is not a gap to close, it is the design. What remained open was whether that inference step
runs *redundantly* against a mode the daemon had already resolved by other means; that question is
answered in **Part 4** below. The technical description of the LLM-fallback mechanism that follows
is retained because Part 4 depends on it.

There is **no static `default_mode` config knob** analogous to `default_agent`. When mode is
unresolved by stored selection, label, or slash command, `tick.go:1420-1421` calls
`d.classifyMode(ctx, t, command.Text)` — a **live LLM call**
(`internal/daemon/mode.go:87-96`) using `d.classifiers[d.defaultAgent]`. In other words: mode
"defaulting" today is not a default at all, it is a per-request inference call, and it reuses
`default_agent`'s classifier to do it — which is why the config comment at `config.go:48-50`
describes `default_agent` as serving *both* roles. [V]

Why a static default is harder for mode than for agent, confirmed in code:

- The four-mode gate (Research → Plan → Implement, plus standalone Spike) is a **prerequisite
  chain expressed only in prose**, inside `CLAUDE.md`'s router table ("Plan requires
  `RESEARCH_BRIEF.md`", "Implement requires `plan_brief.md`"). Grepping the Go source for
  `RESEARCH_BRIEF`/`plan_brief` outside `.aiteam/` returns **no hits** — the daemon's Go code has
  no notion of this gate at all. The gate is enforced entirely by the LLM router reading the
  session directory at prompt time, not by `internal/daemon`. [V]
- `internal/daemon/mode.go` and `tick.go`'s decision path never consult session-state files
  (no check for `RESEARCH_BRIEF.md` / `plan_brief.md` presence) before invoking `classifyMode`.
  There is no Go-side shortcut such as "if no brief exists yet, mode can only sensibly be
  Research" — every unresolved request pays one full LLM classification call regardless of what
  the session's own file state would already tell a human. [V]
- A conflicting mode selection (label + slash command disagree) is refused outright
  (`tick.go:1414-1416`, `ReplyConflictingModeSelection`) rather than either taking precedence —
  consistent with the "refuse loudly" philosophy, but it means no automatic conflict resolution
  exists either.

**Conclusion: mode defaulting is only half-solved.** A default *exists* in the sense that a
request always gets classified into some mode without requiring a label — but it is an LLM
inference cost on every unlabeled request, it is not configurable as a fixed fallback the way
agent is, and it has no session-state awareness that a Go-side gate check could cheaply provide.
This matches the requester's own read that mode "is a bit more complicated" than agent — the
complication is structural (mode depends on session state and a prose-only prerequisite chain;
agent does not), not merely a missing default value.

### Part 3 — Other usage-friction points found

1. **Non-owner / unassigned issues get zero feedback.** `runIssueBody`
   (`internal/daemon/tick.go:1076-1127`) silently no-ops (debug log only, no thread reply) when:
   - the issue is not assigned to the configured owner (`tick.go:1079-1080`) — a deliberate
     authorization boundary, documented at `tick.go:1034-1038` as "the whole reason the old ban
     could be lifted." This one is *correct* to stay silent on the merits (replying would let any
     GitHub user fingerprint the bot as authenticated/active), but it doubles as a **silent
     onboarding dead end**: a first-time user who opens an issue without self-assigning it gets
     absolutely nothing back — no hint that assignment is what activates litefactory. [V]
   - the body is empty (`tick.go:1081-1082`) — not a security boundary, currently silent for no
     stated reason.
   - the issue already has comments (`tick.go:1083-1084`) — deliberate, AC-2.16d: a body is only
     a *first* instruction; replaying it once comments exist "would be a second voice nobody
     asked for."
   - the parsed command spawns nothing, e.g. a bare `/plan` with nothing else
     (`tick.go:1109-1119`) — deliberate, AC-2.16c, to avoid "answering a question nobody asked."

2. **No detection of installed/registered coding-agent tooling at `init` time.**
   `internal/cli/cmd_init.go:72-152` (`runInit`) is fully non-interactive: it resolves the GitHub
   owner/bot identity and writes the config template, but never inspects which agent binaries
   (`claude`, `codex`, …) are actually present or which `agents.*` stanzas are uncommented in the
   template. `default_agent` is always seeded `"Claude"`
   (`internal/config/template.go:51`, filled from `config.Defaults()`,
   `template.go:278`/`config.go:449`) even for an operator whose only real agent is Codex
   (commented out by default at `template.go:245-258`). The failure this produces is deferred to
   the first real daemon run rather than caught at `init` time. [V]

3. **No user-facing documentation of the label conventions at all.** `README.md` is a two-line
   stub; no `docs/` directory exists. The three recognized label literals
   (`"Coding Agent: "`, `"Mode: "`, `"lf:tools="`) appear only in Go source/tests and in the
   `.claude`/`.codex` agent-skill files, which are the AI team's own operating instructions, not
   anything a human operator would read. `writeInitSummary` (`cmd_init.go:213-229`) prints paths
   and identities on `init` but never mentions labels or how to get started. [V]

4. **Mid-conversation agent/profile mismatches are refused, not resolved.**
   `ReplyAgentSwitchRefused` (`internal/report/reply.go:90-97`) fires when a comment asks to
   continue an existing thread under a different coding-agent tool than the one the conversation
   segment was created with (an agent is fixed per segment). `ReplyProfileNotAvailableForAgent`
   (`reply.go:99-105`) fires when a resolved `lf:tools=` profile has no stanza for the agent that
   would run (e.g., a profile defining only `claude:` used on a Codex-routed run) — refused rather
   than silently falling back to that agent's default tool surface. Both are correct-by-design
   (consistent with "refuse loudly, never silently misinterpret") but are additional places where
   a single-agent shop could hit friction if it ever adds a second agent later or authors a
   profile carelessly.

### Part 4 — `CLAUDE.md`'s Classify section re-infers mode unconditionally, even when the daemon already resolved one (new, 2026-09-04)

**The author's claim is confirmed true.** `CLAUDE.md`'s `## Classify` section (`CLAUDE.md:15-22`)
reads, verbatim:

```
## Classify
For every new request, in this order:
1. Slash command — /research, /plan, /implement, /spike.
2. Explicit natural language — the user named a mode.
3. Inferred intent — classify by what is being asked for...
```

"For every new request, in this order" is unconditional. Neither `CLAUDE.md` nor `AGENTS.md`
(the only other project-instructions file in the repo, root-level only — confirmed by search, no
other `AGENTS.md` exists) contains any branch that says "skip this if the harness already told
you the mode." `AGENTS.md:41-44` explicitly defers to `CLAUDE.md`'s Classify section for Codex
runs and adds nothing of its own on this point. [V]

**Why this happens, traced end to end:**

1. The daemon resolves mode entirely in Go, *before* any prompt is built: stored selection → issue
   label → slash command → LLM classifier fallback (`internal/daemon/tick.go:1387-1440`, already
   established in Part 2 above). [V]
2. The resolved mode is passed into the turn as `turn.Request.Mode`
   (`internal/daemon/tick.go:1507-1508`), carried through `Request.mode()`
   (`internal/turn/execute.go:310-323`) into `harness.PromptInput.Mode`
   (`internal/turn/prepare.go:226,228`). [V]
3. `harness.BuildPrompt`'s `writeHeader` renders it as one plain-text line — `fmt.Fprintf(b, "-
   Mode: %s\n", strings.TrimSpace(in.Mode))` (`internal/harness/prompt.go:423`) — positioned
   *before* the `## Instruction` section of the stdin prompt the coding-agent CLI receives. [V]
4. The slash-command token itself, if any (e.g. `/research`), is **stripped** before it reaches the
   prompt: `Command.Text = strings.TrimSpace(rest)` keeps only the text *after* the first token
   (`internal/turn/command.go:203-208`). So even on a run triggered by an explicit `/research`
   slash command, the agent's own copy of `CLAUDE.md`-Classify-step-1 ("slash command") has no
   command text left to match against — the only surviving evidence of the resolved mode is the
   `- Mode: X` header line. [V]
5. `claude` (or Codex) is invoked as a subprocess with the prompt on stdin, and reads `CLAUDE.md` /
   `AGENTS.md` **unconditionally**, because the CLI reads project-instructions files by working
   directory as an unconditional CLI convention — the process's cwd is the run's worktree
   (`internal/harness/doc.go:51-52`, documenting: "claude runs *inside the worktree*, so it reads
   that branch's CLAUDE.md... A branch is third-party content."). There is no
   `--system-prompt`-equivalent flag or other mechanism in the documented invocation contract
   (`doc.go:13-17`) that swaps in a different instructions file, or skips loading one, depending on
   whether Go already resolved a mode. [V]
6. The one place the codebase *does* deliberately bridge Go's resolution to `CLAUDE.md`'s classify
   logic is `writeInferGuidance` (`internal/harness/prompt.go:608-634`), and it fires **only** when
   `strings.TrimSpace(in.Mode) == InferMode` (`prompt.go:444`, resume path `prompt.go:574`) — i.e.,
   only in the one case where Go itself could **not** resolve a mode. Its own doc comment
   (`prompt.go:608-622`) frames pointing the agent at the target repo's own `CLAUDE.md` as
   deliberately cheap ("costs one paragraph") *precisely because* it is reserved for the unresolved
   case — confirming the authors already treat `CLAUDE.md`'s classify prose as a **fallback**, not
   as logic that should run on every request. [V]

**So the honest framing is not "CLAUDE.md is buggy," it is "CLAUDE.md's prose and the daemon's Go
resolution are two separate, non-communicating decisions run in sequence."** Go picks a mode (by
whatever means) and stamps one plain-text header line into the prompt; the agent then reads that
prompt and, per `CLAUDE.md`'s own unconditional Classify instructions, re-derives a mode itself
from the (command-word-stripped) instruction text — regardless of what the header line already
says. It is technically possible for `CLAUDE.md`'s prose to reference that header line, since it is
plain text sitting in the prompt before the instructions the agent follows are even reached in
Claude's own reading order — but nothing in `CLAUDE.md` or `AGENTS.md` currently does so, and
nothing in the Go code enforces that the two stay in agreement; the "contract" between
`tick.go`'s ladder and `CLAUDE.md`'s Classify prose is entirely informal, since `CLAUDE.md` is a
plain markdown file outside Go's compile/test boundary. [V]

**What "avoiding it" would concretely look like:** add one conditional branch to the top of
`CLAUDE.md`'s `## Classify` section (and mirror it in `AGENTS.md`, or have `AGENTS.md` keep
deferring to `CLAUDE.md` as it already does) that reads roughly: *"If this prompt already states a
resolved mode (the `- Mode: X` line above), treat that as authoritative and skip steps 1–3 below;
only classify from scratch when no such line is present or it reads `infer`."* This is a
documentation-only change — no new Go capability is required, because the header line already
exists in every prompt today (`prompt.go:423`) regardless of mode; the only thing missing is an
instruction telling the agent to look at it. Two things Plan mode should settle before sizing this,
surfaced by this research:

- **Exact wording of the "authoritative" line.** Should the check be "trust it always" or "trust it
  unless it's obviously wrong for the request" — the latter re-introduces exactly the redundant
  inference the fix is meant to remove, so the recommendation is the former: trust the header
  unconditionally when present and not `infer`.
- **`Gate` interacts with this too.** `CLAUDE.md`'s `## Gate` section (`CLAUDE.md:31-43`) checks
  session-artifact prerequisites (e.g. Plan requires `RESEARCH_BRIEF.md`) independently of mode
  resolution; a mode trusted from the header still needs its Gate prerequisite checked exactly as
  today. This finding only concerns skipping the *Classify* re-derivation, not the *Gate* check,
  and the two should not be conflated when this is scoped.

### Concrete zero-label walkthrough (fresh `litefactory init`, brand-new user, single-agent shop)

Traced end-to-end in code:

1. If the issue is not self-assigned to the owner: nothing happens at all, silently
   (`tick.go:1079-1080`). This is the single biggest first-run trap found in this research.
2. If assigned, non-empty body, no prior comments: `ParseCommand` classifies it `KindFreeText`
   (`internal/turn/command.go:123`), `Daemon.run` proceeds. No `Mode:` label → one LLM
   classification call (`mode.go:87-96`) picks a mode, and the resulting mode is written back as a
   label (`tick.go:1443-1444`) for future turns. No `Coding Agent:` label → falls back to
   `default_agent` = `"Claude"` from the uninteractively-seeded config. No `lf:tools=` label →
   default tool surface used unchanged. The run proceeds end-to-end with **zero explicit
   configuration**, silently relying on three separate, undocumented defaulting mechanisms
   (mode-via-LLM-call, agent-via-config, tools-via-config-no-op). [V]

## Constraints & Risks

- **Ambiguity in "standard agent" — resolved 2026-09-04.** The issue text could originally have
  meant either (a) the coding-agent *tool* (Claude vs. Codex), which is what
  `internal/daemon/agent.go` and `default_agent` actually control, or (b) a sub-agent *persona*
  within a mode (e.g. `go-architect`, `codebase-tour`). The author confirmed sense (a) directly
  ("yes, coding agent tool"); this brief's original assumption was correct, and sense (b) is
  confirmed out of scope for this issue.
- **Config-level default already exists and is documented in the template comment** — any change
  here should extend `default_agent`/`config.Defaults()` rather than reinvent it, to preserve the
  parallel structure across the three label ladders (explicitly called out at `agent.go:73-77` as
  intentional symmetry with `resolveTools`).
- **The mode gate (Research→Plan→Implement, Spike standalone) lives only in `CLAUDE.md` prose**,
  not in Go — this observation stands, but its original implication (that Plan mode should choose
  between a session-state-aware Go-side shortcut and leaving the gate logic where it is) is now
  **superseded**: the author confirmed the LLM-classification fallback is the intended
  mode-defaulting mechanism (Open Question 2, closed), so no new `.aiteam/`-layout awareness should
  be added to `internal/daemon`. What the author *did* flag as unresolved — whether that
  classification step runs redundantly against an already-daemon-resolved mode — is a narrower,
  separate concern, investigated and answered in **Part 4** above; it does not require the daemon
  to gain session-state awareness, only for `CLAUDE.md`'s prose to trust a value the daemon already
  computes.
- **`CLAUDE.md` and `AGENTS.md` sit outside Go's compile/test boundary (Part 4).** Both are plain
  markdown files read by the coding-agent CLI via working directory
  (`internal/harness/doc.go:51-52`), not validated or type-checked against
  `internal/daemon`/`internal/turn`'s actual mode-resolution behavior. Any fix here (the
  recommended Classify-section branch from Part 4) is a prose change with no compiler or test
  suite to catch drift if the Go side's header format (`prompt.go:423`, `"- Mode: %s\n"`) ever
  changes — worth a code comment cross-reference at `prompt.go:423` pointing at the `CLAUDE.md`
  section that depends on its exact format, if this is implemented.
- **The "refuse loudly" philosophy is the dominant design invariant** across all label handling in
  this codebase (explicit inline documentation at multiple sites: `agent.go`, `tools.go`, the
  `ReplyKind` enum). Any new default behavior must default only on *absence*, never paper over
  *malformed or ambiguous* input — that distinction is already correctly implemented for agent and
  tools and should be preserved for any mode-defaulting change too.
- **Silence at the assignment gate is a deliberate security boundary**
  (`tick.go:1034-1038`), not a bug, and should not simply be made to reply. Any "ease onboarding"
  fix here needs a different channel than a same-thread reply to the un-activated issue — e.g., a
  one-time message printed by `litefactory init`/`writeInitSummary`, or documentation, rather than
  a runtime reply that would leak "this bot is watching" to unauthenticated openers.

## Open Questions

Questions 1, 3, 4 and 5 below were answered directly by the issue author on 2026-09-04 and are
recorded here as **closed**, with the answer kept alongside the original question rather than
deleted, so the history of what was asked and resolved stays intact. Question 2's premise was
confirmed by the author as intended behavior, but it depended on the new Part 2/Part 4 question
the author raised in the same comment — see the resolution note under Question 2.

1. ~~Does "default to the standard agent" mean the coding-agent tool (Claude/Codex, sense (a)
   above) or something else?~~ **CLOSED — confirmed 2026-09-04.** Author's answer: "yes, coding
   agent tool." Sense (a) is confirmed correct, and per Part 1 above, that reading is already
   fully implemented — no new work needed for this half of the original issue.
2. ~~For mode: does the requester want (i) a static `default_mode` config knob, (ii) a
   session-state-aware classification shortcut, or (iii) confirmation that the existing
   LLM-classification fallback already constitutes "mode defaulting"?~~ **CLOSED — confirmed
   2026-09-04, reading (iii).** Author's answer: "mode defaulting: if no mode is set, it should
   infer the mode from the llm" — i.e., the current LLM-classification fallback (`mode.go:87-96`)
   is the intended behavior, not a gap to close with a static default or session-state shortcut.
   **This resolution is conditional on the follow-up question the author raised in the same
   comment** — whether that inference step runs redundantly against a mode the daemon had *already*
   resolved by other means (stored selection / label / slash command), which would make the
   "infer from the LLM" design partially undermined by the router agent re-inferring on top of it.
   That question is investigated in full as **Part 4** above. Short answer from Part 4: yes, this
   redundant re-inference is real and confirmed in code; Part 4 gives a concrete, documentation-only
   recommendation to eliminate it while preserving reading (iii) as the intended design.
3. ~~Should `litefactory init` be taught to detect which agent(s) are actually
   configured/available... and seed `default_agent` accordingly?~~ **CLOSED — declined
   2026-09-04.** Author's answer: "no, litefactory should not know that at the moment." Out of
   scope; do not pursue agent-tooling auto-detection at `init` time.
4. ~~What is the acceptable channel for improving the silent-unassigned-issue experience?~~
   **CLOSED — confirmed as correct-by-design, 2026-09-04.** Author's answer: "unassigned is no
   problem; if no assignment is done it is fine to not react, this is what a human must resolve."
   The silence at `tick.go:1079-1080` is confirmed intentional and should not be changed; no
   onboarding-message or docs work is needed to soften it.
5. ~~Is the empty-body silent no-op (`tick.go:1081-1082`) intentionally undocumented, or an
   oversight worth a one-line doc comment / reply?~~ **CLOSED — skipped, 2026-09-04.** Author's
   answer: "skip this." Not being pursued; the empty-body no-op is left exactly as found, without
   further documentation work.

### New open items surfaced by Part 4 (2026-09-04)

No further clarification is needed from the author to act on Part 4's finding — the investigation
answered its own question definitively ("is this true?" → yes, confirmed in code) and produced a
concrete, scoped recommendation (a conditional branch at the top of `CLAUDE.md`'s `## Classify`
section, mirrored in/deferred-to by `AGENTS.md`, trusting an already-present `- Mode: X` header line
when it is not `infer`). What remains for **Plan mode** to settle, not the author:

- Whether to word the new Classify-section branch as "trust the header unconditionally when
  present and not `infer`" (this brief's recommendation, see Part 4) or something more
  conservative, and how to phrase it so a router agent reads it correctly on every run.
  This is an implementation/wording decision, not a fact this research left unresolved.
- Whether `AGENTS.md` should gain the identical conditional text directly, or continue deferring
  entirely to `CLAUDE.md`'s Classify section (its current pattern, `AGENTS.md:41-44`) so the fix
  only needs to be written once. Both are structurally workable given AGENTS.md's existing
  deferral; Plan mode should pick one for consistency with the rest of that file.

No web-research questions were identified in either research pass — every question in this brief
resolves against this repository's own Go source and prose documentation (`CLAUDE.md`, `AGENTS.md`,
`internal/daemon`, `internal/turn`, `internal/harness`); nothing here turns on third-party library
or platform behavior.

## Recommended scope for Plan mode

**Updated 2026-09-04** following the author's clarifications. Most of the original candidate scope
has now been explicitly closed by the author; what remains is narrower and more concrete than run
0001 left it.

**In scope:**
- **Part 4's `CLAUDE.md`/`AGENTS.md` fix.** Add a conditional branch to the top of `CLAUDE.md`'s
  `## Classify` section (`CLAUDE.md:15-22`) that checks the prompt's existing `- Mode: X` header
  line (`internal/harness/prompt.go:423`) and treats it as authoritative — skipping steps 1–3 —
  whenever it is present and not `infer`. This is the one substantive piece of new work this
  research identified as both requested and unresolved. It is documentation-only: no Go code
  change is required, since the header line is already emitted on every run regardless of mode.
  Plan mode should decide the exact wording (see Open Questions, "New open items") and whether
  `AGENTS.md` gets the same text directly or keeps deferring to `CLAUDE.md`.

**Confirmed already solved / correct-by-design — no work needed:**
- Agent defaulting (Question 1) — already fully implemented (`default_agent`, Part 1 above).
- Mode defaulting via LLM-classification fallback (Question 2, reading (iii)) — confirmed as the
  intended design once Part 4's redundant-inference fix lands; no static `default_mode` knob and no
  session-state-aware shortcut is wanted.
- Silent unassigned-issue behavior (Question 4) — confirmed a deliberate, correct security
  boundary; explicitly not a UX gap to fix.

**Explicitly out of scope (declined by the author or already ruled out in run 0001):**
- `litefactory init` agent-tooling auto-detection (Question 3) — declined: "litefactory should not
  know that at the moment."
- Any change to the silent unassigned-issue path (Question 4) or the empty-body no-op (Question 5)
  — both declined/skipped by the author.
- Re-implementing or restructuring the existing agent-label or tools-label ladders — both are
  already correct and intentionally symmetric; do not touch `agent.go`/`tools.go` resolution logic
  itself.
- A session-state-aware Go-side mode shortcut (reading (ii) from run 0001) — superseded by the
  author's confirmation of reading (iii); do not build new `.aiteam/`-layout awareness into
  `internal/daemon`.
- User-facing documentation of label conventions — not requested in the follow-up comment; drop
  from scope unless raised again separately.
- Sub-agent/persona selection within a mode run (Task-tool orchestration) — no label mechanism
  exists for this today and the issue's own wording does not clearly ask for one; out of scope
  unless the requester confirms sense (b) from the original ambiguity note in Constraints & Risks.
