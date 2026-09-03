# Research Brief — lf-69-we-need-to-make-more-defaults-apply-on

## Request

> Right now, we always expect issues to be correctly labeled. This is a bit odd when e.g. we
> only have one coding agent. To improve that, we should default to the standard agent when no
> label is present. The same applies to the mode — but here it is a bit more complicated. How can
> we solve this? Or is this already solved? Also, check where else we can ease the usage here!

Headless run; no clarification available. Ambiguity in "standard agent" (coding-agent tool vs.
sub-agent persona) is resolved below under Constraints & Risks, with the assumption stated
explicitly.

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

- **Ambiguity in "standard agent."** The issue text could mean either (a) the coding-agent *tool*
  (Claude vs. Codex — the AI harness invoked), which is what `internal/daemon/agent.go` and
  `default_agent` actually control, or (b) a sub-agent *persona* within a mode
  (e.g. `go-architect`, `codebase-tour`), which are Task-tool invocations internal to an
  orchestrator run and have no label-driven selection at all — they are chosen by orchestrator
  logic/prompts, not by GitHub labels. **Assumption taken for this brief: the request means sense
  (a), the coding-agent tool**, because that is the only sense in which "one coding agent" and "a
  label is present" map onto real code (`agent.go`), and because the request explicitly pairs it
  with mode, which is also a top-level label-driven daemon concept. Flagged as an open question
  below in case the requester meant something narrower or broader.
- **Config-level default already exists and is documented in the template comment** — any change
  here should extend `default_agent`/`config.Defaults()` rather than reinvent it, to preserve the
  parallel structure across the three label ladders (explicitly called out at `agent.go:73-77` as
  intentional symmetry with `resolveTools`).
- **The mode gate (Research→Plan→Implement, Spike standalone) lives only in `CLAUDE.md` prose**,
  not in Go. Any change that tries to make mode defaulting "session-state aware" (e.g., "default
  to Research if no brief exists yet") would need to either (a) teach `internal/daemon` to read
  session-artifact presence before calling `classifyMode` — a materially new capability, since the
  daemon currently has zero knowledge of `.aiteam/` session file layout — or (b) leave the gate
  logic where it is (LLM router prompt) and merely skip the *label-independent* classification
  call when a cheaper heuristic is confidently correct. These are different-sized changes; Plan
  mode should scope which one is intended before estimating stories.
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

1. Does "default to the standard agent" mean the coding-agent tool (Claude/Codex, sense (a)
   above) or something else? This brief assumes (a); Plan mode should confirm before scoping any
   work, since the (a) reading is already fully solved and the story might otherwise be empty.
2. For mode: does the requester want (i) a genuinely static `default_mode` config knob analogous
   to `default_agent` (simple, but loses the "infer intent from the request text" behavior that
   currently makes unlabeled issues usable at all), (ii) a session-state-aware shortcut that
   skips the LLM classification call in unambiguous cases (e.g., no brief yet ⇒ Research) —
   requiring new Go-side awareness of `.aiteam/` session layout, or (iii) simply documenting that
   the existing LLM-classification fallback already constitutes "mode defaulting" and closing this
   half of the issue as already-solved, matching how agent defaulting is already solved? These are
   different-sized pieces of work and Plan mode cannot size them without this answer.
3. Should `litefactory init` be taught to detect which agent(s) are actually configured/available
   (e.g., inspect which `agents.*` stanzas are uncommented, or probe for the `claude`/`codex`
   binaries) and seed `default_agent` accordingly, rather than always writing `"Claude"`? This
   directly serves the issue's own example ("when we only have one coding agent") but was not
   explicitly asked for — flagged as a plausible Part 3 addition rather than assumed in scope.
4. What is the acceptable channel for improving the silent-unassigned-issue experience, given
   that a same-thread reply is ruled out by the security rationale at `tick.go:1034-1038`? Candidates
   surfaced by this research: a friendlier `litefactory init` completion message describing the
   assignment requirement, and/or a README/docs page — neither was explicitly requested, both fall
   under the issue's own "check where else we can ease the usage" clause.
5. Is the empty-body silent no-op (`tick.go:1081-1082`) intentionally undocumented, or an
   oversight worth a one-line doc comment / reply? Low-stakes; noted for completeness rather than
   as a likely story.

No web-research questions were identified — every question in this brief resolves against this
repository's own Go source and prose documentation; nothing here turns on third-party library or
platform behavior.

## Recommended scope for Plan mode

**In scope:**
- Closing the loop on Question 1 and Question 2 with the requester (or, if headless, taking the
  most conservative reading: report agent-defaulting as already-solved/no-op, and scope mode
  changes to the smallest of the three (i)/(ii)/(iii) options — likely (iii), documenting the
  existing behavior, unless the requester's next comment says otherwise).
- If (ii) is confirmed in scope: designing the session-state-aware mode shortcut, including how
  much of `.aiteam/` layout knowledge is acceptable to add to `internal/daemon` (a genuinely new
  capability, not a small tweak) versus keeping the gate check in the LLM router only.
- The `litefactory init` agent-detection improvement (Question 3), if confirmed in scope — a
  self-contained, independently shippable story regardless of the mode-defaulting decision.
- A first pass at user-facing documentation of the three label conventions and the assignment
  requirement (Question 4), sized as documentation-only work, not a code change.

**Out of scope:**
- Re-implementing or restructuring the existing agent-label or tools-label ladders — both are
  already correct and intentionally symmetric; do not touch `agent.go`/`tools.go` resolution logic
  itself.
- Changing the silent behavior of the authorization gate at `tick.go:1079-1080` — that silence is
  a deliberate security property, not a defaulting gap, and should not be "fixed" by adding a
  reply.
- Sub-agent/persona selection within a mode run (Task-tool orchestration) — no label mechanism
  exists for this today and the issue's own wording does not clearly ask for one; out of scope
  unless the requester confirms sense (b) from the ambiguity note above.
