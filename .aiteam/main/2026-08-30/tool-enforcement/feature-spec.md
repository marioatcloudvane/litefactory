# Feature Spec: Enforced tool boundary (tool-enforcement)
# Status: APPROVED
# Date: 2026-08-30

## Goal

Close the ambient-permission leak that makes every shipped tool profile advisory, so that the
`--tools` surface litefactory grants is the surface a run actually has.

Measured against `claude 2.1.251`: `WebFetch`/`WebSearch` `tool_use` blocks appear in run
transcripts while granted in neither `--tools` nor `--allowedTools`; `permissionMode` is
`default`. Same ambient leak as the T-LF3.0 spike — 250 allow-rules from the operator's
`~/.claude/settings.json`, reachable because `HOME` is forwarded (`internal/harness/env.go:42`).

## Decisions Carried In (settled, not re-opened)

| ID | Decision |
|----|----------|
| D-Q1 | `--setting-sources` is passed with an **empty value**: no discovery at all. `--setting-sources user` is **rejected** — it preserves exactly the scope the leak came from. `--settings <file>` is still passed explicitly. |
| D-Q2 | `--permission-mode dontAsk` is a **constant in code**, never a config key. A knob there is a way to weaken the boundary from `config.yaml`. |
| D-Q3 | `WebFetch`/`WebSearch` are grantable **unrestricted** — no domain rules, no partial hedge. Accepted risk, see *Accepted Risk*. |
| D-Q4 | Sandbox enabled for **every** run. Writes in the task worktree plus explicit carve-outs; `denyWrite` over `$LF_HOME`, registered clones and credential stores. |
| D-Q5 | The settings document is **overwritten unconditionally at the default path only**. A configured `claude.settings_path` is the operator's and is never rewritten. |
| D-Q6 | AC-2.3 is **superseded**: the enforcement flags are constant across every run; profiled and unprofiled argv still differ only in the two tool lists. |
| D-Q7 | **Option D (comment directive / `/tools`) is OUT** — a standing owner decision, reaffirmed 2026-08-30. This is a decision, not an omission: no second resolution path, no extension hook. |

## User Stories

| ID | Story |
|----|-------|
| US-1 | As the operator, I want every run spawned with settings discovery switched off, so a run's permissions come from litefactory and not from whatever accumulated in `~/.claude`. |
| US-2 | As the operator, I want every run spawned in `dontAsk` permission mode, so an unlisted call is denied by design rather than by the incidental absence of a prompt. |
| US-3 | As the operator, I want every run's `Bash` confined by the OS sandbox to the task worktree plus named build caches, so an allow-listed command is meaningfully less than arbitrary code execution. |
| US-4 | As a maintainer, I want `WebFetch`/`WebSearch` grantable through a profile with no domain restriction, so a research-shaped run can actually research. |
| US-5 | As the operator, I want the litefactory-owned settings document refreshed at the default path on `init` and on `daemon run` startup, so a change to `DefaultSettings` reaches existing installs. |
| US-6 | As a reviewer, I want `bypassPermissions` to be unconstructible and the enforcement flags pinned by a test, so the boundary cannot be widened by a later edit that looks harmless. |

## Data Model

No new `config.yaml` key. `Defaults()`, `template.go` and `TestDefaultFileMatchesDefaults`
are unchanged by this feature — that is a scope boundary, not an oversight (D-Q2).

### `harness.claudeSettings` — added / changed fields

| Field | JSON key | Type | Required | Constraints |
|-------|----------|------|----------|-------------|
| `Permissions.DefaultMode` | `permissions.defaultMode` | string | yes | `"dontAsk"` (was `"default"`). Falls back to `"default"` on probe failure P-2. |
| `Sandbox.Enabled` | `sandbox.enabled` | bool | yes | Always `true`. Not operator-settable. |
| `Sandbox.AllowWrite` | `sandbox` write allowlist | []string | yes | `$TMPDIR`, `~/Library/Caches/go-build`, `~/go/pkg/mod`, plus every registered repo's `worktree_root`. Sorted, deduplicated. |
| `Sandbox.DenyWrite` | `sandbox.denyWrite` | []string | yes | `$LF_HOME/**`, every registered repo's clone `path`, `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gh`, `~/.claude`, `~/.netrc`. Sorted, deduplicated. |
| `Sandbox.Network` | — | — | no | **Not configured.** No `allowedDomains`, no egress restriction (D-Q3). |

The nine existing `permissions.deny` `Read(...)` rules (`internal/harness/settings.go:86-95`) are
unchanged and remain load-bearing — they are the reason probe gate P-1 exists.

### `harness.SandboxPaths` — new input struct

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `WorktreeRoots` | []string | no | absolute; empty at `init` time |
| `ClonePaths` | []string | no | absolute; empty at `init` time |

### Relationships

- `store.Repo.Path` → `Sandbox.DenyWrite` via `daemon.Open` reading the store at startup.
- `store.Repo.WorktreeRoot` → `Sandbox.AllowWrite` via the same read.
- `config.Paths.SettingsPath()` → the only path this feature ever overwrites (D-Q5).

## API / Interface Changes

### Child argv — `Runner.args` (`internal/harness/runner.go:468`)

Exact target order. Everything but the two tool lists is constant across every run (D-Q6).

| Position | Flag | Value | Notes |
|----------|------|-------|-------|
| 1 | `-p` | — | unchanged |
| 2 | `--output-format` | `stream-json` | unchanged |
| 3 | `--verbose` | — | unchanged |
| 4 | `--session-id` / `--resume` | session id | unchanged |
| 5 | `--tools` | joined list | varies with profile |
| 6 | `--settings` | `r.settingsPath` | unchanged, **still explicitly passed** |
| 7 | `--setting-sources` | `""` (empty argv element) | **new**, gated on P-1 |
| 8 | `--permission-mode` | `dontAsk` | **new**, gated on P-2 |
| 9 | `--allowedTools` | joined list | varies with profile; still omitted when empty |
| 10 | `--strict-mcp-config` | — | unchanged |

### Go surface

| Symbol | Change | Notes |
|--------|--------|-------|
| `harness.DefaultSettings(home string)` | → `DefaultSettings(home string, sb SandboxPaths)` | sandbox block needs the store-derived paths |
| `harness.WriteSettings(path, home string)` | → `WriteSettings(path, home string, sb SandboxPaths)` | |
| `harness.EnsureSettings(path, home string)` | → `EnsureSettings(path, home string, sb SandboxPaths)` | unchanged semantics: creates only when absent |
| `harness.SandboxPaths` | new struct | |
| `config.bypassFlag()` | new, unexported | mirrors `forbiddenFlag()` (`policy.go:68-72`), assembled from fragments |
| `config.validateNoForbiddenFlag` | extended | also refuses `bypassPermissions` in every checked value |
| `daemon.Open` | new call | `harness.WriteSettings` before `harness.New`, only at the default path |
| `cli.runInit` | new call | `harness.WriteSettings` after `paths.EnsureHome()` |
| `cli` `repo add` / `repo remove` | new call | rewrite the document, so the deny list matches the registered set |

`internal/config` stays stdlib-only; the two new guards are pure string work.

## Business Rules

- **The enforcement flags are unconditional.** They do not depend on whether a profile
  resolved, on the label, or on any config key. Flags-only-on-profiled-runs was considered
  and rejected: it leaves the default unlabelled run as the leaky one.
- **`--setting-sources user` is never constructed.** It scopes discovery to exactly the file
  the leak came from. The empty value is the only accepted form.
- **`bypassPermissions` is unconstructible.** `config.bypassFlag()` assembles the literal from
  fragments so a repo-wide grep for the whole string stays a reliable review invariant and the
  guard does not defeat its own test — the same trick as `forbiddenFlag()`.
- **`permission-mode` is not a config key**, is not read from `config.yaml`, and has no
  override. The only two values that may ever appear in the tree are `dontAsk` and `default`.
- **The document's `defaultMode` and the argv `--permission-mode` always agree.** They are
  written from one constant, following the `--strict-mcp-config` / `enableAllProjectMcpServers`
  precedent at `settings.go:99-103`.
- **Overwrite only at `config.Paths.SettingsPath()`.** The write happens when
  `cfg.Claude.SettingsPath == paths.SettingsPath()`; otherwise the file is the operator's and
  is left alone. A configured value that happens to equal the default path is treated as the
  default path — the default path is litefactory's, whether or not it was written out.
- **A run still calls `EnsureSettings`, never `WriteSettings`.** A run must not discard an
  operator's edit mid-turn (`runner.go:389`).
- **The false doc comment at `settings.go:116-118` is corrected as part of this change.** Both
  halves of it are false today: `init` and `daemon run` do not call `WriteSettings`, and
  `internal/cli` references settings nowhere. After this feature the comment becomes true and
  must say so, naming the default-path condition.
- **Go build and test must keep working inside the sandbox.** `defaultAllowedTools()`
  (`policy.go:50-66`) grants `Bash(go build:*)`, `Bash(go test:*)`, `Bash(go vet:*)` and
  `Bash(gofmt:*)`, all of which write outside the worktree. Without the carve-outs these break
  *inside the agent*, not at startup, surfacing as mysterious agent failure. The carve-out is
  load-bearing, not convenience.
- **No committed test spawns a real `claude`.** The regression test for the ambient leak is a
  unit test over the constructed argv and the settings document. The probe document is the
  empirical half.

## Probe Gates and Contingencies

One probe document: `.aiteam/main/2026-08-30/tool-enforcement/CLAUDE_CLI_ENFORCEMENT_PROBE.md`,
following the `CLAUDE_CLI_CONTRACT.md` convention — read-only, in a scratch directory outside
the repo, stating the exact `claude` version (`2.1.251`) and how to re-verify after an upgrade.
Extend, do not start a third convention.

| ID | Probe case | Gate |
|----|-----------|------|
| P-1 | With `--setting-sources` empty, does an explicit `--settings <file>` still apply? Assert one of the nine `Read(...)` deny rules still denies. | **GATE, not a formality.** |
| P-1b | With `--setting-sources` empty, is an ambient `~/.claude/settings.json` allow-rule no longer honoured? | correctness of the fix |
| P-2 | Does `--permission-mode dontAsk` start and report `permissionMode: dontAsk` in the init event? | gate on shipping `dontAsk` |
| P-3 | With `sandbox.enabled` true, does a write inside the working directory succeed? | gate on assumption A-2 |
| P-4 | With `sandbox.enabled` true, does **`go test ./...` actually run** in a module with dependencies, with the carve-outs in place? | **required case** |
| P-5 | With `sandbox.enabled` true, is a write to a `denyWrite` path refused? | correctness of the deny list |
| P-6 | Is `WebFetch` reachable when granted on `--tools` under all of the above, and unreachable when not granted? | proves the leak is closed |

Contingencies — state the failure, do not assume the probe passes:

| If | Then |
|----|------|
| **P-1 fails** — an explicit `--settings` no longer applies with discovery off | **Do not ship `--setting-sources`.** litefactory would lose the nine `Read(...)` deny rules at `settings.go:86-95` and the change inverts into a regression. US-1 ships nothing; argv keeps today's shape on that axis. Record in the probe document and in `harness/doc.go` that the ambient leak remains open and that profiles remain advisory on that axis. US-2 and US-3 ship regardless. |
| **P-1 fails and `--bare` exists with the same effect and preserves `--settings`** | Ship `--bare` instead. `--setting-sources user` is still never constructed. |
| **P-2 fails** — `dontAsk` is rejected or unrecognised | Fall back to today's `default`, in both argv and `permissions.defaultMode`. The `bypassPermissions` guard (US-6) ships regardless: it is independent of the probe. |
| **P-3 fails** — the working directory is not writable by sandbox default | The daemon adds each registered repo's `worktree_root` to the write allowlist when it writes the document at startup; `init` writes it without them, no repo being registered yet. This is already the `SandboxPaths` mechanism, so no design change. |
| **P-4 fails** — `go test` cannot run under the sandbox with the listed carve-outs | The probe determines the minimal additional write paths and the spec's carve-out list is the probe's answer. Sandbox does not ship until `go test` runs green under it. |

## Accepted Risk — unrestricted `WebFetch`/`WebSearch` (D-Q3)

**Accepted by the user, 2026-08-30, having been told plainly what it costs. Recorded, not
re-litigated.**

Untrusted issue and comment text reaches the agent inside `<note>` blocks
(`internal/turn/prompt.go:328-363`). The sandbox configures no egress restriction, so a profile
that grants `WebFetch` or `WebSearch` gives that run an open outbound path. An instruction
injected into an issue body can therefore exfiltrate anything the agent can read.

Option (b) — domain-restricted web tools — was offered and declined. **No partial hedge is
implemented.** Prompt-injection hardening stays deferred; this feature changes the price of
that deferral.

## Edge Cases & Error Handling

- Probe P-1 fails → `--setting-sources` is not constructed at all; the leak is documented as
  open. No half-measure, no `--setting-sources user`.
- Probe P-2 fails → `permissions.defaultMode` and `--permission-mode` both stay `default`; a
  single constant means they cannot disagree.
- `claude.settings_path` configured away from the default → never overwritten; `EnsureSettings`
  still creates it if absent, exactly as today.
- `$LF_HOME` missing at `daemon run` startup → `WriteSettings` creates the directory at 0700
  via the existing `writeFileAtomic` path; no behaviour change.
- Settings write fails at `daemon run` startup → `Open` returns an error and the daemon does
  not start. A run under an unknown permission document is the outcome this feature exists to
  prevent; it fails closed.
- `repo add` / `repo remove` while the daemon is running → the document is rewritten
  immediately and atomically, so the deny list matches the registered set without a restart. A
  claude process already spawned keeps the document it read at start.
- `$TMPDIR` unset in the daemon's environment → the carve-out entry is omitted rather than
  written as an empty or `/`-rooted path; `TMPDIR` is already in the child env allowlist
  (`env.go:48`) and macOS sets it in practice.
- A registered clone whose `worktree_root` is inside the clone → cannot occur;
  `naming.DefaultWorktreeRoot` places it as a sibling and `naming/paths_test.go:71` pins it. If
  an operator configures an overlapping root, `denyWrite` wins, the run cannot write its
  worktree, and the run fails loudly rather than escaping the deny.
- A tool call denied by `dontAsk` → returns an error to the model and the turn continues; it
  does not hang and does not abort the run. Unchanged from today's effective behaviour.

## Out of Scope

- **Option D — comment directive / `/tools`.** Out by standing owner decision, reaffirmed
  2026-08-30. No second resolution path, no extension hook, no comment parsing.
- Options B (per-tool labels) and C (repo-committed policy file) — rejected outright, still.
- Anything already shipped in merge `a21d89a`: tool profiles, the ceiling, label ingestion, the
  three refusal replies, requested-vs-resolved reporting. Not re-specified here.
- Domain restriction, allow-listing or any other partial restriction of `WebFetch`/`WebSearch`
  (D-Q3).
- Prompt-injection hardening.
- `--permission-prompt-tool` and `PreToolUse` hooks. Still the escape hatch if per-call
  adjudication is ever required.
- Any new `config.yaml` key. In particular, no key selecting the permission mode, the setting
  sources, or the sandbox state.
- Webhooks or any change to the polling trigger model.
- A committed test that spawns a real `claude`.

## Assumptions

Stated because they are not yet measured. Each is a probe case above; none is a placeholder.

| ID | Assumption | Settled by |
|----|-----------|------------|
| A-1 | `--setting-sources` accepts an empty value as a separate argv element and reads it as "no sources", rather than swallowing the next flag. | P-1 |
| A-2 | The sandbox permits writes in the child's working directory (the task worktree) by default. | P-3 |
| A-3 | `go build` / `go test` need write access to `~/Library/Caches/go-build`, `~/go/pkg/mod` and `$TMPDIR`, and nothing else outside the worktree. The probe's answer is authoritative over this list. | P-4 |
| A-4 | The sandbox JSON key names are `sandbox.enabled` and `sandbox.denyWrite`, with a sibling write-allow list. The probe fixes the exact spelling before implementation; an invented key is at best ignored and at worst a hard child error that looks like a hung run (`settings.go:44-47`). | P-3, P-5 |
| A-5 | `WebFetch`/`WebSearch` are ordinary `--tools` names already accepted by `toolNameRE` (`policy.go:19`), so making them grantable needs no code change beyond configuring no network restriction. | P-6 |

## Acceptance Criteria

- [ ] AC-1.1 Every run's argv carries `--setting-sources` with an empty value, in the position
      given in *API / Interface Changes*, whether or not a profile resolved.
- [ ] AC-1.2 `--settings <path>` is still passed explicitly on every run.
- [ ] AC-1.3 The literal `--setting-sources user` is constructed nowhere in the tree; a unit
      test asserts the value litefactory passes is empty.
- [ ] AC-1.4 A unit test over the constructed argv and the settings document is the regression
      test for the ambient leak. No committed test spawns `claude`.
- [ ] AC-2.1 Every run's argv carries `--permission-mode dontAsk`.
- [ ] AC-2.2 `permissions.defaultMode` in the written document is `dontAsk`, from the same
      constant as the flag; a test asserts the two cannot disagree.
- [ ] AC-2.3 No `config.yaml` key selects the permission mode; `TestDefaultFileMatchesDefaults`
      passes unmodified.
- [ ] AC-2.4 `config.bypassFlag()` assembles `bypassPermissions` from fragments; a repo-wide
      test fails on any `.go` file containing the whole literal, and the guard's own source does
      not contain it.
- [ ] AC-2.5 `validateNoForbiddenFlag` refuses a configured `claude.binary`, `settings_path`,
      tool list, ceiling or profile list that smuggles in `bypassPermissions`, with the existing
      aggregated-error shape.
- [ ] AC-3.1 The written document sets `sandbox.enabled` true for every run, unconditionally.
- [ ] AC-3.2 The write allowlist contains `$TMPDIR`, the Go build cache and the Go module
      cache, sorted and deduplicated, plus every registered `worktree_root`.
- [ ] AC-3.3 The deny list contains `$LF_HOME`, every registered clone path, and `~/.ssh`,
      `~/.aws`, `~/.gnupg`, `~/.config/gh`, `~/.claude`, `~/.netrc`.
- [ ] AC-3.4 The nine existing `permissions.deny` `Read(...)` rules are present and unchanged.
- [ ] AC-3.5 Probe case P-4 runs a real `go test` under the sandbox and is recorded in the probe
      document. Sandbox does not ship until it is green.
- [ ] AC-3.6 No sandbox network restriction is configured; the document contains no domain
      allowlist.
- [ ] AC-4.1 A profile granting `WebFetch` or `WebSearch` produces those names on `--tools`; no
      domain rule is generated anywhere.
- [ ] AC-4.2 The accepted risk is recorded verbatim in `internal/harness/doc.go` with its
      attribution and date, so it is readable from the code.
- [ ] AC-5.1 `litefactory init` overwrites the document at `Paths.SettingsPath()`
      unconditionally.
- [ ] AC-5.2 `daemon run` startup overwrites the document at `Paths.SettingsPath()`
      unconditionally, before `harness.New`, and fails to start when the write fails.
- [ ] AC-5.3 A configured `claude.settings_path` other than the default is never overwritten; a
      test asserts an operator-authored file at a custom path survives both `init` and
      `daemon run`.
- [ ] AC-5.4 `repo add` and `repo remove` rewrite the document so the deny list matches the
      registered set.
- [ ] AC-5.5 A run calls `EnsureSettings` only; a test asserts a run never overwrites an
      existing document.
- [ ] AC-5.6 The doc comment at `settings.go:116-118` is corrected and now names its real
      callers and the default-path condition.
- [ ] AC-6.1 AC-2.3 (old) is superseded. `TestRunWithNoProfileArgvIsByteIdentical`
      (`internal/harness/runner_test.go:1200`) is replaced by a test asserting that the
      enforcement flags are present and identical on both a profiled and an unprofiled run, and
      that the two argvs differ only in the values of `--tools` and `--allowedTools`.
- [ ] AC-6.2 `internal/turn/harness_test.go:275-279` still passes: a resumed run keeps the tool
      flags, and now the enforcement flags too.
- [ ] AC-6.3 `CLAUDE_CLI_ENFORCEMENT_PROBE.md` exists, names `claude 2.1.251`, records every
      probe case P-1 to P-6 with its outcome, and carries a re-verification checklist for the
      next CLI upgrade.
- [ ] AC-6.4 `make check` is green: `gofmt`, `go vet`, golangci-lint v2, `go test -race ./...`.
