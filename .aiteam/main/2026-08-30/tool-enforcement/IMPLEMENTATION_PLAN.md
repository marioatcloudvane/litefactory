# Implementation Plan — tool-enforcement
# Branch: main
# Date: 2026-08-30

Successor to `.aiteam/main/2026-08-26/tool-permissions/`, whose label→profile feature shipped in
merge `a21d89a`. This plan covers only the enforcement residue that plan deferred under its
decision D-8. Nothing already shipped is re-planned here.

**The problem.** Measured on `claude 2.1.251`: `WebFetch`/`WebSearch` `tool_use` blocks appear in
run transcripts while granted in neither `--tools` nor `--allowedTools`; `permissionMode` is
`default`. Same ambient leak as the T-LF3.0 spike — 250 allow-rules from the operator's
`~/.claude/settings.json`, reachable because `HOME` is forwarded (`internal/harness/env.go:42`).
**Every shipped tool profile is therefore advisory, not enforced.** That is what this plan fixes.

## Invariants (every story is subordinate to these)

- **The enforcement flags are unconditional.** They do not depend on a resolved profile, on the
  label, or on any config key. Flags-only-on-profiled-runs was considered and rejected: it leaves
  the default unlabelled run as the leaky one.
- **No new `config.yaml` key.** Not for the permission mode, the setting sources, or the sandbox.
  `Defaults()`, `template.go` and `TestDefaultFileMatchesDefaults` are untouched — a scope
  boundary, not an oversight.
- **No committed test spawns a real `claude`.** Probes are read-only, in a scratch directory
  outside the repo, written up in one document naming the exact CLI version, per the
  `CLAUDE_CLI_CONTRACT.md` convention. Extend it; do not start a third convention.
- **`internal/config` stays stdlib-only** (`purity_test.go`). `go.mod` stays at three direct
  requires. No mocking framework.
- **Untrusted input goes on stdin, never argv.** The child env stays an allowlist.
- **A probe is a gate, not a formality.** No lever is adopted on documentation alone, and every
  probe has a written failure path below.

## Decisions carried in (settled by the user 2026-08-30, not re-opened)

| ID | Decision |
|----|----------|
| D-Q1 | `--setting-sources` is passed with an **empty value**: no discovery at all. `--setting-sources user` is **rejected** — it preserves exactly the scope the leak came from. `--settings <file>` is still passed explicitly. |
| D-Q2 | `--permission-mode dontAsk` is a **constant in code**, never a config key. |
| D-Q3 | `WebFetch`/`WebSearch` are grantable **unrestricted**. Accepted risk — see below. No partial hedge. |
| D-Q4 | Sandbox enabled for **every** run, with Go-cache and `$TMPDIR` write carve-outs. |
| D-Q5 | The settings document is **overwritten unconditionally at the default path only**. A configured `claude.settings_path` is the operator's and is never rewritten. |
| D-Q6 | AC-2.3 *of the 2026-08-27 plan* is **superseded**: the enforcement flags are constant across every run; profiled and unprofiled argv still differ only in the two tool lists. |
| D-Q7 | **Option D (comment directive / `/tools`) is OUT** by standing owner decision. A decision, not an omission: no second resolution path, no extension hook. |

## Decisions taken during planning

| ID | Decision | Why |
|----|----------|-----|
| D-A1 | The `cli → harness` edge is **accepted, for `cmd_init.go` only**, pinned by a policy test in the shape of `identity/policy_test.go`. | Acyclic and points down the map. Routing through `daemon` couples `init` to the tick loop; moving authorship into `config` splits the document from the argv constant that AC-2.2 requires them to share. |
| D-A2 | `AC-5.4 as specified is replaced by R1`: the **daemon** keeps the deny list in sync on the tick, change-detected on marshalled bytes. `repo add`/`repo remove` write no settings document. | A CLI command cannot know whether a daemon runs, which `$LF_HOME` it resolved, or whether its settings path is the default. Three uncoordinated writers of one file race: atomic rename protects *readers*, not writers from each other. |
| D-A3 | The three settings functions take **one `SettingsOptions` struct**, not `(path, home string, sb SandboxPaths)`. | Two adjacent absolute-path strings are swappable at the call site with no compile error, and the change grows call sites from 2 to 5. |
| D-A4 | The argv pin stays **absolute**. AC-6.1's differential test is added *alongside* it, not instead of it. | A relative "the two argvs differ only in the tool lists" assertion passes when both argvs are wrong in the same way — which is every future edit that adds or drops a constant flag. |
| D-A5 | **`--bare` is rejected outright**, not held as a P-1 fallback. | Its 2.1.251 help: auth becomes strictly `ANTHROPIC_API_KEY` or `apiKeyHelper`; OAuth and keychain are never read. This deployment authenticates through the macOS Keychain. `--bare` would break every run. |
| D-A6 | **US-4 is a documentation story, not an implementation story.** | If A-5 holds, `toolNameRE` already accepts both names and they are operator-authored config. AC-4.1 is already true today; it needs a test and a probe, not code. |
| D-A7 | The guard must also catch **`--allow-dangerously-skip-permissions`**, a real flag on 2.1.251. | Verified by inspection: `forbiddenFlag()` builds `--dangerously-skip-permissions`; the allow-variant contains only `-dangerously-skip-permissions`, one hyphen, so `strings.Contains` does not match. It is the enabling half of the pair and slips both today's guard and the spec's extension. |
| D-A8 | **Open, needs an owner answer before T-5 concludes:** if P-4 shows the shared Go caches must be writable, prefer per-run `GOCACHE`/`GOMODCACHE` under `$TMPDIR` and deny the shared caches. | A writable `GOMODCACHE` poisons every later build on the machine, not just this run. But the alternative adds two names to `harness/env.go`'s allowlist — a boundary change needing its own justification. Do not decide this inside Implement. |
| D-A9 | **`Read(~/.claude/**)` is too broad and is replaced by an enumerated set (AC-3.3a).** It denies litefactory's own runtime data alongside the operator's secrets. Measured on issue #7, runs 0001-0011: **2 `Read` denials on the run's own persisted tool-output overflow files** under `~/.claude/projects/<project>/<session>/tool-results/`, and **1 `Write` denial** on `~/.claude/projects/<project>/memory/`. | The rule exists to keep credentials and the operator's settings away from a run, and that part is right — `.credentials.json` holds an OAuth token, `history.jsonl` holds every prompt typed on this machine, and `projects/**/*.jsonl` holds every session transcript across every repo. But `tool-results/` is **the run's own output**, persisted by the CLI and handed back to the model by path: denying it makes the overflow mechanism unusable and the failure is silent. Deny patterns cannot negate, so the fix is enumeration, not a carve-out expression. **Memory stays denied on purpose** — `projects/*/memory/` is global and cross-repo, and a run driven by a third-party issue comment must not write there. Found while clearing lf/7's blockers; folded here rather than hand-patched, because **US-3 rewrites this document anyway and P-8b can verify it** where a hand-patch could only assert it. |

## Probe gates

One document: `.aiteam/main/2026-08-30/tool-enforcement/CLAUDE_CLI_ENFORCEMENT_PROBE.md`, naming
`claude 2.1.251`, recording **the exact argv per case**, with a re-verification checklist.

| ID | Case | Grade |
|----|------|-------|
| P-0 | Settle the sandbox settings-key spelling from the CLI's own schema. Does an unknown key error, or is it silently ignored? | **GATE** — all sandbox work |
| P-1 | With `--setting-sources` empty, does an explicit `--settings <file>` still apply? Assert one of the `Read(...)` rules still denies. | **GATE** |
| P-1b | With `--setting-sources` empty, is an ambient `~/.claude` allow-rule no longer honoured? | **GATE** |
| P-2 | Does `--permission-mode dontAsk` start and report `permissionMode: dontAsk`? | **GATE** |
| P-3 | With the sandbox on, does a write inside the working directory succeed? | GATE — A-2 |
| P-4 | Does a real `go test ./...` run in a module with dependencies, with the carve-outs in place? | **GATE** |
| P-5 | Is a write to a `denyWrite` path refused? | correctness |
| P-5b | **Negative control:** with the sandbox **off**, does that same write *succeed*? | **GATE** |
| P-6 | Is `WebFetch` reachable when granted and unreachable when not — **and does egress exist at all with no network key configured?** | GATE — A-5 |
| P-7 | Does `dontAsk` actually **deny** a tool that is on `--tools` but absent from `--allowedTools`? | **GATE** |
| P-8 | The **composed** argv, all levers at once: do the `Read(...)` rules of **AC-3.3a's amended set** still deny? | **GATE** |
| P-8b | **The `~/.claude` split, both halves in one case (D-A9).** `.credentials.json`, `history.jsonl` and another session's `projects/**/*.jsonl` transcript are **refused**; the run's own `projects/**/tool-results/*.txt` overflow file is **read successfully**. A pass on only the deny half is a failing probe | **GATE** |

**Contingencies — state the failure, do not assume the probe passes.**

| If | Then |
|----|------|
| **P-0 shows unknown keys are ignored** | The bad outcome is not an error but an inert document: US-3 ships, nothing is sandboxed, and AC-3.1 still passes because it only inspects what litefactory wrote. P-5b is the only thing that catches this. Sandbox does not ship without P-5b green. |
| **P-1 outcome (a)** empty parses as the empty list, discovery off, `--settings` still applies | Ship. |
| **P-1 outcome (b)** hard error on an unknown source name (`[""]`) | **Do not ship `--setting-sources`.** There is no "valid but harmless" source — all three *are* the leak surface. |
| **P-1 outcome (c)** silently ignored | **Do not ship.** argv would look hardened while nothing changed; AC-1.4's unit test would pass regardless. Caught only by P-1b. |
| **P-1 fails** on any of the above | US-1 ships nothing; argv keeps today's shape on that axis. Record in the probe document **and in `harness/doc.go`** that the ambient leak remains open and profiles remain advisory on that axis. **US-2, US-3 and US-6 ship regardless.** `--bare` is not a fallback (D-A5). |
| **P-2 fails** — `dontAsk` rejected | Fall back to today's `default`, in both argv and `permissions.defaultMode`, from the one shared constant. US-6 ships regardless. |
| **P-7 shows `dontAsk` auto-allows** | **US-2 is a widening, not a hardening.** Do not ship it; keep `default`. `auto` sits beside `dontAsk` in the CLI's choice list and the name is genuinely ambiguous — this is the same inversion shape P-1 guards against. |
| **P-3 fails** — cwd not writable by default | The daemon adds each registered `worktree_root` to the write allowlist. Already the `SandboxPaths` mechanism; no design change. |
| **P-4 fails** | The probe determines the minimal additional write paths and its answer is authoritative over the carve-out list. **Sandbox does not ship until `go test` runs green under it.** If shared caches are implicated, escalate D-A8 to the owner. |

---

## User Story 1 — Settings discovery is switched off

> As the operator, I want every run spawned with settings discovery switched off, so a run's permissions come from litefactory and not from whatever accumulated in `~/.claude`.

Closes the measured ambient leak by removing the discovery path that made `--allowedTools` additive.

### Architecture Notes
- `--setting-sources` with an **empty value** at argv position 7; `--settings` still passed explicitly at position 6.
- Platform: the CLI's `--setting-sources <sources>` is a value-taking option, so an empty next element cannot swallow `--permission-mode`. The real unknown is whether `""` parses as the empty list or as `[""]`, one invalid source name — P-1 must distinguish them.
- Risk: the dangerous failure is **silent**, not loud. argv looks hardened, nothing changed, and a unit test over argv passes anyway. P-1b is the only detection, which is why it is graded a gate.
- Shares `Runner.args` and the argv pin with US-2 — serialise the *edits*, not the design.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2 | Probe P-1/P-1b; record all three P-1 outcomes distinctly | infra | — | M |
| T-7 | Probe P-8: composed argv; nine `Read(...)` rules still deny | infra | T-2, T-3, T-4 | S |
| T-21 | Update the argv pin to the new constant sequence; keep whole-argv `slices.Equal`; rename off `…ByteIdentical` | test | — | S |
| T-22 | Add `--setting-sources ""` at position 7 | api | T-2, T-7, T-21 | XS |
| T-23 | Tests: the value passed is empty; `--setting-sources user` appears nowhere; `--settings` still passed | test | T-22 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-1.1 | Every run's argv carries `--setting-sources` with an empty value, at the stated position, whether or not a profile resolved |
| AC-1.2 | `--settings <path>` is still passed explicitly on every run |
| AC-1.3 | The literal `--setting-sources user` is constructed nowhere in the tree; a unit test asserts the value passed is empty |
| AC-1.4 | A unit test over the constructed argv and the settings document is the regression test for the ambient leak; no committed test spawns `claude` |
| AC-1.5 | If P-1 fails, `--setting-sources` is not constructed at all and the open leak is recorded in `harness/doc.go` |

---

## User Story 2 — Unlisted calls are denied by design

> As the operator, I want every run spawned in `dontAsk` permission mode, so an unlisted call is denied by design rather than by the incidental absence of a prompt.

Replaces today's reliance on `default` mode having nobody to prompt with an explicit automation mode.

### Architecture Notes
- One `permissionMode` constant shared by the argv flag and `permissions.defaultMode`, following the `--strict-mcp-config` / `enableAllProjectMcpServers` precedent at `settings.go:99-103`. They cannot disagree because there is one source.
- Platform: `dontAsk` is confirmed present in the CLI's mode list on 2.1.251, alongside `acceptEdits, auto, bypassPermissions, manual, plan`.
- Risk: the mode's *semantics* are unverified. If `dontAsk` means "do not prompt, and auto-allow", this story is a **widening**. P-7 is the gate; do not ship on the name alone.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3 | Probe P-2/P-7: `dontAsk` starts, is reported in the init event, and **denies** a tool on `--tools` absent from `--allowedTools` | infra | — | M |
| T-24 | One `permissionMode` constant shared by argv and `permissions.defaultMode`; add `--permission-mode` at position 8 | api | T-3, T-21 | S |
| T-25 | Tests: argv and document cannot disagree; `TestDefaultFileMatchesDefaults` passes unmodified | test | T-24 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-2.1 | Every run's argv carries `--permission-mode dontAsk` |
| AC-2.2 | `permissions.defaultMode` is `dontAsk`, from the same constant as the flag; a test asserts the two cannot disagree |
| AC-2.4 | No `config.yaml` key selects the permission mode; `TestDefaultFileMatchesDefaults` passes unmodified |
| AC-2.5 | P-7 is green before this ships; if it shows auto-allow, the mode stays `default` |

*(There is deliberately no AC-2.3 in this story. The spec drafted one, but `AC-2.3` already names the superseded argv criterion of the 2026-08-27 plan and reusing the ID in the same tree would be ambiguous.)*

---

## User Story 3 — Bash is confined by the OS sandbox

> As the operator, I want every run's `Bash` confined by the OS sandbox to the task worktree plus named build caches, so an allow-listed command is meaningfully less than arbitrary code execution.

Makes an allow-listed `Bash` less than arbitrary execution, which prefix rules cannot do because they do not survive `&&`.

### Architecture Notes
- Enabled unconditionally for every run. Write allowlist and `denyWrite` list both **sorted and deduplicated** — this is load-bearing, not cosmetic: `ListRepos` ordering would otherwise make the document non-deterministic and defeat R1's byte-comparison change detection.
- The Go carve-out is load-bearing. `defaultAllowedTools()` grants `Bash(go build:*)`, `Bash(go test:*)`, `Bash(go vet:*)`, `Bash(gofmt:*)`, all of which write outside the worktree. Without it they break *inside the agent*, not at startup, surfacing as mysterious agent failure.
- Deny list gains `~/Library/LaunchAgents` (a run that can write its own LaunchAgent plist has persistence, and `internal/launchd` is the package that puts one there) and `~/go/bin` (on the child's PATH while `~/go/pkg/mod` is write-allowed).
- Platform: there is no `--sandbox` flag; the sandbox is settings-only. Key spelling is therefore entirely unmeasured and is the highest-risk unknown in this plan — P-0 first.
- Risk: a configured-but-inert sandbox. An ignored key produces a document that passes AC-3.1 while sandboxing nothing. P-5b is the negative control that separates "configured" from "believed".

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1 | Probe P-0: settle the sandbox key spelling; determine whether an unknown key errors or is ignored | infra | — | S |
| T-4 | Probe P-3/P-5/P-5b: cwd write succeeds; `denyWrite` refused; **negative control with the sandbox off** | infra | T-1 | M |
| T-5 | Probe P-4: real `go test ./...` under the sandbox; determine the minimal carve-out set, including the per-run `GOCACHE`/`GOMODCACHE` alternative (D-A8) | infra | T-1, T-4 | M |
| T-26 | Sandbox block in `claudeSettings` with probe-settled key names; lists sorted + deduped; add `~/Library/LaunchAgents`, `~/go/bin` | data | T-1, T-5, T-12 | M |
| T-27 | Tests: enabled unconditionally; allow-list contents; deny-list contents; **`Read(...)` rules match AC-3.3a's amended set**; no network key | test | T-26 | M |
| T-27a | **D-A9: replace `Read(~/.claude/**)` with the enumerated set in `DefaultSettings` (`harness/settings.go`), and update `settings_test.go`.** Deny `.credentials.json`, `settings.json`, `settings.local.json`, `history.jsonl`, `sessions/**`, `session-env/**`, `shell-snapshots/**`, `backups/**`, `projects/**/*.jsonl`; leave `projects/**/tool-results/**` readable; **keep `projects/*/memory/` denied**. Gated on P-8b | data | T-26 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-3.1 | The written document enables the sandbox for every run, unconditionally |
| AC-3.2 | The write allowlist contains `$TMPDIR`, the Go build cache and module cache, sorted and deduplicated, plus every registered `worktree_root` |
| AC-3.3 | The deny list contains `$LF_HOME`, every registered clone path, `~/Library/LaunchAgents`, `~/go/bin`, and `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gh`, `~/.claude`, `~/.netrc` |
| AC-3.3a | **`Read(~/.claude/**)` is replaced, not kept.** The deny list enumerates the sensitive entries individually — `.credentials.json`, `settings.json`, `settings.local.json`, `history.jsonl`, `sessions/**`, `session-env/**`, `shell-snapshots/**`, `backups/**`, `projects/**/*.jsonl` — and **`~/.claude/projects/**/tool-results/**` is readable**. See D-A9 |
| AC-3.4 | The `permissions.deny` `Read(...)` rules are present and match **AC-3.3a's amended set**, not the original nine. Every entry outside `~/.claude` — `$LF_HOME`, `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gh`, `~/.netrc`, `./.env`, `./.env.*` — is unchanged |
| AC-3.4a | A run can read back its own persisted tool-output overflow file under `~/.claude/projects/<project>/<session>/tool-results/`, and **`.credentials.json`, `history.jsonl` and another session's `*.jsonl` transcript all still deny**. P-8 covers both halves |
| AC-3.5 | P-4 runs a real `go test` under the sandbox and is recorded; the sandbox does not ship until it is green |
| AC-3.6 | P-5b is green: with the sandbox off, the same denied write succeeds |
| AC-3.7 | No sandbox network restriction is configured; the document contains no domain allowlist |
| AC-3.8 | `$TMPDIR` unset → the entry is omitted, never written as empty or `/`-rooted |

---

## User Story 4 — Web tools are grantable through a profile

> As a maintainer, I want `WebFetch`/`WebSearch` grantable through a profile with no domain restriction, so a research-shaped run can actually research.

Documentation and verification only — per D-A6 there is no code to write if A-5 holds.

### Architecture Notes
- `toolNameRE` (`policy.go:19`) already accepts both names, and profile entries are operator-authored config, so AC-4.1 is already true today. The deliverables are the probe and the recorded risk.
- Risk: the spec assumes "no network key configured" means unrestricted egress. On a sandbox it is equally likely to mean **default-deny**, in which case this story ships and `WebFetch` silently never works — and `go test` on a module needing a download breaks inside the agent. P-6 must settle it.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6 | Probe P-6: `WebFetch` reachable when granted / unreachable when not, **and whether egress exists at all with no network key** | infra | T-1 | S |
| T-28 | Record the accepted risk verbatim in `internal/harness/doc.go` with attribution and date | infra | — | XS |
| T-29 | Test: a profile granting `WebFetch`/`WebSearch` produces those names on `--tools`; no domain rule generated anywhere | test | — | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-4.1 | A profile granting `WebFetch`/`WebSearch` produces those names on `--tools`; no domain rule is generated anywhere |
| AC-4.2 | The accepted risk is recorded in `internal/harness/doc.go` with its attribution and date, readable from the code |
| AC-4.3 | P-6 establishes whether an unconfigured network key means unrestricted or default-deny, and the answer is recorded |

---

## User Story 5 — The settings document reaches existing installs

> As the operator, I want the litefactory-owned settings document refreshed at the default path on `init` and on `daemon run` startup, so a change to `DefaultSettings` reaches existing installs.

The prerequisite for US-3: without it, no sandbox block ever reaches a machine that already has a settings file.

### Architecture Notes
- Today `WriteSettings` has **no production caller that replaces the file**. The only paths are `settings_test.go` and the internal call inside `EnsureSettings`, which writes only when the file is absent. The doc comment at `settings.go:116-118` claims `init` and `daemon run` call it; both halves are false — `internal/cli` references settings nowhere.
- Overwrite only where `cfg.Claude.SettingsPath == paths.SettingsPath()`, compared **`filepath.Clean`ed on both sides**: `Paths` cleans in `NewPaths`, the operator's YAML string does not. Consequence to state in the corrected comment: an operator who explicitly writes the default path is treated as having omitted it. That is the right trade — the alternative makes the litefactory-owned filename inside litefactory's own `$LF_HOME` not litefactory's.
- **One definition of the "is this the default path" predicate.** `harness.New` already re-derives the default independently at `runner.go:116-118`, alongside `applyPathDefaults` — three hand-written `==` comparisons across `cli`, `daemon` and `harness` is how AC-5.3 silently stops holding.
- `EnsureSettings` stays on the run path but becomes the one path that can create a document **without** the store-derived sandbox paths. `Runner` must therefore hold `SandboxPaths` too, or a configured-path install creates a sandbox-less document on a run.
- Platform: do not introduce a `SettingsWriter` interface to dodge the `cli → harness` import. `github/surface_test.go` pins "no interface type is declared in the package" precisely against this reflex. Take the concrete import and constrain it with a policy test.
- Risk: `SandboxPaths` entries come from the store and are unvalidated. A relative or empty entry becomes a deny rule matching nothing — it **fails open, silently**. `DefaultSettings` already rejects a non-absolute `home`; it must reject non-absolute entries too.
- Risk: `init` failing its settings write must be a **hard** failure. `init` is best-effort elsewhere (token failures are warnings); if it is best-effort here it reports success on a machine whose next run is unbounded.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-12 | `SettingsOptions` struct replacing the `(path, home string)` pair; thread through all three functions; clone slices at the boundary, validate absolute, sort, dedupe | data | — | M |
| T-13 | One definition of "is this the default settings path" (`Clean`ed), used by `cli`, `daemon` and `harness.New` | logic | T-12 | S |
| T-14 | `daemon.Open` writes at startup from `ListRepos`, before `harness.New`; fails closed; settings path surfaced on `Startup` | api | T-12, T-13 | M |
| T-15 | `cli.runInit` writes after `EnsureHome()`; **hard failure**; the new `cli → harness` edge | api | T-12, T-13 | S |
| T-16 | Policy test pinning the edge: only `cmd_init.go` imports `harness` from `cli`, and only its settings surface | test | T-15 | S |
| T-17 | `Runner` holds `SandboxPaths` so `EnsureSettings` cannot create a weaker document; test byte-identity with `WriteSettings` | logic | T-12 | S |
| T-18 | R1: the daemon keeps the deny list in sync on the tick, change-detected on marshalled bytes held in memory | logic | T-14 | M |
| T-19 | Correct the `settings.go:116-118` doc comment — real callers, default-path condition, explicit-default consequence | infra | T-14, T-15 | XS |
| T-20 | Tests: init overwrites, daemon overwrites, a configured path survives both, a run never overwrites | test | T-14, T-15, T-17 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-5.1 | `litefactory init` overwrites the document at `Paths.SettingsPath()` unconditionally, and **fails hard** when the write fails |
| AC-5.2 | `daemon run` startup overwrites at `Paths.SettingsPath()` before `harness.New`, and does not start when the write fails |
| AC-5.3 | A configured `claude.settings_path` other than the default is never overwritten; a test asserts an operator-authored file at a custom path survives both `init` and `daemon run` |
| AC-5.4 | The daemon keeps the deny list in sync with the registered set, refreshed on the tick; **`repo add`/`repo remove` write no settings document** |
| AC-5.5 | A run calls `EnsureSettings` only; a test asserts a run never overwrites an existing document |
| AC-5.6 | A document created by `EnsureSettings` on a run is byte-identical to what `WriteSettings` would write from the same inputs at that moment |
| AC-5.7 | Non-absolute or empty `SandboxPaths` entries are rejected, not silently emitted as inert rules |
| AC-5.8 | The doc comment at `settings.go:116-118` is corrected and names its real callers and the default-path condition |
| AC-5.9 | The settings path in force is visible on the `Startup` log line |

---

## User Story 6 — The boundary cannot be widened by a later edit

> As a reviewer, I want `bypassPermissions` to be unconstructible and the enforcement flags pinned by a test, so the boundary cannot be widened by a later edit that looks harmless.

The only story gated on nothing — land it first.

### Architecture Notes
- `config.bypassFlag()` mirrors `forbiddenFlag()`'s assembled-from-fragments trick, so a repo-wide grep for the whole literal stays a reliable review invariant and the guard does not defeat its own test.
- **`--allow-dangerously-skip-permissions` is a real flag on 2.1.251 and slips today's guard** (D-A7). It is the enabling half of the escalation pair. Add it as a third needle, assembled the same way.
- Extend the existing repo-wide walker with a **needle slice** rather than copy-pasting a second walk. It already skips `.aiteam`/`.claude` and filters to `.go`, so the probe document's prose cannot trip it.
- The `#nosec G204` justification enumerates argv's contents and becomes untrue once flags are added. All four new elements are package constants and `SandboxPaths` reaches the settings *file*, never argv — so the justification holds, but updating its text is part of the change.
- The argv pin stays absolute (D-A4). The differential test is added alongside.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-9 | `config.bypassFlag()` + extend `validateNoForbiddenFlag`; include the `--allow-dangerously-skip-permissions` gap | logic | — | S |
| T-10 | Extend the existing repo-wide walker to a needle slice; `.go`-only, `.aiteam`/`.claude` skipped | test | T-9 | S |
| T-11 | Unit tests: the guard refuses the mode name in binary / settings_path / tools / ceiling / profiles, aggregated | test | T-9 | S |
| T-8 | Write `CLAUDE_CLI_ENFORCEMENT_PROBE.md`: every case, **exact argv per case**, `claude 2.1.251`, re-verification checklist | infra | T-1..T-7 | M |
| T-30 | Extend `turn/harness_test.go:275-279`'s flag list with the two enforcement flags | test | T-22, T-24 | XS |
| T-31 | Extend the `#nosec G204` justification and the `args` doc comment to name the new flags | infra | T-22, T-24 | XS |
| T-32 | Update `APP_CONTEXT.md`: package map (`cli → harness`), new invariants, superseded AC-2.3 | infra | T-16, T-18 | S |
| T-33 | `make check` green: gofmt, vet, golangci-lint v2, `go test -race ./...` | test | all | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-6.1 | `config.bypassFlag()` assembles the mode name from fragments; a repo-wide test fails on any `.go` file containing the whole literal, and the guard's own source does not contain it |
| AC-6.2 | The guard also refuses `--allow-dangerously-skip-permissions`; a test covers it specifically |
| AC-6.3 | `validateNoForbiddenFlag` refuses a configured binary, settings_path, tool list, ceiling or profile list that smuggles in any needle, with the existing aggregated-error shape |
| AC-6.4 | The absolute whole-argv pin survives, updated to the new constant sequence and renamed for what it pins |
| AC-6.5 | A second, differential test asserts a profiled and an unprofiled argv differ only in the values of `--tools` and `--allowedTools` |
| AC-6.6 | `internal/turn/harness_test.go:275-279` still passes: a resumed run keeps the tool flags, and now the enforcement flags too |
| AC-6.7 | `CLAUDE_CLI_ENFORCEMENT_PROBE.md` exists, names `claude 2.1.251`, records P-0 to P-8 with outcomes **and exact argv per case**, and carries a re-verification checklist |
| AC-6.8 | `APP_CONTEXT.md` records the new `cli → harness` edge, the new invariants, and that AC-2.3 of the 2026-08-27 plan is superseded |
| AC-6.9 | `make check` is green |

---

## Implementation order

1. **US-6's guard (T-9, T-10, T-11) — gated on nothing.** Land it before the rest of the change makes the boundary more interesting.
2. **Probes in parallel with it**, in three separately-gated tranches: P-0 + P-1/P-1b; P-2/P-7; P-0 → P-3/P-4/P-5/P-5b. P-8 composes and runs last.
3. **US-5 plumbing (T-12, T-13, T-17)** — lands green with an *empty* sandbox block. This is what "the sandbox is gated on the `WriteSettings` fix" means, and why plumbing and sandbox content are separate tasks: a red P-4 must not block US-5. T-12 and T-17 land **together** — between them is a window where a configured-path install creates a sandbox-less document on a run.
4. **T-21 (the argv pin) once, before both flag additions.** One edit, or two engineers fight over `runner_test.go:1200`.
5. **US-1 and US-2** — independent of each other and of the sandbox; parallel once their probes are green. Serialise the edits to `Runner.args`, not the design.
6. **US-3 sandbox content** once P-0/P-4/P-5b are green.
7. **US-4** — documentation plus P-6.
8. T-32, T-33 last.

## Accepted risk — unrestricted `WebFetch`/`WebSearch`

**Accepted by the user, 2026-08-30, having been told plainly what it costs. Recorded, not
re-litigated.**

Untrusted issue and comment text reaches the agent inside `<note>` blocks
(`internal/turn/prompt.go:328-363`). No egress restriction is configured, so a profile granting
`WebFetch` or `WebSearch` gives that run an open outbound path. An instruction injected into an
issue body can therefore exfiltrate anything the agent can read.

Domain-restricted web tools were offered and declined. **No partial hedge is implemented.**
Prompt-injection hardening stays deferred; this feature changes the price of that deferral.

## Out of Scope

- **Option D — comment directive / `/tools`.** Out by standing owner decision, reaffirmed
  2026-08-30. A decision, not an omission: no second resolution path, no extension hook, no
  comment parsing.
- Options B (per-tool labels) and C (repo-committed policy file) — rejected outright, still.
- Anything shipped in merge `a21d89a`: tool profiles, the ceiling, label ingestion, the three
  refusal replies, requested-vs-resolved reporting.
- Domain restriction or any partial restriction of `WebFetch`/`WebSearch`.
- Prompt-injection hardening.
- `--permission-prompt-tool` and `PreToolUse` hooks — still the escape hatch if per-call
  adjudication is ever required.
- `--bare` (D-A5) and `--setting-sources user` (D-Q1).
- Any new `config.yaml` key.
- `repo add`/`repo remove` writing a settings document (D-A2).
- A `SettingsWriter` interface, or a new package, to avoid the `cli → harness` edge.
- Webhooks or any change to the polling trigger model.
- A committed test that spawns a real `claude`.
