# Feature Spec: tool-permissions
# Status: APPROVED
# Date: 2026-08-27

Source: `RESEARCH_BRIEF.md` in this session, including its `Addendum — owner decision, 2026-08-27`.
Decision: **Option A only.** Options B (per-tool labels), C (repo-committed policy file) and
D (comment directive / `/tools`) are rejected outright — not deferred, not extension points.

## Goal

Let an operator define named tool profiles in `$LF_HOME/config.yaml` and let a GitHub label
(`lf:tools=<profile>`) select one per run, so a research- or build-shaped issue gets the tools it
needs without any GitHub-supplied string ever reaching the `claude` argv or exceeding an
operator-authored ceiling.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As the operator, I want to define named tool profiles and a hard ceiling in `config.yaml`, validated at daemon startup, so a bad profile map fails at boot and never mid-turn. |
| US-2 | As a maintainer, I want to put `lf:tools=<profile>` on an issue so that the next run triggered by a comment on it runs with that profile's tools, capped by the ceiling. |
| US-3 | As the operator, I want an unknown, malformed or ambiguous `lf:tools=` label to refuse the run and say so on the thread, so a permission question never resolves permissively or silently. |
| US-4 | As the operator, I want the requested and resolved tool surface plus its provenance recorded in the run report, the metadata document and the logs, so I can tell after the fact which boundary a run actually had. |

## Data Model

### Config (`$LF_HOME/config.yaml`, `internal/config`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Claude` | `tools` | `[]string` (yaml `tools`) | yes (existing) | Unchanged. This is the **implicit default profile** — used whenever no `lf:tools=` label is present. Existing `validateTools`. |
| `Claude` | `allowed_tools` | `[]string` (existing) | no | Unchanged defaults (`policy.go:34-50`, the 8 read-only/build Bash rules stay exactly as they are). Existing `validateAllowedTools`. |
| `Claude` | `tool_ceiling` | `[]string` | no | Bare tool names. Absent/empty ⇒ defaults to `claude.tools`. Validated with `validateTools` verbatim. Must be a superset of `claude.tools`. |
| `Claude` | `tool_profiles` | `map[string]ToolProfile` | no | Absent/empty ⇒ feature inert, zero behaviour change. No profile named `default` is required or special. |
| `ToolProfile` | *(map key)* | `string` | yes | `^[a-z0-9][a-z0-9_-]{0,39}$`. This is the name a label selects. |
| `ToolProfile` | `tools` | `[]string` (yaml `tools`) | yes | `validateTools` verbatim; additionally every entry must appear in `tool_ceiling`. |
| `ToolProfile` | `allowed_tools` | `[]string` | no | `validateAllowedTools` verbatim; additionally every rule's tool name must appear in this profile's `tools`. |

`validateNoForbiddenFlag` (`policy.go:134-151`) extends to every profile's `tools` and
`allowed_tools` and to `tool_ceiling`. The repository-wide grep invariant is already tested at
`internal/config/purity_test.go:84-91` — verified present, no new test needed for it.

### Forge → turn → harness

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `github.issuePayload` (`issues.go:144-156`) | `Labels []struct{ Name string }` | decoded field | no | Already on the wire, discarded today. One change covers both `ListIssues` (`tick.go:190`) and `GetIssue` (`tick.go:268`). |
| `github.Issue` | `Labels []string` | `[]string` | no | Label names only. |
| `turn.Issue` | `Labels []string` | `[]string` | no | Carried at `tick.go:528-538`. |
| `harness.Request` (`runner.go:189-227`) | `Tools []string` | `[]string` | no | Empty ⇒ `Runner` uses its configured default. Non-empty ⇒ replaces it wholesale. |
| `harness.Request` | `AllowedTools []string` | `[]string` | no | Empty ⇒ `Runner` default. |
| `harness.Request` | `ToolProfile string` | `string` | no | Provenance only; never joined into argv. Empty means "no label, default surface". |
| `harness.Runner` | `ceiling []string` | `[]string` | yes | Built at daemon boot from `claude.tool_ceiling`. `Request.validate` rejects any `Tools` entry outside it. |
| `harness.Meta` (`meta.go:41-99`) | `ToolsRequested []string` | `[]string` | yes | What litefactory put on `--tools`. |
| `harness.Meta` | `Tools []string` (existing) | `[]string` | yes | Unchanged meaning: what the init event **resolved**. |
| `harness.Meta` | `ToolProfile string` | `string` | no | Profile name, or empty for the default surface. |
| `turn.ReplyRequest` (`report.go:182-199`) | `ToolsRefusal *ToolsRefusal` | pointer | no | Set ⇒ run-less refusal reply; nil ⇒ today's command-reply behaviour. |
| `ToolsRefusal` | `Reason` | enum: `unknown_profile`, `malformed_label`, `ambiguous_labels` | yes | — |
| `ToolsRefusal` | `Profile string` | `string` | no | Set only when the name matched the shape regex; never the raw label text. |
| `report.Reply` (`internal/report/reply.go`) | new `ReplyKind` `ReplyToolsRefused` | enum value | yes | Rendered by the existing reply renderer. |

### Relationships

- `github.Issue.Labels` → `turn.Issue.Labels` → profile lookup → `harness.Request.{Tools, AllowedTools, ToolProfile}` → `Runner.args` → argv.
- Label → `config.Claude.ToolProfiles[name]` **by key only**. The label is a lookup key; the argv values come from config.
- `harness.Request` → `harness.Meta.{ToolsRequested, ToolProfile}`; init event → `harness.Meta.Tools`.
- `internal/turn/harvest.go` needs **no** change: `harvest.go:126-129` mutates `prep.request` in place, so new `Request` fields ride the cold-rerun path for free. (Corrects the brief body; per the addendum.)

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| — | GitHub label `lf:tools=<profile>` on an issue | none (see C3) | Exact prefix `lf:tools=`, case-sensitive. Read fresh every tick; no new API call. |
| internal | `config.Claude.ToolProfiles` / `ToolCeiling` | operator-local file | New optional keys; surfaced in `config.go:114-146` and `template.go:77-115`. |
| internal | `harness.Request.{Tools, AllowedTools, ToolProfile}` | — | `turn.Runner` stays the one-method `Run(ctx, harness.Request)` interface — no interface change. |
| internal | `Runner.args` (`runner.go:388-412`) | — | `--tools` / `--allowedTools` take the request's values when set, the Runner's otherwise. Flag set and order otherwise unchanged. |
| internal | `turn.Reporter.ReportReply` (`report.go:337`) | — | Gains the `ToolsRefusal` path. Existing mechanism, no new machinery. |
| CLI | `litefactory logs <issue>` (`cmd_logs.go:456`) | — | Prints resolved tools, requested tools when they differ, and the profile. |
| log | `"claude started"` (`runner.go:485-492`) | — | Gains `allowed_tools` and `tool_profile` alongside the existing `tools`. |
| report | `**Tools:**` bullet in `metadataList` (`report/run.go:440-506`), after `Model` (`:485-487`) | — | **Required**, not optional. Lands in `secFixed`, never truncated. |

## Business Rules

- **No label ⇒ today's defaults, unchanged.** `claude.tools` / `claude.allowed_tools` are the implicit default profile. Every currently-open issue behaves exactly as it does today.
- **A label may widen, never beyond the ceiling.** `tool_ceiling` is the maximum; a profile that exceeds it fails validation at daemon startup. The default surface stays narrow.
- **Validation is at boot, never mid-turn.** A malformed profile map, an out-of-ceiling profile, or a ceiling narrower than `claude.tools` fails `daemon run` startup with the existing config-error reporting.
- **The label is a lookup key, never a value (C2).** Only config-authored strings are ever joined into argv. The `#nosec G204` justification at `runner.go:436-441` ("Nothing else on this argv is caller-supplied") remains true as written.
- **Grant on `--tools` (C1).** `--allowedTools` is additive to the operator's ambient settings and has been measured not to hold (250 ambient allow-rules leaked; Bash executed while absent from `--allowedTools`). Capability is granted and revoked on `--tools` only; `--allowedTools` is defence in depth.
- **Labels are re-read fresh on every tick that has work.** No caching, no extra API call. Relabel-then-comment is the interaction model.
- **A label change alone triggers nothing.** Only comments trigger runs (`classify.go:204-206`). This is correct and stays.
- **Granularity is per-tick, not per-comment.** `drain` (`tick.go:373-401`) loops over every queued comment against one `t.issue` snapshot, so two comments drained in one tick share one label snapshot. **Documented, not fixed** — fixing costs an issue read per comment. This is precisely why the `**Tools:**` report line is required.
- **A relabel during a run applies to the next run.** Labels are read before the run; the argv is fixed at spawn.
- **Resume takes its boundary from the current argv** (`CLAUDE_CLI_CONTRACT.md:58-71`), so widening mid-conversation works with the conversation intact. Narrowing binds new calls, but earlier turns ran wider and their output remains in context — an asymmetry to state, not a mechanism to build.
- **`WebFetch` and `WebSearch` are grantable and unrestricted.** No domain rules. See *Accepted risk*.
- **Bash gains read-only navigation only.** Profiles may add `Bash(ls:*)`, `Bash(cat:*)`, `Bash(find:*)`, `Bash(rg:*)`, `Bash(head:*)`, `Bash(tail:*)`. Nothing that writes: no `mkdir`, `make`, `go generate`, `go mod`. Enforced by operator authorship and the existing `validateAllowedTools` (which already rejects bare `Bash` and `Bash(*)`) — no new denylist. Prefix rules do not survive `&&` (documented CLI behaviour: `Bash(go test)` permits `go test && rm -rf /`); a known limitation, not fixed here.
- **`TodoWrite` is dropped from `defaultTools()` (`policy.go:27`).** Requested in all four sampled runs, resolved in none, no error raised — requesting a tool that resolves to nothing creates a false impression of granted capability. (Existing installs whose `config.yaml` still lists it keep resolving 7 of 8 harmlessly; the requested-vs-resolved report line now makes that visible.)
- **`Task` is a live tool name** — verified resolved in `7/0003`, `7/0002`, `7/0001` and `5/0001`. No investigation task. No rename to `Agent`.

## Edge Cases & Error Handling

Fail-closed is non-negotiable (C9). No path falls back to something more permissive.

- **Unknown profile name** (shape-valid, not in `tool_profiles`) → refuse the run; `ReportReply` with `ReplyToolsRefused` naming the profile; watermark advances, so no retry loop.
- **Malformed label** (`lf:tools=` with a value failing `^[a-z0-9][a-z0-9_-]{0,39}$`, or an empty value) → refuse; reply describes a malformed `lf:tools=` label and **does not echo the raw label text** (untrusted string, never re-emitted into our own markdown).
- **Two or more `lf:tools=` labels on one issue** → refuse as ambiguous; never pick one, never union.
- **No label, and `tool_profiles` absent** → default surface, no reply, no log noise.
- **Profile resolves to tools outside the ceiling** → cannot happen at run time; caught at daemon startup. `Request.validate` re-checks at the harness boundary and returns `ErrInvalidRequest` if it ever does.
- **Requested ≠ resolved** (e.g. a tool name claude does not recognise) → run proceeds; both lists recorded in `Meta`; the report's `**Tools:**` line names what was requested and not granted.
- **Empty `Request.Tools`** → `Runner` uses its configured default. An empty `--tools` is never emitted; an omitted `--tools` means allow-all (`validateTools` already guards this).
- **Refusal reply fails to post** → existing `ReportReply` semantics: nothing consumed, a later tick tries again.
- **Two comments drained in one tick after a mid-tick relabel** → both run under the first snapshot. Expected; visible in each run report's `**Tools:**` line.

## Accepted risk — prompt injection with unrestricted `WebFetch`

Recorded, not re-litigated.

- Untrusted issue and comment text reaches the agent inside `<note>` blocks (`internal/turn/prompt.go:328-363`).
- A profile granting unrestricted `WebFetch`/`WebSearch` turns an injected instruction into an exfiltration channel for anything the agent has read.
- This was accepted knowingly by the owner on 2026-08-27. Injection hardening stays deferred; this feature **changes the price** of that deferral.

## Out of Scope

- Options B (per-tool labels), C (repo-committed policy file) and D (comment directive / `/tools`). Rejected outright. No comment parsing of any kind, no second resolution path, no extension hook to accommodate them later.
- Prompt-injection hardening (still deferred — see *Accepted risk*).
- `--permission-prompt-tool` and `PreToolUse` hooks. Recorded as the escape hatch if per-call adjudication is ever needed.
- Webhooks, or any change to the polling trigger model.
- Fixing the per-tick granularity of `drain`.
- Domain restriction for `WebFetch`/`WebSearch`.
- Attribution of who applied a label (needs the timeline/events API). Accepted: with a ceiling, an untrusted label can only cause downgrade/DoS, never escalation (C3).
- `WriteSettings` having no non-test caller (C6). Sidestepped: this feature adds no required config key and does not change `DefaultSettings`.
- Waiting on the session-resume work. C7 is clear — `PathResume` (`runner.go:162-185`) and `Runner.args` (`:388`) are committed, tree clean.

## Acceptance Criteria

**US-1 — profiles and ceiling in config**

- [ ] `claude.tool_profiles` and `claude.tool_ceiling` load from `config.yaml`; both optional.
- [ ] Absent `tool_ceiling` defaults to `claude.tools`; absent `tool_profiles` leaves behaviour byte-identical to today.
- [ ] Profile `tools` are validated with `validateTools` and profile `allowed_tools` with `validateAllowedTools`, reused verbatim.
- [ ] A profile whose `tools` exceed `tool_ceiling` fails `daemon run` startup with a message naming the profile and the offending tool.
- [ ] A `tool_ceiling` that is not a superset of `claude.tools` fails startup.
- [ ] A profile key not matching `^[a-z0-9][a-z0-9_-]{0,39}$` fails startup.
- [ ] An `allowed_tools` rule naming a tool absent from that profile's `tools` fails startup.
- [ ] `validateNoForbiddenFlag` covers every profile's `tools` and `allowed_tools` and the ceiling.
- [ ] `template.go` writes both keys with a worked example profile and comments explaining the ceiling.
- [ ] No profile named `default` is required, and none is treated specially.

**US-2 — label selects the profile**

- [ ] Labels decode through `issuePayload` and reach `turn.Issue` on both the `ListIssues` and `GetIssue` paths, with no additional API call.
- [ ] `lf:tools=<profile>` on an issue causes the next comment-triggered run to spawn with that profile's `--tools` and `--allowedTools`.
- [ ] With no `lf:tools=` label, argv is identical to today's.
- [ ] **No string originating from GitHub appears anywhere in the child argv.** A test asserts that a label whose value matches a profile still contributes only config-authored strings, and that argv contains no label text.
- [ ] `--tools` carries the full granted surface; nothing is granted by adding only an `--allowedTools` rule.
- [ ] A tool outside the ceiling on `harness.Request` is rejected by `Request.validate` at the harness boundary.
- [ ] Relabel-then-comment widens the next run; a label change with no comment starts nothing.
- [ ] A resumed run takes its tool boundary from the current argv, and the existing guard at `internal/turn/harness_test.go:275-279` still passes.
- [ ] `internal/turn/harvest.go` is unmodified and the cold-rerun path carries the profile.

**US-3 — fail-closed**

- [ ] An unknown profile name refuses the run, posts a `ReplyToolsRefused` reply naming the profile, spawns nothing, and advances the watermark.
- [ ] A malformed `lf:tools=` value refuses the run and the reply does not echo the raw label text.
- [ ] Two `lf:tools=` labels refuse the run as ambiguous; neither is chosen and they are never unioned.
- [ ] No refusal path ever falls back to the default surface or to any wider surface.
- [ ] The refusal is run-less: no runs row, no worktree, no claude process.

**US-4 — observability**

- [ ] `harness.Meta` carries `ToolsRequested`, the existing resolved `Tools`, and `ToolProfile`.
- [ ] The run report has a `**Tools:**` bullet in `metadataList` after `Model`, inside `secFixed`, listing the resolved tools and the profile provenance, and naming any requested-but-not-granted tool.
- [ ] `litefactory logs <issue>` prints resolved tools, the profile, and requested tools when they differ.
- [ ] The `"claude started"` log line carries `allowed_tools` and `tool_profile` alongside `tools`.
- [ ] `TodoWrite` is removed from `defaultTools()` and a test asserts the default surface is the 7 tools that actually resolve.
- [ ] Redaction is unaffected — `internal/redact` matches credential shapes only; a tool list passes through untouched.

## Unresolved

None. (OQ 1 settled empirically — `Task` resolves. OQ 10 settled — the repository-wide forbidden-flag test is `internal/config/purity_test.go:84-91`. C7 clear. All other open questions closed by the 2026-08-27 owner decisions.)

---

## OPTIONAL — independent of labels, Implement may skip wholesale

Not part of any story above. Recommended, not assumed. These fix a **separately measured** hole
(the 250 ambient allow-rules from SD-2) and would hold whether or not labels ship.

| Lever | What it buys |
|-------|--------------|
| `--setting-sources user` or `--bare` | Stops discovery of project/ambient settings, hooks, plugins and `CLAUDE.md` — the direct fix for the leaked ambient allow-rules and for the executing-hooks residual risk (`harness/doc.go:45-55`). |
| `--permission-mode dontAsk` | Deny anything not pre-approved, explicitly for automation, instead of relying on `default` mode's incidental deny-on-no-prompt. |
| `sandbox` settings | OS-level filesystem/network confinement for Bash, with `denyWrite` paths — the only lever that makes an allow-listed `Bash` meaningfully less than arbitrary code execution, given that prefix rules do not survive `&&`. |

Each needs empirical verification against claude 2.1.246 in the style of `CLAUDE_CLI_CONTRACT.md`
(its five-probe method), and its re-verification checklist (`:137-148`) would gain a case for
whichever flag is adopted. Do not adopt any of these on documentation alone.
