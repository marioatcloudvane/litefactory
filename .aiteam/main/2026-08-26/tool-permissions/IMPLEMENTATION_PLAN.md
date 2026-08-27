# Implementation Plan — tool-permissions
# Branch: main
# Date: 2026-08-27

Source: `RESEARCH_BRIEF.md` (incl. its `Addendum — owner decision, 2026-08-27`) and
`feature-spec.md`, both in this session, plus a four-lens architect review.

**Decision: Option A only.** GitHub labels of the exact shape `lf:tools=<profile>` select
operator-authored tool profiles stored in `$LF_HOME/config.yaml`. Options B (per-tool labels),
C (repo-committed policy file) and D (comment directive / `/tools`) are **rejected outright** —
not deferred, not extension points. No comment parsing of any kind, no second resolution path.

## Invariants (every story is subordinate to these)

- **C1 — grant on `--tools`.** `--tools` is the only real boundary. `--allowedTools` is additive
  to the operator's ambient `~/.claude/settings.json` and was measured not to hold (250 ambient
  rules leaked; Bash executed while absent from it). Capability is granted and revoked on
  `--tools`; `--allowedTools` remains defence in depth and the only lever that grants Bash
  families on a clean install.
- **C2 — the label is a lookup key, never a value.** Nothing originating from GitHub reaches the
  child argv as a value. Only config-authored strings are ever joined into argv.
- **C3 — the ceiling lives outside GitHub.** Labels are authenticated by nothing.
- **C9 — fail-closed.** Unknown, malformed, ambiguous, or unconfigured ⇒ refuse and reply. No
  path falls back to a wider or default surface.

## Settled before planning (do not re-investigate)

- **`Task` is a live tool name.** Resolved init events in
  `/Users/mariomh/.litefactory/logs/runs/github/marioatcloudvane/litefactory/{7/0003,7/0002,7/0001,5/0001}.jsonl`
  all read `"tools":["Task","Bash","Edit","Glob","Grep","Read","Write"]`. Not stale, not renamed
  to `Agent`. Sub-agent spawning has worked all along.
- **`TodoWrite` is requested but silently dropped** — 8 requested, 7 resolved, no error raised.
  No todo-shaped tool name appears in any log, so "correct the name" has no candidate. This is
  the worked example motivating requested-vs-resolved reporting.
- **C7 is clear.** Session-resume has landed; `PathResume` (`internal/harness/runner.go:162-185`)
  and `Runner.args` (`:388`) are committed, tree clean. No sequencing blocker.
- **OQ 10 is closed.** The repository-wide forbidden-flag test exists:
  `internal/config/purity_test.go:84-91`, with `internal/harness/runner_test.go:284` guarding argv.
- **`internal/turn/harvest.go` needs no change** — `harvest.go:126-129` mutates `prep.request` in
  place, so new `harness.Request` fields ride the cold-rerun path for free.

## Decisions taken during planning

| # | Decision | Rationale |
|---|----------|-----------|
| D-1 | Resolution lives in `internal/daemon/tick.go`, **not** `internal/turn/prepare.go` | US-3 requires a run-less refusal. By `prepare`, the lock, clone, fetch, worktree and runs row already exist (`prepare.go:64-213`), and `ReportTurn.validate` requires `RunID > 0` (`turn/report.go:170-178`). Resolve at the head of `d.run`. |
| D-2 | Keep the boot check **and** the `Request.validate` ceiling re-check | Not duplicates. Boot proves the *config* is sound; `Request.validate` proves the `[]string` that crossed `daemon → turn → harness` as a value is still in-ceiling. ~5 lines, and it is what keeps the `#nosec` justification honest. |
| D-3 | **Keep** per-profile `allowed_tools` (architect's CUT-1 rejected) | CUT-1 argued `--allowedTools` "grants nothing" per C1. C1 says it cannot *revoke* (additive, ambient leak) — it still **grants** on a clean install, where Bash is denied unless allowed. Owner decision 4 ("profiles may add `ls`, `cat`, `find`, `rg`, `head`, `tail`") depends on exactly that path. |
| D-4 | **Adopt** CUT-2: three flat `ReplyKind` values, no `ToolsRefusal` struct or `Reason` enum | Matches the package's own precedent — `ReplyUnsupportedCommand`, `ReplyUnknownCommand`, `ReplyEmptySpike` (`report/reply.go:26-37`). Less machinery, and the malformed branch structurally has no field to leak into. |
| D-5 | **Label present + `tool_profiles` absent ⇒ refuse** | The one case the spec left genuinely undecided. C9 is non-negotiable and outranks "byte-identical to today": silently ignoring an explicit permission request is the worst available failure. AC-1.2 narrows to *absent profiles **and** no label*. |
| D-6 | Match the `lf:tools=` **prefix** case-insensitively; keep the value regex lowercase-only | As specified, `LF:Tools=research` misses silently and the run proceeds wide-open on defaults — a silent no-op on a permission request. Case-insensitive prefix turns a typo into a loud refusal. |
| D-7 | Correct the spec's C3 wording | Spec line 129 claimed an untrusted label "can only cause downgrade/DoS, never escalation." False — a label selects any profile up to the ceiling, and a profile may be wider than `claude.tools`. That is *bounded escalation*. Correct framing: **the ceiling, not the label, is the boundary.** An operator who believes labels cannot escalate will set a lax ceiling. |
| D-8 | The OPTIONAL section is **not** part of any story | Independent of labels; each lever needs its own empirical probe. Implement may skip it wholesale. |

---

## User Story 1 — Operator-authored profiles and ceiling in config

> As the operator, I want to define named tool profiles and a hard ceiling in `config.yaml`, validated at daemon startup, so a bad profile map fails at boot and never mid-turn.

Two new **optional** keys under `claude:` — `tool_ceiling` and `tool_profiles` — validated wholly at boot, with absence leaving today's behaviour untouched.

### Architecture Notes
- Both keys optional. `tool_profiles` absent ⇒ nil map ⇒ feature inert. `tool_ceiling` absent ⇒ defaults to `claude.tools`, the narrowest possible default: no profile can then widen anything at all.
- **Resolve the effective ceiling once, in `Load`, before `validate` runs** (E-7). Computing it in both config validation and `harness.New` risks divergence, and the boundary check is the one that matters. This also keeps the profile map read-only after boot — no lazy fill-in on a hot path.
- **`validateTools`/`validateAllowedTools` cannot be reused *verbatim*** (E-3). Both hardcode their error key (`policy.go:66`, `:89`), so a bad `research` profile would report `claude.tools[3]`. Add a leading `key string` parameter; two existing call sites (`validate.go:146-147`). This is the only signature change in the feature.
- **Sort profile names before iterating the map** (P-7). Go randomises map order, so two bad profiles otherwise yield a different error list per boot and any full-list test is flaky.
- **The template's worked example must be YAML comments only** (P-2). `template.go` renders from `Defaults()` (`config.go:148-150`, `template.go:87,98`); a live example profile would land in `Defaults()` and the feature would stop being inert on every fresh install. Follow the commented `token_env` precedent at `template.go:72-75`.
- `TodoWrite` drops from `defaultTools()` (`policy.go:27`) here, not in US-4 — it is a *defaults* change and moves with the template golden.
- Platform: no new package, no new module; `go.mod` untouched. Config errors keep the existing aggregated `[]error` shape (`validate.go:146-148`).
- Risk: `Defaults()`/template drift is the trap in this story. The round-trip test in T-3 is what catches it.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1 | `ToolProfile` type; `tool_profiles`/`tool_ceiling` decode; effective-ceiling resolution in `Load`; drop `TodoWrite` from `defaultTools()`; commented example in `template.go` | data | — | M |
| T-2 | Validation: key-parameterised `validateTools`/`validateAllowedTools`; ceiling ⊇ `claude.tools`; profile-key regex; per-profile ceiling containment; `allowed_tools` rules ⊆ profile `tools`; `validateNoForbiddenFlag` extension; sorted iteration | logic | T-1 | M |
| T-3 | Table-driven unit tests: inert config, every startup-failure case, error text names the profile, `Defaults()`/template round-trip | test | T-2 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-1.1 | `claude.tool_profiles` and `claude.tool_ceiling` load from `config.yaml`; both optional. |
| AC-1.2 | Absent `tool_profiles` **and no `lf:tools=` label** leaves behaviour byte-identical to today. (Label-present is D-5, covered by AC-3.4.) |
| AC-1.3 | Absent `tool_ceiling` defaults to `claude.tools`, resolved once in `Load`, so config validation and `Runner.ceiling` read the same slice. |
| AC-1.4 | A profile whose `tools` exceed `tool_ceiling` fails `daemon run` startup with a message naming **the profile** and the offending tool. |
| AC-1.5 | A `tool_ceiling` that is not a superset of `claude.tools` fails startup. |
| AC-1.6 | A profile key not matching `^[a-z0-9][a-z0-9_-]{0,39}$` fails startup. |
| AC-1.7 | An `allowed_tools` rule naming a tool absent from that profile's `tools` fails startup. |
| AC-1.8 | `validateNoForbiddenFlag` covers every profile's `tools` and `allowed_tools` and the ceiling. |
| AC-1.9 | `template.go` writes both keys as **comments only**, with a worked example and prose explaining the ceiling; `Defaults()` gains no profile. |
| AC-1.10 | No profile named `default` is required, and none is treated specially. |
| AC-1.11 | Startup errors are deterministic across boots for a config with multiple bad profiles. |
| AC-1.12 | `TodoWrite` is removed from `defaultTools()`; a test asserts the default surface is the 7 tools that actually resolve. |

---

## User Story 2 — A label selects the profile for the next run

> As a maintainer, I want to put `lf:tools=<profile>` on an issue so that the next run triggered by a comment on it runs with that profile's tools, capped by the ceiling.

Labels flow from the existing issue decode to the child argv, with the label used only as a map key.

### Architecture Notes
- **One decode change covers both fetch paths** (E-5, verified): `issuePayload` has a single mapper, `(*issuePayload).issue()` at `issues.go:162`, called from `ListIssues` (`issues.go:102`) and `GetIssue` (`issues.go:139`). Check `internal/github/surface_test.go` — it is an AST-walking surface test and may assert the decoded field set.
- Resolution sits at the head of `d.run` (D-1). The daemon already holds `config.Claude` (`setup.go:134`); keep the profile map and ceiling as a `Daemon` field.
- **Copy the profile's slices at the resolution boundary** (P-3): `append([]string(nil), p.Tools...)`. The config value lives for the daemon's whole lifetime, and `prep.request` is mutated in place on the cold-rerun path (`harvest.go:126-129`) — one `append` with spare capacity downstream would corrupt the operator's profile for every later run in the process. Matches the existing `runner.go:131` idiom.
- **`args` must preserve two existing behaviours** (P-5): `--allowedTools` is emitted only when non-empty (`runner.go:405`) — empty and absent are different; `--tools` is emitted unconditionally, because an omitted `--tools` is allow-all (`policy.go:60-64`).
- **The `#nosec G204` justification at `runner.go:436-441` must be rewritten** (E-4). It currently reads "Nothing else on this argv is caller-supplied"; once `args` reads the request that sentence is false as written, even though the property still holds. That comment is the written form of C2 — updating it is a required edit, not a tidy-up.
- **Labels must never reach the prompt** (S-13). They land on `turn.Issue.Labels`, one struct away from `harness.PromptInput`. State the guardrail so nobody later adds a "labels:" line to `prompt.go`. `harness.Meta.ToolProfile` records the *validated name*, never raw label text.
- Behaviour: labels are re-read fresh every tick that has work, no caching and no extra API call; a label change alone triggers nothing (only comments trigger runs, `classify.go:204-206`); a relabel during a run applies to the next run.
- **Granularity is per-tick, not per-comment.** `drain` (`tick.go:373-401`) loops over every queued comment against one `t.issue` snapshot, so two comments drained in one tick share one label snapshot. **Documented, not fixed** — fixing costs an issue read per comment. This is precisely why the `**Tools:**` report line is required.
- Resume: a resumed session takes its boundary from the current argv (`CLAUDE_CLI_CONTRACT.md:58-71`), so widening mid-conversation works with the conversation intact. Narrowing binds new calls, but earlier turns ran wider and their output remains in context — an asymmetry to state, not a mechanism to build.
- Risk (E-6): an `ErrInvalidRequest` from `Run` happens after the worktree and runs row exist, and `turn.Execute`'s contract (`execute.go:184-192`) leaves the comment unconsumed for the next tick — so a "cannot happen" ceiling rejection would retry forever, building a worktree each time. AC-2.7 closes this.
- Risk (C3, per D-7): anyone with triage permission can relabel, then wait for the owner's next comment. Bounded by the ceiling, detected only by the `**Tools:**` line.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4 | `issuePayload.Labels` → `github.Issue.Labels` → `turn.Issue.Labels` via the single mapper; reconcile `surface_test.go` | data | — | S |
| T-5 | Harness: `Request.{Tools, AllowedTools, ToolProfile}`; `Runner.ceiling`; `args` selection preserving the empty-`--allowedTools` guard; `Request.validate` ceiling check; **`#nosec` justification rewrite**; `"claude started"` attributes | api | T-1 | M |
| T-6 | Daemon resolver and wiring: case-insensitive prefix scan, exact-key lookup, three refusal decisions, copy-on-read, resolved lists onto `turn.Request` → `prepare` → `harness.Request` | logic | T-2, T-4, T-5 | M |
| T-7 | Unit tests: resolver decision table; **argv contains no label-derived string**; ceiling rejection at `Request.validate`; no-label argv byte-identical to today; resume guard `harness_test.go:275-279` still green; cold-rerun carries the profile | test | T-6 | M |
| T-12 | **Integration probe** — spawn a real `claude` under a profile granting a tool absent from `claude.tools`; assert the init event resolves it. `CLAUDE_CLI_CONTRACT.md` five-probe style | test | T-6 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-2.1 | Labels decode through `issuePayload` and reach `turn.Issue` on both the `ListIssues` and `GetIssue` paths, with no additional API call. |
| AC-2.2 | `lf:tools=<profile>` on an issue causes the next comment-triggered run to spawn with that profile's `--tools` and `--allowedTools`. |
| AC-2.3 | With no `lf:tools=` label, argv is byte-identical to today's. |
| AC-2.4 | **No string originating from GitHub appears anywhere in the child argv.** A test asserts that a label matching a profile contributes only config-authored strings and that argv contains no label text. |
| AC-2.5 | `--tools` carries the full granted surface; nothing is granted by adding only an `--allowedTools` rule. |
| AC-2.6 | A tool outside the ceiling on `harness.Request` is rejected by `Request.validate` at the harness boundary. |
| AC-2.7 | An `ErrInvalidRequest` from `Run` is reported as a run failure and **consumes** the comment — no perpetual retry, no worktree churn. |
| AC-2.8 | The `#nosec G204` justification at `runner.go:436-441` is rewritten to describe the request-supplied lists and why C2 still holds. |
| AC-2.9 | Profile slices are copied at the resolution boundary; no downstream mutation can reach `config.Claude.ToolProfiles`. |
| AC-2.10 | Relabel-then-comment widens the next run; a label change with no comment starts nothing. |
| AC-2.11 | A resumed run takes its boundary from the current argv, and the guard at `internal/turn/harness_test.go:275-279` still passes. |
| AC-2.12 | `internal/turn/harvest.go` is unmodified and the cold-rerun path carries the profile. |
| AC-2.13 | No label text reaches `harness.PromptInput`; a test asserts labels are absent from the rendered prompt. |
| AC-2.14 | T-12 observes a profile-granted tool in the resolved init event of a real `claude` run. |

---

## User Story 3 — Fail-closed on unknown, malformed, ambiguous or unconfigured

> As the operator, I want a `lf:tools=` label that cannot be resolved to refuse the run and say so on the thread, so a permission question never resolves permissively or silently.

Four refusal cases, all run-less, all answered through the existing reply mechanism.

### Architecture Notes
- **Three flat `ReplyKind` values** (D-4), matching `report/reply.go:26-37`, plus one `Reply.Profile` field. No `ToolsRefusal` struct, no `Reason` enum.
- **Two live guards in `ReportReply` actively reject this today** (E-2) and must branch on the refusal first, or the engineer hits them at compile-and-fail time:
  - `ReplyRequest.validate()` returns `invalidRequest("%s draws no reply", …)` unless `req.Command.NeedsReply()` (`turn/report.go:202-205`);
  - `ReportReply` calls `replyKind(req.Command.Kind)` and bails if `!ok` (`turn/report.go:341-344`), and `replyKind` returns `false` for `KindSpike`, `KindFreeText`, `KindNone` (`:545-546`).
  A tools refusal is triggered by precisely a *spawning* command, so both guards are in the way.
- **Re-apply the profile-name regex inside `internal/report`** (S-8). The package sets its own house rule at `reply.go:87-92`: *"this is the one place where attacker-controlled text is echoed into a comment litefactory signs its own name to, so the restriction is re-applied here rather than assumed."* Validating in `daemon`/`turn` and trusting in `report` breaks that precedent.
- **Make "cannot echo" structural, not disciplinary** (S-9): assign `Profile` **only** on the unknown-profile branch, so the malformed branch has no field to leak into. For the ambiguous case, **name neither label** — naming them re-introduces exactly the echo the malformed case forbids.
- Watermark: `ReportReply` advances after a successful post (`turn/report.go:381`), so a failed post leaves the comment unconsumed and a later tick retries. Correct — no retry loop, no permissive fallback.
- Self-retrigger is already handled: the reply carries `ReplyMarker(r.Ref)` and `Filter.selfReason` drops any body containing `<!-- lf:` (`classify.go:196-197`).
- Platform (P-6): the refusal is **not an error**. It is a result reported as a comment, like every other badly-going turn (`execute.go:177-183`). Do not model it as an error type.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8 | Three `ReplyKind` values + `Reply.Profile` with the regex re-applied in `internal/report`; `ReplyRequest.validate` and `ReportReply` guard branches; daemon calls `ReportReply` instead of `d.run` | api | T-6 | M |
| T-9 | Unit tests: each refusal refuses, replies, spawns nothing, advances the watermark; rendered body contains no raw label text; ambiguous names neither label; label-present-profiles-absent | test | T-8 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-3.1 | An unknown profile name refuses the run, posts a reply naming the profile, spawns nothing, and advances the watermark. |
| AC-3.2 | A malformed `lf:tools=` value (failing `^[a-z0-9][a-z0-9_-]{0,39}$`, or empty) refuses, and the reply **does not echo the raw label text**. |
| AC-3.3 | Two or more `lf:tools=` labels refuse as ambiguous; neither is chosen, they are never unioned, and **neither is named** in the reply. |
| AC-3.4 | A `lf:tools=` label present while `tool_profiles` is absent **refuses** (D-5) — it does not fall through to defaults. |
| AC-3.5 | A label whose prefix matches case-insensitively but whose value is not lowercase (`LF:Tools=research`) refuses as malformed rather than being silently ignored (D-6). |
| AC-3.6 | No refusal path ever falls back to the default surface or to any wider surface. |
| AC-3.7 | The refusal is run-less: no runs row, no worktree, no `claude` process. |
| AC-3.8 | The profile-name regex is re-applied inside `internal/report` before rendering. |
| AC-3.9 | A failed reply post leaves the comment unconsumed for a later tick. |

---

## User Story 4 — Requested vs resolved surface, visible after the fact

> As the operator, I want the requested and resolved tool surface plus its provenance in the run report, the metadata document and the logs, so I can tell after the fact which boundary a run actually had.

The `TodoWrite` finding is the worked example: 8 requested, 7 resolved, no error raised.

### Architecture Notes
- **The report line has no data path as specced — this is the blocking correction in this story** (E-1). Routing `ToolsRequested`/`ToolProfile` into `harness.Meta` alone does not work: `metadataList` takes a `report.Run` (`report/run.go:440`) built at `turn/report.go:490-501` from `turn.Result`; `Meta` is a JSON document written to disk and never reaches the renderer. Putting them on `RunResult` also fails, because `RunResult` is nil for a run that died before the init event (`report/run.go:56-59`) — exactly the run whose requested surface matters most.
  **Fix:** `turn.Result` gains `ToolsRequested []string` + `ToolProfile string`; `report.Run` gains the same two; `metadataList` renders resolved-from-`run.Result` when present and requested-from-`run` always. ~8 lines across three files.
- The `**Tools:**` bullet goes in `metadataList` after `Model` (`run.go:485-487`), landing in `secFixed`, which is never truncated (`run.go:169-175`). **Required, not optional** — it is the only after-the-fact evidence of which snapshot a per-tick-resolved run used, and the only detection for an unauthenticated relabel (C3). Say so in the code comment so nobody later downgrades it.
- `internal/cli/cmd_logs_test.go:704` asserts over a `logsSourceFiles` list; adding fields to `logs` output may touch it.
- Redaction is a non-issue: `internal/redact` matches credential shapes only, so tool lists pass through untouched.
- Platform: `slog` throughout; the `"claude started"` line (`runner.go:485-492`) gains two `slog.String` attributes. No new goroutines anywhere in the feature; the harness keeps its documented two readers (`runner.go:494-497`). Run new tests with `-race`.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-10 | `turn.Result` + `report.Run` tool fields (E-1); `**Tools:**` bullet in `metadataList` after `Model` inside `secFixed`; `harness.Meta.{ToolsRequested, ToolProfile}`; `cmd_logs.go:456` fields | logic | T-6 | M |
| T-11 | Unit tests: report line for granted / requested≠resolved / nil-result runs; `logs` output; redaction leaves tool lists untouched | test | T-10 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-4.1 | `harness.Meta` carries `ToolsRequested`, the existing resolved `Tools`, and `ToolProfile`. |
| AC-4.2 | The run report has a `**Tools:**` bullet in `metadataList` after `Model`, inside `secFixed`, listing resolved tools and profile provenance and naming any requested-but-not-granted tool. |
| AC-4.3 | The `**Tools:**` line renders for a run whose `RunResult` is nil, showing the requested surface and the profile. |
| AC-4.4 | `litefactory logs <issue>` prints resolved tools, the profile, and requested tools when they differ. |
| AC-4.5 | The `"claude started"` log line carries `allowed_tools` and `tool_profile` alongside `tools`. |
| AC-4.6 | Redaction is unaffected — a tool list passes through `internal/redact` untouched. |

---

## Implementation Order

Config lands first, the daemon resolver lands last. T-4 and T-8's rendering work are genuinely
independent of T-1/T-2 and can proceed in parallel.

```
T-1 → T-2 → T-3                    (US-1, config)
        ↓
      T-5 → ┐                      (US-2, harness)   T-4 ── parallel
            ├→ T-6 → T-7, T-12     (US-2, resolver + wiring)
      T-4 → ┘        ↓
                     ├→ T-8 → T-9  (US-3, refusal)
                     └→ T-10 → T-11 (US-4, observability)
```

T-6 is the integration point and touches the most surface — schedule it when T-1 to T-5 are green.

## Testing

All tasks are `go-unit-test-engineer` work except **T-12**, which is the single
`go-integration-test-engineer` task. The `daemon` and `turn` packages already carry
`fixture_test.go` fakes; label-set → decision is a textbook table (empty, one valid, one unknown,
one malformed, one empty-value, two labels, unrelated `lf:` labels, case variants). **No new test
dependency, no mocking framework, no new module.**

T-12 earns its place because C1 was established empirically rather than from documentation, and
this feature's whole value proposition is that a profile actually widens the *resolved* surface.
It is the one cuttable task — cutting it means shipping having never observed the feature work.

## Accepted risk — prompt injection with unrestricted `WebFetch`

Recorded, not re-litigated.

- Untrusted issue and comment text reaches the agent inside `<note>` blocks
  (`internal/turn/prompt.go:328-363`).
- A profile granting unrestricted `WebFetch`/`WebSearch` turns an injected instruction into an
  exfiltration channel for anything the agent has read.
- This was accepted knowingly by the owner on 2026-08-27. Injection hardening stays deferred;
  **this feature changes the price of that deferral.**

## Known limitations (documented, not fixed)

- **Bash prefix rules do not survive `&&`** — documented CLI behaviour: `Bash(go test)` permits
  `go test && rm -rf /`. Profiles may add read-only navigation (`ls`, `cat`, `find`, `rg`, `head`,
  `tail`) and nothing that writes; this is enforced by operator authorship plus the existing
  `validateAllowedTools`, which already rejects bare `Bash` and `Bash(*)`. No new denylist.
- **Per-tick, not per-comment label granularity** — see US-2 architecture notes.
- **Anyone with triage permission can apply a label.** Bounded by the ceiling; per D-7 this is
  bounded *escalation*, not merely downgrade/DoS. The `**Tools:**` report line is the detection.

## Out of Scope

- Options B (per-tool labels), C (repo-committed policy file) and D (comment directive / `/tools`).
  Rejected outright. No comment parsing of any kind, no second resolution path, **no extension hook
  to accommodate them later**.
- Prompt-injection hardening — see *Accepted risk*.
- Domain restriction for `WebFetch`/`WebSearch`.
- `--permission-prompt-tool` and `PreToolUse` hooks. Recorded as the escape hatch if per-call
  adjudication is ever required.
- Webhooks, or any change to the polling trigger model.
- Fixing the per-tick granularity of `drain`.
- Attribution of who applied a label (needs the timeline/events API).
- `WriteSettings` having no non-test caller (C6). Sidestepped: this feature adds no required config
  key and does not change `DefaultSettings`.
- Waiting on session-resume (C7 is clear).

---

## OPTIONAL — independent of labels, Implement may skip wholesale

Not part of any story, not depended on by any task. Recommended, not assumed. These fix a
**separately measured** hole — the 250 ambient allow-rules from SD-2 — and would hold whether or
not labels ship.

| Lever | What it buys |
|-------|--------------|
| `--setting-sources user` or `--bare` | Stops discovery of project/ambient settings, hooks, plugins and `CLAUDE.md` — the direct fix for the leaked ambient allow-rules and for the executing-hooks residual risk (`harness/doc.go:45-55`). |
| `--permission-mode dontAsk` | Deny anything not pre-approved, explicitly for automation, instead of relying on `default` mode's incidental deny-on-no-prompt. |
| `sandbox` settings | OS-level filesystem/network confinement for Bash with `denyWrite` paths — the only lever that makes an allow-listed `Bash` meaningfully less than arbitrary code execution, given that prefix rules do not survive `&&`. |

Each needs empirical verification against claude 2.1.246 in the style of `CLAUDE_CLI_CONTRACT.md`
(its five-probe method), and its re-verification checklist (`:137-148`) would gain a case for
whichever flag is adopted. **Do not adopt any of these on documentation alone.** If skipped, note
in handover that this remains the real fix for the ambient-rule leak.
