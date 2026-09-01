# Feature Spec: Codex as a second coding-agent tool
# Status: APPROVED
# Date: 2026-09-01

Source: `RESEARCH_BRIEF.md` (same session directory). Headless run — no human was
available; every judgement call the brief left open is resolved below and recorded in
[Assumptions & resolved policy decisions](#assumptions--resolved-policy-decisions).
`APPROVED` here means "complete and implementation-ready", not "a human said yes".

## Goal
Let an issue's GitHub labels select, per tick, which coding-agent CLI (Claude Code or Codex)
executes a turn, behind a vendor-neutral runner interface and a tool-aware session identity, so a
third tool (OpenCode) can be added later without re-cutting the seam.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As a maintainer, I want a read-only, reversible probe of the real `codex` binary recorded as `CODEX_CLI_CONTRACT.md`, so that argv, event-stream and session-id design rests on measurement rather than on documentation |
| US-2 | As an issue owner, I want an `lf:agent=<tool>` label re-read every tick to route the run to Claude or Codex, so that I can switch tools mid-thread without editing config or restarting the daemon |
| US-3 | As an issue owner, I want a tool switch on a session that already has history to be refused with a clear comment rather than silently resumed or silently restarted, so that a Claude conversation is never handed to Codex as a resume token it cannot honour |
| US-4 | As a maintainer, I want the store to record which tool owns a segment and to carry a tool-reported resume token separately from litefactory's own pre-computed identity, so that Codex's discovered-after-the-fact thread id and Claude's pre-computed UUID both fit without either being falsified |
| US-5 | As a developer, I want `Request`/`RunResult` and the `Runner` interface to live in a vendor-neutral package with no Claude-named fields, and a factory selecting the implementation from the resolved label, so that adding OpenCode is a new implementation and not a new seam |
| US-6 | As a maintainer, I want specialist agent definitions expressed in a vendor-neutral capability tier (`flagship`/`balanced`/`fast`) with a per-tool model mapping, so that `model: opus` stops being the source of truth once the executing tool might not be Claude |
| US-7 | As an operator, I want a `lf:tools=` permission profile to carry an explicit per-tool permission mapping and to refuse a run under a tool it has no mapping for, so that a grant written for Claude's `--tools` is never silently reinterpreted as a Codex sandbox mode |
| US-8 | As a Codex user of this repository, I want a root `AGENTS.md` carrying the tool-independent working rules, so that a Codex run in this repo is not operating with zero project instructions |

### Story order and gating

| Story | Depends on | Gate |
|-------|-----------|------|
| US-1 | — | Must land and be merged first. No other story may be implemented until `CODEX_CLI_CONTRACT.md` exists and its `[W]` → `[V]` conversion table is complete |
| US-4 | US-1 | Schema shape depends on measured Codex session-id format/length |
| US-5 | US-1, US-4 | Event mapping and `ResumeToken` semantics depend on measured event schema |
| US-2 | US-5 | Routing needs somewhere to route to |
| US-3 | US-2, US-4 | Refusal needs both the label decision and the segment's `agent` column |
| US-6 | US-1 | Codex tier names must be confirmed against `codex --help` / config, not web research |
| US-7 | US-1 | Codex sandbox flag values must be measured before they become a config enum |
| US-8 | US-1 | Content depends on whether Codex has usable subagent/hook primitives |

## Data Model

### Store — migration `0002_segment_agent.sql`

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| segments | id | INTEGER | yes | PK, unchanged |
| segments | session_id | INTEGER | yes | FK → sessions(id) ON DELETE CASCADE, unchanged |
| segments | ordinal | INTEGER | yes | UNIQUE(session_id, ordinal), unchanged |
| segments | session_ref | TEXT | yes | NOT NULL. **Renamed from `claude_session_id`** via `ALTER TABLE segments RENAME COLUMN`. litefactory's own pre-computed identity (`naming.ClaudeSessionID`, UUIDv5, pinned namespace). Computed for every tool. For Claude it is additionally the value passed to `--session-id` |
| segments | agent | TEXT | yes | NOT NULL. Discriminator: `claude` \| `codex`. Existing rows backfilled to `claude` in the same migration. Written once, at segment creation; never updated |
| segments | resume_token | TEXT | no | NULLABLE. The **tool-reported** handle used to continue this conversation, written after the first successful run of the segment. NULL means "no successful run yet, or the tool reports none" |
| runs | agent | TEXT | yes | NOT NULL, `claude` \| `codex`, backfilled to `claude`. Which tool produced this run. Needed for reports and `litefactory logs` without a join through segments |

Migration mechanics: three `ALTER TABLE` statements plus two `UPDATE` backfills in one file, one
transaction, no table rebuild. `RENAME COLUMN` requires SQLite ≥ 3.25 — satisfied by
`modernc.org/sqlite`. No new migration library (explicitly rejected dependency, per `APP_CONTEXT.md`).

### `internal/agent` — new leaf package (vendor-neutral types)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Kind | — | string | yes | Closed set: `claude`, `codex`. Validated at config load and at label resolution. `String()` is the label value and the stored column value |
| SessionRef | Kind | Kind | yes | Which tool owns the conversation |
| SessionRef | Ref | string | yes | litefactory's pre-computed identity (`segments.session_ref`) |
| SessionRef | ResumeToken | string | no | Tool-reported; empty on a cold run |
| Request | Ref | naming.Ref | yes | unchanged from `harness.Request` |
| Request | RunOrdinal | int | yes | ≥ 1 |
| Request | SegmentOrdinal | int | yes | ≥ 1 |
| Request | Session | SessionRef | yes | **Replaces `ClaudeSessionID string`**. Per-tool validation moves into the implementation |
| Request | Mode | string | yes | non-blank |
| Request | Path | RunPath | yes | `""`\|`cold`\|`resume`\|`cold_after_resume_refused` — unchanged values |
| Request | WorktreePath | string | yes | absolute; the task's worktree |
| Request | Prompt | string | yes | delivered on the child's stdin, never argv (AC-3.12 invariant preserved for both tools) |
| Request | Permissions | Permissions | no | see below. Zero value = "no profile resolved, use the runner's configured surface" |
| RunResult | Outcome | Outcome | yes | `succeeded`\|`failed`\|`timed_out`\|`abandoned`\|`no_result` — unchanged set, moved package |
| RunResult | ResumeToken | string | no | **New.** The tool-reported handle for continuing this conversation. Claude sets it to the session id it was given; Codex sets it from the measured thread-id source (US-1) |
| RunResult | Reason, ExitCode, LogPath, Command, Model, Tools, ResultText, CostUSD, IsError, NumTurns, HasResult, Events, Malformed, LongestLine, Stderr, StreamError, StartedAt, FinishedAt, Duration | as today | — | Moved verbatim from `harness.RunResult`. `SessionID string` is replaced by `Session SessionRef` |
| Permissions | Profile | string | no | Operator-authored profile name, already shape-validated. Logging/metadata only; never joined into argv |
| Permissions | ClaudeTools | []string | no | `--tools` surface. Read only when Kind is `claude` |
| Permissions | ClaudeAllowedTools | []string | no | `--allowedTools`; read only when `ClaudeTools` is non-empty |
| Permissions | CodexSandbox | string | no | Codex sandbox mode. Value enum fixed by US-1 measurement. `danger-full-access` (or whatever the measured bypass value is) is never constructible — see Business Rules |

### `internal/config`

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Agents | Default | string | no | Which `Kind` runs when no `lf:agent=` label is present. Default `claude` |
| Agents | Claude | ClaudeAgent | no | The existing `Claude` struct, unchanged in shape, now nested under `agents:` with the top-level `claude:` key retained as a deprecated alias |
| Agents | Codex | CodexAgent | no | Binary path, timeout, sandbox default, model/tier map, env-forward prefix |
| CodexAgent | Binary | string | no | Resolved through the existing generic `config.ResolveExecutable(key, name, pathEnv)` |
| CodexAgent | TimeoutSeconds | int | no | Same semantics as `claude.timeout_seconds` |
| CodexAgent | DefaultSandbox | string | no | Enum fixed by US-1. Default is the most restrictive measured value |
| Tiers | Tier | string | yes | `flagship` \| `balanced` \| `fast`. Closed set, validated at load |
| Tiers | ModelByTier | map[Kind]map[Tier]string | yes | e.g. `claude: {flagship: opus, balanced: sonnet, fast: haiku}`, `codex: {...}` (Codex values set from US-1, not from web research). Each model string validated by the existing `modelNameRE` |
| ToolProfile | Name | string | yes | unchanged: `^[a-z0-9][a-z0-9_-]{0,39}$` |
| ToolProfile | Claude | {Tools, AllowedTools []string} | no | The existing two fields, nested |
| ToolProfile | Codex | {Sandbox string} | no | Absent means "this profile has no Codex meaning" → refusal, not fallback |

### Relationships
- sessions → segments via `segments.session_id`; a segment is owned by exactly one `agent` for its whole life
- segments → runs via `runs.segment_id`; `runs.agent` always equals its segment's `agent`
- `agent.Kind` → concrete `agent.Runner` implementation, via the factory (`RunnerSet.For`)
- `agent.Kind` + `Tier` → concrete model string, via `config.Tiers.ModelByTier`
- `ToolProfile` + `agent.Kind` → `agent.Permissions`, resolved once per tick

### Package dependency direction (new arrows)
```
agent    →  config, naming          (new leaf-ish package; no litefactory deps beyond these)
harness  →  agent, config, naming   (Claude implementation; keeps its name)
codex    →  agent, config, naming   (new Codex implementation)
report   →  agent, git, naming, redact          (no longer imports harness)
turn     →  agent, config, git, github, naming, redact, report, store
daemon   →  agent, codex, harness, + everything it imports today
```
`report` still must not import `turn` (would cycle). Nothing above introduces a cycle.

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| label | `lf:agent=claude` \| `lf:agent=codex` on a GitHub issue | issue write | Re-read every tick from the tick's fresh label snapshot; never cached, never persisted, no extra API call |
| Go | `agent.Runner` interface: `Run(ctx, agent.Request) (*agent.RunResult, error)` | — | Declared in `internal/turn` at the point of consumption, as `turn.Runner` is today. One method; a test double stays cheap |
| Go | `turn.RunnerSet` interface: `For(agent.Kind) (Runner, error)` | — | Replaces `turn.Options.Claude Runner`. One method. `nil` set is refused, never treated as a no-op |
| Go | `daemon.buildRunners(cfg) (turn.RunnerSet, error)` | — | The factory. Constructs every configured implementation **once at startup**, where `daemon/setup.go` constructs the single Claude runner today. Per-tick selection is a map lookup, not a construction |
| Go | `(*Daemon).resolveAgent(labels []string) agentDecision` | — | New; mirrors `resolveTools`'s control flow byte for byte (see Business Rules) |
| Go | `agent.ErrSessionNotResumable` | — | Moved from `harness`. The cold-rerun-on-refusal protocol in `turn/harvest.go` becomes tool-agnostic: it reruns cold on this sentinel whichever implementation returned it |
| Go | `store.SetSegmentResumeToken(ctx, segmentID, token)` | — | Called after a successful run that reported a `ResumeToken` and the segment has none |
| Go | `store.GetOrCreateSegment(ctx, sessionID, ordinal, sessionRef, kind)` | — | Signature gains the agent kind; the existing "stored row wins" semantics are unchanged |
| report | `report.ReplyAmbiguousAgentLabels` | — | New `ReplyKind` + case in `turn/report.go`'s `replyKind()` switch |
| report | `report.ReplyMalformedAgentLabel` | — | New `ReplyKind` + switch case |
| report | `report.ReplyUnknownAgent` | — | New `ReplyKind` + switch case. The only agent refusal allowed to name the value it could not find |
| report | `report.ReplyAgentSwitchRefused` | — | New `ReplyKind` + switch case. Names both the owning agent and the requested agent (both are closed-set values, never raw label text) |
| report | `report.ReplyProfileNotAvailableForAgent` | — | New `ReplyKind` + switch case. Names the profile and the agent; both already shape-validated |
| CLI | `litefactory logs --json` | local | Gains `agent` and `session_ref` keys. Retains `claude_session_id` as a deprecated alias populated from `session_ref` when `agent == "claude"`, empty otherwise |
| CLI | `litefactory logs` human output | local | The "claude session" label becomes "<agent> session" |
| artifact | `CODEX_CLI_CONTRACT.md` | — | US-1's deliverable. Written into this session directory, modelled on `.aiteam/main/2026-08-25/session-resume/CLAUDE_CLI_CONTRACT.md` |
| artifact | `AGENTS.md` at repo root | — | US-8's deliverable |

## Business Rules

### Label resolution (US-2)
- The prefix is `lf:agent=`, chosen to match the existing `lf:tools=` convention. The requester's
  phrasing "a label that says 'Coding Agent: Codex \| Claude'" is read as intent, not as a literal
  string (Assumption A-1).
- `resolveAgent` reuses `resolveTools`'s ladder exactly, in this order:
  1. No label whose prefix matches `lf:agent=` case-insensitively → zero-value decision, no refusal, and the configured default agent runs. Byte-identical behaviour to today for every issue with no such label.
  2. More than one matching label (mis-cased ones count toward the total) → `ReplyAmbiguousAgentLabels`. One is never picked; two are never merged.
  3. A single label whose prefix is not lowercase `lf:agent=` byte for byte → `ReplyMalformedAgentLabel`, with a `MiscasedPrefix` bool for the log line only.
  4. A value that is not the shape of an agent name (`^[a-z0-9][a-z0-9_-]{0,39}$`, including empty) → `ReplyMalformedAgentLabel`.
  5. A well-formed value that is not a member of the `Kind` set / not a configured agent → `ReplyUnknownAgent`, which may name the value.
  6. Otherwise resolved.
- The malformed and ambiguous refusals carry no field capable of holding label text, so they cannot
  echo forge-authored strings (the S-9 property `toolDecision` is built around).
- Resolution happens once per tick, at `tick.go`'s existing `resolveTools` call sites
  (`handleComment` and `runIssueBody`), alongside the tool decision. Two comments drained in one
  tick share one label snapshot — the same documented per-tick (not strictly per-turn) granularity
  that `lf:tools=` already has.
- An agent refusal and a tools refusal in the same tick: the **agent** refusal is reported and the
  tools decision is not evaluated. Which binary runs is upstream of what that binary may touch.

### Session identity and tool switching (US-3, US-4)
- `session_ref` is computed for every segment of every tool, exactly as today, before any process
  starts. It is litefactory's identity for the conversation, not necessarily the tool's.
- `resume_token` is written only from a `RunResult` produced by a real run. It is never derived,
  never guessed, and never computed ahead of a run.
- Claude implementation: passes `session_ref` as `--session-id` on a cold run and as `--resume` on a
  resume; sets `RunResult.ResumeToken = session_ref`.
- Codex implementation: does not pass `session_ref` to the binary at all on a cold run; extracts the
  thread id from the measured source (US-1) and returns it as `RunResult.ResumeToken`. A resume run
  passes the stored `resume_token`.
- Cold-vs-resume becomes tool-aware. `prepare.go` decides `resume` only when **all** hold:
  a prior terminal run exists for this segment, that segment's `agent` equals the resolved agent,
  and `resume_token` is non-empty. Otherwise cold.
- **A resolved agent that differs from the segment's `agent` refuses the run** with
  `ReplyAgentSwitchRefused`. Nothing is started, no segment is created, no row is mutated
  (Assumption A-2).
- Rationale for refusing rather than starting a new segment: honouring the switch requires a second
  segment on the session, and "M0 always creates exactly one segment per session and never advances
  past this". Multi-segment sessions are litefactory's own separate, unbuilt milestone (confusingly
  also numbered LF-8 — unrelated to GitHub issue #8). The `agent` column makes the future
  `new_segment` policy an additive code change with **no further migration**, so this default is
  reversible by design.
- A session with no segment yet (first run) adopts the resolved agent. There is no switch to refuse.

### Permissions across two incompatible models (US-7)
- A `lf:tools=<profile>` label resolves against the profile map as today, then is narrowed to the
  resolved agent: `claude` reads the profile's `claude:` stanza, `codex` reads its `codex:` stanza.
- A profile with no stanza for the resolved agent → `ReplyProfileNotAvailableForAgent`. The run is
  refused. A grant is never translated across tools and never falls back to the other tool's
  default surface (Assumption A-3).
- No `lf:tools=` label at all → each implementation's own configured default surface, unchanged for
  Claude, and `codex.default_sandbox` for Codex.
- The Codex sandbox value set is a closed enum whose members are fixed by US-1 measurement, not by
  the brief's `[W]`-tagged flag names.
- `internal/config/purity_test.go`'s `TestForbiddenFlagIsAbsentFromTheTree` gains a Codex clause:
  the measured full-access/sandbox-bypass value must not be assemblable anywhere in the tree, by
  the same string-splitting-defeating grep the Claude clause uses.
- `internal/config`'s stdlib-only invariant is preserved: all litefactory-side Codex configuration
  is expressed in the existing YAML config. No TOML decoder enters `internal/config`; litefactory
  never parses or writes Codex's own `config.toml`.

### Capability tiers (US-6)
- The tier vocabulary is `flagship` \| `balanced` \| `fast`. Closed set, validated at config load.
- The canonical specialist→tier assignment lives in one litefactory-owned table
  (`.aiteam/AGENT_TIERS.md`), not in `.claude/agents/*.md` frontmatter (Assumption A-4).
- `.claude/agents/*.md` keeps a literal `model:` value, because that frontmatter is parsed by Claude
  Code, not by litefactory: an unknown `tier:` key would be ignored and the specialist would
  silently fall back to the session default. Consistency is enforced instead by a test that fails
  when a file's `model:` value differs from `ModelByTier[claude][tier]` for that specialist.
- The session-wide `--model` flag, which litefactory *does* own and already passes unconditionally
  on every run, is resolved from tier → model at dispatch time by the selected implementation.
  Empty still means "the default", never "omit the flag".
- Existing values map as: `opus → flagship`, `sonnet → balanced`. No specialist changes tier in this
  feature; only the source of truth moves.

### Runner interface and factory (US-5)
- The interface method set stays at one method. Two methods' worth of surface is already too much
  for a business-path test double, and that constraint is unchanged by there being two tools.
- The neutralisation that matters is the **data types**, not the signature: no exported field
  crossing the interface may carry a vendor name. `ClaudeSessionID` is the specific field this rule
  exists to kill.
- Both implementations must preserve the invariants the Claude harness measured into existence:
  prompt on stdin never argv; a fixed env allowlist, never `os.Environ()` wholesale; line reading
  via `bufio.Reader.ReadBytes` with a 64 KiB starting buffer, never `bufio.Scanner`; the run's
  event stream written to `NNNN.jsonl`; a `RunResult` returned for failed runs, with only setup
  failures returning an error.
- The env allowlist prefix is per-implementation (`ANTHROPIC_` for Claude; the Codex prefix set is
  fixed by US-1). The allowlist *mechanism* is shared.
- The factory constructs every configured implementation once, at daemon startup. A misconfigured
  agent fails at boot, not on the tick that first routes to it.
- The interface is sized for a third implementation: nothing in `agent.Kind`, `RunnerSet`, or the
  config shape is a two-valued switch. Adding OpenCode must be one new package plus config, with no
  edit to `turn` or `report`. This is an acceptance criterion, not an aspiration.

### `AGENTS.md` (US-8)
- A root `AGENTS.md` carries only what is genuinely tool-independent: build/test/lint commands, the
  `dist/` and `$LF_HOME` prohibitions, the store and package-map rules, the mode vocabulary.
- It states explicitly that the four-mode team (orchestrator delegation, per-agent tool
  restrictions, the `PreToolUse` hook) is a Claude Code mechanism and is not reproduced by this
  file.
- Whether Codex's `/agent` subagents and hooks can host an equivalent team is **not decided here**;
  US-1 records what the binary actually offers and a later feature decides.
- `AGENTS.md` does not become a marker file for repo registration in this feature — see Out of Scope.

## Edge Cases & Error Handling

| Case | Expected behaviour |
|------|--------------------|
| No `lf:agent=` label | Configured default agent (`claude`) runs. Argv byte-identical to today |
| Two `lf:agent=` labels, any casing | Refused, `ReplyAmbiguousAgentLabels`. Neither is picked, neither is echoed |
| `LF:Agent=codex` (mis-cased prefix) | Refused, `ReplyMalformedAgentLabel`. Log line records that the prefix was mis-cased; the reply cannot say which label |
| `lf:agent=Codex` (mis-cased value) | Refused, `ReplyMalformedAgentLabel`. Values are lowercase-only, as profile names are |
| `lf:agent=` (empty value) | Refused, `ReplyMalformedAgentLabel` |
| `lf:agent=opencode` before OpenCode exists | Refused, `ReplyUnknownAgent`, naming the value |
| `lf:agent=codex` on a session whose segment is `claude` | Refused, `ReplyAgentSwitchRefused`, naming both. No process starts; no row changes |
| `lf:agent=claude` on a session whose segment is `claude` | Normal resume. Unchanged from today |
| Label flips away and back within one thread | Still refused. There is one segment and it has one owner |
| Codex run succeeds but reports no thread id | Segment keeps `resume_token` NULL. The next Codex turn on that segment runs **cold**, and the run report says so. Never a resume attempt with a fabricated token |
| Codex resume rejected by the binary | Implementation returns `agent.ErrSessionNotResumable`; the existing rerun-cold-once protocol in `harvest.go` applies, and `Path` is recorded as `cold_after_resume_refused` |
| `codex` binary absent from PATH at startup, with Codex configured | Daemon fails to start with a named error. Not a per-tick surprise |
| `codex` binary absent and Codex not configured | Daemon starts normally. Claude-only operation is a supported configuration |
| `lf:agent=codex` on a daemon with no Codex configured | Refused, `ReplyUnknownAgent`. Never a silent fallback to Claude |
| `lf:tools=research` profile has no `codex:` stanza, `lf:agent=codex` | Refused, `ReplyProfileNotAvailableForAgent` |
| Both an agent refusal and a tools refusal apply | The agent refusal is reported; the tools decision is not evaluated |
| Codex reports no cost | `HasResult` semantics unchanged: the runs row stores NULL and the report's existing `HasResult && CostUSD > 0` gate omits the Cost line. Cost is unknown, never zero |
| Codex emits an over-long JSON line | Read correctly. `LongestLine` is recorded, as for Claude. A `bufio.Scanner` in either implementation is a review failure |
| Existing databases at upgrade | Migration `0002` backfills `agent='claude'` and `runs.agent='claude'`. Every pre-existing segment resumes under Claude exactly as before, with `resume_token` NULL and `session_ref` carrying its old `claude_session_id` value |
| Rollback after `0002` | Not supported (no down-migrations exist in this codebase). The migration is additive plus one rename; forward-only is the existing contract |
| US-1 probe finds a `[W]` claim false | The dependent story's design is revised before implementation. The probe is a gate, not a formality |
| `codex` binary not installed on the machine running US-1 | US-1 fails loudly and records "unavailable". Stories US-2..US-8 stay blocked. No story proceeds on documentation alone |

## Out of Scope

| Excluded | Why |
|----------|-----|
| Executing any Codex run against a real repository worktree in US-1 | The probe is read-only and runs in a scratch directory outside every worktree, following the `CLAUDE_CLI_CONTRACT.md` precedent |
| OpenCode implementation | Named as a future third tool. The interface is *sized* for it (and that sizing is tested); no `internal/opencode` package is written |
| Multi-segment / multi-turn sessions (litefactory's own unbuilt milestone) | Required for the `new_segment` switch policy; a separate feature with its own scope. The `agent` column is added now so that feature needs no further migration |
| The `new_segment` tool-switch policy | Depends on the above. `refuse` is the shipped behaviour; the schema makes the change additive |
| Deciding whether Codex's `/agent` subagents and hooks can host the four-mode team | Open question 3 in the brief. Needs the binary probed and then designed against; US-1 records capabilities only |
| Porting the four-mode team to `AGENTS.md` | Same reason. `AGENTS.md` ships as tool-independent working rules only |
| `internal/repos` accepting `AGENTS.md` as an aiteam presence marker | Onboarding Codex-only repositories is a user-visible capability with its own failure modes (what does a mode router mean in a repo with no team?). `MarkerClaudeMD` is unchanged; Codex runs today only in repos that already have `CLAUDE.md` |
| `internal/cli/router.go`'s "runs Claude Code in git worktrees" tagline | Cosmetic prose. Deferred to whenever a second tool is actually usable end to end |
| Vendor-named prose in `internal/report/run.go` (four strings, e.g. "`claude` exited with status") | Deferred. In-scope `report` changes are limited to the five new `ReplyKind`s and dropping the `internal/harness` import in favour of `internal/agent` — the two-front import change the brief flagged |
| Renaming `internal/harness` to `internal/claude` | Churn across 26 importing files with no behavioural gain. It keeps its name and becomes one implementation of `agent.Runner` |
| Retiring the top-level `claude:` config key | Kept as a deprecated alias for `agents.claude:` so existing `~/.litefactory/config.yaml` files keep working. Removal is a later, announced change |
| litefactory reading or writing Codex's `config.toml` / `CODEX_HOME` contents | Would drag a TOML decoder toward `internal/config` and its stdlib-only invariant. litefactory passes flags and env; it does not manage Codex's own config |
| Changing any specialist's actual model tier | US-6 moves the source of truth only. `opus → flagship`, `sonnet → balanced`, no behavioural change |

## Acceptance Criteria

### US-1 — Codex CLI probe
- [ ] `CODEX_CLI_CONTRACT.md` exists in this session directory and records the `codex` binary's version and resolved path, or records "unavailable" and blocks every other story
- [ ] Every probe ran in a scratch directory outside every git worktree and every registered clone; no file in this repository was modified by a probe
- [ ] The contract answers, each with the literal command run and its literal output: (a) the argv shape of a non-interactive run, (b) whether a prompt can be delivered on stdin, (c) the session/thread id's format and exactly where it is surfaced, (d) the per-event JSON field schema for each event type emitted with `--json`, (e) resume semantics for both `resume --last` and `resume <id>`, (f) resume behaviour on an unknown id — specifically whether it fails closed, (g) exit codes for success, tool-reported failure, and refused resume, (h) which env vars authenticate a non-interactive run, (i) the exact accepted values of the sandbox flag, (j) whether subagent and hook primitives exist and how they are configured
- [ ] Each claim in the contract is tagged `[V]` with its command, or explicitly marked "could not be measured"
- [ ] A table maps every `[W]`-tagged Codex claim in `RESEARCH_BRIEF.md` to confirmed / refuted / unmeasurable
- [ ] The contract carries a "re-verify after a Codex upgrade" section, as `CLAUDE_CLI_CONTRACT.md` does

### US-2 — Label routing
- [ ] `resolveAgent` returns the zero decision for a label set containing no `lf:agent=` label, and the resulting argv for a Claude run is byte-identical to the pre-feature argv (table test with a golden argv)
- [ ] Table test covers each ladder rung: absent, two labels, mis-cased prefix, mis-cased value, empty value, unknown value, resolved — asserting the exact `ReplyKind` and that `Agent` is empty on the ambiguous and malformed rungs
- [ ] A test asserts no refusal path can carry raw label text into a rendered reply
- [ ] Labels are read from the tick's existing snapshot: no new GitHub API call is made per tick (asserted against the fake forge's request log)
- [ ] Both call sites (`handleComment`, `runIssueBody`) resolve the agent
- [ ] A test asserts that when both an agent refusal and a tools refusal apply, the agent refusal is the one reported

### US-3 — Switch refusal
- [ ] A session with a `claude` segment and an `lf:agent=codex` label produces `ReplyAgentSwitchRefused`, starts no subprocess, and leaves `segments` and `runs` unchanged
- [ ] The refusal reply names both the owning and the requested agent, and both are closed-set values
- [ ] A first run on a session with no segment adopts the labelled agent and is not refused
- [ ] A `resume` path is chosen only when prior terminal history exists **and** the segment's agent matches **and** `resume_token` is non-empty; all three are covered by separate table cases

### US-4 — Schema
- [ ] Migration `0002_segment_agent.sql` applies to a database populated by `0001` and preserves every existing `claude_session_id` value in `session_ref`
- [ ] Existing rows are backfilled `agent = 'claude'` and `runs.agent = 'claude'`
- [ ] `resume_token` is NULL for every backfilled row and a post-migration Claude resume still resumes
- [ ] `store` exposes `SetSegmentResumeToken`; it is written only after a successful run and never overwrites a non-empty token
- [ ] No `claude_session_id` identifier remains in `internal/store`
- [ ] `litefactory logs --json` emits `agent` and `session_ref`, and `claude_session_id` still parses for a Claude run

### US-5 — Interface and factory
- [ ] `internal/agent` exists and imports only `internal/config` and `internal/naming`
- [ ] No exported identifier in `internal/agent` contains "Claude", "Anthropic", "Codex" or "OpenAI"
- [ ] `internal/report` imports `internal/agent` and no longer imports `internal/harness`; it still does not import `internal/turn`
- [ ] `turn.Options` carries a `RunnerSet`, not a single runner; a nil `RunnerSet` is refused at construction, never treated as a no-op
- [ ] The factory constructs every configured implementation at daemon startup; a Codex binary that cannot be resolved fails startup with a named error rather than failing on a later tick
- [ ] `go vet ./...`, `gofmt -l .`, `make lint` and `make test` (with `-race`) are green
- [ ] Both implementations deliver the prompt on stdin; a test asserts the prompt string appears in no argv
- [ ] Both implementations build the child environment from a fixed allowlist; a test asserts `os.Environ()` is not forwarded wholesale
- [ ] A Codex run writes an `NNNN.jsonl` event stream and populates `Events`, `Malformed` and `LongestLine`
- [ ] A Codex run whose event stream contains a line larger than 64 KiB is read in full
- [ ] `agent.ErrSessionNotResumable` returned by either implementation triggers exactly one cold rerun, recorded as `cold_after_resume_refused`
- [ ] A failed Codex run returns a `RunResult` (not an error) and is reported, logged, and has its metadata written
- [ ] **Third-tool sizing test**: a test-only third `Kind` with a stub runner routes end to end through label resolution, the factory, `turn` and `report` without any edit to `internal/turn` or `internal/report`

### US-6 — Capability tiers
- [ ] `Tier` is a closed set of `flagship`, `balanced`, `fast`, validated at config load with a named error on an unknown value
- [ ] `.aiteam/AGENT_TIERS.md` assigns every file in `.claude/agents/` exactly one tier
- [ ] A test fails when a `.claude/agents/*.md` `model:` value disagrees with `ModelByTier[claude][tier]` for that specialist
- [ ] Every model string in the mapping passes the existing `modelNameRE`
- [ ] `--model` is still passed unconditionally on every Claude run; empty config still means the default, never an omitted flag
- [ ] The Codex tier→model values come from US-1's measurement, cited in a comment

### US-7 — Permissions
- [ ] A profile with only a `claude:` stanza refuses a Codex-routed run with `ReplyProfileNotAvailableForAgent`
- [ ] A profile's Claude stanza is never used to derive a Codex sandbox value, and vice versa
- [ ] Profile `Tools`/`AllowedTools` slices are still copied, not aliased, when they reach a request
- [ ] `TestForbiddenFlagIsAbsentFromTheTree` gains a Codex clause and passes
- [ ] `TestValidationHasNoThirdPartyImports` still passes: `internal/config` gains no decoder beyond `gopkg.in/yaml.v3`
- [ ] With no `lf:tools=` label, each implementation uses its own configured default surface, and the Codex default is the most restrictive measured sandbox value

### US-8 — `AGENTS.md`
- [ ] `AGENTS.md` exists at the repository root
- [ ] It carries the build/test/lint commands, the `dist/` and `$LF_HOME` prohibitions, the store rules and the package-map rules
- [ ] It states that the four-mode team is a Claude Code mechanism not reproduced by the file
- [ ] It contains no instruction that contradicts `CLAUDE.md` or `.aiteam/APP_CONSTRAINTS.md`
- [ ] `internal/repos`' presence marker is unchanged (`CLAUDE.md`), and a test asserts that adding `AGENTS.md` alone does not make a repository registrable

## Assumptions & resolved policy decisions

Recorded because this was a headless run: each is the single most defensible reading of something
`RESEARCH_BRIEF.md` left open, taken so implementation is not blocked.

| ID | Open item | Decision taken | Why this reading, and how reversible |
|----|-----------|----------------|--------------------------------------|
| A-1 | Brief OQ-1: literal label string — `Coding Agent: Codex` vs a prefixed value | `lf:agent=<kind>`, one label carrying a value | The requester's phrasing is compatible with both; this one reuses `lf:tools=`'s prefix convention, its regex, its ladder and its refusal vocabulary, so the whole mechanism is precedent rather than invention. Reversible: the prefix is one constant, and a second accepted spelling is additive |
| A-2 | Brief OQ-2: how a mid-session tool switch is resolved | **Refuse** (`ReplyAgentSwitchRefused`). Not silently resumed, not silently restarted | Matches the codebase's existing posture of refusing rather than guessing on permission and identity questions. The alternative (`new_segment`) needs multi-segment sessions, which are unbuilt and are a separate milestone. **Sized to stay reversible**: `segments.agent` is added now, so switching the policy later is a code change with no further migration |
| A-3 | Requester-level policy: what a "permission profile" means across two incompatible sandbox models | A profile carries explicit per-tool stanzas; a missing stanza refuses the run | Translating a `--tools` grant into a `--sandbox` mode would either silently widen or silently narrow what an operator wrote. Neither is safe, and there is no third answer that is not a guess. Reversible: a translation table can be added later; a silently-translated grant cannot be un-granted |
| A-4 | Brief §3: where the tier vocabulary lives | `.aiteam/AGENT_TIERS.md` is canonical; `.claude/agents/*.md` keeps a literal `model:` value, kept honest by a test | `.claude/agents/*.md` frontmatter is parsed by Claude Code, not by litefactory. A `tier:` key there would be ignored and the specialist would silently drop to the session default — a worse failure than the duplication the test removes |
| A-5 | Migration mechanics for `claude_session_id` | `RENAME COLUMN` to `session_ref` (kept NOT NULL) + `ADD COLUMN agent` + `ADD COLUMN resume_token` (nullable) | The brief flagged the `NOT NULL` as a blocker. It is not, once the column's meaning is split: litefactory's own identity is always computable (stays NOT NULL); the *tool's* handle is what can be absent (new nullable column). This is a migration decision, taken deliberately, and forward-only per the existing contract |
| A-6 | Which package owns the neutral types | New leaf `internal/agent`; `internal/harness` keeps its name as the Claude implementation | `report` imports `harness` directly today, so the seam has two fronts. Moving the types to a leaf both fronts can import resolves it without renaming 26 files' worth of imports |
| A-7 | Whether a `codex` binary exists on this machine | Unknown — could not be checked from this agent (no shell access) | This is precisely why US-1 gates everything. If the binary is absent, US-1 fails loudly and the feature stops rather than proceeding on documentation |
| A-8 | Brief OQ-7: rationale behind the "runs on sonnet" agent-pinning commits | Not relied upon | US-6 preserves every current model assignment exactly (`opus → flagship`, `sonnet → balanced`), so no historical rationale is load-bearing. The `git show` the brief recommends is worth doing but blocks nothing |
