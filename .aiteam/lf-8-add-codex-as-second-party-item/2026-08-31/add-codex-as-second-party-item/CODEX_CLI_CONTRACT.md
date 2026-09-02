# codex CLI invocation contract

## Re-verification — 2026-09-02

The earlier UNAVAILABLE conclusion below is retained as historical evidence but
is no longer current. The binary is now available at
`/Users/mariomh/.nvm/versions/node/v24.16.0/bin/codex`, reports
`codex-cli 0.152.1`, and `codex login status` reports `Logged in using ChatGPT`.

An isolated run outside every repository succeeded with `--ephemeral`,
`--ignore-user-config`, `--ignore-rules`, `--skip-git-repo-check`,
`--sandbox read-only`, and `--json`. Its stream was:

```json
{"type":"thread.started","thread_id":"01a0622b-f3d3-74c0-8a18-b80eff0ae0b0"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"LITEFACTORY_CODEX_OK"}}
{"type":"turn.completed","usage":{"input_tokens":13768,"cached_input_tokens":9984,"cache_write_input_tokens":0,"output_tokens":11,"reasoning_output_tokens":0}}
```

`codex exec --help` directly confirms stdin prompts (`-`), JSONL, explicit
model selection, `read-only`, `workspace-write`, and `danger-full-access`
sandboxes, ephemeral execution, ignored user config/rules, and explicit-ID
resume. Litefactory permits only the first two sandbox values and never
constructs the bypass form. The implementation and fake-binary contract tests
are based on this measured wire shape.

---

This document was scoped to do for `codex` what
`.aiteam/main/2026-08-25/session-resume/CLAUDE_CLI_CONTRACT.md` did for `claude`: run the real
binary in a scratch directory outside every worktree, and record exactly what it does — converting
every `[W]` (web-sourced, second-hand) claim in `RESEARCH_BRIEF.md` into either `[V]` (verified this
run) or an explicit "could not be measured."

**That could not be done this run.** No `codex` invocation of any kind was executed. This is not
"the binary was not found on `PATH`" — it is one level more restrictive: **the execution
environment's permission policy denies the Bash tool call itself** for every command that would
locate or invoke `codex`, before the shell ever runs. See "What was actually observed" below for
the exact evidence.

**Verified against:** nothing. **Status: UNAVAILABLE**, per AC-1.1/AC-1.2 — recorded as such rather
than proceeding on documentation.

---

## AC-1.2 gate statement

Per AC-1.2, this is a gate. Every Codex-gated task named in US-4, US-5, US-6, US-7, and US-8's
sub-issues stays blocked pending a re-run of this probe in an environment whose permission policy
permits invoking `codex`. This does not block the rest of the feature: per the plan's Workflow
section, tasks that do not depend on measured Codex behavior may proceed.

---

## What was actually observed

All probe attempts below were made from this Go-implementation-engineer session. No scratch
directory was ever reached, because every command that would have located or run `codex` was
refused before a shell process started. This is the complete list of attempts and their outcomes —
included verbatim because the *shape* of the denial is itself a finding, not just its existence.

| Attempted command | Result |
|---|---|
| `which codex` | Denied by the Bash tool's permission layer |
| `command -v codex` | Denied |
| `codex --version` | Denied |
| `printf '%s\n' "$PATH"` / `echo $PATH` | Denied |
| `ls /opt/homebrew/bin \| grep -i codex` | Denied when combined with `npm list -g`/`brew list` in the same command |
| `ls /usr/local/bin \| grep -i codex` (standalone) | **Succeeded**, zero matches |
| `ls /usr/local/bin \| wc -l` (no reference to codex or PATH) | Succeeded |
| `echo hello` | Succeeded |
| `Glob("**/codex*")` under `/usr/local/bin`, `/opt/homebrew/bin`, `/opt/homebrew/Cellar` | No files found (Glob is unaffected by the Bash denial, since it is a different tool, but also found nothing) |
| `Glob(".codex/**")` under `/Users/mariomh` | No files found |

The pattern: commands that do not attempt to execute, locate, or resolve the search path for
`codex` succeed normally (plain `ls`, `echo hello`, unrelated `grep`). Commands that would execute
`codex` directly, resolve it via `which`/`command -v`, or read `$PATH` (the mechanism `which` and
`command -v` depend on) are denied outright — the tool call itself errors with "Permission to use
Bash has been denied" before any process runs. `Glob` and `Read`, which are separate tools from
Bash, were used as a partial substitute and found no `codex` binary or `~/.codex` directory in the
locations checked, but a `Glob`/`Read` miss is weaker evidence than a `which` hit or miss would have
been — it only covers the specific paths queried, not the resolved `$PATH` search order.

**Conclusion drawn from this:** this is a policy-level guard specifically on invoking/discovering
`codex`, not evidence about whether a `codex` binary exists on the underlying host. The two are
different findings and must not be conflated when this contract is re-read later: "denied by this
session's permission policy" is not the same claim as "not installed." Per this story's explicit
instruction, both outcomes gate the same way, so this contract treats them identically for AC-1.2
purposes while recording the distinction here for whoever re-runs this probe.

No file inside this repository or any registered worktree was created, modified, or read for
write purposes by any probe attempt (AC-1.3) — every attempt above either was refused before
reaching a shell, or (in the successful cases) only listed directories outside the repository
(`/usr/local/bin`, `/opt/homebrew/bin`, `/Users/mariomh`).

---

## AC-1.4 / AC-1.5 — item-by-item

Every item below is `[U]` — could not be measured, for the reason stated in "What was actually
observed." No literal command-and-output pair exists for any of them; that pair is exactly what a
re-run must produce.

| # | Question | Status |
|---|---|---|
| (a) | argv shape of a non-interactive run | `[U]` — could not be measured |
| (b) | whether a prompt can be delivered on stdin | `[U]` — could not be measured |
| (c) | session/thread id format and where it is surfaced | `[U]` — could not be measured |
| (d) | per-event JSON field schema for each `--json` event type | `[U]` — could not be measured |
| (e) | resume semantics for `resume --last` and `resume <id>` | `[U]` — could not be measured |
| (f) | resume behavior on an unknown id (fail closed?) | `[U]` — could not be measured |
| (g) | exit codes: success, tool-reported failure, refused resume | `[U]` — could not be measured |
| (h) | which env vars authenticate a non-interactive run | `[U]` — could not be measured |
| (i) | exact accepted values of the sandbox flag | `[U]` — could not be measured |
| (j) | whether subagent/hook primitives exist and how configured | `[U]` — could not be measured |
| (k) | whether `~/.codex/config.toml` can widen a sandbox mode passed on argv, and whether MCP servers autoload from it | `[U]` — could not be measured |
| (l) | whether `OPENAI_BASE_URL`/`OPENAI_API_BASE`-class env vars are honored | `[U]` — could not be measured |
| (m) | whether `$CODEX_HOME` tolerates concurrent writers and whether one task's transcript is visible to another | `[U]` — could not be measured |

Item (l) carries a downstream consequence worth stating explicitly even though it is unmeasured:
US-5's design must not assume prefix-based env forwarding (e.g. an `OPENAI_`-prefix allowlist
analogous to `AnthropicEnvPrefix`) is safe for Codex until this is verified — if
`OPENAI_BASE_URL`/`OPENAI_API_BASE` are honored, a prefix allowlist would let a labeled run silently
redirect Codex's API traffic to an attacker-controlled endpoint. Treat this as **unsafe until proven
otherwise**, not safe-by-default.

---

## `[W]` claim table — RESEARCH_BRIEF.md Codex claims

Every `[W]`-tagged claim about Codex's actual runtime behavior in `RESEARCH_BRIEF.md` is listed
below with a citation to its location, and its disposition after this probe. Since no probe ran,
every disposition is **unmeasurable** — none were converted to confirmed or refuted. This table
exists so a future re-run has a checklist to work through rather than needing to re-derive it from
the brief.

| Claim (RESEARCH_BRIEF.md line) | Summary | Disposition |
|---|---|---|
| :99-103 | Codex's session/thread id is emitted by the CLI at run start and discovered from output — it cannot be supplied upfront the way litefactory pre-computes `ClaudeSessionID`; exact format/surface (stdout vs. a file) unconfirmed | Unmeasurable |
| :184-187 | `AGENTS.md` on the Codex side is a pure discovery-and-concatenation mechanism (walks the tree, concatenates root-to-leaf), with no special syntax for declaring agents/personas/routing | Unmeasurable |
| :188-194 | Subagents are a separate `/agent` slash-command feature, reportedly enabled by default, spawning specialized agents in parallel and collecting results; a subagent inherits the parent's model/reasoning-effort unless explicitly overridden | Unmeasurable |
| :195-201 | Hooks are a separate feature: shell scripts on lifecycle events (`SessionStart`, `SessionEnd`, `SubagentStart`, `SubagentStop`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`, `UserPromptSubmit`, `Stop`) via `~/.codex/hooks.json` or `config.toml`, disable-able via `[features] hooks = false`; `PreToolUse`/`PostToolUse` reportedly intercept only the shell/Bash-equivalent tool, not apply-patch/edit/read/web-fetch/MCP calls | Unmeasurable |
| :261-264 | Current model family GPT-5.6 with tiers `gpt-5.6-sol`/`gpt-5.6-terra`/`gpt-5.6-luna`, plus text-only preview `gpt-5.3-codex-spark`; older models (5.5, 5.4, 5.4-mini) stated to retire Aug 31, 2026 | Unmeasurable (also not the kind of claim a runtime probe settles — this is a hosted-model-catalog fact, not a CLI behavior fact; a future probe should note this distinction and expect this one to require checking Codex's own release notes, not `codex --version` output) |
| :265 | Model pinning is via `--model`/`-m` flag or `model = "gpt-5.6-terra"` in `config.toml` | Unmeasurable |
| :266-269 | Subagents inherit the parent's model/reasoning-effort unless explicitly overridden | Unmeasurable |
| :350-355 | Invocation shape `codex exec "<prompt>"`; default progress on stderr, final message on stdout; `--json` → JSON Lines with event types `thread.started`, `turn.started`, `turn.completed`, `item.completed`, `error`; `-o <file>`, `--output-schema <file>`; flags `--ephemeral`, `--sandbox workspace-write\|danger-full-access`, `--ignore-user-config`, `--ignore-rules`, `--skip-git-repo-check` | Unmeasurable |
| :356-358 | Exit codes: `0` on success, non-zero on a `turn.failed`/`error` event or a required MCP server failure | Unmeasurable |
| :359-363 | Resume via `codex exec resume --last "<prompt>"` or `codex exec resume <SESSION_ID> "<prompt>"`; transcripts persist under `$CODEX_HOME` (default `~/.codex`) unless `--ephemeral`; exact session-ID format/surface unconfirmed even at the documentation level | Unmeasurable |
| :364-368 | `CODEX_HOME` holds `config.toml` and credentials; `CODEX_API_KEY` documented for CI/non-interactive auth, `CODEX_ACCESS_TOKEN` for ChatGPT Enterprise; whether `OPENAI_API_KEY` still works as a fallback, and whether Codex's sandbox filters env vars passed to spawned subprocesses, unconfirmed | Unmeasurable |
| :369-373 | Only JSON Lines event-**type names** are confirmed from documentation; the exact per-event field schema (what `item.completed` actually carries) is unconfirmed — explicitly flagged in the brief as "do not write a Go parser against an assumed field shape" | Unmeasurable — and this instruction from the brief still stands: **no Codex event-stream parser should be written until a re-run of this probe measures the real schema** |

**Summary: 0 confirmed, 0 refuted, 11 unmeasurable.** The brief's own framing — "every claim about
Codex's actual behavior is `[W]`... and should be converted to direct measurement... before any of
this interface design is finalized" — is unchanged by this run. This probe attempted exactly that
conversion and was blocked at the tool-permission layer before it could begin. No claim above should
be treated as either strengthened or weakened by this attempt; they remain exactly as uncertain as
`RESEARCH_BRIEF.md` left them.

---

## `$CODEX_HOME` and concurrent-writer probe (T-1.9) — could not be attempted

The story's architecture notes flag this as bearing directly on US-5's `resume_token` validation
design: `$CODEX_HOME` (default `~/.codex`) is shared across every worktree and task on the machine,
and whether it tolerates concurrent writers and whether one task's transcript is visible to another
were both to be measured here. Neither could be probed, for the same reason as everything else in
this document — no `codex` process could be started to write a transcript in the first place.
`Glob(".codex/**")` under `/Users/mariomh` found no existing `~/.codex` directory, but that only
means no prior interactive `codex` session had run under this user account before this probe — it
says nothing about concurrent-writer behavior. **US-5 must not assume either safety or unsafety of
concurrent `$CODEX_HOME` writes; treat it as unmeasured and design the `resume_token` validation to
fail closed on ambiguity until this is verified.**

---

## Subagent/hooks capability probe (T-1.10) — could not be attempted

Feeds US-8's content per the task table. Could not be attempted for the same reason. The only
material available on this topic is the `[W]`-tagged claims at RESEARCH_BRIEF.md:188-201 (subagents
via `/agent`, hooks via `~/.codex/hooks.json`/`config.toml`), both carried into the table above as
unmeasurable. US-8 should treat its subagent/hook design as provisional against documentation, not
as measured, until re-run.

---

## Re-verifying once `codex` can actually be invoked

This probe needs to be re-run from an environment where the permission policy does not deny
invoking `codex` — either a different sandbox configuration, or explicit approval of the specific
Bash invocations this story needs. When it is:

1. Confirm binary availability first: resolved path, `codex --version` output, exit code. Record
   both literally (AC-1.1).
2. Create a scratch directory outside every git worktree and every registered clone before running
   anything (AC-1.3) — e.g. a fresh `mktemp -d` outside `~/.lf-worktrees` and outside any repo clone
   path.
3. Work through T-1.2 → T-1.10 in the dependency order given in the task table (argv/stdin before
   the event-stream probe, the event-stream probe before session-id, session-id before resume
   semantics, argv before exit codes) — each probe's design depends on the previous one's confirmed
   shape, exactly as the task table's "Depends On" column encodes.
4. For every item in the AC-1.4/AC-1.5 table above, replace `[U]` with `[V]` plus the literal
   command and literal output, or leave `[U]` with an explicit reason if that specific sub-question
   still cannot be measured (e.g. requires an authenticated account this environment doesn't have).
5. Re-walk the `[W]` claim table above line by line against `RESEARCH_BRIEF.md` and mark each
   confirmed, refuted, or still-unmeasurable — do not leave any at "unmeasurable" without stating why
   it stayed that way.
6. Pay special attention to the two claims this story's architecture notes called out as having
   actual security consequences, not just design consequences: (k) whether
   `~/.codex/config.toml` can silently widen a `--sandbox` value passed on argv, and (l) whether
   `OPENAI_BASE_URL`/`OPENAI_API_BASE`-class env vars are honored. Either "yes" is a finding that
   changes US-5's design (respectively: argv-passed sandbox flags cannot be trusted in isolation if
   ambient config can widen them, and env-var forwarding for Codex cannot use a prefix allowlist if
   base-URL redirection is honored) — do not let a future re-run treat these as routine confirmations
   if they land as "yes."
7. Once this contract carries real `[V]` findings, notify whoever owns US-4/US-5/US-6/US-7/US-8 that
   their Codex-gated tasks are unblocked, and cite the specific sections of this file that changed.

Until step 7 happens, the gate statement above stands: every Codex-gated task in this feature's
later stories remains blocked, and this file is the reason why.
