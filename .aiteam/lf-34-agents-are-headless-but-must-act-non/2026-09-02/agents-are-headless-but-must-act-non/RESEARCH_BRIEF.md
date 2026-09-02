# Research Brief — agents-are-headless-but-must-act-non

## Request

Issue #34: "Agents are executed headless and assume stuff, without knowing that
they have a system around them. They need to know that there is litefactory and
should always push back information if they have questions so that litefactory
can post them on the issue."

Research task: understand how litefactory's agents/orchestrators/modes currently
handle ambiguity and missing context in headless runs, whether/how they surface
blocking questions back out to the system, and what's missing that causes agents
to "assume stuff" instead.

**Follow-up round (this run):** the first Research pass ended with 6 open
questions. @marioatcloudvane answered all 6 on the issue. Those answers, read as
evidence rather than as literal spec text, converge on a single clear scope (see
*Domain / Problem Space* and *Resolved by owner clarification* below), and asked
for a deeper, complete per-agent audit of who currently has this self-awareness
and who doesn't — done this run via a second, full-file `codebase-tour` pass
across all 15 files under `.claude/agents/` and `.claude/modes/`.

## Codebase Context

This is litefactory's own repository — the "agents" in question are litefactory's
own mode orchestrators and specialist sub-agents, defined as `.claude/agents/*.md`
and `.claude/modes/*.md` markdown specs, driven by a Go daemon/CLI (`cmd/`,
`internal/`). Findings below are from two direct codebase-tour runs against this
repo `[V]` — an initial pass (mechanism tracing + a grep-only first look at the
agent roster) and a follow-up pass (full read of every agent/orchestrator file,
this run).

### The headless/asking mechanism actually lives in Go, not in markdown

The load-bearing behavior is a single Go string constant injected into **every**
prompt the harness assembles, not something described in an agent's own markdown
instructions:

- `internal/harness/prompt.go:481-485` — `headlessRule`, injected whenever a run
  does **not** have permission to ask. Verbatim: *"You are running without a
  terminal. Nobody will read a question and nobody can answer one, so do not ask
  clarifying questions... Where something is ambiguous, take the most reasonable
  reading, act on it, and say what you assumed in your final message."* `[V]`
- `internal/harness/prompt.go:515-526` — `askingRule`, injected when a run **does**
  have permission. Defines the bar for a question worth asking, the mechanic
  ("Ask by stopping... your final message is posted as a comment"), the batching
  instruction, and the standing "move to the next phase" line. `[V]`
- `internal/harness/prompt.go:550-553` — `askingRevokedRule`, for the resume case
  where permission is withdrawn mid-conversation. Not mentioned anywhere in the
  markdown agent specs. `[V]`
- `internal/harness/prompt.go:535-537` — `inferAskingSeam`, the one place the
  Research/Plan-only restriction is expressed as prompt text rather than Go
  enforcement, because at prompt-build time the mode is genuinely unclassified. `[V]`

**The word "headless" does not appear anywhere under `.claude/`** (confirmed by
grep across the directory) `[V]`. `CLAUDE.md` and every `.claude/modes/*.md` /
`.claude/agents/*.md` file are silent on the fact that a run has no terminal and
no live user — that framing exists exclusively as Go-generated prompt text a
human reading the agent specs would never see.

### The permission-to-ask mechanism is real, machine-enforced plumbing — not aspirational text

`research-orchestrator.md`'s own "Asking" section says "litefactory grants that
per run — you never grant it to yourself." This is not aspirational; there is
concrete code behind it `[V]`:

- `internal/turn/ask.go:27` — `AskTag = "lf: ask"`, a literal, whole-line,
  case-sensitive tag a human puts in a GitHub comment.
- `internal/turn/ask.go:69` — `HasAskTag`, matches that tag in the comment body.
- `internal/turn/ask.go:137-144` — `modeMayAsk`, a Go `switch` that allows asking
  only for `research`, `plan`, and `infer` (unclassified). **`implement` is
  machine-refused regardless of what the comment said** — the code comment at
  `ask.go:123` states this explicitly: "no matter what the comment said."
- `internal/turn/prepare.go:580` — the actual gate:
  `MayAsk = HasAskTag(body) && modeMayAsk(previous.Mode)`.
- `internal/turn/ask.go` code comments describe the design philosophy directly:
  asking is "opt-in and off by default," enforced in Go "so an untagged run
  cannot ask whatever the agent decides." `[V]`

So: when a run is *not* tagged with `lf: ask`, or is in Implement mode, the agent
is told point-blank ("nobody can answer one") not to ask and to record its
assumption instead. This already directly implements half of what the issue is
asking for (telling the agent there's a system, telling it what to do instead of
silently assuming) — but only inside the Go-generated prompt header, invisible to
anyone reading the agent markdown.

### The posting pipeline

- `internal/report/run.go:31` — `maxResultBytes = 32 << 10` (32 KiB cap on the
  final message that becomes the comment). `[V]`
- `internal/report/run.go:732` — `truncateEnd` is the actual truncation call, cut
  from the end. `[V]`
  - Note: `research-orchestrator.md` cites these as lines 25 and 713
    respectively — this is **confirmed doc drift**: the file has grown since that
    documentation was written, and the citations are stale by a handful of lines.
    Not load-bearing today, but a live example of the exact drift risk this
    issue's fix should guard against. `[V]`
- `internal/github/comments.go:230-265` — `Client.PostComment`, the actual GitHub
  API call, itself gated by a `CommentSizeLimit` check before the request is
  made. `[V]`
- `internal/report/reply.go:40-48` — `ReplyUnknownToolProfile`, the generic
  named tool-profile label lookup behind the `lf:tools=web` example cited in
  `research-orchestrator.md`. Not investigated further this run — the owner has
  confirmed this thread has nothing to do with issue #34 (see *Resolved by owner
  clarification*, item 5).

### Full per-agent asking/headlessness audit (this run)

The owner's clarification made the shape of the fix concrete: an agent only
needs to know two things — (a) it is running headless, with no live person on
the other end of this run, and (b) if it hits a question it genuinely cannot
resolve itself, the correct move is to **stop and put the question in its final
message**, because that is what litefactory relays to the issue as a comment,
not to simulate or narrate a conversation with "the user" as though one were
happening live inside the run. The owner explicitly does **not** want agents
taught how litefactory works internally (no Go source citations, no constant
names) — just that framing.

Every file under `.claude/agents/*.md` and `.claude/modes/*.md` (15 files) was
read in full against that framing and classified `[V]`:

| File | Has headless framing (A) | Has "stop & surface via litefactory" framing (B) | Silent "nobody to ask, so I..." fallback (C) | Reads as a live self-dialogue script instead of stop-and-surface (D) | Nothing at all (E) |
|---|---|---|---|---|---|
| `research-orchestrator.md` | Yes | Yes | — | — | — |
| `plan-orchestrator.md` | Yes | Yes | — | — | — |
| `implement-orchestrator.md` | — | Weak/absent (says "pause and ask the user" 4×: lines 11, 171, 292, 339-340, with no acknowledgment Implement mode is machine-refused) | — | Yes — Phase 2, lines 209-219, literal fill-in-the-blank dialogue | — |
| `requirements-engineer.md` | — | — | — | Yes — the entire Phase 1-3 flow (lines 24, 28, 32-34, 46, 50-59, 61) | — |
| `proxy-product-owner.md` | — | — | — | — | Adjacent — "ask if it's in scope" (line 120) with no live user contact and no framing of what "ask" cashes out to |
| `go-architect.md` | — | — | — | — | Has its own separate, pre-existing "Standard Deviation" human-escalation idiom (lines 206-217, names "Mario (human)") — distinct from the ask-tag mechanism |
| `go-implementation-engineer.md` | — | — | — | — | Yes (mentions "litefactory" once, but only re: no-commit rule, unrelated to asking) |
| `go-code-reviewer.md` | — | — | — | — | Yes |
| `go-integration-test-engineer.md` | — | — | — | — | Yes |
| `go-unit-test-engineer.md` | — | — | — | — | Yes |
| `app-context-updater.md` | — | — | — | — | Yes |
| `codebase-tour.md` | — | — | — | — | Yes |
| `web-researcher.md` | — | — | — | — | Yes |
| `.claude/modes/session.md` | — | — | — | — | Yes (pure structural doc, no gap expected) |
| `.claude/modes/spike.md` | — | — | — | — | Yes — does not disclose Spike is excluded from `modeMayAsk` (`ask.go:125-128`), unlike Plan's explicit disclosure |

**Deep dive — `requirements-engineer.md` (the agent the owner named specifically,
because it "asks a lot"):** the file is 114 lines and contains **zero** mentions
of headlessness, litefactory, or the `lf: ask` mechanism. It is invoked only from
Plan mode via `plan-orchestrator.md`, which *does* carry a full "Asking" section
(lines 211-231 of that file) — but `requirements-engineer.md` never
cross-references it or acknowledges it exists. Instead the whole document is
written as a self-contained live-chat script, e.g. `[V]`:
- Line 24: *"If neither file exists, ask the user to describe the application
  before proceeding."*
- Line 28: *"Ask what problem this solves... One question at a time."*
- Lines 32-34: a literal printed template — *"> 'From the brief, I understand:
  [X, Y, Z]. Is that right? I still need to confirm: [gap 1], [gap 2].'"*
- Lines 50-59: a printed confirmation block ending *"Proceed?"*, followed by
  line 61: *"Only write the spec after the user confirms."*

Nothing in the file distinguishes its "ask the user" language from an ordinary
synchronous chat exchange. It has neither bucket A (headless disclosure) nor
bucket B (stop-and-surface instruction) — it simply assumes conversational
turn-taking will happen inside a single run, with no acknowledgment that it only
ever runs foreground inside one `Task` invocation with no ability to wait
mid-run for a live reply. This is the clearest, most concrete instance of what
the owner flagged: the requirements agent is exactly the one that should most
clearly know there is a real person on the other end of its questions, reachable
only through litefactory's stop-and-relay mechanism — and today it has no such
framing at all.

**Deep dive — `implement-orchestrator.md` Phase 2 (lines 209-219):** confirmed
pattern (D), a literal fill-in-the-blank live dialogue script:
```
**At least one story touches real dependencies** → run this conversation with the user before proceeding:

    Some stories need integration tests against real dependencies. Before I run them I need a few things:

    1. Target environment URL — this must NOT be production:
       >
```
Cross-referenced against `internal/turn/ask.go:137-144` (`modeMayAsk`): Implement
mode is **machine-gagged** unconditionally — *"implement may not, ever (AC-A1.6).
Machine-enforced here rather than left to prompt text, so a tagged implement run
gets the gag no matter what the comment said."* `[V]` This is not ambiguous: the
Go layer refuses this exchange regardless of tag, so the Phase 2 prose describes
an interaction the harness structurally cannot support in a headless run — it is
misleading text, not a live capability with unclear semantics. The rest of
`implement-orchestrator.md` has the same gap in miniature: "pause and ask the
user" appears four times (lines 11, 171, 292, 339-340) with no acknowledgment
that Implement mode can never post such a question, and no fallback instruction
analogous to Research/Plan's "record the assumption and continue" for when
asking isn't possible. `[V]`

### No architecture documentation exists for a human reader

- `README.md` is a two-line stub — title and tagline only, no architecture
  content. `[V]`
- No `ARCHITECTURE.md` and no `docs/` directory exist anywhere in the repo
  (confirmed by Glob). `[V]` — noted for completeness only; the owner has
  confirmed this is unrelated to issue #34 and out of scope (see *Resolved by
  owner clarification*, item 6).

### `.aiteam/rulebook.md`

Does not exist in this repository. `[V]` Per `.claude/modes/session.md`, scanners
and orchestrators fall back to generic best practices in its absence — not a gap
specific to this issue, just confirmed as the current state.

## Domain / Problem Space

With the owner's clarification, the issue resolves to a **single, precisely
scoped documentation/prompt-clarity problem**, not a behavior change:

Litefactory's Go harness already does the right thing mechanically — it tells
every run whether it may ask (`headlessRule` / `askingRule` / `askingRevokedRule`
in `internal/harness/prompt.go`), and it already gates the mechanism correctly
(`modeMayAsk`, opt-in `lf: ask` tag). **None of that machinery is being asked to
change.** What's missing is markdown-level self-awareness in the agent
*definitions themselves*, which matters for two separate, observable reasons:

1. **Agents behave as though there is nobody to ask.** The owner reports
   regularly seeing "there was nobody to ask so I..." reasoning in run output.
   The Go-injected `headlessRule` already tells a gagged run this explicitly and
   correctly — the gap is that agents which *do* have permission to ask (Research,
   Plan, and especially `requirements-engineer.md`, which "asks a lot" within
   Plan mode) have no markdown-level reminder that there genuinely *is* someone
   who will read and answer, reachable through the stop-and-relay mechanism. The
   audit above confirms `requirements-engineer.md` is the sharpest instance:
   zero self-awareness language, despite being the single agent in the roster
   that asks the most.
2. **Where "ask" language does exist in specs, it is written as a live dialogue,
   not a stop-and-surface instruction.** `requirements-engineer.md`'s entire
   flow and `implement-orchestrator.md`'s Phase 2 both read as scripts for a
   synchronous back-and-forth ("one question at a time," "Is that right?",
   "Proceed?", blank lines waiting for typed input) with no acknowledgment that
   the only real mechanism is: stop, write the question into the final message,
   litefactory posts it as a comment, a *later* run picks up the reply. This is
   exactly the distinction the owner drew explicitly: *"the prose is to ask the
   user, not to talk to itself."*

The fix is documentation/prompt-content work on the `.md` agent and mode specs:
give agents that can ask an explicit statement of (a) they are headless with no
live user in this run, and (b) an unresolved question means stopping and
surfacing it for litefactory to relay — and correct the specs (chiefly
`requirements-engineer.md` and `implement-orchestrator.md`) that currently read
as live-chat scripts so they describe the real mechanic instead. **The opt-in
`lf: ask` tag, `modeMayAsk`'s mode restrictions, and the Implement-mode refusal
are explicitly not in scope for change** — the owner's answers rule out
loosening or reworking that gate; the ask is for clarity about what already
exists, not new capability.

## Constraints & Risks

- **Existing gating design is intentional and stays as-is.** `internal/turn/ask.go`'s
  own comments state the restrictive default is deliberate, to keep an untagged
  run from "asking whatever the agent decides." The owner's clarification
  confirms this reading directly — no change to `modeMayAsk`, the `lf: ask` tag,
  or the Implement-mode refusal is wanted as part of this issue. `[V]` `[O]`
- **Doc/code drift is a real, demonstrated risk**, not a hypothetical one — the
  `run.go` line-number citations in `research-orchestrator.md` are already stale
  relative to current source. Any fix that adds prose describing the
  headless/asking mechanic into markdown specs should describe the *behavior*
  (headless, stop-and-surface) rather than restate Go line numbers or constant
  names — the owner explicitly does not want agents taught litefactory internals,
  which also sidesteps this drift risk by construction. `[V]` `[O]`
- **Single point of dependency, now scoped as acceptable.** Every specialist
  agent's headless/asking awareness currently depends entirely on the Go harness
  correctly injecting `headlessRule`/`askingRule`/`askingRevokedRule` into its
  prompt; none of the specialist markdown files carry a fallback statement of
  their own. The owner's answers don't ask for every one of the 15 files to
  carry redundant self-awareness prose — the concrete complaint is about agents
  that actively ask (chiefly `requirements-engineer.md`, and to a lesser extent
  `implement-orchestrator.md`'s misleading Phase 2 dialogue) misrepresenting how
  asking actually works. Plan mode should size the fix around those, not treat
  "add this note to all 15 files" as an implied requirement. `[V]`
- **`implement-orchestrator.md`'s Phase 2 is misleading, not just incomplete.**
  It's not merely missing a disclosure — its dialogue script promises an
  interaction the Go layer structurally refuses for Implement mode
  unconditionally (`modeMayAsk`). Left alone, this remains a live source of the
  exact "assumed there was a conversation to have" confusion the issue
  describes, even after `requirements-engineer.md` is fixed. `[V]`
- **The 32 KiB cap and end-truncation behavior are already correct and already
  documented** (in research-orchestrator.md's own "Ordering" section, mirroring
  the actual Go behavior) — not something this issue's fix should need to touch.

## Resolved by owner clarification (2026-09-02)

The owner (@marioatcloudvane) answered the first brief's six open questions on
the issue. Read as evidence rather than literal spec text, here is how each
resolves `[O]`:

1. **Behavior change vs. documentation fix** → Documentation/prompt-clarity fix,
   confirmed. The owner's concern is that agents produce "nobody to ask so I..."
   reasoning even though, in runs where asking is permitted, there genuinely is
   someone to ask. They asked for — and this run performed — a full audit of
   which agents currently have that self-awareness and which don't (see *Full
   per-agent asking/headlessness audit* above). No change to the opt-in gating
   mechanics is requested.
2. **Should specs cite `internal/harness/prompt.go` constants as source of
   truth?** → No. The owner does not want agents to understand litefactory's
   internals. The only two things an agent needs to know are: it is headless
   with no live user in this run, and an unresolved question means stopping so
   litefactory can relay it via the issue. Internal Go constant names and line
   citations should not be pushed into the markdown specs.
3. **Is `implement-orchestrator.md`'s Phase 2 dialogue intentional or a
   contradiction?** → The owner's answer reframes rather than directly resolves
   this: *"the prose is to ask the user, not to talk to itself... the req
   engineer should ASK the user [via litefactory]."* Read together with the
   audit, this identifies the general anti-pattern (scripts that simulate a live
   exchange instead of stopping and surfacing) present in **both**
   `requirements-engineer.md` and `implement-orchestrator.md`'s Phase 2 — the
   fix is to rewrite both so "asking" means stop-and-surface, not synchronous
   chat. For `implement-orchestrator.md` specifically, this also means the
   Phase 2 text needs to acknowledge that Implement mode cannot ask at all
   (per `modeMayAsk`), not merely be reworded to "ask correctly."
4. **Is it accepted that `requirements-engineer.md`, `proxy-product-owner.md`,
   `go-code-reviewer.md`, and `app-context-updater.md` have zero self-awareness
   language?** → No — not accepted. The owner: *"of course they should know
   that they run headless and that they need to ask [via] litefactory."* At
   minimum `requirements-engineer.md` (the agent that actively asks) needs this
   fixed; the others should get it too, sized as a lightweight, consistent note
   rather than a large rewrite per file.
5. **Confirm `lf:tools=web` is real / worth documenting precisely** → Rejected
   as out of scope. Owner: *"this has nothing to do with the issue we want to
   solve here."* Removed from open questions; no further action.
6. **Should litefactory gain an `ARCHITECTURE.md` / `docs/`?** → Rejected as out
   of scope. Owner: *"this has no place there."* Removed from open questions; no
   further action.

## Open Questions

Genuinely unresolved items for Plan mode, surfaced by the deeper per-agent audit
this run (not yet put to the owner):

1. `implement-orchestrator.md` says "pause and ask the user" four times (lines
   11, 171, 292, 339-340) with no acknowledgment anywhere in the file that
   Implement mode is machine-refused from asking at all. Should this file gain
   an explicit non-asking framing — headless disclosure plus an assumption-record
   fallback matching Research/Plan's "record what was assumed and continue"
   pattern — rather than prose that promises an exchange the harness will never
   allow?
2. Should `requirements-engineer.md` and the other agents identified as having
   zero self-awareness language (`proxy-product-owner.md`, `go-code-reviewer.md`,
   `app-context-updater.md`, plus the remaining engineer/reviewer agents in the
   roster table) all get the same short note, or should Plan mode scope this as
   "fix the agents that actually ask" (`requirements-engineer.md` +
   `implement-orchestrator.md`) and leave the rest relying on the Go-injected
   header, consistent with the "single point of dependency, now scoped as
   acceptable" constraint above?
3. `.claude/modes/spike.md` doesn't disclose that Spike is excluded from
   `modeMayAsk` (`ask.go:125-128`), unlike Plan's explicit disclosure — minor,
   low-priority gap; worth a one-line fix alongside the others but not central
   to the issue.
4. `proxy-product-owner.md` says "ask if it's in scope" (line 120) with no live
   user contact and no framing of what that cashes out to — likely loose
   language for "flag in the report" rather than literal asking; worth a small
   reword for precision if Plan mode is already touching this file for item 2.
5. `go-architect.md` has its own separate, pre-existing human-escalation idiom
   ("Standard Deviation," lines 206-217, names "Mario (human)" directly) that
   predates and doesn't route through the `lf: ask` mechanism. This is adjacent
   to the issue, not required by it — flagged for Plan mode's awareness only,
   not a recommended action; given the owner's rejection of two other adjacent
   items (open questions 5 and 6 in the prior round) as out of scope, this
   should not be assumed to be part of this fix without confirmation.

## Recommended scope for Plan mode

**In scope:**
- Rewrite `requirements-engineer.md` so its "ask the user" language explicitly
  states: this run is headless with no live person present; when it has a
  question it can't resolve from the brief/codebase, it stops and puts the
  question in its final message for litefactory to post as a comment; the
  answer arrives in a later run. Replace the live fill-in-the-blank templates
  (lines 32-34, 50-59) with equivalent framing.
- Rewrite `implement-orchestrator.md`'s Phase 2 (lines 209-219) to remove the
  fill-in-the-blank live dialogue script, and correct the four "pause and ask
  the user" instances (lines 11, 171, 292, 339-340) to reflect that Implement
  mode cannot ask at all — replacing them with the same
  record-the-assumption-and-report fallback the Go `headlessRule` already
  specifies.
- Add a short, consistent self-awareness note (headless + stop-and-surface,
  no internal Go citations) to the other agents confirmed to have none —
  `proxy-product-owner.md`, `go-code-reviewer.md`, `app-context-updater.md`,
  and the remaining engineer/reviewer agents — sized as Plan mode decides per
  Open Question 2.
- Fold in the small adjacent fixes noted in Open Questions 3 and 4 if Plan mode
  is already touching those files.

**Out of scope (explicitly ruled out by the owner):**
- Any change to `internal/turn/ask.go`'s `modeMayAsk`, the `lf: ask` tag
  mechanic, or the Implement-mode refusal — the opt-in/off-by-default design
  stays as-is.
- Teaching agents litefactory's internals — no Go source citations, constant
  names, or line-number references belong in the markdown specs.
- The `lf:tools=web` / tool-profile labeling question (prior open question 5) —
  confirmed unrelated to this issue.
- Producing `ARCHITECTURE.md` or a `docs/` directory (prior open question 6) —
  confirmed unrelated to this issue.
- Reconciling `go-architect.md`'s separate "Standard Deviation" escalation idiom
  with the ask-tag system (Open Question 5 above) — flagged for awareness only,
  not recommended as part of this piece of work without owner confirmation.
