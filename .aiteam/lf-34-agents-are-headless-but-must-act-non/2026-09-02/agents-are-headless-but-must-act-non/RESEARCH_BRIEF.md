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

## Codebase Context

This is litefactory's own repository — the "agents" in question are litefactory's
own mode orchestrators and specialist sub-agents, defined as `.claude/agents/*.md`
and `.claude/modes/*.md` markdown specs, driven by a Go daemon/CLI (`cmd/`,
`internal/`). Findings below are from a direct codebase-tour run against this
repo `[V]`.

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
- `internal/report/reply.go:40-48` — `ReplyUnknownToolProfile`. This is the real
  mechanism behind the `lf:tools=web` label research-orchestrator.md cites as an
  example: it is a **generic named tool-profile label lookup**, not a hardcoded
  `"lf:tools=web"` literal anywhere in `internal/`. No such literal string was
  found in the Go source. The doc's specific example may describe one
  operator-configured profile name rather than a fixed system constant — worth
  confirming, not necessarily wrong. `[V]`

### No architecture documentation exists for a human reader

- `README.md` is a two-line stub — title and tagline only, no architecture
  content. `[V]`
- No `ARCHITECTURE.md` and no `docs/` directory exist anywhere in the repo
  (confirmed by Glob). `[V]`
- `.claude/AGENT_ROSTER.md` exists but reads as a routing index for the modes,
  not human-facing architecture documentation (not read in full depth this run). `[R0-ish, not fully verified]`

### Coverage is uneven across the agent roster

- `research-orchestrator.md` and `plan-orchestrator.md` both describe the
  headless/asking mechanism in their own prose (Detour rule, Asking section,
  standing "move to next phase" line) — consistent with the Go text where they
  overlap, but they do not cite `internal/harness/prompt.go` as the source of
  truth, which is exactly how the stale line-number drift above happened. `[V]`
- `implement-orchestrator.md` documents its own "Detour rule" / fallback-to-assumption
  behavior, but this is **redundant with, not the source of**, the actual
  backstop — the real enforcement is upstream in Go (`headlessRule`, plus
  `modeMayAsk` refusing Implement mode outright regardless of any `lf: ask` tag). `[V]`
- `go-implementation-engineer.md`, `requirements-engineer.md`,
  `proxy-product-owner.md`, `go-code-reviewer.md`, and `app-context-updater.md`
  **do not mention litefactory, headlessness, or asking at all** (confirmed by
  targeted grep). They rely entirely on the Go-injected prompt header for that
  framing. `[V]`
- `implement-orchestrator.md`'s own Phase 2 material contains an interactive
  dialogue template that reads as though it could address a live human, even
  though `modeMayAsk` machine-refuses asking in Implement mode unconditionally.
  Whether this is intentional (aspirational/PM-facing prose, overridden in
  practice by the Go header) or a genuine internal contradiction was not
  resolved this run — see Open Questions. `[V]`

### `.aiteam/rulebook.md`

Does not exist in this repository. `[V]` Per `.claude/modes/session.md`, scanners
and orchestrators fall back to generic best practices in its absence — not a gap
specific to this issue, just confirmed as the current state.

## Domain / Problem Space

The issue names two distinct failure modes that this research separates:

1. **A self-knowledge gap**: agents (particularly specialist agents further from
   the orchestrators, and Implement-mode agents specifically) have no markdown-level
   statement that litefactory exists, that they are running headless, or that
   their final message is what reaches a human. This framing exists, but lives
   entirely in Go-generated prompt text that no human reviewing or maintaining
   the agent *definitions* would ever see, and that individual agent authors have
   no reason to know about when writing or revising a `.md` spec.
2. **A behavior/default question**: the system as built is **deliberately**
   restrictive about asking — opt-in via a literal tag, off by default, and
   entirely disallowed for Implement mode "no matter what the comment said." The
   issue's literal wording ("should always push back information if they have
   questions") could be read as asking for this default to change, not just for
   agents to be told more clearly what the current default is and why. These are
   different-sized changes, and this research cannot determine from the issue
   text alone which one is wanted.

## Constraints & Risks

- **Existing design is intentional, not accidental.** `internal/turn/ask.go`'s own
  comments state the restrictive default is deliberate, to keep an untagged run
  from "asking whatever the agent decides." A fix that loosens this needs to be a
  conscious product decision, not an incidental side effect of "make agents more
  self-aware." `[V]`
- **Doc/code drift is a real, demonstrated risk**, not a hypothetical one — the
  `run.go` line-number citations in `research-orchestrator.md` are already stale
  relative to current source. Any fix that adds more prose describing Go behavior
  into markdown specs should either cite the Go source directly (so drift is at
  least locatable) or avoid restating line-level implementation detail in
  markdown at all. `[V]`
- **Single point of dependency.** Every specialist agent's headless/asking
  awareness currently depends entirely on the Go harness correctly injecting
  `headlessRule`/`askingRule`/`askingRevokedRule` into its prompt. None of the
  specialist markdown files carry a fallback statement of their own. This is
  efficient (one place to get it right) but also means a gap in the harness'
  injection logic would silently strip *all* self-awareness for that run, with no
  markdown-level backstop anywhere. `[V]`
- **The 32 KiB cap and end-truncation behavior are already correct and already
  documented** (in research-orchestrator.md's own "Ordering" section, mirroring
  the actual Go behavior) — not something this issue's fix should need to touch.

## Open Questions

1. Does issue #34 want a **behavior change** (loosen `modeMayAsk` / the opt-in
   `lf: ask` tag gating, e.g. allow Implement-mode runs to ask under some
   circumstance) or a **self-knowledge/documentation improvement** (agents told
   more explicitly, in their own markdown, that litefactory exists and mediates
   their output, with the current opt-in/off-by-default philosophy unchanged)?
   This needs an explicit decision before Plan mode scopes stories — the two are
   very different sizes of change.
2. Should the markdown agent/orchestrator specs be updated to explicitly cite
   `internal/harness/prompt.go` (and the specific constants: `headlessRule`,
   `askingRule`, `askingRevokedRule`, `inferAskingSeam`) as the source of truth
   for headless/asking framing, so that future doc drift (like the confirmed
   stale line-number citation found this run) is at least locatable?
3. Is `implement-orchestrator.md`'s Phase 2 interactive dialogue template
   intentionally aspirational/PM-facing prose, understood internally to be
   overridden in practice by the Go-injected header (which unconditionally
   refuses asking for Implement mode) — or is it a genuine contradiction the fix
   should resolve, e.g. by rewriting that section to acknowledge the
   headless/no-asking reality directly?
4. Is it accepted design that `requirements-engineer.md`, `proxy-product-owner.md`,
   `go-code-reviewer.md`, and `app-context-updater.md` carry zero explicit
   headless/litefactory-awareness language of their own? Or should each get a
   short explicit self-awareness note for documentation/maintenance robustness,
   even though enforcement already lives correctly in the Go harness?
5. Confirm whether `lf:tools=web`, as cited by name in `research-orchestrator.md`,
   is a real operator-configured tool-profile name in this deployment, or whether
   the doc should instead describe the generic named-profile mechanism at
   `internal/report/reply.go:40-48` without citing a specific literal that
   doesn't exist as a constant in the Go source.
6. Should litefactory gain a canonical architecture document (`ARCHITECTURE.md`
   or a `docs/` directory) explaining the headless execution model, the
   comment-posting pipeline, and the tag/label gating system, for human
   maintainers — given the current `README.md` is a two-line stub and no such
   document exists anywhere in the repo today? This is adjacent to, not
   required by, the core issue, but was a repeated gap surfaced during research.

## Recommended scope for Plan mode

**In scope**, pending resolution of Open Question 1:
- If documentation-only: add explicit, source-cited self-awareness language
  (litefactory exists, this run is headless, here is what happens to my final
  message) to the specialist agent files that currently have none
  (`go-implementation-engineer.md`, `requirements-engineer.md`,
  `proxy-product-owner.md`, `go-code-reviewer.md`, `app-context-updater.md`), and
  correct the stale `run.go` line-number citations in the orchestrator specs.
- If behavior-changing: a scoped decision, made explicitly with the issue owner,
  about which mode(s)/conditions should be allowed to ask beyond the current
  opt-in default, implemented in `internal/turn/ask.go`'s `modeMayAsk` and tested
  with the same rigor as the existing `ask_test.go` table tests.
- Either way: resolving the apparent tension in `implement-orchestrator.md`'s
  Phase 2 interactive-dialogue language versus the unconditional Go-level refusal
  to ask in Implement mode.

**Out of scope**:
- Rewriting the prompt-assembly pipeline, the 32 KiB comment cap, or the
  end-truncation ordering behavior — these are already correct and already
  documented correctly (modulo the stale line numbers noted above).
- Changes to `internal/github` comment-posting internals unrelated to the
  headless/asking framing.
- Producing a full `ARCHITECTURE.md` (Open Question 6) unless the issue owner
  confirms that is wanted as part of this fix rather than a separate piece of
  work.
