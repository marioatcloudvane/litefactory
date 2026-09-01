# Research Brief — add-codex-as-second-party-item

Issue #8, "Add Codex as second party item". Research run 2026-08-31, headless —
no human was available to answer a question, so every ambiguity is recorded in
*Assumptions* or *Open Questions* rather than asked.

## Provenance key

Every claim below carries one of these. They are not decoration: the difference
between "I read this file" and "a blog post said so" is the difference between a
plan that works and a plan that gets rewritten in week two.

| Tag | Meaning |
|---|---|
| `[V]` | **Verified this run** by reading the file in this repository. `path:line` given. |
| `[R0]` | **Carried forward** from an artifact already in the tree (`APP_CONTEXT.md`, `initialplan.md`, `CLAUDE_CLI_CONTRACT.md`, `AC_AMENDMENTS.md`). Trustworthy, but written by an earlier run. |
| `[W]` | **Second-hand, unverified.** Web documentation or a third-party writeup. **No `codex` binary was executed during this run** — this agent had no shell. Every `[W]` claim about Codex is documentation, not measurement. |
| `[O]` | Reported by the owner (the issue text). |
| `[?]` | **Inference.** Mine, from `[V]`/`[W]` facts. Reasoned, not observed. |

**The single most important caveat in this brief:** litefactory's own stated
convention for external binaries is *probe read-only in a scratch directory and
write up the findings, naming the exact version verified against* `[R0]`
(`APP_CONTEXT.md:405-424`; two such documents already exist). This run could not
follow that convention — it had no shell. **Everything below about Codex CLI is
`[W]`.** The first task of any plan must be to convert it to measurement.

---

## Request

> Right now, everything only works with Claude Code. Evaluate how to plug-in
> Codex into litefactory. `[O]`

Read as: OpenAI's **Codex CLI** — the headless terminal coding agent that is the
nearest peer to the `claude` binary litefactory shells out to today — driven as a
*second* agent tool alongside Claude Code, not as a replacement. `[?]`

---

## Codebase Context

### 0. Prior intent — this was considered once, deliberately not built, and never revisited

`initialplan.md:325-348` `[V]` is a section titled **"12. Harness boundary —
Claude only"**. It is the only statement of intent in the tree, and it is
unusually direct:

> One interface, one implementation. No registry, no YAML, no second adapter
> until there's a real reason for one.
>
> **What this boundary is for, and what it isn't.** It exists so the subprocess
> handling, event parsing and allowlist logic sit in one file rather than smeared
> through the reconciler. It is *not* an attempt to anticipate opencode. When a
> second harness arrives, its needs will change these signatures, and that will
> be a small honest refactor of one module. Designing for it now would mean
> guessing, and guesses in an interface are load-bearing forever.

Three consequences for this brief:

- The absence of an abstraction is a **decision that was taken**, not an
  oversight. Issue #8 is the "real reason" that clause anticipated. `[?]`
- The design predicted the cost as **"a small honest refactor of one module"**.
  Sections 1–12 below are the evidence that this estimate no longer holds — the
  Claude assumption has since spread to five packages that do not execute the
  binary, plus the store schema and ten golden files. `[V]`
- `.aiteam/AC_AMENDMENTS.md` has **no entry** bearing on multi-tool support: not
  an amendment, not a ruling, not a review, and — checked specifically — not in
  the Deferrals section (`AC_AMENDMENTS.md:1185-1314`, which covers per-repo
  concurrency, `naming.WorktreePath`, the LF-11 in-flight view, a rejected
  `runs.owner` column, and `Daemon.Quiesce`). `[V]` So: **considered at design
  time, not abstracted, never deferred, never rejected as a feature, never
  looked at again.**

A repository-wide case-insensitive grep for `codex`, `openai` and `gpt-` returns
**exactly one file**: this session's own `MANIFEST.md`. `[V]` There is no
groundwork, no stub, no half-built seam.

### 1. Where execution actually lives

`internal/harness` is the only package that executes the binary `[R0]`
(`APP_CONTEXT.md:78-80`), confirmed `[V]`: its import set is `config, naming`
only. `internal/harness/doc.go` `[V]` is the package's real specification and
states the invocation verbatim (`doc.go:11-13`):

```
<absolute claude> -p --output-format stream-json --verbose \
  --session-id <uuid> --tools <surface> --settings <file> \
  --allowedTools <rules...> --strict-mcp-config
```

argv is built in `internal/harness/runner.go:468-495` `[V]` in fixed order:

| Position | Flag | Governed by |
|---|---|---|
| 1 | `-p` | constant |
| 2 | `--output-format stream-json` | constant |
| 3 | `--verbose` | constant — **mandatory**; without it `claude` hard-errors and writes nothing to stdout, indistinguishable from a hang (`doc.go:23-24`, SD-7) |
| 4 | `--session-id <uuid>` **or** `--resume <uuid>` | `req.Path`; `PathResume` ⇒ `--resume` (`runner.go:473-476`) |
| 5 | `--tools <comma-joined>` | `Runner.surface(req)` (`runner.go:514-519`) — unconditional |
| 6 | `--settings <path>` | `r.settingsPath` |
| 7 | `--allowedTools <comma-joined>` | only when non-empty (`runner.go:488-493`) |
| 8 | `--strict-mcp-config` | constant, always last |

No `--model`, no `--append-system-prompt`, no `--fork-session`. `--continue` is
**never constructed anywhere in the tree** and is explicitly forbidden
(`runner.go:446-448`) because it fails *open* where `--resume` fails *closed*
`[V]`/`[R0]`.

`New()` guards at wiring time (`runner.go:98-164`) `[V]`: `Claude.Tools` must be
non-empty ("it is the tool boundary"), `TimeoutSeconds > 0`, `settings_path`
absolute, `$LF_HOME` absolute, binary resolved via `ResolveBinary(pathEnv)`, and
the ceiling falls back to `Tools` when empty (`runner.go:147-150`).

`Request.validate(ceiling)` (`runner.go:289-350`) `[V]` re-checks immediately
before argv is built: a **canonical UUID** session id via
`naming.ValidClaudeSessionID`, `Path` ∈ `{"", cold, resume,
cold_after_resume_refused}`, worktree absolute + stat + isDir, prompt non-empty,
and every `Tools` entry present in the ceiling.

### 2. Prompt delivery

`internal/harness/prompt.go` `[V]`. `BuildPrompt(PromptInput) (string, error)` is
pure — no clock, no filesystem, no environment. The result is piped:
`cmd.Stdin = strings.NewReader(req.Prompt)` (`runner.go:565`), per AC-3.12,
because that removes the ~1 MB macOS `ARG_MAX` ceiling and keeps third-party text
out of every `ps` listing (`doc.go:29-31`).

**There is no system prompt.** Everything — role, mode, rules, task, context — is
one user turn of Markdown. `[V]` Sections: an `# litefactory run NNNN of <ref>`
header, bullets (Task / Mode / Feature slug / Branch / Working directory), the
D-3 feature-slug sentence, optional infer guidance, then
`headlessRule` | `askingRule` (+ `inferAskingSeam`) + `worktreeRule`, then
`## Instruction`, then `## Context` with `<note>` blocks. Two shapes, cold and
resume (`writeResumeHeader`, `prompt.go:564-606`). Caps: issue body 8 KiB,
comment 8 KiB, title 1 KiB, history 32 KiB; the instruction itself uncapped.

**One line of the prompt names a Claude-specific file.** `writeInferGuidance`
(`prompt.go:623-634`) tells the agent to read *"this repository's CLAUDE.md"*.
`[V]` That is the only vendor filename in the prompt text.

### 3. The child environment

`internal/harness/env.go:40-49` `[V]` — an allowlist, never `os.Environ`:

```
PATH  HOME  USER  SHELL  LANG  LC_ALL  TERM  TMPDIR
```

Deliberately absent, each with a written reason: `SSH_AUTH_SOCK`, and
**`CLAUDE_CONFIG_DIR`** — "Setting it breaks Keychain auth even when the
directory is seeded (SD-4). It is not in this list and must not be added."
`[V]`

Two entries are Claude-shaped and load-bearing:

- **`USER` is required because Claude Code's auth is the macOS Keychain**
  (`apiKeySource: "none"`). Without `USER` the run dies with "Not logged in",
  exit 1 (`env.go:22-25`). `[V]`
- **`ANTHROPIC_*` is an explicit opt-in carve-out** from `secretNameRE`
  (`env.go:53,63`), gated by `claude.forward_anthropic_env`, default false, and
  sorted for determinism (`anthropicEnv`, `env.go:116`). `[V]`

`fallbackPATH = "/usr/bin:/bin:/usr/sbin:/sbin"` (`env.go:58`) — the launchd
PATH. `[V]`

The stated secret boundary (`doc.go:57-67`) `[V]`: *"Redaction is hygiene, not a
boundary… The boundary is not giving the subprocess the secret in the first
place."* Claude's ambient Keychain auth is what makes that affordable — no
credential is ever handed to the child.

### 4. Session identity and resume

- `naming.ClaudeSessionID(ref, segmentOrdinal)` (`internal/naming/session.go:56-70`)
  `[V]` — a **UUIDv5** over the string `"<ref>/segment/<ordinal>"` in a pinned
  namespace `sessionNamespace = uuidV5(dnsNamespace, "litefactory")`
  (`session.go:18,36`), with the version/variant nibbles pattern-pinned
  (`session.go:42`).
- It is **derived before the run** and stored on `segments.claude_session_id`,
  declared `TEXT NOT NULL` (`0001_init.sql:77`) `[V]`. `store.GetOrCreateSegment`
  (`segments.go:44`) makes the stored value always win over recomputation.
- Cold vs resume is decided in `internal/turn/prepare.go:190-193` `[V]` from
  `previousRun(...)`; the harness turns `PathCold`/`PathResume` into
  `--session-id`/`--resume`.
- Refusal is detected **structurally**, never by matching stderr prose:
  `resumeRefused` (`runner.go:724-729`) is
  `resuming && Outcome == Failed && Model == "" && ExitCode > 0` →
  `ErrSessionNotResumable`, and `turn/harvest.go:97-146` reruns **the same row
  and ordinal** cold with `PathColdAfterResumeRefused`, exactly once. `[V]`

`.aiteam/main/2026-08-25/session-resume/CLAUDE_CLI_CONTRACT.md` `[R0]` (verified
against `claude 2.1.246`) is the measured contract behind that: reusing
`--session-id` exits 1 with **zero stdout**; `--resume` resolves `--tools` from
argv and not from the transcript it continues; `--resume` and `--session-id` are
mutually exclusive without `--fork-session`; an unknown id exits 1 having emitted
**one** result event — which is exactly why the refusal test keys on `Model == ""`
rather than `Events == 0`.

### 5. Permissions, tools, sandbox — the actual security boundary

This is the deepest coupling in the tree and the one least likely to transfer.

`internal/harness/doc.go:33-43` `[V]`, stated as measurement not opinion:

> `--allowedTools` is *additive* to the operator's own `~/.claude/settings.json`.
> Measured (SD-2): 250 ambient allow-rules leaked through, and Bash executed
> while absent from `--allowedTools`. Treating it as the sandbox is a hole.
>
> The boundary is `--tools`, which restricts the built-in tool surface claude may
> use at all; the init event's `.tools` array reflects it.

Supporting machinery:

- `internal/config/policy.go` `[V]`: `toolNameRE` `^[A-Za-z][A-Za-z0-9_]*$`
  (`:19`), `toolRuleRE` `^Name(\(qualifier\))?$` (`:23`), `profileKeyRE` (`:30`),
  `defaultTools()` = `Read Glob Grep Edit Write Bash Task` (`:43` — "the seven
  names that actually resolve"; `TodoWrite` was measured being silently dropped),
  `forbiddenFlag()` assembling the skip-permissions flag from two fragments so
  the whole literal appears nowhere (`:72`), `validateNoForbiddenFlag` (`:155`)
  covering binary, settings path, tools, allowed tools, ceiling and every
  profile, `resolveToolCeiling` resolved **once** in `Load` (`config.go:281`).
- `internal/daemon/tools.go` `[V]`: `toolLabelPrefix = "lf:tools="` (`:29`),
  `resolveTools(labels)` (`:120-188`) — detected case-insensitively, accepted
  byte-exact (D-6), ambiguity counted before any label is judged, never resolved
  by picking or unioning.
- `internal/harness/settings.go` `[V]`: the staged settings file is **Claude
  Code's own JSON schema** — `permissions{defaultMode, allow, ask, deny,
  additionalDirectories}`, `enableAllProjectMcpServers`, `enabledMcpjsonServers`,
  `includeCoAuthoredBy` (`:48-61`). `DefaultSettings(home)` (`:69`) denies reads
  of `$LF_HOME`, `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gh`, `~/.claude`,
  `~/.netrc`, `./.env*`. Re-staged before **every** invocation
  (`runner.go:389`).
- Three refusal replies exist for label problems: `ReplyUnknownToolProfile`,
  `ReplyMalformedToolLabel`, `ReplyAmbiguousToolLabels`
  (`internal/report/reply.go:25-67`). `[V]`

Two residual risks are **accepted and written down**, not solved
(`doc.go:45-55`) `[V]`: the child runs *inside the worktree*, so it reads that
branch's `CLAUDE.md`, `.claude/settings.json` and **executes its hooks** — and a
branch is third-party content; `--strict-mcp-config` closes only the MCP half.
And `--settings` is additive: a run with `--settings` was verified still
reporting the operator's personal agents in its init event.

### 6. The JSONL event stream — narrower than it looks

`internal/harness/stream.go` `[V]`. This is the most consequential finding in the
whole map, and it cuts *in favour* of the work being tractable.

```go
const ( eventTypeSystem = "system"; eventTypeResult = "result"; subtypeInit = "init" )  // :35-39

type eventEnvelope struct {                       // :43
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

type initEvent struct {                           // :51
	Model          string   `json:"model"`
	Tools          []string `json:"tools"`
	SessionID      string   `json:"session_id"`
	CWD            string   `json:"cwd"`
	PermissionMode string   `json:"permissionMode"`
	APIKeySource   string   `json:"apiKeySource"`
}

type resultEvent struct {                         // :65
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	DurationMS   int64   `json:"duration_ms"`
	NumTurns     int     `json:"num_turns"`
}
```

**The harness interprets exactly two event kinds and ignores every other.**
`observe` (`:145-187`): a non-JSON line is counted as `malformed` and skipped
(never fatal); any event that is neither `system/init` nor `result` is counted in
`events` and **no field of it is read**. Every line is written verbatim to
`NNNN.jsonl` *before* interpretation (`record`/`write`, `:114-138`), so a crash
of `claude`, of the daemon, or of the machine leaves the events that already
happened on disk.

Two rules recorded as learned expensively (`stream.go:15-26`) `[V]`:
`bufio.Scanner` must never be used (over its token limit it returns `ErrTooLong`
and *stops the loop*, turning "one long tool result" into "the run had no result
event"; lines of **72,411 bytes** were measured) — the reader is
`bufio.Reader.ReadBytes` with a 64 KiB starting buffer that is not a limit.

`.subtype` is **never** branched on (`stream.go:62-64`): it reads `"success"`
even when `is_error` is true (SD-3). `[V]` Session-id confirmation is a
**warning only** — a mismatch is logged, never an error (`runner.go:672-679`).
`[V]`

### 7. Outcomes and process control

`internal/harness/result.go:8-38` `[V]` — **five** outcomes, and the distinctions
are required by S-9 / AC-3.20: `OutcomeSucceeded`, `OutcomeFailed`,
`OutcomeTimedOut`, `OutcomeAbandoned` (the daemon's context was cancelled — never
reported as a timeout), `OutcomeNoResult` (clean exit, no terminal event; in the
spike a stale credential produced ~ten internal retries then silence). This third
category is why the hard wall-clock timeout is independent of any event-driven
completion detection (`doc.go:74-80`).

`classify` (`runner.go:813-852`) `[V]`, in order: success; outer context done →
abandoned; run context `DeadlineExceeded` → timed out; `*exec.ExitError` → failed
with the status (or `SignalExitCode = -1` if negative); no result event → no
result; else failed on `is_error`.

`kill.go` `[V]` keys on **pgid, never on a process name**: `killSubtree`
(`:62`) enumerates the process tree via `/usr/bin/pgrep -P <ppid>` with an
explicit empty environment (`:171-198`), collects distinct pgids (`:120`),
SIGTERMs each, sleeps `killGrace = 300ms`, **re-scans** (a tool may spawn between
enumeration and kill), SIGKILLs, then SIGKILLs the direct pid. Bounds:
`maxProcessNodes 4096`, `maxProcessDepth 32`, `killScanTimeout 2s`; never signals
pgid ≤ 1 or its own. The reason is measured (`doc.go:82-90`): `Setpgid` plus
`kill(-pgid)` does **not** reap Claude's grandchildren — 2 of 2 survivors
reparented to PID 1 because the Bash tool `setsid`s its shell into a new session
(SD-1). **This mechanism is vendor-neutral** and would work unchanged for any
child. `[?]`

### 8. `RunResult` — already most of the way to tool-neutral

`internal/harness/result.go:73-153` `[V]`. 18 fields. The Claude-specific ones
are `Model`, `Tools`, `SessionID`, `CostUSD`, `IsError`, `NumTurns`, `HasResult`.
Everything else — `Outcome`, `Reason`, `ExitCode`, `LogPath`, `Command`,
`ResultText`, `Events`, `Malformed`, `LongestLine`, `Stderr`, `StreamError`,
`StartedAt`/`FinishedAt`/`Duration` — is vendor-neutral already.

Notably, litefactory **already declined** to depend on Claude's `duration_ms`
(`result.go:148-152`): *"Duration is the wall clock between them — not the result
event's `duration_ms`, which excludes process startup and does not exist for a
run that never finished."* `[V]` That is one gap that does not have to be
crossed.

One field conflates two things and will matter: `HasResult` documents that when
false, *"CostUSD, IsError, NumTurns and ResultText are all zero and the run's cost
is unknown rather than zero — the runs row stores NULL"* (`result.go:119-122`).
`[V]` It means both "a terminal event arrived" **and** "cost is known". For a
tool that reports a terminal event but no cost, those separate. `[?]`

### 9. Log rendering — the widest vendor vocabulary outside the harness

`internal/cli/cmd_logs_render.go` `[V]`. This is where Anthropic's schema is
baked in most deeply, and it is in a package that never touches the binary.

- Kinds: `system text think tool output result user other` (`:26-48`).
- Protocol constants `eventTypeSystem/Assistant/User`, `subtypeInit` (`:53-58`);
  `terminalEventType = "result"` lives in `cmd_logs.go:46` and is
  substring-prechecked by `isTerminalEvent` (`cmd_logs.go:524-537`) to end
  `--follow`.
- `logEvent` (`:239-266`) reads fifteen fields: `type, subtype,
  parent_tool_use_id, session_id, model, tools, cwd, message, text, is_error,
  result, error, total_cost_usd, duration_ms, num_turns`.
- `logBlock` (`:277-291`) and the hard-coded **Anthropic content-block
  vocabulary** (`:381-397`): `"text"`, `"thinking"`, `"redacted_thinking"`,
  `"tool_use"`, `"tool_result"`.
- `toolInputKeys` (`:441`) = `command file_path notebook_path path pattern url
  query description prompt`. `shortID` (`:651-657`) strips the literal prefixes
  `"toolu_"` and `"sess_"`.
- `logsMeta` (`cmd_logs.go:394-402`) surfaces `claude_session_id`; the table row
  is labelled `"claude session"` (`:469`).

Mitigating: an unrecognised event falls to `kindOther` and is **never dropped**
(`:26-48`). `[V]` A Codex log would render — badly, but visibly. `[?]`

### 10. Reporting

`internal/report/run.go` `[V]`. `metadataList` (`:510-592`) renders Branch,
Commit, Research brief, Exit status (or "killed by litefactory, so there is no
exit code" for `SignalExitCode`), Reason, Duration, **Cost** as `$%.4f` when
`HasResult && CostUSD > 0`, **Model** from the init event, **Tools**, the log
command and path, and a stream-error note. `toolsEntry` (`:614-657`) prints
resolved-vs-requested-vs-profile and is documented as the whole detection story
for a relabel.

Four strings name the vendor in prose the owner reads on the thread `[V]`:
`"litefactory could not get run %d as far as \`claude\`"` (`:377`),
`` "`claude` exited with status %d" `` (`:422`),
`` "ran past `claude.timeout_seconds`" `` (`:429`), and
`` "usually a stale `claude` credential … Run `claude` once by hand" `` (`:445-446`).
Ten golden files in `internal/report/testdata/` pin these and pin
`` - **Model:** `claude-sonnet-4-5` ``.

Caps: `maxResultBytes = 32<<10` (`:25`), `maxFileEntries 200`, `TailLines 40`;
`truncateEnd` in `internal/report/text.go`.

### 11. Config

`internal/config/config.go:140-188` `[V]`, YAML block `claude:`:

| Field | YAML key | Note |
|---|---|---|
| `Binary` | `binary` | default `DefaultClaudeBinary = "claude"` (`:243`) |
| `Tools` | `tools` | the boundary; non-empty enforced |
| `AllowedTools` | `allowed_tools` | additive second layer |
| `SettingsPath` | `settings_path` | defaults to `$LF_HOME/claude-settings.json` (`paths.go:24,93`) |
| `ToolCeiling` | `tool_ceiling` | resolved once in `Load` |
| `ToolProfiles` | `tool_profiles` | `map[string]ToolProfile{Tools, AllowedTools}` |
| `TimeoutSeconds` | `timeout_seconds` | 60–86400 |
| `ForwardAnthropicEnv` | `forward_anthropic_env` | default false |

**Confirmed, and it refutes the hypothesis in the run brief:** `tool_profiles` is
about **permissions, not about which binary**. `ToolProfile`
(`config.go:200-206`) has exactly two fields, both tool lists;
`validateToolProfiles` checks subset-of-ceiling; the profile name never reaches
argv (`runner.go:466-467`). `internal/config/toolprofiles_test.go` (17 tests) is
entirely about permission profiles. **There is no existing notion of an agent
tool anywhere in `internal/config`.** `[V]`

**The one piece of genuinely generic groundwork that does exist:**
`config.ResolveExecutable(key, name, pathEnv)` (`executable.go:78`) `[V]` is
already generic over key and name, and `Executable{Name, Path, FromPATH}` /
`*ExecutableError{Key, Name, Searched, Err}` are agent-neutral. Only the one-line
wrapper hardcodes the key:

```go
// internal/config/executable.go:110-113
// ResolveBinary locates the configured claude executable.
func (c Claude) ResolveBinary(pathEnv string) (Executable, error) {
	return ResolveExecutable("claude.binary", c.Binary, pathEnv)
}
```

`executable_test.go:133` already exercises it with the key `"git"`. `[V]` A
second binary needs no new resolution machinery.

Constraints that bind any config change `[V]`/`[R0]`: `internal/config` is
stdlib-only apart from `gopkg.in/yaml.v3`, enforced by
`purity_test.go` → `TestValidationHasNoThirdPartyImports`; the shipped template
cannot drift from `Defaults()` (`TestDefaultFileMatchesDefaults`); decoding is
strict (`KnownFields(true)`) so an unknown key is a startup error; validation
aggregates via `errors.Join`; out-of-range is refused, never clamped.

### 12. Store

`internal/store/migrations/0001_init.sql:84-105` `[V]`:

```sql
CREATE TABLE runs (
    id INTEGER PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    segment_id INTEGER NOT NULL REFERENCES segments (id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL, mode TEXT NOT NULL, trigger_comment_id INTEGER NOT NULL,
    status TEXT NOT NULL, exit_code INTEGER, error TEXT,
    started_at TEXT NOT NULL, finished_at TEXT, duration_ms INTEGER,
    cost_usd REAL,
    log_path TEXT NOT NULL, commit_sha TEXT, result_comment_id INTEGER,
    UNIQUE (session_id, trigger_comment_id), UNIQUE (session_id, ordinal));
```

- `segments.claude_session_id TEXT NOT NULL` (`:77`) is the only *named*
  Claude column. `[V]`
- `runs.cost_usd` is nullable (good — "unknown" is already representable) and
  `runs.exit_code` is nullable. `[V]`
- **There is no `model` column, no `tool` column, no `agent` column.** Model and
  tools live only in the `NNNN.meta.json` sidecar. A second tool's runs would be
  indistinguishable in the database. `[V]`
- Only two migrations exist; they are hand-rolled, embedded, forward-only, one
  transaction each. `[R0]`

### 13. The seam that exists today

```go
// internal/turn/execute.go:24
type Runner interface {
	Run(ctx context.Context, req harness.Request) (*harness.RunResult, error)
}
```
`[V]` Wired as `turn.Options.Claude Runner` (`execute.go:69`) from
`daemon/setup.go:157-162,234`. One method — but **not narrow**: it is typed on
`harness.Request` (11 fields, including a `ClaudeSessionID` validated as a
canonical UUIDv5 and a `Path` cold/resume/refused tri-state) and
`*harness.RunResult` (18 fields), and a second implementation would additionally
have to honour the `ErrSessionNotResumable` two-return protocol **and write
`NNNN.jsonl` itself**, because nothing above the harness writes it. `[V]`/`[?]`

Above it, `daemon.Executor` (`daemon/daemon.go:107-109`) is
`Execute(ctx, turn.Request) (turn.Result, error)` and never sees a
`harness.Request` — `daemon/fixture_test.go` fakes at that level. `[V]`
`turn/fixture_test.go:250-267` is the "configurable fake claude"
(`stubRunner{fn, calls, requests}`), mutex-guarded, recording every request.
`[V]`

### 14. Surprises outside `harness` and `config`

- **`internal/repos/aiteam.go` hardcodes `CLAUDE.md`** as the aiteam-presence
  marker used by `repo add` — `MarkerClaudeMD Marker = "CLAUDE.md"` (`:19`),
  `claudeMDName` (`:27`), `presenceMarker = "aiteam"` (`:43`),
  `teamMarkerRE` matching `<!-- aiteam:team=go-backend -->` (`:51`). `[V]`
  Read carefully, this is an **aiteam** coupling that happens to reuse Claude's
  filename, not a Claude-runtime coupling — but Codex reads `AGENTS.md`, not
  `CLAUDE.md` `[W]`, so a Codex-only repository would fail registration. `[?]`
- `internal/cli/router.go:15` — the product tagline says "runs Claude Code".
  `[V]`
- Grep sweep: **1012** case-insensitive `claude` occurrences across 126 files
  (excluding `.aiteam/` and `dist/`). `[V]` Non-test concentrations:
  `harness/runner.go` 75, `config/policy.go` 31, `config/config.go` 24,
  `config/validate.go` 20, `harness/settings.go` 19, `config/template.go` 18,
  `daemon/setup.go` 16, `turn/harvest.go` 15, `store/segments.go` 14,
  `cli/cmd_daemon.go` 13, `turn/prepare.go` 13, `turn/execute.go` 12,
  `naming/session.go` 11, `report/run.go` 11, `harness/doc.go` 11.
  `anthropic|sonnet|opus` in production code is confined to `harness/env.go`
  (6 sites), `config/config.go:176-187`, `config/template.go:178-200`,
  `harness/runner.go:160`.

### 15. One drift found in `APP_CONTEXT.md`

`APP_CONTEXT.md:273-274` says *"`go.mod` has exactly three direct requires:
`go-cmp`, `yaml.v3`, `modernc.org/sqlite`. Keep it that way."* `[R0]` The actual
`go.mod` `[V]` has **four**: `github.com/google/go-github/v90 v90.0.0` has since
been added. This matters here only because "no new dependency" reasoning about a
TOML encoder (§ Constraints) should be argued on its merits rather than on a
count that has already moved once.

---

## Domain / Problem Space — what Codex CLI actually offers

**Everything in this section is `[W]`.** No `codex` binary was executed. Where a
source conflicts with another I say so. The most detailed source is a
third-party experiment log covering 81 flag/feature tests against **CLI version
0.114.0** — useful, but one person's log, not a specification.

### Invocation

`codex exec` is the headless, non-interactive mode: one session run to
completion, no TUI, exits when the agent decides the task is done. `[W]`

```
codex exec "task"                       # prompt as argv
codex exec -                            # prompt from stdin
codex exec < prompt.txt                 # prompt from stdin
codex exec "instruction" < context.txt  # argv instruction + stdin context
```

**Gotcha, measured in the experiment log:** when a prompt argument is *also*
provided, stdin content is **not** received unless the `-` form is used — the
resolver prefers the argument. `[W]` This directly affects litefactory's AC-3.12
invariant (prompt on stdin, never argv); `codex exec -` appears to be the correct
form, and it is the first thing a probe must confirm.

Flags relevant here `[W]`:

| Flag | Values | Note |
|---|---|---|
| `--json` | bool | JSONL events to stdout. Older versions used `--experimental-json` — **version-sensitive** |
| `-o` / `--output-last-message` | path | final agent message to a file |
| `--output-schema` | path | JSON Schema constraining the final response |
| `--sandbox` / `-s` | `read-only` \| `workspace-write` \| `danger-full-access` | default `read-only` |
| `--ask-for-approval` / `-a` | `untrusted` \| `on-request` \| `never` | in exec, an approval request **fails the run** unless auto-approved |
| `--cd` / `-C` | path | working directory |
| `--config` / `-c` | `key=value` | repeatable, dotted paths, beats every config file |
| `--profile` / `-p` | name | layers `$CODEX_HOME/<name>.config.toml` |
| `--model` / `-m` | string | model override |
| `--skip-git-repo-check` | bool | `codex exec` **requires a git repository by default** |
| `--ephemeral` | bool | do not persist session files |
| `--ignore-user-config` | bool | skip `$CODEX_HOME/config.toml` — but auth still reads `CODEX_HOME` |
| `--search` | bool | live web search |
| `--dangerously-bypass-approvals-and-sandbox` | bool | Codex's own escape hatch |

`--full-auto` is deprecated in favour of explicit `--sandbox`. `[W]`

### The event stream

`--json` writes one JSON object per line to stdout; **progress goes to stderr**
and the final message to stdout in non-JSON mode. `[W]` The vocabulary:

```
{"type":"thread.started","thread_id":"0199a213-81c0-7800-8aa1-bbab2a035a53"}
{"type":"turn.started"}
{"type":"item.started","item":{...}}
{"type":"item.updated","item":{...}}
{"type":"item.completed","item":{...}}
{"type":"turn.completed","usage":{"input_tokens":24763,"cached_input_tokens":24448,"output_tokens":122}}
{"type":"turn.failed","error":{"message":"model response stream ended unexpectedly"}}
{"type":"error","message":"stream error: broken pipe"}
```

Item types and their fields `[W]`: `agent_message{text}`, `reasoning{text}`,
`command_execution{command, aggregated_output, exit_code, status}`,
`file_change{changes[{path,kind}], status}`,
`mcp_tool_call{server, tool, arguments, result, error, status}`,
`web_search{query}`, `todo_list{items[{text,completed}]}`, `error{message}`.
Statuses: `in_progress` | `completed` | `failed`.

**What the stream does not carry:** no cost in USD (tokens only), no model name
(an open upstream issue), no turn count, no tool-surface array, no
permission-mode field, no `cwd`. `[W]`

### Sessions

- The thread id is emitted **once, at the start**, in `thread.started` — it is
  *discovered from the output*, not supplied. `[W]`
- Resume: `codex exec resume <SESSION_ID> "prompt"` or `codex exec resume --last
  "prompt"`. A third-party source also reports `codex exec --resume-session-id
  <ID>`; **I could not confirm that flag against the official reference**, which
  documents only the `resume` subcommand. `[W]`
- **There is no way to specify a session id upfront for a new session.** A
  feature request asking for exactly Claude's `--session-id` semantics exists and
  is closed, with no visible maintainer comment — closed-without-comment is *not*
  evidence it was implemented. `[W]` The thread id is described as purely
  client-side, sent to the API only as correlation headers.
- Sessions persist under `~/.codex/sessions/`. Resuming an **ephemeral** session
  silently starts a *new* one rather than erroring. `[W]`
- Resuming replays history: input tokens roughly double each turn. `[W]`

### Permissions

Codex's model is **two orthogonal axes**, not one list:

- `sandbox_mode` — what the agent is *technically capable of*, enforced by the
  OS: **Apple Seatbelt on macOS**, Landlock + seccomp on Linux. Deny-by-default
  profile, parameterised writable roots. `[W]`
- `approval_policy` — when Codex *pauses to ask*. Irrelevant headless beyond
  "never", since an approval request fails the run. `[W]`

Plus coarse capability toggles in a `[features]` table (`shell_tool`,
`web_search`, `unified_exec`, `multi_agent`, `hooks`, `memories`,
`network_proxy`), settable in `config.toml` or via `-c features.shell_tool=false`.
`[W]` The experiment log confirms `shell_tool` disabled actually removes the tool
(the model reports it cannot execute shell commands) rather than merely hiding
it. `[W]` MCP servers additionally support `enabled_tools` / `disabled_tools`
allow/deny lists. `[W]`

Sandbox specifics that bear on litefactory's worktree design `[W]`:

- In `workspace-write`, **network access is denied by default**; sandboxed
  processes reach the network only through a configured loopback proxy.
- `.git/` inside a writable root is carved out **read-only**, to stop the agent
  planting a git hook that would execute outside the sandbox.
- **In a git worktree the real git metadata lives in the main repository's
  `.git`, outside the writable root**, so git operations that write metadata fail
  with a permission error. The documented workaround is `--add-dir` at the main
  checkout, or `writable_roots` in `[sandbox_workspace_write]`.
- The main Codex process runs **unsandboxed**; only the shell commands it spawns
  are sandboxed. It sets `CODEX_SANDBOX` and `CODEX_SANDBOX_NETWORK_DISABLED` on
  those children, and strips `DYLD_*` before spawning.

### Config and auth

- `$CODEX_HOME` (default `~/.codex`) is the root for **everything**: `config.toml`,
  `auth.json`, logs, session transcripts, skills, plugins. `[W]`
- Precedence, highest first: CLI flags and `-c` overrides → project
  `.codex/config.toml` → profile file → user `~/.codex/config.toml` → system
  `/etc/codex/config.toml` → built-in defaults. `[W]`
- Auth: ChatGPT sign-in writing `$CODEX_HOME/auth.json`, **or** the `CODEX_API_KEY`
  environment variable (the recommended CI path). The experiment log reports that
  only `CODEX_API_KEY` overrides stored credentials; `OPENAI_API_KEY` is
  deprioritised. `[W]`
- `shell_environment_policy` controls what environment the *agent's* shell
  commands inherit (`inherit` = `all|core|none`, plus include/exclude filters and
  explicit `set`), with KEY/SECRET/TOKEN excluded by default. `[W]`

### Instructions

Codex reads **`AGENTS.md`**, merged from repository root down, files nearer the
working directory overriding earlier ones. `[W]` There is **no
`--system-prompt`, `--system-prompt-file` or `--append-system-prompt`** — these
are open feature requests. `[W]`

### Exit codes

Non-zero on failure; git-repo-check failure, missing session, required-MCP
failure and argument parse errors return 1–2; an invalid `CODEX_API_KEY` gives a
401 and exit 1. No published table of distinct codes. `[W]` One open upstream
issue asks for a non-zero exit on SIGINT in exec mode, which implies that is
currently **not** the case — relevant to litefactory's kill path. `[W]`

### Other surface

`codex app-server --listen stdio://` is a separate, stateful, long-lived
JSON-RPC 2.0 interface, distinct from `codex exec --json`, and is what the
official TypeScript SDK is built on. `[W]` It is a genuine third option for the
seam and is noted as such below.

---

## The Gap

Aligned, and cheaply so:

| Concern | Claude Code | Codex CLI | Verdict |
|---|---|---|---|
| Headless one-shot | `-p` | `codex exec` | Aligned `[V]`/`[W]` |
| Prompt on stdin | `cmd.Stdin` | `codex exec -` | Aligned, with the argv-beats-stdin gotcha `[W]` |
| Machine-readable stream | `--output-format stream-json --verbose` | `--json` | Aligned in *form* — JSONL on stdout `[W]` |
| Working directory | `cmd.Dir` | `cmd.Dir` or `--cd` | Aligned `[W]` |
| Absolute binary, no `LookPath` | `config.ResolveExecutable` | same helper, different key | Aligned — the helper is already generic `[V]` |
| Process-tree kill | pgid sweep via `pgrep` | vendor-neutral mechanism | Aligned `[?]` |
| Wall-clock timeout | independent of events | independent of events | Aligned `[V]` |
| Final text | `result.result` | `agent_message.text` or `-o <file>` | Aligned, different extraction `[W]` |
| Non-zero exit / failure | `classify` on `ExitError` | non-zero on failure | Broadly aligned `[W]` |
| Ambient auth, no secret to the child | macOS Keychain (`USER` required) | `$CODEX_HOME/auth.json` (`HOME` already allowlisted) | **Aligned, and it preserves the secret boundary** `[?]` |

Genuinely different, in rough order of difficulty:

**G1 — the security boundary does not transfer, at all.** litefactory's real
sandbox is `--tools`, a *tool-name allowlist* whose vocabulary
(`Read Glob Grep Edit Write Bash Task`) is Claude Code's, and whose whole
supporting apparatus — `claude.tools`, `claude.tool_ceiling`,
`claude.tool_profiles`, `claude.allowed_tools`, the `lf:tools=<profile>` label,
`resolveTools`, `validateTools`, `validateAllowedTools`, three refusal reply
kinds, the ceiling re-check in `Request.validate` — is built on it. `[V]` Codex
has **no equivalent**: it has an OS-enforced sandbox (`sandbox_mode`), an
orthogonal approval policy, and coarse `[features]` capability toggles. `[W]`
There is no mapping from `lf:tools=readonly` to a Codex invocation that preserves
meaning. This is not a translation problem; it is a **product decision about what
"a tool profile" means when there are two tools.** `[?]`

**G2 — session identity runs in the opposite direction.** litefactory *derives*
a deterministic UUIDv5 before the run, stores it `NOT NULL`, passes it in, and
resumes by it. `[V]` Codex *emits* a thread id at run start and offers no way to
supply one for a new session. `[W]` Consequences: `segments.claude_session_id
NOT NULL` cannot be satisfied at insert time for a Codex segment; the
`PathCold`/`PathResume`/`PathColdAfterResumeRefused` tri-state and the
`resumeRefused` structural test (which keys on `Model == ""`, a field Codex never
emits) have no analogue; and `naming.ClaudeSessionID`'s pinned-namespace
determinism buys nothing. `[V]`/`[?]`

**G3 — no cost.** `turn.completed` carries `input_tokens`,
`cached_input_tokens`, `output_tokens` and nothing else. `[W]` litefactory
renders `Cost: $%.4f` in every run report and stores `runs.cost_usd`. `[V]` The
column is already nullable and the report already gates on `HasResult &&
CostUSD > 0`, so a Codex run degrades to "no cost line" without a schema change —
but `HasResult` currently means both "terminal event arrived" and "cost known",
and those separate here. `[V]`/`[?]`

**G4 — no model name, no resolved-tool array.** The init event is where
litefactory learns what Claude *actually resolved*, which is the whole detection
story for a mis-set tool surface (`report/run.go:614-657`). `[V]` Codex's stream
carries neither. `[W]` The run report's Model and Tools lines, and the
requested-vs-resolved comparison that is litefactory's only evidence a relabel
took effect, have no Codex equivalent. `[?]`

**G5 — the log renderer speaks Anthropic.** `cmd_logs_render.go` decodes fifteen
event fields and five Anthropic content-block types, and `shortID` strips the
literal prefixes `toolu_` / `sess_`. `[V]` Codex's items are a different, flatter
vocabulary. `[W]` Unknown events fall to `kindOther` and are not dropped, so
`litefactory logs` on a Codex run would produce something — mostly undifferentiated
lines. `[?]` Separately, `isTerminalEvent` substring-matches `"result"` to end
`--follow`; Codex's terminal event is `turn.completed`, so `logs -f` on a Codex
run would **never terminate**. `[V]`/`[?]`

**G6 — the sandbox actively fights the worktree.** litefactory's entire execution
model is "run the agent inside a git worktree" `[V]`. In Codex's
`workspace-write` the worktree's real gitdir is outside the writable root, and
`.git` is carved read-only anyway `[W]`; and **network is denied by default**,
which would break any `go build`/`go test` needing a module fetch. `[?]` Claude
Code imposes none of this. Mitigations exist (`--add-dir`, `writable_roots`,
`network_access`), but each is a deliberate widening of a sandbox litefactory
does not currently have to reason about.

**G7 — the product is a Claude-Code-shaped agent team, and that is bigger than
the harness.** litefactory's value is not "run an LLM in a worktree" — it is
"run the four-mode aiteam". That team is defined in `.claude/agents/*.md`,
dispatched through Claude Code's subagent mechanism, gated by `.claude/hooks/`,
detected by a `CLAUDE.md` at the repo root (`repos/aiteam.go:19,27,43`), and
delivered as **one user turn with no system prompt** (`prompt.go`). `[V]` Codex
reads `AGENTS.md`, has no `--append-system-prompt`, and its multi-agent
capability is an experimental `[features]` flag. `[W]` Running "Research mode"
under Codex is therefore not a harness change — it is a question about how the
orchestrator instructions reach a tool that has a different instruction file, a
different subagent story, and no hook mechanism litefactory is currently relying
on. **This is the finding most likely to be missed by a plan that starts from
argv.** `[?]`

---

## Options for the seam

### Option A — a second `turn.Runner` implementation, types unchanged

A `harness.CodexRunner` satisfying the existing one-method `turn.Runner`, reusing
`harness.Request` and `*harness.RunResult` as they stand.

- **Respects the arrows.** `harness → config, naming` is unchanged; no package
  gains an import. `[V]`
- **Cheapest to start, and matches `initialplan.md`'s prediction.**
- **But it does not survive G2.** `harness.Request.ClaudeSessionID` is validated
  as a canonical UUIDv5 in `Request.validate` `[V]` and Codex cannot be given
  one `[W]`; a Codex runner would either carry a field it ignores (dishonest, and
  the id is still written `NOT NULL` to the store) or force the field to change,
  which is no longer Option A.
- `harness.Options.Claude` is typed `config.Claude` `[V]` — a second runner
  cannot be constructed without either a second Options type or reusing a struct
  named for the other vendor.

### Option B — a normalised internal event model, both tools translate into it

Define litefactory's own event vocabulary; each adapter maps its vendor's stream
into it; `cmd_logs_render.go` and `report` read only the normalised form.

- **The only option that fixes G5 properly**, and the only one under which
  `litefactory logs` is honest about a Codex run.
- **Collides with a stated invariant.** `NNNN.jsonl` is written *verbatim, line by
  line, before interpretation*, specifically so a crash leaves real evidence on
  disk (`stream.go:24-26`, `doc.go:111-117`, S-1). `[V]` Normalising in place
  discards the vendor's own record. The resolution is a **dual artifact** — keep
  `NNNN.jsonl` raw and vendor-shaped, and put the normalisation in the
  `NNNN.meta.json` sidecar (which `internal/turn` already writes via
  `harness.WriteMeta` `[V]`) or a second file. That is more disk and one more
  thing to keep in step. `[?]`
- Largest blast radius: `cli`, `report`, ten golden files.

### Option C — keep Claude's shape canonical and map Codex onto it

Synthesise a `system/init` and a `result` event from Codex's stream.

- Smallest change to `report` and `cli`.
- **Rejected on principle.** It manufactures fields that do not exist — cost,
  model, resolved tools, `num_turns` — and litefactory's house style is that a
  bad outcome is a *value* and unknown is *NULL*, never a plausible zero
  (`result.go:119-122`, and the store's "numbers whose zero is a real value are
  pointers" rule). `[V]` It would also make Anthropic's 2026 schema litefactory's
  permanent internal format, which is precisely the "guesses in an interface are
  load-bearing forever" failure `initialplan.md:348` warns about. `[V]`/`[?]`

### Option D — do not build a seam yet; measure first

Probe `codex exec` read-only in a scratch directory outside the repo and write
`CODEX_CLI_CONTRACT.md` in the established convention, naming the exact version
verified against.

- **This is what this codebase's own written convention says to do** before
  changing how it invokes an external binary (`APP_CONTEXT.md:405-424`); two such
  documents already exist and the instruction is explicit: *"If you change how
  litefactory invokes `claude`, add to these rather than starting a third
  convention."* `[R0]`
- It costs almost nothing and retires most of the `[W]` risk in this brief —
  including the four items I could not settle: the stdin form, whether any
  upfront session id exists, the exit-code table, and whether `--json` or
  `--experimental-json` is current for the installed version.
- It is not an alternative to A/B — it is the thing that has to happen before
  either can be sized honestly.

### Option E — drive `codex app-server` over JSON-RPC instead of `codex exec`

- A stateful, long-lived process with an SDK-backed protocol. `[W]`
- **Wrong shape for litefactory.** The daemon's model is one subprocess per run,
  killed by a pgid sweep, bounded by a wall-clock timeout, with a goroutine
  budget documented as *four fixed plus at most `max_concurrent_runs` workers*
  and a hard ceiling of twenty (`APP_CONTEXT.md:342-362`). `[R0]` A long-lived
  JSON-RPC peer per repository breaks that inventory and the kill story.
  Recorded so it is visibly considered and rejected, not overlooked.

---

## Constraints & Risks

**Hard constraints any plan must respect** (each has a test that fails):

- `internal/config` is stdlib-only apart from `gopkg.in/yaml.v3`
  (`purity_test.go`). `[V]`/`[R0]` **There is no TOML encoder in the module and
  one cannot be added to `internal/config`.** If litefactory needs to stage a
  `config.toml`, it must either hand-write a small fixed-schema file (feasible —
  `harness/settings.go` already hand-builds JSON, though via `encoding/json`) or
  avoid the file entirely using repeated `-c key=value` overrides `[W]`, which
  keeps everything in argv where it can be logged and reviewed. **`-c` overrides
  look like the better answer** and would also keep the argv-is-safe-to-print
  property (`result.go:94-96`). `[?]`
- The forbidden skip-permissions flag must not appear in any `.go` file, and
  `validateNoForbiddenFlag` refuses any configured value smuggling it in. `[V]`
  Codex's analogue (`--dangerously-bypass-approvals-and-sandbox`) should get the
  same treatment rather than being merely discouraged. `[?]`
- The shipped `config.yaml` template must decode back to `Defaults()`
  (`TestDefaultFileMatchesDefaults`); strict decoding means a new key is a
  startup error if the template is not updated. `[V]`/`[R0]`
- Untrusted input goes on stdin, never argv (AC-3.12). `[V]` The Codex
  argv-beats-stdin gotcha `[W]` makes `codex exec -` the only acceptable form,
  and this must be *verified*, not assumed.
- The child environment stays an allowlist. Adding `CODEX_HOME` would be a
  deliberate reversal of the precedent that keeps `CLAUDE_CONFIG_DIR` out
  (`env.go`) `[V]` — permissible, but it must be argued and written down, not
  slipped in.
- Migrations are forward-only, hand-rolled, one file each. `[R0]`
- `internal/report` may not import `internal/turn`. `[R0]`
- No mocking framework, no new module for tests. `[R0]`

**Risks, roughly by expected cost:**

1. **G1 has no engineering answer.** Deciding what `lf:tools=` means for a Codex
   run is a product decision, and it gates the security story. Shipping Codex
   with a weaker boundary than Claude's, silently, would be the worst outcome
   available. `[?]`
2. **Everything about Codex in this brief is `[W]`.** Three of the sources
   disagree in small ways already (`--json` vs `--experimental-json`;
   `--disable shell_tool` vs `--disable-feature`; whether
   `--resume-session-id` exists). A plan built on this brief without a probe will
   be re-planned. `[?]`
3. **Codex CLI moves fast.** The most detailed source is version 0.114.0 and
   describes flags that were renamed or deprecated within recent releases.
   litefactory already pins measured behaviour to exact `claude` versions
   (2.1.246, 2.1.247) `[R0]`; a Codex contract document needs the same discipline
   and will need re-verification more often. `[?]`
4. **G6 — the sandbox/worktree interaction** could turn out to be the practical
   blocker rather than a footnote. Unknown until probed. `[?]`
5. **`logs -f` hanging forever on a Codex run** (G5) is a small bug with an
   irritating shape: it looks like the daemon is stuck. `[V]`/`[?]`
6. **Selecting the tool per issue repeats the `lf:tools=` security lesson.** If
   selection is a label (`lf:agent=codex`), then anyone who can label an issue
   can choose which sandbox model applies — and D-6's "detected leniently,
   accepted strictly" and the ceiling-is-the-boundary reasoning must be
   re-derived for the new axis, not assumed. `[V]`/`[?]`

**What is likely irreversible:**

- The **store migration**. Migrations are forward-only. Whatever shape is chosen
  for `segments.claude_session_id` (relax to nullable? add
  `segments.agent_session_id`? add an `agent` column to `runs` or `sessions`?) is
  the shape litefactory lives with. `[V]`/`[R0]` This deserves the most design
  attention per line of code of anything in the feature.
- **Config key names** under strict decoding — renaming `claude:` to something
  generic is a breaking change to every existing `config.yaml`. `[V]`
- `naming.ClaudeSessionID`'s **pinned UUIDv5 namespace** must not change:
  changing it re-keys every existing segment. Renaming the *function* is free;
  changing the namespace string is not. `[V]`

**What is cheap to try first:** the probe (Option D). It is read-only, outside
the repo, needs no litefactory code change at all, and answers most of the open
questions below.

---

## Blast radius

Packages that move, with what moves in each `[V]` unless noted:

| Package | What changes | Size |
|---|---|---|
| `internal/harness` | new adapter files; `Request`/`Options`/`RunResult` generalisation; a `settings.go` analogue or `-c` override builder; `stream.go` sibling | Large |
| `internal/config` | new config block; `Claude.ResolveBinary` → generic (one line); template + defaults + `TestDefaultFileMatchesDefaults`; validation; secret-scan coverage | Medium |
| `internal/store` | migration `0003`; `segments.claude_session_id` nullability or a sibling column; an agent discriminator column; `runs.go` accessors | Medium, **irreversible** |
| `internal/naming` | `ClaudeSessionID` naming; possibly a no-op for Codex | Small |
| `internal/turn` | `prepare.go` cold/resume decision; `harvest.go`'s `resumeRefused` protocol; `turnMeta`'s `claude_session_id` JSON key; `turn.Runner`'s types | Medium |
| `internal/report` | four vendor-named prose strings; Cost/Model/Tools metadata entries; `toolsEntry`; **ten golden files** | Medium |
| `internal/cli` | `cmd_logs_render.go` block vocabulary; `isTerminalEvent`; the `"claude session"` label; `router.go`'s tagline | Medium |
| `internal/daemon` | `resolveTools`/`lf:tools=` semantics; tool-selection label if one is added; `setup.go` wiring | Medium |
| `internal/repos` | `CLAUDE.md` marker vs `AGENTS.md` for aiteam detection | Small but user-visible |
| tests | `turn/fixture_test.go`'s `stubRunner`; `config/template_test.go`; `purity_test.go`; goldens | Medium |

Packages that do **not** move: `git`, `github`, `secrets`, `redact`, `identity`,
`launchd`, `logging`, `store`'s concurrency design, and the whole
concurrent-turns machinery (`gate`, `pool`, `inflight`). `[?]` The scheduler does
not care which binary a worker runs.

---

## Assumptions

Recorded because nobody could be asked. Each is my most reasonable reading; a
human should correct any that is wrong before Plan mode starts.

1. **"Codex" means OpenAI's Codex CLI in `exec` mode**, not the Codex cloud
   product, not the IDE extension, not the older 2021 Codex model.
2. **Second, not replacement.** Claude Code stays the default and every existing
   run keeps its exact current behaviour. This mirrors AC-2.3's precedent that an
   unprofiled run's argv must stay byte-identical. `[R0]`
3. **Selection is per-issue or per-repository, not global.** The value of a
   second tool is choosing between them; a global switch would be a much smaller
   feature. I have *not* assumed which mechanism (label, repo config, or config
   key) — that is an open question.
4. **The operator authenticates Codex out of band** (`codex login`, writing
   `$CODEX_HOME/auth.json`), exactly as they already do for Claude's Keychain, so
   litefactory never handles an OpenAI credential. This preserves the stated
   secret boundary and is the assumption I would most like confirmed.
5. **macOS only.** litefactory is a macOS LaunchAgent; Codex's Seatbelt sandbox
   is the only one that matters.
6. **The aiteam prompt content is out of scope for the first slice.** G7 is real
   but is a separate piece of work from making the harness able to run a second
   binary.

---

## Open Questions

For a human. Ordered by how much a wrong answer would cost.

1. **What does a tool profile mean when there are two tools?** (G1) Options
   include: profiles become Claude-only and a Codex run refuses any `lf:tools=`
   label; profiles gain a Codex dialect (sandbox mode + features); or a new
   ceiling concept spans both. This gates the security story and cannot be
   deferred to implementation.
2. **What is the selection mechanism, and what is its ceiling?** A label
   (`lf:agent=codex`) inherits the `lf:tools=` lesson that anyone who can label
   can choose — so what is the operator-authored boundary that constrains it?
3. **Is `NNNN.jsonl` a vendor artefact or litefactory's own format?** Option B
   turns on this and the tree does not settle it: `harness` writes the vendor's
   raw stream `[V]` while `cli` reads it as Claude's schema `[V]`.
4. **What happens to `segments.claude_session_id NOT NULL`?** Relax to nullable,
   add a sibling column, or populate it post-hoc from `thread.started`? The
   migration is forward-only.
5. **Should `runs` gain an agent/tool discriminator column?** Today a Codex run
   and a Claude run are indistinguishable in the database. Note the ledger
   already records `runs.owner` as *rejected, not deferred*
   (`AC_AMENDMENTS.md:1286`) `[V]` — worth reading that reasoning before adding
   a column.
6. **Is a Codex run without a cost figure acceptable in the run report?** The
   column is nullable and the report already gates on it, so "no Cost line" is
   the free answer — but it is a visible reduction in what the thread tells the
   owner.
7. **Does `--append-system-prompt`'s absence matter?** litefactory sends no
   system prompt today `[V]`, so possibly not at all — but G7's team-definition
   question may reopen it.
8. **`repos/aiteam.go`'s `CLAUDE.md` marker** — is that a Claude coupling or an
   aiteam coupling? If aiteam ships an `AGENTS.md` too, this is a two-line
   change; if not, a Codex-only repository cannot be registered. `[V]`
9. **`internal/git/exec.go:78-83` uses `exec.LookPath`**, contradicting
   `APP_CONTEXT.md`'s "Never `exec.LookPath`" and `executable_test.go:131-133`'s
   comment that "the same helper resolves both executables at startup". `[V]`
   Which is the intended contract for a *second* agent binary? This is a
   pre-existing inconsistency this feature would otherwise inherit silently.
10. **Would a second vendor need an env-prefix carve-out** equivalent to
    `ANTHROPIC_*` / `forward_anthropic_env`, and does that generalise or become
    two special cases? `[V]`
11. **`.aiteam/main/2026-08-26/tool-permissions/TOOL_SURFACE_PROBE.md` was not
    read this run** (out of the tour's scope). It should be read before G1 is
    decided.

---

## Recommended scope for Plan mode

**Recommendation: Option D first, then a narrow Option A/B hybrid.** Do not plan
the seam from this brief alone — every Codex fact in it is documentation.

### Slice 1 — measure (small; hours, not days)

Probe `codex exec` read-only in a scratch directory outside the repo and write
`CODEX_CLI_CONTRACT.md` alongside the two existing contract documents, naming the
exact version verified against and how to re-verify. `[R0]` It must settle: the
stdin form; `--json` vs `--experimental-json`; the full event sequence for
success, model failure, and a killed process; the exit-code table including
SIGINT/SIGTERM; whether any upfront session id exists and what `resume` actually
accepts; whether `workspace-write` can write inside a git *worktree* whose gitdir
is elsewhere; and whether network-denied breaks a `go build`.

**This slice writes no production Go code**, so it is reversible in full. If it
finds that G2 or G6 is fatal, the feature stops here having cost almost nothing —
which is a successful outcome, not a failed one.

### Slice 2 — decide the two questions engineering cannot answer

Open Questions 1 and 2 (the tool-permission boundary, and the selection
mechanism plus its ceiling). These are product decisions and they gate
everything. They should be recorded as `D-*` decisions in the amendment ledger,
because they will need to be cited later.

### Slice 3 — the seam, sized only after 1 and 2

Generalise `harness.Request`/`RunResult`/`Options` to be honestly tool-neutral
(they are ~80% there already `[V]`), add the Codex adapter, keep `NNNN.jsonl`
raw and vendor-shaped, and put the normalisation in the meta sidecar. Fix
`isTerminalEvent` so `logs -f` terminates. Take the store migration only once
Question 4 is answered.

### Explicitly out of scope for the first plan

- G7, the agent-team question — `AGENTS.md`, subagents, hooks. It is real, it is
  large, and it deserves its own research run. A plan that folds it in will not
  ship.
- Option E (`app-server`). Considered, rejected on the goroutine and kill-story
  grounds above.
- Any change to the concurrency machinery. It is genuinely tool-agnostic.
- Any change that makes an existing Claude run's argv differ by one byte. The
  AC-2.3 precedent is the right bar. `[R0]`

**Honest sizing.** `initialplan.md:348` predicted "a small honest refactor of one
module". `[V]` On the evidence above that is no longer true: the Claude
assumption has spread to `config`, `naming`, `store`, `cli`, `report` and
`repos`, plus ten golden files and a forward-only migration. The *harness* part
is a small honest refactor. The rest is a medium feature with one irreversible
decision in it and one open product question that engineering cannot settle.
