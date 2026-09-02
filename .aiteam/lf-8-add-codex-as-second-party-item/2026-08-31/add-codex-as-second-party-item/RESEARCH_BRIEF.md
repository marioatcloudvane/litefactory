# Research Brief — add-codex-as-second-party-item (redo)

## Provenance key
`[V]` verified this run (read directly, path:line cited) · `[W]` web-sourced, second-hand,
never executed against a real `codex` binary · `[O]` owner-stated (the requester's own words,
taken as intent, not as settled design) · `[R0]` carried forward unverified from the first
research pass (2026-08-31) — used only where this run had no way to re-check it.

## Why this brief exists
The first research pass (`git log` — session `2026-08-31`) produced a 1072-line brief that the
requester says "did not land." Re-reading their follow-up comment `[O]`, the gap was not missing
information but missing *shape*: they want four specific things investigated on their own terms —
a per-turn label switch, the `AGENTS.md` convention, the model-naming vocabulary problem, and a
concrete interface/factory design — not folded into general prose about "Claude Code coupling."
This redo restructures around exactly those four asks, re-verifies every load-bearing claim from
the first pass rather than trusting it, and corrects several things the first pass got wrong or
went stale on (see the correction table in each section and the summary table at the end).

**Also corrected up front:** the litefactory codebase's own migration comments (`0001_init.sql`,
`segments.go`) refer to "LF-8" as the feature that will let a session grow more than one segment
(multi-turn resumption / comment coalescing). That is **litefactory's own internal milestone
numbering**, unrelated to and pre-dating GitHub issue #8 ("Add Codex as second party item") — the
shared digit is coincidence between two independent numbering schemes. Nothing in those comments
is Codex-aware groundwork; do not read them that way. `[V]`

---

## Request

> "re-do the research, please. also, the research brief did not land; basically, how i would
> differentiate between codex and claude is by having a label that says 'Coding Agent: Codex |
> Claude' and every turn it would be re-read and thus routed. I think we also need to adjust the
> agents (codex uses agents.md) and another thing - we have currently the modles such as 'Opus' in
> the agents, so we need to figure out how to deal with that. basically, i would expect to have an
> interface that i can exchange and we use the interface necessary (from a factory, since we might
> use different agents and later we also add opencode, ...!)" `[O]`

Unpacked into four investigation threads, each covered as its own section below:

1. A GitHub label, re-read every turn, selecting which coding-agent CLI runs.
2. `AGENTS.md` (Codex's convention) vs `CLAUDE.md` and the four-mode team it drives.
3. Model names (`opus`, `sonnet`) baked into `.claude/agents/*.md` frontmatter, and what a
   tool-neutral equivalent would look like once Codex (GPT-series) and later OpenCode are in the
   mix.
4. A pluggable-runner interface with a factory, sized for at least three implementations
   (Claude Code, Codex, OpenCode).

---

## 1. Per-turn label-based routing

### The re-read-every-turn mechanism already exists, almost verbatim
- `internal/daemon/tools.go:29` — prefix `lf:tools=`.
- `resolveTools(labels []string) toolDecision`, `internal/daemon/tools.go:120-188`. Order of
  checks: no matching prefix → silent pass-through zero-value; more than one matching label
  (case-insensitive) → `report.ReplyAmbiguousToolLabels`; malformed value (must match
  `toolProfileName`, `^[a-z0-9][a-z0-9_-]{0,39}$`, `tools.go:40`) → `report.ReplyMalformedToolLabel`;
  unknown profile key → `report.ReplyUnknownToolProfile`; otherwise resolved, and the profile's
  `Tools`/`AllowedTools` slices are copied rather than aliased (`tools.go:178-187`) specifically so
  a downstream append-in-place can't corrupt the operator's shared map. `[V]`
- **It is explicitly documented as read fresh, not cached**: `tools.go:88-89` — "The labels are the
  tick's fresh snapshot and are read again on every tick; nothing here is cached, and no API call
  is made for them." `[V]`
- Call sites: `internal/daemon/tick.go:1382` (`tools := d.resolveTools(t.issue.Labels)`), reached
  once per drained comment via `handleComment` (`tick.go:1268-1314`, call at `:1313`) and once for
  the issue-body-triggered first run via `runIssueBody` (`tick.go:1074-1125`, call at `:1124`). `[V]`
- **Granularity nuance, documented as deliberate**: this is "per tick," not strictly "per turn" —
  `tick.go:1379-1381`: "The labels are this tick's snapshot, so two comments drained in one tick
  share it." If two comments (two conversational turns) land in a single daemon tick, they share
  one label read. `[V]`
- Labels themselves are never persisted in the store (checked the full `0001_init.sql` schema — no
  labels column anywhere) and are re-fetched from GitHub every tick, either through the tick's
  `ListIssues` (`tick.go:281`) or a lazy `GetIssue` in `materialise` (`tick.go:891-902`), populated
  via `internal/github/issues.go:67-77` / `labelNames` at `:260`. `[V]`

**Conclusion:** the plumbing the requester is asking for — "re-read a label every turn and act on
it" — is not hypothetical; it is the exact shape `lf:tools=` already uses today, down to the
ambiguity/malformed/unknown-key refusal ladder. A second prefix (e.g. `lf:agent=codex` /
`lf:agent=claude`, or however Plan mode chooses to spell "Coding Agent: Codex | Claude" as an
actual label string — see open question below) can reuse `resolveTools`'s exact control flow as
direct precedent. **What does not exist is anywhere for that decision to go once resolved** — see
below.

### What breaks if the label value changes mid-conversation
This is the load-bearing risk, and it is deeper than "which binary gets exec'd."

- `internal/store/migrations/0001_init.sql:73-80` — `segments.claude_session_id TEXT NOT NULL`. `[V]`
- `internal/store/segments.go:26-30` — field `ClaudeSessionID`, documented as "the value passed to
  `claude --session-id`." `[V]`
- `internal/naming/session.go:56-70` — `ClaudeSessionID(ref, segmentOrdinal)` **derives** a
  deterministic UUIDv5 over `"<ref>/segment/<ordinal>"` in a pinned namespace, computed by
  litefactory *before* any CLI runs. `[V]`
- `internal/turn/prepare.go:109-123` computes/stores this id **unconditionally, every turn**, with
  no branch on which agent will run. Segment ordinal is always 1 today (migration comment: "M0
  always creates exactly one segment per session… and never advances past this" — the unrelated,
  unbuilt "LF-8 multi-turn session" feature noted above is what would change that). `[V]`
- `internal/turn/prepare.go:190-193` decides cold-vs-resume purely from whether **any** prior run
  of this session exists, not from what tool produced that prior run. `[V]`
- Compare to Codex: per web research, Codex's session/thread id runs the **opposite direction** —
  it is emitted by the CLI at run start and discovered from output; it cannot be supplied upfront
  the way litefactory pre-computes `ClaudeSessionID`. `[W]`, and the exact ID format/where it
  surfaces (stdout vs a file) is unconfirmed even in web research — an open question in its own
  right, not just a litefactory-side gap.

**Concretely:** turn N runs Claude under label value `claude`. Turn N+1, label flips to `codex`.
`prepare.go` sees a prior run exists → decides "resume" → hands a Codex runner a
`harness.Request{Path: PathResume, ClaudeSessionID: <the pre-computed Claude UUID>}`. That UUID is
not a Codex thread id, was never produced by Codex, and Codex has no mechanism to accept it as a
resume token. **This is a new, concrete failure mode the requester's "re-read every turn" design
introduces**, distinct from anything in the first pass — cold/resume selection and session-id
derivation are today conditioned on "does history exist," never on "which tool produced it." `[V]`

Additionally: `internal/turn/harvest.go:97-146` (`runClaude`) is single-binary-shaped by
construction — it hard-codes a rerun-cold-on-refusal protocol keyed on
`harness.ErrSessionNotResumable`, calling `opts.Claude.Run` twice inline. There is exactly one
`Runner` slot in `turn.Options` (`internal/turn/execute.go:67-69`, field `Claude Runner`), wired
once at daemon startup (`internal/daemon/setup.go:157-234`). **No per-turn selection between two
runners exists anywhere in the tree today** — the label-*reading* half is proven-out precedent; the
routing-to-a-second-runner half is entirely unbuilt. `[V]`

**Worth flagging as a small but real positive:** the field is already named `Runner`, and its type
in `turn.Options` reads as an interface slot rather than a concrete struct pointer. Plan mode
should read the `Runner` interface's actual method set directly (not assumed here) — if it already
abstracts over "run something and get a result," adding a second implementation may be more
tractable than the Request/Result *data shape* suggests. That data shape, however, already leaks a
vendor-specific name into what should be a neutral boundary: `harness.Request` carries a field
literally named `ClaudeSessionID`, not e.g. `SessionRef` or `ResumeToken`. An interface with a
sound method set sitting on top of a request struct with a Claude-named field is only half a seam
— see section 4.

### Correction vs. the first pass
The first pass did not address the label-mechanism question at this level of specificity — it
treated `lf:tools=` and any agent-routing label as parts of one general "coupling surface." This
pass finds the label-reading precedent is strong and directly reusable, while the session-identity
incompatibility is sharper and more concrete than "Claude and Codex have incompatible session-id
semantics" — it is specifically that litefactory computes the id **before** running anything and
Codex's id can only be **discovered after**, which is a directional mismatch, not just a format
mismatch.

---

## 2. `AGENTS.md` vs `CLAUDE.md` and the team mechanism

### What exists today
- No `AGENTS.md` exists anywhere in this repository (confirmed by direct search). `[V]`
- `CLAUDE.md` at repo root is the mode router: classifies a request into Research / Plan /
  Implement / Spike, gates each mode on a prerequisite artifact, scaffolds a session directory, and
  routes to exactly one of four orchestrator sub-agents — it never invokes a specialist directly.
  States explicitly "You route; you do not work" and "One Run, One Mode." `[V]`
- `.claude/agents/*.md` frontmatter configures **real runtime mechanism**, not prose alone: every
  agent file declares `name:`, `description:`, `model:`; several also declare `tools:` (an enforced
  restriction on that agent's tool surface). Confirmed examples: `web-researcher.md` →
  `tools: WebSearch, WebFetch, TodoWrite`, `model: sonnet`; `research-orchestrator.md` →
  `tools: Read, Write, Edit, Task, TodoWrite`, `model: sonnet`; `requirements-engineer.md` →
  `tools: Glob, Grep, Read, Write, TodoWrite`, `model: opus`; `go-architect.md` → `model: opus`, no
  `tools:` override (inherits the default surface). `[V]`
- `.claude/settings.json` wires a `PreToolUse` hook on the Bash matcher to
  `check-test-env.sh`, which **blocks** (exit 1) integration-test invocations unless specific env
  vars are set — an enforced gate at invocation time, not a request written into a prompt. `[V]`

### What a plain-prose `AGENTS.md` cannot replicate
Three concrete gaps, not one:

1. **Subagent delegation** — Claude Code's Task tool gives an orchestrator an isolated context
   window, a restricted tool surface, and a model override, invoked programmatically per call. A
   single merged instructions file has no unit smaller than "the whole document" to delegate a
   sub-scope to.
2. **Hooks** — the `PreToolUse` gate above is enforced by the harness at tool-invocation time. A
   prose instruction ("please don't run integration tests without X set") is not an enforcement
   mechanism.
3. **Per-agent model routing** — `opus` for `go-architect`/`requirements-engineer`, `sonnet`
   elsewhere, is a property of *which specialist is running*, not of the session as a whole. One
   `AGENTS.md` file has no per-section model dial. `[V]`

**On direct re-inspection this run, the first pass's core claim here holds**: the four-mode team
(orchestrators + subagents + hooks) is a Claude Code-specific delegation mechanism, and `AGENTS.md`
alone — as a filename convention for "read this file, prepend it as instructions" — could not
reproduce it. `[V]`

### But this needs an important qualifier the first pass did not have
Web research this run (re-verifying, since the first pass flagged single-sourcing here) finds that
**Codex CLI is not architecturally a single flat agent** as of its current (2026) feature set:

- **`AGENTS.md`** is, on the Codex side, purely a discovery-and-concatenation mechanism — Codex
  walks the directory tree, finds `AGENTS.md` / `AGENTS.override.md` at each level, and
  concatenates them root-to-leaf as narrative instruction text. No special syntax for declaring
  agents, personas, or routing rules. `[W]`
- **Subagents are a separate, genuinely distinct feature** (a documented `/agent` slash command):
  Codex "can run subagent workflows by spawning specialized agents in parallel and then collecting
  their results in one response," and this is reportedly enabled by default, not opt-in. A subagent
  inherits the parent's model/reasoning-effort unless explicitly overridden. `[W]`, medium-high
  confidence, but resting on a small number of documentation-page fetches, not a raw page dump
  verified verbatim, and **never confirmed against a running `codex` binary** (none was executed in
  this environment — see below).
- **Hooks are also separate**: shell scripts triggered on lifecycle events
  (`SessionStart`, `SessionEnd`, `SubagentStart`, `SubagentStop`, `PreToolUse`, `PermissionRequest`,
  `PostToolUse`, `PreCompact`, `PostCompact`, `UserPromptSubmit`, `Stop`) via `~/.codex/hooks.json`
  or `config.toml`, disable-able via `[features] hooks = false`. Community sources (not the primary
  docs) date `PreToolUse`/`PostToolUse` to a specific recent version range and note `PreToolUse`
  reportedly intercepts only the shell/Bash-equivalent tool, not apply-patch/edit/read/web-fetch/MCP
  tool calls. `[W]`, lower confidence, version numbers single-sourced from community posts.

**This is a genuine tension between the two research threads in this brief, and it should be
carried into Plan mode as an open question rather than resolved here**: codebase-tour's finding
that "Claude's team mechanism has no Codex equivalent" is correct about `AGENTS.md`-as-a-filename,
but if the subagents/hooks findings hold up, Codex has *its own*, differently-shaped delegation and
enforcement primitives — just not exposed through `AGENTS.md` itself, and not exposed the same way
(a slash command a human/agent invokes explicitly, versus Claude Code's programmatic Task-tool
delegation driven by orchestrator code). Whether Codex's `/agent` subagent feature and hooks are
rich enough to host something equivalent to litefactory's four-mode team is **not something this
brief can settle** — it needs a running `codex` binary to probe, which does not exist in this
environment (confirmed: zero `.go` files in the tree reference "codex" at all, and `which codex`
could not be run this pass — see section 4's Codex-availability note).

### Correction vs. the first pass
The first pass's flat "no Codex equivalent" reads as an overstatement in light of this run's web
research. The corrected framing: `AGENTS.md` itself is thin (a text-concatenation convention), but
Codex CLI as a whole is not flat — it has separate subagent and hook primitives whose depth
relative to Claude Code's is genuinely unverified, not merely "absent."

---

## 3. Model naming leaking into agent definitions

### What's actually in this repo
- Grep of `.claude/agents/*.md` frontmatter for `model:` finds exactly two distinct values:
  `opus` (`requirements-engineer.md`, `go-architect.md`) and `sonnet` (all 11 other agent files). `[V]`
- Recent commit subjects (from git log context) name a pattern of pinning specific agents to
  `sonnet` — "agents: go-implementation-engineer runs on sonnet," "agents: codebase-tour runs on
  sonnet," and similar for orchestrators/web-researcher. **The commit bodies/diffs themselves could
  not be independently re-verified this run** (the codebase-tour pass had no shell access this
  invocation) — this is a genuine gap, not a settled finding, and Plan mode should re-run `git show`
  on those commits before relying on stated rationale. `[V]` for the frontmatter state as it stands
  today; **unconfirmed** for the historical rationale.

### A correction that changes the shape of this problem
The first pass stated flatly, tagged as verified, that **no `--model` flag is ever constructed
anywhere in the tree**. Direct re-verification this run finds that claim is **wrong as of the
current tree state**:

- `internal/harness/runner.go:500-506` unconditionally appends `"--model", r.model` to argv. `[V]`
- `r.model` traces to `opts.Claude.Model` (`runner.go:131`) → `config.Claude.Model`
  (`config.go:159`) → default `DefaultClaudeModel = "sonnet"` (`config.go:262`), validated by
  `modelNameRE` (`internal/config/policy.go:21-43`, `^[A-Za-z][A-Za-z0-9._-]*$`). `[V]`
- The doc comment at `config.go:146-158` is explicit about scope: "It sets the session's model and
  nothing else: a sub-agent whose definition carries its own `model:` frontmatter still runs on
  that one… Empty means the default, not 'omit the flag'. litefactory always passes `--model`." `[V]`

**Either this landed in the single day between the two research passes, or the first pass
mis-verified it — either way, treat the first pass's claim here as superseded.** The practical
consequence: litefactory already threads a configurable, validated model-name string into argv on
every single run, unconditionally. This is the closest existing precedent for "a per-run string
selecting a variant of what executes" — but note it selects a *Claude model tier for the whole
session*, which is a **different mechanism** from the `.claude/agents/*.md` `model:` frontmatter,
which lets an individual **delegated sub-agent** override the model for just that delegated call.
Both exist; they answer different questions ("what model does this whole run use" vs "what model
does this one specialist use"), and both are Claude-vocabulary today (`sonnet`, `opus`, and the
`--model` flag's string literally being a Claude Code CLI argument name).

### Codex's own vocabulary (web-sourced, re-verified this run)
- Current model family: **GPT-5.6**, with three named capability tiers — `gpt-5.6-sol` (flagship),
  `gpt-5.6-terra` (balanced), `gpt-5.6-luna` (fastest/cheapest) — plus a text-only preview,
  `gpt-5.3-codex-spark`. Older models (5.5, 5.4, 5.4-mini) are stated to retire Aug 31, 2026. `[W]`,
  high confidence on naming/existence.
- Pinning is via `--model`/`-m` flag or `model = "gpt-5.6-terra"` in `config.toml`. `[W]`
- Per web research, **subagents inherit the parent's model/reasoning-effort unless explicitly
  overridden** — meaning per-role model-tier pinning is reportedly supported on the Codex side too,
  structurally similar in shape (though different in mechanism and vocabulary) to Claude Code's
  `model:` frontmatter on a delegated sub-agent. `[W]`, medium confidence, single-sourced.

### The actual design problem, stated plainly
"Model" as a config vocabulary term is currently three overlapping but distinct things in this
repo: (a) the harness's `--model` flag selecting a Claude session-wide tier (`sonnet` by default),
(b) `.claude/agents/*.md` frontmatter selecting a per-delegated-specialist tier (`opus`/`sonnet`),
and neither is abstracted away from Claude's own naming scheme. Codex has its own three-tier
naming (`sol`/`terra`/`luna`) with what looks like the same two-axis shape (session-wide vs
per-subagent). **What would not survive unchanged into a multi-tool world is the literal string
vocabulary** (`opus`, `sonnet` vs `gpt-5.6-sol`, `-terra`, `-luna`) — a specialist definition that
says `model: opus` is meaningless once the tool running it might be Codex or (later) OpenCode.

A workable direction, offered here as a design sketch for Plan mode to evaluate rather than a
decision: introduce a **tool-neutral capability tier** in agent/specialist definitions (e.g.
`tier: flagship | balanced | fast`, or reuse `high`/`standard`/`low`) with a small mapping table
per concrete tool implementation (`flagship → opus` for Claude, `flagship → gpt-5.6-sol` for
Codex), resolved at dispatch time by whichever implementation the factory selects (section 4). This
keeps agent definitions vendor-neutral while still letting each tool's own naming evolve
independently (Codex's tier names alone are due to partially retire by the end of this month per
the web research above — a vocabulary layer would insulate agent definitions from that churn too).

### `internal/config`'s existing groundwork and its constraint
- `internal/config/executable.go:78-113` — `ResolveExecutable(key, name, pathEnv)` is already
  generic over a config key and a binary name; `Claude.ResolveBinary` is a one-line wrapper
  hardcoding `"claude.binary"`. Real, reusable, unchanged groundwork for "resolve a configured
  binary path," not specific to model naming. `[V]`
- `internal/config/policy.go` has **no notion of "agent tool" tiering at all** — `defaultTools()`,
  `defaultAllowedTools()`, `toolRuleRE`, `profileKeyRE` are entirely about the `--tools`/
  `--allowedTools` **permission** surface, keyed by `lf:tools=` labels, never about which binary
  runs or which model it uses. `ToolProfile` has exactly two fields (`Tools`, `AllowedTools`); a
  profile name never reaches argv as a model or binary selector. `[V]`
- `internal/config/purity_test.go:29-73` (`TestValidationHasNoThirdPartyImports`) enforces a
  stdlib-only invariant on `internal/config`, with one named exception (`gopkg.in/yaml.v3` in
  `config.go`). **Consequence for Codex config**: if Codex's config needs a different format (e.g.
  TOML, matching its own `config.toml` convention), that decoder cannot live inside
  `internal/config` — it must either standardize on YAML (joining the one existing exception) or
  live in a separate package that hands `internal/config` only a resulting typed struct. `[V]`
- `internal/config/purity_test.go:82-124` (`TestForbiddenFlagIsAbsentFromTheTree`) is a repo-wide
  grep guarding against an assembled "skip permissions" flag string. A Codex equivalent (its
  sandbox-bypass flag, if one exists) would need the same protection once Codex code exists in the
  tree — it doesn't exist yet, confirmed by zero `.go` files referencing "codex" anywhere. `[V]`

### Correction vs. the first pass
The `--model` flag correction above is the single most consequential fix in this redo — the first
pass's design reasoning proceeded from "model selection doesn't exist in argv today," when in fact
it exists, is unconditional, and is validated by regex. Any Plan-mode design must account for a
flag that is *already* being passed on every run, not one that needs to be added from scratch.

---

## 4. Interface/factory design for pluggable agent tools

### The exact I/O shape `internal/harness` constructs today (re-verified, not copied from the first pass)
- **argv construction**: `internal/harness/runner.go:481-515`,
  `func (r *Runner) args(req Request) []string`. Fixed order: `-p`;
  `--output-format stream-json`; `--verbose`; `--session-id`/`--resume <id>`;
  `--tools <csv>` (unconditional — an empty value here means allow-all, not "omit the flag");
  `--settings <path>`; `--model <r.model>` (unconditional, see section 3);
  optional `--allowedTools <csv>` when non-empty; `--strict-mcp-config` always last.
  `Runner.surface(req)` (`:534-539`) selects which of two tool-list pairs argv is built from. `[V]`
- **prompt delivery**: `internal/harness/prompt.go:313`, `BuildPrompt(in PromptInput) (string,
  error)` — a pure function. Per `doc.go:29-31`, the prompt is delivered on **stdin**, never in
  argv (an invariant tagged `AC-3.12` in the doc comment). `[V]`
- **env allowlist**: `internal/harness/env.go` — `:40` fixed `envAllowlist` slice (never
  `os.Environ()` wholesale); `:68` `AllowedEnvNames()`; `:53` `AnthropicEnvPrefix = "ANTHROPIC_"`;
  `:58` `fallbackPATH`; `:63` `secretNameRE`; `:88` `buildEnv(parent, forwardAnthropic)`. `[V]`
- **session/resume**: `naming.ClaudeSessionID` (`internal/naming/session.go:56-70`), stored
  `NOT NULL` (`0001_init.sql:77`), cold-vs-resume decided in `prepare.go:190-193`, a
  refusal-and-rerun protocol in `harvest.go:92-146`. `[V]`
- **output stream parsing**: `internal/harness/stream.go` — `:35-39`
  `eventTypeSystem`/`eventTypeResult`, `subtypeInit`; `:43` `eventEnvelope{Type, Subtype}`; `:51`
  `initEvent{Model, Tools, SessionID, CWD, PermissionMode, APIKeySource}`; `:65`
  `resultEvent{IsError, Result, TotalCostUSD, DurationMS, NumTurns}`; `:97` `consume()`; `:145`
  `observe()` — a two-event-kind interpreter; everything else is counted, not read. The reader is a
  `bufio.Reader.ReadBytes` with a 64KiB starting buffer, **deliberately not `bufio.Scanner`**
  (`doc.go:106-109` — Scanner returns `ErrTooLong` and silently aborts on over-long lines; a
  72,411-byte line was measured in practice). `[V]`
- **exit code handling**: `internal/harness/result.go:12-62` (`Outcome` enum, `SignalExitCode =
  -1`); `RunResult` struct `:73-156` with `Succeeded()`/`Clone()`. `[V]`

### Codex's corresponding shape (web-sourced, never executed here)
- Invocation: `codex exec "<prompt>"`. Default: progress on stderr, final message on stdout. With
  `--json`, stdout becomes JSON Lines with event types `thread.started`, `turn.started`,
  `turn.completed`, `item.completed`, `error`. `-o <file>` writes the final message to a file too;
  `--output-schema <file>` constrains the final message to a JSON schema. Related flags:
  `--ephemeral`, `--sandbox workspace-write|danger-full-access`, `--ignore-user-config`,
  `--ignore-rules`, `--skip-git-repo-check`. `[W]`, medium-high confidence on flag names.
- Exit codes: `0` on success, non-zero on a `turn.failed`/`error` event or a required MCP server
  failure. `[W]`, medium confidence — inferred from multiple community sources agreeing, not a
  directly quoted spec line.
- Session/resume: `codex exec resume --last "<prompt>"` or `codex exec resume <SESSION_ID>
  "<prompt>"`. Transcripts persist under `$CODEX_HOME` (default `~/.codex`) unless `--ephemeral`.
  **The exact session-ID format and where it is surfaced (stdout? a file?) could not be confirmed**
  from an official source this run — an explicit open question, not an assumption to design
  against. `[W]`, low confidence on this specific point.
- Env/config: `CODEX_HOME` holds `config.toml` and credentials; `CODEX_API_KEY` documented for
  CI/non-interactive auth, `CODEX_ACCESS_TOKEN` for ChatGPT Enterprise. Whether `OPENAI_API_KEY`
  still works as a fallback, and whether Codex's sandbox filters env vars passed to spawned
  subprocesses, **could not be confirmed from a primary source** this run — flagged, not guessed
  at. `[W]`, low confidence on both points.
- Structured output field shapes: only the JSON Lines **event-type names** above are confirmed; the
  exact per-event field schema (what `item.completed` actually carries) could not be verified from
  a directly-opened page. **Do not write a Go parser against an assumed field shape — this needs
  direct verification against a running `codex exec --json` invocation first.** `[W]`, low
  confidence on field-level detail specifically.

### Package-map / dependency-arrow grounding (re-verified against the current, much shorter, `APP_CONTEXT.md`)
Note: the first pass cited `APP_CONTEXT.md` line ranges (`:405-424`, `:342-362`, `:273-274`) that no
longer exist — the file has since been rewritten down to roughly 170 lines. **Any brief or plan
citing the old line numbers is citing a document that no longer has those passages; this section
re-cites against the current file.** `[V]`

Current dependency arrows (`APP_CONTEXT.md:43-67`):
```
harness → config, naming
report  → git, harness, naming, redact
turn    → config, git, github, harness, naming, redact, report, store
daemon  → config, git, github, harness, naming, redact, report, repos, secrets, store, turn
```
`[V]`

**A nuance the first pass's blast-radius table did not surface: `internal/report` imports
`internal/harness` directly**, not only reachably through `turn` — confirmed both from the arrow
above and a direct grep of the import string across the 26 files that import `internal/harness`,
including `internal/report/run.go`. Any generalization of the harness boundary is a two-front
change (`report` and `turn` both consume it directly), not a one-front change reached only through
`turn`. `[V]` A binding constraint noted alongside it (`APP_CONTEXT.md:77-83`): `report` cannot
import `turn` (would cycle, since `turn` imports `report` to render replies) — this constrains
where any Codex-specific reply-rendering logic could physically live.

### Other places assuming Claude is the only vendor, outside `internal/harness` (re-verified, all still current)
- `internal/repos/aiteam.go:19,27,43` — `MarkerClaudeMD = "CLAUDE.md"`, `claudeMDName =
  "CLAUDE.md"`, `presenceMarker = "aiteam"`. This is **aiteam-detection** coupling: a repository
  that only has an `AGENTS.md` (no `CLAUDE.md`) cannot be registered via `repo add` today. `[V]`
- `internal/cli/router.go:15` — the CLI's own tagline hardcodes "runs Claude Code in git
  worktrees." `[V]`
- `internal/cli/cmd_logs.go:398,469` — a `ClaudeSessionID` JSON field and a rendered "claude
  session" label in log output. `[V]`
- `internal/report/run.go:377,422,429,445,564` — four vendor-named prose strings in run reports
  (e.g. `` `claude` exited with status ``), plus a `HasResult && CostUSD > 0` gate controlling
  whether a Cost line renders at all. `[V]`
- `internal/config/config.go` — the `Claude` struct (`:140-202`) holds every field under a `claude:`
  YAML key; the Go type is literally named `Claude`. **No generic `Agent`/`Tool` type exists
  anywhere in `internal/config` today.** `[V]`

**None of these sit behind `internal/harness`.** An interface drawn at the harness boundary alone —
however clean — does not touch any of them. Each needs its own seam: the store schema needs a
tool-discriminator column (or a polymorphic session-identifier type) alongside whatever replaces
`claude_session_id`; `internal/report`'s prose and cost-gating need to stop assuming one vendor's
result shape; `internal/cli`'s log rendering and router tagline need to stop assuming one vendor's
field names; `internal/config` needs an `Agent`/`Tool`-shaped type distinct from `Claude`; and
`internal/repos`' aiteam-presence detection needs to accept `AGENTS.md` as an alternative marker if
Codex-only repos are to be onboardable at all.

### What a candidate interface would need — offered as a design sketch, not a decision
Grounded in the I/O shape above, a `Runner`-shaped interface sitting where `turn.Options.Claude
Runner` already sits would need, at minimum:
- A method to run a request and return a normalized result — but the **data types crossing that
  boundary already leak vendor identity**: `harness.Request` carries a field literally named
  `ClaudeSessionID`, not something neutral like `SessionRef`. A real interface needs the *data
  shape*, not just the method signature, generalized — a `SessionRef` (or similar) carrying both an
  opaque token and a discriminator for which tool produced it, since (per section 1) Claude's token
  is pre-computable and Codex's is only discoverable after the fact — two genuinely different
  session-identity *directions*, not just two different string formats.
- A prompt-delivery method — likely reusable as-is in shape (both tools appear to accept a prompt
  via stdin/argument), though Codex's exact acceptance mechanism for a prompt build like
  litefactory's `BuildPrompt` output is unverified.
- An env-construction method — the allowlist *mechanism* (fixed slice, never `os.Environ()`
  wholesale) generalizes cleanly; the *prefix* (`ANTHROPIC_` vs whatever Codex needs, plausibly
  `CODEX_`/`OPENAI_` per the env findings above, themselves unconfirmed against a primary source)
  does not.
- An event-stream-to-normalized-result method — Claude's two-event-kind interpreter
  (`system`/`result`) and Codex's five-event-kind JSON Lines stream
  (`thread.started`/`turn.started`/`turn.completed`/`item.completed`/`error`) would both need to
  map onto one shared internal event/outcome vocabulary. This is plausible in shape but the exact
  Codex field-level schema is unverified (see above), so the mapping itself is speculative until
  that is resolved.
- An exit-code/outcome mapping method — both tools appear to use conventional 0/non-zero exit
  codes, though Codex's non-zero cases are inferred from community sources, not confirmed from a
  spec.

**Where the factory would live and what selects an implementation**: the natural selector is the
same per-turn label mechanism from section 1 (`lf:agent=` or equivalent), read through the same
`resolveTools`-shaped ladder, resolved once per tick alongside the existing tool-profile decision —
i.e. the same mechanism, not a separate one, since both are "read a label this tick, resolve to a
typed decision, refuse ambiguity/malformed input the same way." The factory itself would sit
wherever the `Runner` is currently wired at daemon startup (`internal/daemon/setup.go:157-234`),
selecting a concrete implementation based on that resolved decision rather than always constructing
the one Claude `Runner`.

**How much of this is genuinely speculative, stated plainly**: all of the interface method-set
sketch above is paper design against a tool that has never been executed in this environment. No
`codex` binary was run this pass — confirmed by grep that zero `.go` files anywhere in the tree
reference "codex," and `which codex` could not be executed this pass (the codebase-tour sub-agent
had no shell access this invocation). **The same measurement caveat the first pass carried forward
still applies and is repeated here, not softened**: every claim about Codex's actual behavior is
`[W]`, second-hand, and should be converted to direct measurement (a read-only probe run against a
real `codex` binary) before any of this interface design is finalized.

### `AC_AMENDMENTS.md` — one correction to the first pass
Grepped this run for "codex," "agent tool," "second tool," "which tool," "multi-tool," "runs.owner,"
"tool column," "agent column" — **zero hits for anything multi-tool-specific.** The one hit,
`runs.owner` (`AC_AMENDMENTS.md:1286-1313`, "Rejected, not deferred: an owner column on runs"), is
about which daemon/worker process owns a currently-running row for mid-life reclaim purposes (ruled
out by a different, unrelated decision, "RD-7") — a different axis entirely from "which CLI tool
produced a completed run." **The first pass cited this ruling as precedent relevant to an
agent/tool discriminator column; it is not, and should not be cited either for or against such a
column going forward.** `[V]`

---

## Constraints & Risks

1. **Session-identity direction mismatch** (section 1): litefactory pre-computes a session id
   before running anything; Codex's id can only be discovered after a run starts. A label that
   changes mid-conversation would hand a Codex runner a Claude-shaped resume token it has no way to
   honor. This is a design blocker for "re-read every turn," not a cosmetic gap — resolving it
   requires the store schema and `prepare.go`'s cold/resume decision to become tool-aware, not just
   the label-reading path.
2. **Single `Runner` slot, wired once at daemon startup** (section 1, section 4): today there is
   exactly one, constructed once. Making it per-turn-selectable is new plumbing, not an extension
   of existing plumbing.
3. **Two independent direct importers of `internal/harness`** (`turn` and `report` — section 4):
   any interface change at that boundary is a two-front change.
4. **Claude-vendor assumptions live outside `internal/harness` too** (section 4): store schema,
   `internal/report`'s prose/cost-gating, `internal/cli`'s log rendering and tagline,
   `internal/config`'s `Claude`-named type, and `internal/repos`' `CLAUDE.md`-only detection. An
   interface at the harness boundary alone does not touch any of these.
5. **`internal/config`'s stdlib-only purity invariant** (section 3): a Codex config format other
   than YAML cannot live inside `internal/config` as-is.
6. **The team mechanism (orchestrators/subagents/hooks) is Claude Code-specific** (section 2), but
   Codex is reported (unverified against a real binary) to have its own, differently-shaped
   subagent and hook primitives — the relative depth of the two is a genuine open question, not a
   settled "Codex has nothing."
7. **Vendor-specific vocabulary in agent definitions** (section 3): `model: opus`/`sonnet` in
   `.claude/agents/*.md` frontmatter is meaningless once the executing tool might be Codex (whose
   own tier names, `sol`/`terra`/`luna`, are themselves due to partially retire within the month per
   web research) or, later, OpenCode.
8. **No `codex` binary has been executed in this environment at any point across either research
   pass.** Every Codex-side claim in this brief is `[W]`, second-hand from documentation, and
   several specific points (session-ID format/surfacing, exact JSON event field schema,
   `OPENAI_API_KEY` fallback, sandbox env-var filtering, exact hooks/subagents version history) are
   explicitly unconfirmed even at the documentation level, not merely unexecuted.

## Open Questions

1. What is the literal GitHub label string/scheme Plan mode should use — two separate labels
   (`Coding Agent: Codex` / `Coding Agent: Claude`) or one prefix+value pair analogous to
   `lf:tools=<profile>` (e.g. `lf:agent=codex`)? The requester's phrasing ("a label that says
   'Coding Agent: Codex | Claude'") is compatible with either reading.
2. How should cold-vs-resume and session-identity be redesigned so that a label change mid-session
   is either (a) safely refused with a clear message, or (b) safely honored by starting a new
   segment/session under the new tool rather than attempting to resume under the old one? This is a
   design decision, not a research fact, and belongs to Plan mode.
3. Does Codex's `/agent` subagent feature and hooks mechanism have enough depth to host something
   resembling litefactory's four-mode team, or only enough for much shallower delegation? Requires
   direct experimentation against a running `codex` binary — not resolvable from documentation
   alone (web research this run found the two primary sources' framing of "how much delegation
   Codex has" already in tension with the first pass's flatter characterization).
4. Exact Codex session-ID format and where it's surfaced (stdout/file) — unconfirmed from a primary
   source.
5. Exact per-event JSON field schema for `codex exec --json` — only event-type names confirmed.
6. Whether `OPENAI_API_KEY` still works as an auth fallback for Codex CLI, and whether/how Codex's
   sandbox filters env vars passed to spawned subprocesses — both unconfirmed from a primary
   source.
7. Rationale behind the three "runs on sonnet" agent-pinning commits — commit bodies were not
   re-read this pass (no shell access in the codebase-tour sub-agent invocation); Plan mode should
   `git show` them directly before treating their stated rationale as established.
8. Whether a `codex` binary is available on `PATH` in the actual execution environment — `which
   codex` was not run this pass (no shell access in the invoking sub-agent); zero `.go` files
   reference "codex," confirming no code exists yet, but binary availability itself remains
   unchecked.
9. Whether OpenCode (the explicitly named third tool) has per-subagent/per-role model-tier
   configuration analogous to what's sketched in section 3 — confirmed to exist as a project and to
   use the `AGENTS.md` convention, but its model-config mechanics were not confirmed from a
   directly-opened source.

## Recommended scope for Plan mode

Plan mode should cover:
- A concrete label scheme and its exact resolution ladder (reusing `resolveTools`'s shape),
  including the refusal behavior when the label changes mid-conversation in a way that would break
  session-resume.
- A store-schema change generalizing `segments.claude_session_id` to a tool-aware session
  identifier, and the corresponding change to `prepare.go`'s cold/resume decision to condition on
  which tool produced prior history.
- A `Runner`-shaped interface redesign whose **data types** (not just method signatures) are
  vendor-neutral — starting from `harness.Request`'s `ClaudeSessionID` field — plus a factory
  selecting a concrete implementation from the resolved label decision, wired where
  `daemon/setup.go` currently constructs the one Claude `Runner`.
- A tool-neutral capability-tier vocabulary for agent/specialist definitions, plus a per-tool
  mapping table, replacing the direct `model: opus`/`sonnet` values in `.claude/agents/*.md`.
- Explicit scoping decisions for the non-`harness` seams named in this brief (`internal/report`,
  `internal/cli`, `internal/config`, `internal/repos`) — whether they're in-scope for a first slice
  or deliberately deferred.

Plan mode should **not** attempt to design Codex's own delegation/hooks depth (open question 3) or
finalize the interface's event-mapping (open question 5) until a read-only probe against a real
`codex` binary has converted the `[W]`-tagged claims in this brief to `[V]` — consistent with the
first pass's own recommendation to slice a measurement step first, which this redo reaffirms rather
than overrides.

## Assumptions taken (headless run, no human available to ask)

- Treated the requester's comment as a statement of intent/shape, not as a specification to
  implement literally — e.g. did not assume the exact label string, only that it follows the
  existing `lf:` prefix convention as the most consistent reading.
- Where web research and the first pass's findings conflicted (the AGENTS.md/subagents/hooks
  question), carried both forward as an open tension rather than picking one, since neither is
  verified against a running binary.
- Did not attempt to run `which codex` myself (this orchestrator has no shell access); relied on
  the codebase-tour sub-agent's grep-based confirmation that no Codex code exists in the tree as
  the closest available signal, and left binary availability as an explicit open question.
