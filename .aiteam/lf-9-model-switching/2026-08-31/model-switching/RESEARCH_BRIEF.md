# Research Brief — model-switching

litefactory issue #9. Branch `lf/9-model-switching`. 2026-08-31. Headless run — no human was
available to answer a question, so every ambiguity below is stated as readings plus a recommended
default, and the assumptions taken are recorded explicitly in *Assumptions taken*.

**Provenance key.** Every claim carries one:

| Tag | Meaning |
|---|---|
| `[V]` | Verified this run — the cited file was read this run (by the orchestrator or a sub-agent that cited `path:line`) |
| `[Vb]` | Verified this run **from the shipped `claude` binary's embedded strings** — the flag/value/catalog exists as written. **This is not behavioural verification**: see R-0 |
| `[O]` | Reported by the owner, in the triggering comment |
| `[W]` | Unverified — inferred, or awaiting a behavioural probe. **Never rely on a `[W]` without probing it first** |

---

## Request

Issue #9 has no body. The whole of the ask is one comment from @marioatcloudvane `[O]`:

> We need a way to switch models - e.g. "Opus" or "Sol" or "Qwen 3.6" or so alongside a thinking
> switcher. I think it should be a label: "Model: Opus" and "Thinking: High" as different labels.
> How would we implement that? Research it!

Read as evidence of intent: two dials the owner believes are orthogonal, each expressed as its own
GitHub issue label, both reaching the `claude` invocation.

**The single most important finding in this brief is that they are not orthogonal.** See F-3 and R-3.

---

## Headline findings

| # | Finding |
|---|---|
| **F-1** | **Half the feature already exists.** The model a run used is already parsed from the init event, already persisted to `NNNN.meta.json`, and already printed in the run report. What is missing is *selection*, not observation. |
| **F-2** | **Both flags exist.** `--model <model>` and `--effort <low\|medium\|high\|xhigh\|max>` are real flags in the installed `claude 2.1.251`. Thinking is **flag-controlled, not prompt-controlled** — do not build an "ultrathink" prompt-injection layer. |
| **F-3** | **Effort is model-gated.** Only Sonnet 4.6/5, Opus 4.6/4.7/4.8/5 and Fable 5 carry the `effort` capability. `Model: Sonnet 4.5` + `Thinking: High` silently does nothing on the effort axis. The two dials interact. |
| **F-4** | **Non-Anthropic models are not supported by the CLI at all.** No OpenAI/Qwen/Gemini provider path exists in the binary. "Qwen 3.6" is reachable only through a translating proxy at `ANTHROPIC_BASE_URL` — a proxy feature, not a CLI feature. |
| **F-5** | **Neither flag validates strictly.** `--model` has no parse-time validation; `--effort` warns on a bad value and continues. Both plausibly **fail open**, which is the exact "my label did nothing" failure the existing label design was built to prevent. |
| **F-6** | **No store migration is needed**, and there is a ledger precedent for refusing one. |
| **F-7** | **`lf:tools=` is a near-perfect template** for both dials — but it is hardcoded to one prefix, and generalising-vs-copying is the largest structural decision in the work. |

---

## Codebase Context

### 1. Labels today — there is exactly one label reader, and it is a very good template

`[V]` The entire label vocabulary is one constant, `internal/daemon/tools.go:29`:

```go
const toolLabelPrefix = "lf:tools="
```

`[V]` The matcher, `internal/daemon/tools.go:130-139`:

```go
for _, label := range labels {
    if len(label) < len(toolLabelPrefix) { continue }
    prefix, value := label[:len(toolLabelPrefix)], label[len(toolLabelPrefix):]
    if !strings.EqualFold(prefix, toolLabelPrefix) { continue }
    found = append(found, seen{exactPrefix: prefix == toolLabelPrefix, value: value})
}
```

The conventions this encodes, all of which a second dial inherits or must consciously reject:

- **Detected leniently, accepted strictly (D-6)** `[V]`. `strings.EqualFold` *sees* `LF:Tools=`;
  `prefix == toolLabelPrefix` then *refuses* it. `internal/daemon/tools.go:14-23` gives the reason:
  GitHub preserves label case, so a case-sensitive scan would not see the mis-typed label at all and
  the run would proceed having silently ignored the request. Seeing-and-refusing turns a typo into a
  loud refusal. **"My label did nothing" is named there as the one outcome the feature exists to
  prevent** — hold that thought for F-5.
- **Value shape** `[V]`, `internal/daemon/tools.go:40`: `^[a-z0-9][a-z0-9_-]{0,39}$`. Lowercase only,
  so `lf:tools=Research` is malformed. The same pattern is deliberately declared **three times** —
  `internal/config/policy.go:30`, `internal/daemon/tools.go:40`, `internal/report/reply.go:162` — and
  `reply.go:151-161` explains why it is re-declared rather than imported: "A rule that is only
  enforced by whoever remembers to enforce it upstream is not a rule."
- **Ambiguity is counted before anything is judged** `[V]`, `internal/daemon/tools.go:157-163`. Two
  labels are two requests; one is never picked and two are never unioned. `tools.go:141-156` records
  that this ordering is chosen rather than incidental — judging first would make the refusal depend
  on the order GitHub returned the labels.
- **Refusals carry no field for label text to travel in** `[V]`, `internal/daemon/tools.go:60-75`.
  Only the *unknown* refusal sets `Profile`; malformed and ambiguous leave it empty. That absence is
  the mechanism (S-9), not a habit.
- **Labels enter once, ride the existing payload** `[V]`, `internal/github/issues.go:67-77, 260`.
  No extra API call. `issues.go:72-77` states invariants **C2** ("A label is a *lookup key* and never
  a value") and **C3** ("nobody's write access to this repository is a permission grant").
- **Resolved once per tick, never mid-run** `[V]`, `internal/daemon/tick.go:1376-1382`. Two comments
  drained in one tick share one label snapshot. `internal/daemon/tools_test.go:84-112`
  (`TestRelabelThenComment`, AC-2.10) pins both halves: a label applied between two comments widens
  the *next* comment's run, and a bare relabel with no following comment starts nothing.

`[V]` The refusal path is `internal/daemon/tick.go:1487-1532` (`Daemon.refuseTools`) →
`turn.ReplyRequest` → `internal/report/reply.go:248-275`. Three reply kinds
(`internal/report/reply.go:40-67`): unknown / malformed / ambiguous. "No profiles configured at all"
takes the *unknown* path. `[V]` When the trigger comment id is `0` (issue body as first instruction) a
refusal **posts nothing** (`tick.go:1505-1511`).

### 2. The invocation path, end to end

`[V]` Discovery → dispatch → `Daemon.run` → `resolveTools` (`internal/daemon/tick.go:1382`) →
`turn.Execute` (`tick.go:1399-1419`) → `internal/turn/prepare.go:218-235` builds `harness.Request` →
`internal/harness/runner.go:412` `r.args(req)` → `runner.go:560` `exec.CommandContext`.

**`harness.Request`**, `[V]` `internal/harness/runner.go:214-281` — every field:

| Field | Type | Line |
|---|---|---|
| `Ref` | `naming.Ref` | :217 |
| `RunOrdinal` | `int` | :221 |
| `SegmentOrdinal` | `int` | :226 |
| `ClaudeSessionID` | `string` | :236 |
| `Mode` | `string` | :239 |
| `Path` | `RunPath` | :244 |
| `WorktreePath` | `string` | :248 |
| `Prompt` | `string` | :251 |
| `Tools` | `[]string` | :260 |
| `AllowedTools` | `[]string` | :270 |
| `ToolProfile` | `string` | :280 |

**The full argv today**, `[V]` `internal/harness/runner.go:468-495`, verbatim and in order:

```go
sessionFlag := "--session-id"              // "--resume" when req.Path.resuming()
tools, allowedTools := r.surface(req)
args := []string{
    "-p",
    "--output-format", "stream-json",
    "--verbose",
    sessionFlag, req.ClaudeSessionID,
    "--tools", strings.Join(tools, ","),
    "--settings", r.settingsPath,
}
if len(allowedTools) > 0 {
    args = append(args, "--allowedTools", strings.Join(allowedTools, ","))
}
return append(args, "--strict-mcp-config")
```

**Is a model set today? No.** `[V]` No `--model` in argv, no `model` key on `config.Claude`
(`internal/config/config.go:140-188`), no `Model` field on `harness.Request`. **Any thinking knob?
No.** `[V]`

**But the model is already observed — F-1.** `[V]`:

| Where | What | Cite |
|---|---|---|
| init event → `RunResult.Model` | the model claude actually resolved | `internal/harness/stream.go:52,167`; `result.go:98-102` |
| `Meta.Model` in `NNNN.meta.json` | persisted per run (AC-3.17) | `internal/harness/meta.go:60-61,142` |
| `- **Model:**` bullet | rendered in `secFixed`, the never-trimmed section | `internal/report/run.go:567-569` |
| `litefactory logs` | `model` row | `internal/cli/cmd_logs.go:400,460` |

`[V]` `internal/harness/stream.go:48-50`: the init event "reports what claude actually resolved,
which is not necessarily what was asked for". That is exactly the requested-vs-resolved verification
loop a model dial needs, and it is already closed on the observation side.

**The `--tools` feature already built the requested-vs-resolved pattern in full** `[V]`, and it is the
precedent to copy field-for-field:

- `Meta.ToolsRequested` beside `Meta.Tools` (`internal/harness/meta.go:66-73`), with the reason
  written out: the two "have been observed to differ silently: TodoWrite was requested on every run
  for the life of the project and never resolved, with no error raised."
- `turn.Result.ToolsRequested` / `ToolProfile` (`internal/turn/result.go:160-169`), set from the
  *request* at `internal/turn/execute.go:328-329`, so they survive a turn that never started a
  subprocess.
- `internal/report/run.go:614-657` (`toolsEntry`) renders resolved, else "requested …, but the run
  recorded no resolved surface", plus profile provenance, plus `notGranted` (`run.go:659-669`) —
  "requested but not granted: …".

**Child environment** `[V]`, `internal/harness/env.go:40-49`: `PATH HOME USER SHELL LANG LC_ALL TERM
TMPDIR`, and nothing else. Empty values dropped (`env.go:97`); `IsSecretEnvName` (`env.go:63,74`)
applied **even to allowlisted names**. One carve-out: `anthropicEnv` (`env.go:116-130`) forwards
`ANTHROPIC_*` when `Claude.ForwardAnthropicEnv` is true.

`[V]` **There is no per-run environment channel.** `Runner.environ` (`runner.go:93`) is the *daemon's*
environ, captured once at startup (`internal/daemon/setup.go:150-162`), and `buildEnv(parent,
forwardAnthropic)` takes no per-run argument. An env-delivered per-issue dial needs a new parameter
through the file that documents itself as "built by naming what goes in rather than by naming what
stays out" (`env.go:10-15`). **Given F-2, no design needs to go near this.**

`[V]` **`ANTHROPIC_MODEL` is already forwardable today** (`internal/harness/env_test.go:149` uses it
as fixture data). With `forward_anthropic_env: true` an operator already has a daemon-wide,
undocumented model override. It is off by default and `internal/config/config.go:176-186` says
emphatically it should stay off.

### 3. Config

`[V]` `config.Claude`, `internal/config/config.go:140-188`:

| Field | YAML key | Default (`Defaults()`, :212-232) |
|---|---|---|
| `Binary` | `binary` | `"claude"` |
| `Tools` | `tools` | `defaultTools()` |
| `SettingsPath` | `settings_path` | `""` → `$LF_HOME` default |
| `AllowedTools` | `allowed_tools` | `defaultAllowedTools()` |
| `ToolCeiling` | `tool_ceiling` | **absent** — resolves to a copy of `Tools` |
| `ToolProfiles` | `tool_profiles` | **absent** (nil map) |
| `TimeoutSeconds` | `timeout_seconds` | `1800` |
| `ForwardAnthropicEnv` | `forward_anthropic_env` | `false` |

`[V]` The two optional keys are the precedent for an optional new one: both **absent on a fresh
install**, appearing in the shipped template as **commented examples only**
(`internal/config/template.go:129-166`). That keeps `Defaults()` zero for them and
`TestDefaultFileMatchesDefaults` green without a live key.

`[V]` **Per-repo override: none.** The `repos` table
(`internal/store/migrations/0001_init.sql:30-45`) carries `base_branch`, `feature_prefix`,
`worktree_root` and nothing from `claude:`. The block is global to the daemon.

`[V]` `harness.New` receives the *whole* `config.Claude` block (`internal/daemon/setup.go:157-162`;
`internal/harness/runner.go:52-71`), so a new map on that struct reaches the harness with no new
wiring.

### 4. Store and persistence — no migration needed, with a ledger precedent

`[V]` The `runs` table (`internal/store/migrations/0001_init.sql:84-105`) has no `tool_profile` and
no `model` column. `[V]` There are exactly two migrations in the tree.

**The tool-profile feature persisted its selection to SQLite not at all** — it lives in
`NNNN.meta.json` and the report comment. `[V]` The ledger carries an explicit rejection of a new
`runs` column in a closely analogous case, `.aiteam/AC_AMENDMENTS.md:1286-1312` ("Rejected, not
deferred: an owner column on `runs`"), ending: "The migration budget went to `sessions.needs_poll`,
which is load-bearing."

### 5. Reporting

`[V]` `internal/report/run.go:563-574` — the `Model` bullet is emitted from `res.Model`, and the
`Tools` bullet immediately follows it. Both sit in `secFixed`, index 0 (`run.go:232-238`),
documented as "marker, heading, status callout, metadata — **never cut**".

`[V]` `internal/report/run.go:600-613` states why the Tools line is non-negotiable, and every word
transfers:

> Profiles are resolved per tick, not per comment: two comments drained in one tick share one label
> snapshot. This bullet is the only after-the-fact evidence of which snapshot the run in front of you
> used. … Labels are authenticated by nothing (C3). … this line is the only thing that makes it
> visible afterwards, so it is the whole detection story for a relabel.

`[V]` The 32 KiB cap is `maxResultBytes = 32 << 10` (`run.go:25`), cut with `truncateEnd`
(`run.go:285`, `:713`).

### 6. Prior external-behaviour probes, and the convention they set

`[V]` Two documents exist, both naming the exact `claude` version and a re-verification checklist:

- `.aiteam/main/2026-08-25/session-resume/CLAUDE_CLI_CONTRACT.md` — `claude 2.1.246`, five probes.
- `.aiteam/main/2026-08-26/tool-permissions/TOOL_SURFACE_PROBE.md` — `claude 2.1.247`, three probes.

`[V]` **Neither says anything about model selection or thinking.** `[Vb]` The installed binary is now
**2.1.251** — both documents are already behind.

`[V]` The convention, `.aiteam/APP_CONTEXT.md:405-424`: "probe read-only, in a scratch directory
outside the repo, and write it up in a document — **do not commit a test that spawns a real
`claude`**." A test like that would be slow, quota-consuming and flaky, and the repo has no
build-tag or env-var gate for one.

`[V]` And there is a directly relevant precedent for how badly this CLI can fail *open*.
`TOOL_SURFACE_PROBE.md:42-54`: eight tools requested, seven resolved, `TodoWrite` silently dropped —
**exit code 0, stderr empty, no error event**. `:66-68` records the property: "an unknown tool name
is dropped silently rather than rejected."

`[V]` The tool-enforcement plan shows the house mechanism: a **Probe gates** table with a
**Contingencies** table beside it
(`.aiteam/main/2026-08-30/tool-enforcement/IMPLEMENTATION_PLAN.md:58-91`), instructed as "state the
failure, do not assume the probe passes."

---

## Domain / Problem Space

### What the `claude` CLI actually supports

**Evidence basis, stated precisely.** A sub-agent was dispatched to run `claude --help`. **Bash was
denied for the session and `~/.claude` was unreadable**, so no command was run and no runtime output
exists. Instead it located the installed package and extracted the option definitions, the model
catalog and the env-var registry **from the shipped binary's embedded JavaScript**. For *what the
flags are and what they accept*, that is stronger than `--help` — it includes the validation code and
the alias→ID map. For *how they behave*, it is not evidence at all. Hence the `[Vb]` tag and R-0.

`[Vb]` Installation:
`/Users/mariomh/.nvm/versions/node/v24.16.0/lib/node_modules/@anthropic-ai/claude-code/package.json`
→ `"version": "2.1.251"`.

#### Model selection

`[Vb]` Verbatim from the binary:

```js
.option("--model <model>","Model for the current session. Provide an alias for the latest model
        (e.g. 'fable', 'opus', or 'sonnet') or a model's full name (e.g. 'claude-fable-5').")
```

**There is no `.argParser(...)` on `--model`** `[Vb]` — no parse-time validation whatsoever.

`[Vb]` Accepted aliases: `["sonnet","opus","haiku","fable","best","sonnet[1m]","opus[1m]",
"fable[1m]","opusplan"]`. `[1m]` denotes 1M-context variants; `opusplan` is Opus-for-planning /
Sonnet-for-execution; `best` resolves to `fable`.

`[Vb]` Known full IDs: `claude-3-5-haiku`, `claude-3-5-sonnet`, `claude-3-7-sonnet`, `claude-fable-5`,
`claude-haiku-4-5`, `claude-mythos-5`, `claude-opus-4-0`, `claude-opus-4-1`, `claude-opus-4-5`,
`claude-opus-4-6`, `claude-opus-4-7`, `claude-opus-4-8`, `claude-opus-5`, `claude-sonnet-4-0`,
`claude-sonnet-4-5`, `claude-sonnet-4-6`, `claude-sonnet-5`.

`[Vb]` **Alias resolution is provider-dependent** — this matters for reproducibility:

```js
opus:   {default:"claude-opus-5",   per_provider:{ …, gateway:"claude-opus-4-7"}}
sonnet: {default:"claude-sonnet-…", per_provider:{bedrock:"claude-sonnet-4-5", …,
                                                  gateway:"claude-sonnet-4-6"}}
haiku:  {default:"claude-haiku-4-5"}
fable:  {default:"claude-fable-5"}
```

`[Vb]` `--fallback-model <model>` also exists — comma-separated list, "only works with `--print`",
which litefactory already uses.

`[Vb]` `CLAUDE_MODEL` does **not** exist. `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,
FABLE}_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL` all do.

#### Thinking / effort — F-2, and it overturns the obvious hypothesis

`[Vb]` Verbatim:

```js
addOption(new U("--thinking <mode>","Thinking mode: enabled (equivalent to adaptive), disabled"))
addOption(new U("--thinking-display <display>","How thinking content appears in the response"))
addOption(new U("--max-thinking-tokens <tokens>","[DEPRECATED. Use --thinking instead for newer
        models] Maximum number of thinking tokens (only works with --print)"))
addOption(new U("--effort <level>",`Effort level for the current session (${Uh.join(", ")})`))
```

`[Vb]` `Uh = ["low","medium","high","xhigh","max"]`. Also accepted: `auto`, and an integer
(frontmatter validation says "Valid options: … or an integer"); `ultracode` is gated behind dynamic
workflows.

**"Thinking: High" maps to `--effort high`, not `--thinking`.** `--thinking` is a binary
enabled/disabled mode; `--effort` is the level. `[Vb]`

`[Vb]` **`--effort` warns rather than exits on a bad value**:

```js
.argParser((P)=>{ let {level:A, warning:M} = g_t(P);
                  if (M !== void 0) process.stderr.write(`Warning: ${M}\n`) ... })
```
with the message `Ignoring unrecognized effort "…"; valid: …`.

`[Vb]` Thinking defaults to `{type:"adaptive"}` when nothing is set; `MAX_THINKING_TOKENS` is read at
runtime; settings keys are `maxThinkingTokens`, `alwaysThinkingEnabled`, `effort`, `defaultEffort`.

`[Vb]` `ultrathink` appears as a literal string; `megathink` and `think harder` do **not**. In 2.1.x
the flag path is the real control surface. **Do not build a prompt-injection layer.**

#### F-3 — the dials are not orthogonal

`[Vb]` Effort is a **per-model capability**. Models carrying `"effort"`: `claude-sonnet-4-6`,
`claude-sonnet-5`, `claude-opus-4-6`, `claude-opus-4-7`, `claude-opus-4-8`, `claude-opus-5`,
`claude-fable-5`. Models **without** it: `claude-3-5-haiku`, `claude-3-5-sonnet`, `claude-3-7-sonnet`,
`claude-haiku-4-5`, `claude-sonnet-4-0`, `claude-sonnet-4-5`, `claude-opus-4-0`, `claude-opus-4-1`,
`claude-opus-4-5`, `claude-mythos-5`.

So `lf:model=haiku` + `lf:thinking=high` is a combination where **half the request silently does
nothing**. The owner's model of "two different labels" `[O]` is the right *interface* but the wrong
*semantics*: the valid effort range depends on the selected model. A design that treats them as
independent will produce exactly the "my label did nothing" outcome D-6 exists to prevent.

`[Vb]` There is also a cost dimension: `claude-opus-5` carries
`effort_index:{low:0.67,medium:0.76,high:1,xhigh:1.6,max:1.7}`. `xhigh`/`max` are ~1.6–1.7× the cost
of `high`. Relevant to R-11.

#### F-4 — non-Anthropic models

`[Vb]` **Evidenced backends:** Bedrock (`CLAUDE_CODE_USE_BEDROCK`, per-model `provider_ids.bedrock`),
Vertex (`CLAUDE_CODE_USE_VERTEX`, per-model `provider_ids.vertex`), and a custom gateway
(`ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN`, with a gateway model-discovery subsystem caching
`gateway-models.json`). `ANTHROPIC_CUSTOM_MODEL_OPTION` injects one arbitrary row into the picker.

`[Vb]` **Not evidenced:** no `"openai"` provider, and no `gpt-*`, `qwen*`, `deepseek*` or `gemini-*`
model-ID literals anywhere in the binary. There is no OpenAI-compatible client path.

`[W]` **Inference:** the CLI speaks the Anthropic Messages API and nothing else. All three "backends"
are transports for *Anthropic* models, not a multi-vendor abstraction. "Qwen 3.6" could only reach it
via a gateway at `ANTHROPIC_BASE_URL` that translates Anthropic Messages → Qwen. That is somebody
standing up a proxy, with its own auth, its own failure surface, and no relationship to this feature.

### The naming problem — "Sol" and "Qwen 3.6"

Three readings:

**(a) Friendly aliases mapping to Claude models.** "Opus" → `claude-opus-5`; "Sol" a codename.
**(b) Genuinely non-Claude models via another backend.**
**(c) Both.**

**The evidence supports (a), and (b) is now positively excluded at the CLI level.**

- `[Vb]` F-4: no non-Anthropic model support exists in the binary.
- `[Vb]` The real alias namespace is *already* codenames — `fable`, `mythos`, `opusplan`, `best`.
  "Sol" fits that pattern exactly and is most plausibly an anticipated or misremembered codename,
  not a different vendor. It is **not** in the 2.1.251 catalog.
- `[V]` litefactory's only execution path is the `claude` binary
  (`internal/config/config.go:243`), authenticated by the macOS Keychain — the init event reports
  `apiKeySource: "none"` (`internal/harness/env.go:21-25`). The product has no API-key dependency and
  `internal/config/config.go:176-186` treats acquiring one as a hazard.
- `[V]` **C2 forbids the naive form of (b) outright**: `internal/turn/execute.go:221-226` — "Every
  string here is operator-authored configuration. The label only ever chose *which* profile; nothing
  that originated on the forge travels in these fields, and that is what keeps C2 true all the way to
  argv."

**Recorded assumption (A-1):** read as **(a)**. "Qwen 3.6" is treated as the owner gesturing at
"model names in general" rather than as a requirement for a non-Anthropic backend. If they genuinely
want Qwen, this brief under-scopes the work by a large margin and Plan mode must stop and re-ask.

**The alias table: config or hardcoded?** A real decision. **Surfaced, not settled.**

| | Config (`claude.models:` map) | Hardcoded in Go |
|---|---|---|
| New model IDs | No release needed. `[Vb]` The catalog holds 17 IDs and churns faster than this daemon ships | Needs a litefactory release |
| C2 | Preserved — label is a lookup key into operator-authored config, exactly like `tool_profiles` | Also preserved — the argv string is litefactory-authored |
| Operator control | Operator decides which models are reachable — the structural analogue of `tool_ceiling` (R-11) | None. Every alias litefactory knows is reachable by anyone who can label an issue |
| Encoding F-3 | Operator must maintain the effort-capability matrix, and will get it wrong | litefactory ships the matrix and can keep it correct |
| Reproducibility | Operator can pin full IDs, dodging the provider-dependent alias problem | litefactory pins them |
| Cost | Another `Defaults()`/template/validation/`validateNoForbiddenFlag` quadruple | Zero config surface |

**F-3 tilts this.** The effort-capability matrix is knowledge about *models*, not about *this
operator's preferences*, and an operator hand-maintaining it in YAML will get it wrong silently. That
is an argument for shipping the matrix in Go. A hybrid has direct precedent — `tool_ceiling`
**defaults to a copy of `tools`** when absent (`internal/config/config.go:155-163`) — so: built-in
alias table and capability matrix in Go, plus an optional config map that extends or restricts it.

### The label format — the owner's proposal conflicts with the house convention

`[O]` The owner proposed `Model: Opus` / `Thinking: High`. `[V]` The house form is `lf:tools=research`.

Reasons to prefer `lf:model=opus` / `lf:effort=high`:

- **Namespace collision.** `[V]` `internal/daemon/tools_internal_test.go:92-94` already exercises
  `priority:high` as an ordinary label that must be ignored — colon-style labels are in use in the
  wild, and `Model:` is a plausible label in a repo unrelated to litefactory. `lf:` is also the
  marker prefix for litefactory's own comments (`internal/turn/classify.go:28`) and the default
  branch prefix (`internal/naming/slug.go:22`).
- **D-6 cannot express the owner's form as written.** `[V]` The rule is *fold case to detect,
  byte-exact lowercase to accept*. `Model: Opus` is capitalised and carries a space, so "byte-exact
  lowercase" has no meaning for it. Either the rule changes or the format does.
- **The value pattern is lowercase-only** `[V]`, so `Qwen 3.6` → `qwen-3-6` and `Opus` → `opus`
  regardless. A human-friendly display name can live in the config entry.
- **Naming the dial `effort` rather than `thinking`** matches the flag that actually exists `[Vb]`
  and avoids promising a `--thinking` mode the design is not using. Against: "thinking" is what the
  owner said and is more legible to a non-expert.

Against all of this: the owner's form reads better in the GitHub UI, and this is their product's UX.
**Flagged as the owner's call**, with `lf:model=` recommended.

---

## Constraints & Risks

**R-0 — Static extraction is not behavioural verification, and this repo's convention demands the
latter.**

> **Narrowed after the fact.** The orchestrator could not run commands, so everything below was
> extracted from the shipped binary. `claude --help` has since been run directly at top level and
> **confirms both flags verbatim**: `--model <model>` ("Provide an alias for the latest model (e.g.
> 'fable', 'opus', or 'sonnet') or a model's full name (e.g. 'claude-fable-5')"), `--effort <level>`
> ("Effort level for the current session (low, medium, high, xhigh, max)"), and `--fallback-model`
> and `--max-budget-usd` both carrying "(only works with --print)". Installed version 2.1.251. The
> flags' *existence and accepted values* are therefore `[V]`, not `[Vb]`. **Everything in the rest of
> R-0 still stands** — composition, the init event, and exit codes remain unverified.

`[Vb]` tells us `--model` and `--effort` exist and what they accept. It does **not** tell us
they compose with litefactory's argv shape (`-p --output-format stream-json --verbose --session-id …
--tools … --settings … --strict-mcp-config`), nor whether the init event's `model` field reflects
`--model`, nor what the exit code is on a bad value. `[V]` The existing probes exist precisely
because "the interesting question is not what the flags do alone but whether they still compose"
(`TOOL_SURFACE_PROBE.md:12-15`). **A behavioural probe document remains a hard gate.**

**R-1 — `RunResult.Model == ""` is load-bearing, and a careless model feature breaks it silently.**
`[V]` `internal/harness/runner.go:724-729`:

```go
func resumeRefused(resuming bool, result *RunResult) bool {
	return resuming &&
		result.Outcome == OutcomeFailed &&
		result.Model == "" &&
		result.ExitCode > 0
}
```

`runner.go:715-719` explains: an empty `Model` is the test for "no init event arrived". If anyone
pre-populates `RunResult.Model` with the *requested* model as a convenience, resume-refusal detection
stops firing, `ErrSessionNotResumable` is never returned, and the cold-rerun that unwedges a task with
a lost transcript (`internal/turn/harvest.go:88-96`) never happens — leaving the task "wedged
permanently: every run asks to resume, is refused, and reports the same failure forever."
**`RunResult.Model` must remain strictly the init event's value.** A requested model needs a separate,
unmistakably-named field.

**R-2 — An unconditional new argv element breaks a whole-argv golden test.** `[V]`
`internal/harness/runner_test.go:1200-1218` (`TestRunWithNoProfileArgvIsByteIdentical`, AC-2.3)
compares argv with `slices.Equal` against a literal list. Both flags must be **omitted entirely** when
not selected — the shape `--allowedTools` already uses (`runner.go:488-493`). This also argues against
a *config-level* default model: one would change argv for every existing run and could not be absent.

**R-3 — The two dials interact, and the naive design silently half-fails (F-3).** `[Vb]` Effort is
model-gated. `lf:model=haiku` + `lf:effort=high` is a combination where the effort request evaporates.
Plan mode must decide: refuse the combination, warn in the report, or silently allow it. Refusing is
most consistent with D-6; reporting it is the minimum.

**R-4 — Fail-open is the realistic bad outcome (F-5).** `[Vb]` `--model` has no parse-time validation
and the alias resolver passes unknown strings through unchanged; `--effort` warns to stderr and
continues. `[V]` `TOOL_SURFACE_PROBE.md:42-54` shows this CLI silently dropping a requested
capability with exit 0 and empty stderr. `[W]` A bogus model most likely reaches the API and returns
a 400 "invalid model name" — the binary has explicit handling for that
(`e.status===400 && e.message.toLowerCase().includes("invalid model name")`) — meaning a wasted
round-trip per run rather than a fast local failure. **Mitigation, already idiomatic here:** validate
the name in litefactory before spawning, *and* compare requested vs the init event's resolved model in
the run report, exactly as `toolsEntry`/`notGranted` do (`internal/report/run.go:614-669`).

**R-5 — A model value reaching argv is a new argv-injection surface.** `[V]`
`internal/config/policy.go:155-184` (`validateNoForbiddenFlag`) enumerates *by hand* every config
string that reaches argv — `binary`, `settings_path`, `tools`, `allowed_tools`, `tool_ceiling`, and
each profile's lists. A new model-identifier config value joined into argv **must be added to that
function**, or it becomes an unchecked channel for the permission-skipping flag (assembled from
fragments at `policy.go:72-74` precisely so a repo-wide grep stays a reliable review invariant, and
enforced tree-wide by `internal/config/purity_test.go:90`). `[Vb]` Note the CLI has **two** such
flags — the well-known one and `--allow-dangerously-skip-permissions`. The existing validation
matches on the first as a substring; whether the second needs its own check is a question for Plan.

**R-6 — Report bullet ordering is pinned.** `[V]`
`internal/report/report_test.go:1119-1145` (`TestToolsBulletFollowsModelImmediately`, AC-4.2) requires
the `Tools` bullet to *immediately* follow `Model`. A new bullet cannot go between them.
`report_test.go:1153-1165` requires the Tools bullet to survive truncation because it sits in
`secFixed`; a model/effort provenance line needs the same treatment for the same C3 reason.

**R-7 — The resume path must present identical argv.** `[V]`
`internal/harness/runner_test.go:1021-1048` (`TestRunResumeSwapsOnlyTheSessionFlag`) asserts cold,
resume and `PathColdAfterResumeRefused` argv differ **only** by the session flag. Both new flags must
appear identically on all three. Note the cold-rerun path *mutates the request in place*
(`internal/turn/harvest.go:130-133`).

**R-8 — Two dials multiply the refusal surface.** `[V]` Tools has three reply kinds
(`internal/report/reply.go:40-67`). Two more dials, each with unknown/malformed/ambiguous, naively add
six, each needing a `report.ReplyKind`, a case in `replyKind()` (`internal/turn/report.go:612-625`),
prose, and tests — plus at least one for the R-3 incompatible-combination case. **This is the largest
structural decision in the work:** `resolveTools` (`internal/daemon/tools.go:120`) is hardcoded to one
prefix, one decision struct and one map lookup. Either it is generalised into a label-vocabulary
resolver, or it is copied twice.

**R-9 — Pattern duplication is already at three copies by design.** `[V]` A naive addition makes
nine. The triplication is deliberate (`internal/report/reply.go:151-161`), but the reasoning should be
restated rather than reflexively multiplied.

**R-10 — Alias resolution is provider-dependent.** `[Vb]` `opus` → `claude-opus-5` first-party but
`claude-opus-4-7` via gateway. If reproducibility across environments matters, the alias table should
map to **full model IDs**, not to CLI aliases.

**R-11 — Labels are authenticated by nothing (C3), and this dial's blast radius is cost.** `[V]`
`internal/config/template.go:134-140`: "a label is authenticated by nothing, and anyone who can label
an issue can select ANY profile below — so it is the ceiling above, not the label, that bounds what a
run can do." Tools has `tool_ceiling`. A model/effort dial's exposure is **spend**, not capability:
`[Vb]` `max` effort on Opus 5 is ~1.7× `high`, and model choice moves cost by a larger factor again.
A config-only alias table *is* the natural allowlist. `[Vb]` Separately, the CLI offers
`--max-budget-usd <amount>` ("only works with --print"), which is a ready-made ceiling primitive if a
hard bound is wanted — **out of scope here, but worth recording so it is not rediscovered later.**

**R-12 — The shared settings file is not a per-run channel** (relevant only if a design reaches for
it). `[V]` `internal/harness/settings.go:133-154`: written only when missing, one path per daemon,
shared by `max_concurrent_runs` workers. `settings.go:43-47`: "An invented key is at best ignored and
at worst a hard startup error in the child, and a hard error in the child looks exactly like a hung
run." Given F-2, no design needs this.

**R-13 — Test-writing constraint.** `[V]` `.aiteam/APP_CONTEXT.md:405-424` — no committed test may
spawn a real `claude`. Every CLI fact must be established by a probe document.

---

## Open Questions

Each carries a recommended default. None was settled by this run.

**Q-1 — Do `--model` and `--effort` compose with litefactory's argv, and does the init event reflect
`--model`?** *Recommendation:* a **behavioural probe gate** (R-0) before implementation, written up as
a third document beside the two existing ones, naming `claude 2.1.251`, per
`.aiteam/APP_CONTEXT.md:405-424`. Model the gate/contingency tables on
`.aiteam/main/2026-08-30/tool-enforcement/IMPLEMENTATION_PLAN.md:58-91`. **Minimum cases:** flags
compose and exit 0; init event `model` reflects `--model`; bogus model — exit code and stderr; bogus
effort — confirm warn-and-continue; effort on a non-effort model — confirm silent no-op; `--model`
alongside `--resume`, and whether a resumed conversation honours a *changed* model.

**Q-2 — How should the model/effort incompatibility (F-3) be handled?** *Recommendation:* ship the
capability matrix in Go and **refuse the combination** with its own reply kind, consistent with D-6's
"a request we cannot honour is refused loudly, not silently ignored". Minimum acceptable alternative:
allow it but state plainly in the run report that effort was not applicable to the selected model.

**Q-3 — Aliases to Claude IDs, non-Claude backends, or both?** *Recommendation:* **(a)**, per A-1.
F-4 positively excludes (b) at the CLI level. Map aliases to **full model IDs** (R-10).

**Q-4 — Alias table in config or hardcoded?** *Recommendation:* **surfaced, not settled.** If forced:
the hybrid — capability matrix and default aliases in Go (F-3, R-10), optional config map to
extend/restrict (R-11) — on the `tool_ceiling`-defaults-to-`tools` precedent.

**Q-5 — Label format: `lf:model=opus`/`lf:effort=high`, or the owner's `Model: Opus`/`Thinking:
High`?** *Recommendation:* the `lf:` form, for namespace and D-6 reasons. **Owner's call** — this is
product UX and they proposed otherwise.

**Q-6 — No label present?** *Recommendation:* **emit no flag at all**, argv byte-identical to today
(R-2). Not a configured default model.

**Q-7 — Unknown model or effort name?** *Recommendation:* **refuse the run**, mirroring
`ReplyUnknownToolProfile`. Given F-5 the CLI will not refuse for us, and silently running a different
model than was asked for is precisely the failure D-6 exists to prevent. Counter-argument to record: a
model is not a *permission* grant, so refusing is harsher than the tools case strictly requires. Plan
mode should choose deliberately.

**Q-8 — Two `lf:model=` labels on one issue?** *Recommendation:* **ambiguous, refuse, count before
judging** — copy `internal/daemon/tools.go:141-163` including its reasoning. Per vocabulary, so a
model label and an effort label together are two *different* dials, not an ambiguity.

**Q-9 — Label added mid-run?** *Recommendation:* identical to tools — a per-tick snapshot resolved
once in `Daemon.run`; a bare relabel starts nothing; the run report names what was requested so the
snapshot is auditable (AC-2.10, `TestRelabelThenComment`).

**Q-10 — Interaction with per-repo / global config?** *Recommendation:* **global only.** No per-repo
`claude:` override exists. Do not invent one for this feature.

**Q-11 — Interaction with `forward_anthropic_env` and a forwarded `ANTHROPIC_MODEL`?**
*Recommendation:* probe which wins. At minimum document it; consider logging loudly when both a
label-selected model and a forwarded `ANTHROPIC_MODEL` are in play.

**Q-12 — Persist the selection?** *Recommendation:* **no migration.** `meta.json` (a
`model_requested` / `effort_requested` pair beside the existing `model`, mirroring `tools_requested`)
plus the report comment. Precedent: the tool-profile feature added no column, and
`.aiteam/AC_AMENDMENTS.md:1286-1312` rejected a `runs` column on a closely analogous argument.

**Q-13 — Generalise `resolveTools` or copy it?** *Recommendation:* surfaced for the architect (R-8).
Do not pre-commit; the existing code's value is largely in its very specific, heavily-reasoned
comments, which a generalisation would dilute.

**Q-14 — Is a spend ceiling in scope?** *Recommendation:* **no, but record it.** R-11 — `--max-budget-usd`
exists and is the natural bound if the cost exposure of C3 ever needs closing. Out of scope for this
feature; noted so it is not rediscovered from scratch.

---

## Assumptions taken

Recorded because this was a headless run and nobody could be asked.

- **A-1** — "Sol"/"Qwen 3.6" read as **friendly aliases for Claude models** (reading (a)), not a
  request for a non-Anthropic backend. Basis: F-4 (no non-Anthropic support in the CLI), the fact
  that the real alias namespace is already codenames (`fable`, `mythos`), litefactory's Keychain-only
  auth, and C2. **If the owner meant a genuinely different vendor, this brief under-scopes the work
  substantially and Plan mode should stop and re-ask.**
- **A-2** — "as different labels" `[O]` read as **two independent label namespaces**, each separately
  resolvable and refusable — while noting (F-3) that their *values* are not independent.
- **A-3** — "Thinking: High" `[O]` maps to `--effort high`, not to `--thinking`. Basis: `--thinking`
  is a binary enabled/disabled mode `[Vb]`; the graded control the owner described is `--effort`.
- **A-4** — The dials apply to the **main `claude` run of a turn**. No other `claude` invocation
  exists in the tree today.
- **A-5** — The owner's `Model: Opus` phrasing is read as **intent** ("a label, one per dial, with a
  readable name"), not a byte-exact format specification. Q-5 surfaces the conflict rather than
  silently overriding them.

---

## Recommended scope for Plan mode

**Plan should cover:**

1. A **behavioural probe gate first**, with a contingency table — Q-1, R-0. Static extraction
   established the flags exist; it did not establish they compose. Write it up as a third
   external-behaviour document naming `claude 2.1.251`, and note that the two existing documents are
   now version-behind.
2. The **`lf:model=` dial end to end**, on the `lf:tools=` groove: config → validation (including
   `validateNoForbiddenFlag`, R-5) → `daemon.Options` → resolution in `Daemon.run` → `turn.Request` →
   `harness.Request` → conditional argv (R-2) → `meta.json` → run report.
3. The **`lf:effort=` dial**, in the same shape. F-2 means this is no longer blocked on an unknown —
   it can ship alongside the model dial rather than waiting.
4. **The interaction between them (F-3, Q-2)** — the capability matrix, and what happens when a label
   pair is incompatible. This is the part most likely to be missed and most likely to produce the
   owner's original complaint.
5. The **requested-vs-resolved report line**, modelled on `toolsEntry`/`notGranted`, respecting
   bullet-ordering and truncation-survival (R-6). Given F-5 this is not decoration — it is the
   detection story.
6. **Refusal semantics** — Q-7, Q-8 — with reply kinds, the `replyKind()` switch case, and the
   no-field-for-label-text discipline (S-9).
7. Explicit decisions on **Q-4** (config vs hardcoded vs hybrid) and **Q-13** (generalise vs copy),
   both with the architect.
8. A **test plan** against R-1, R-2, R-6, R-7.

**Plan should NOT cover:**

- **Any non-Anthropic backend** — out of scope per A-1 and F-4. It is a proxy feature with an auth
  story this product deliberately does not have.
- **A store migration** — Q-12.
- **A per-repo model override** — Q-10.
- **A spend ceiling** — Q-14. Recorded, not built.
- **Any prompt-injection thinking mechanism** — F-2 makes it unnecessary and it would be strictly
  worse.
- **Any change to `RunResult.Model`'s meaning** — R-1. Closest thing to a hard invariant in this work.
- **Any per-run settings-file or environment-variable channel** — R-12, R-5. Both flags go in argv.

**Where Plan must stop and ask the owner:** if the Q-1 probe shows the flags do not compose with
litefactory's argv shape; or if A-1 is wrong and a genuinely non-Claude backend was meant. Either
invalidates the shape of this brief rather than a detail in it.
