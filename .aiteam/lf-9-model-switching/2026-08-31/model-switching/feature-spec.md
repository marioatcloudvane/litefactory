# Feature Spec: Model switching (issue #9)
# Status: APPROVED
# Date: 2026-08-31
# Source: RESEARCH_BRIEF.md (same session) + owner rulings on that brief
# Headless run — no human was available. Gaps filled are recorded in *Assumptions taken*.

## Problem

Today every litefactory run uses whatever model and thinking level `claude` happens to default
to; the model is *observed* (init event → `meta.json` → run report) but never *selected*. The
owner wants two dials on the issue itself — `Model: Opus` and `Thinking: High`, as separate
GitHub labels — that reach the headless `claude` invocation as `--model <model>` and
`--effort <level>`. They are separate labels on purpose: a large number of harnesses is coming,
and one combined dial would multiply into thousands of labels, so `Key: Value` is a *family* of
dials that `Tools:` will later join.

---

## Decisions already settled

Owner rulings on the brief's open questions. **These are closed. Do not relitigate them.**

| # | Decision | Reason given |
|---|---|---|
| D-MS-1 | **Label format is the owner's `Key: Value` notation** — `Model: Opus`, `Thinking: High`. NOT the house `lf:model=opus` form. Q-5 settled. | The owner intends to migrate the existing tools dial onto the same scheme (`Tools: <something>`) later. `Key: Value` is a dial *family*, not one label. |
| D-MS-2 | The brief's two objections to that format **survive as requirements, not as debate**: (a) namespace collision with a repo's own colon-style labels, (b) case/whitespace variance (`Model: Opus` / `model:opus` / `Model:␣␣Opus` / `MODEL:OPUS`). Both get explicit matching-rule acceptance criteria (MS-2). | The objections were real; the format decision does not make them go away. |
| D-MS-3 | **Model/effort incompatibility is allowed, then reported — never refused.** Q-2 settled toward "allow and report". When effort was requested and the selected model carries no `effort` capability, the run report **states so plainly**. | Precedent is `toolsEntry`/`notGranted` in `internal/report/run.go`: requested-vs-resolved is reported, not refused. This is a first-class acceptance criterion (MS-5), not a nicety. |
| D-MS-4 | **The vocabulary and resolution layer must be harness-agnostic.** Many harnesses are coming; adding one later must not require redesigning the scheme. | Two consequences: it strengthens the case for a shared `Key: Value` dial resolver over a third copy of `resolveTools` (Q-13, still open); and it reframes A-1 — "Sol"/"Qwen 3.6" now read as forward-looking references to **future non-Claude harnesses**, not as nicknames for Claude models. |
| D-MS-5 | **Implementation is scoped to Claude models only** (F-4: the CLI reaches no other vendor), **but no Claude-only assumption may be baked into the label/vocabulary layer.** | Stated explicitly in *Out of scope* so it is not read as an oversight. |
| D-MS-6 | **The tools migration is future work.** Build the mechanism so `Tools:` can migrate onto it later. Do **not** migrate `lf:tools=` now, and do **not** break it. | Keeps this feature's blast radius off a shipped, tested security boundary. |
| D-MS-7 | **A behavioural probe of `claude` is a hard prerequisite** (R-0/Q-1), not a nicety. It is MS-1 and gates every story that reaches argv. | `claude --help` on 2.1.251 confirms `--model <model>` and `--effort <low\|medium\|high\|xhigh\|max>` verbatim, so flag *existence and accepted values* are verified. **Behaviour is not**: composition with litefactory's argv, whether the init event's `model` reflects `--model`, and what a bad value does. `.aiteam/APP_CONTEXT.md` ("Where durable external-behaviour findings live") plus two existing probe documents make hands-on confirmation the house rule. |

---

## User Stories

### MS-1 — Behavioural probe of `--model` and `--effort`
**Size: M · Depends on: —**

As a **litefactory maintainer**, I want the composition and failure behaviour of `--model` and
`--effort` confirmed hands-on against the installed `claude`, so that no implementation story is
built on an unverified assumption about an external binary.

*Delivery:* a third external-behaviour document beside `CLAUDE_CLI_CONTRACT.md` and
`TOOL_SURFACE_PROBE.md`, naming `claude 2.1.251`, with a probe/contingency table modelled on
`.aiteam/main/2026-08-30/tool-enforcement/IMPLEMENTATION_PLAN.md:58-91`.

- **AC-MS-1.1** A document exists at `.aiteam/lf-9-model-switching/2026-08-31/model-switching/MODEL_EFFORT_PROBE.md`, states the exact `claude` version probed, and carries a re-verification checklist, matching the two existing probe documents' shape.
- **AC-MS-1.2** It records the result of running litefactory's **full argv shape** (`-p --output-format stream-json --verbose --session-id … --tools … --settings … --strict-mcp-config`) with `--model` added, and separately with `--effort` added, and with both: exit code and whether the run completed.
- **AC-MS-1.3** It records whether the init event's `model` field reflects the value passed to `--model`, verbatim, for at least one alias and one full model ID.
- **AC-MS-1.4** It records the observed behaviour of a **bogus model value**: exit code, stderr, whether an init event arrived, and whether the failure is local or a round-trip to the API.
- **AC-MS-1.5** It records the observed behaviour of a **bogus effort value** (expected: warn on stderr and continue) and of a **valid effort on a model with no `effort` capability** (expected: silent no-op).
- **AC-MS-1.6** It records `--model` alongside `--resume`, including whether a resumed conversation honours a *changed* model.
- **AC-MS-1.7** It records which wins when `forward_anthropic_env: true` puts `ANTHROPIC_MODEL` in the child environment and `--model` is also passed.
- **AC-MS-1.8** Every probe was run read-only in a scratch directory outside the repo. **No test that spawns a real `claude` is committed** (R-13).
- **AC-MS-1.9** Each probe row carries a contingency: what the design does if that probe fails. A probe whose result invalidates the argv delivery shape (AC-MS-1.2) stops the feature and goes back to the owner.

---

### MS-2 — `Key: Value` dial labels are detected, matched and refused deterministically
**Size: L · Depends on: MS-1**

As a **repository maintainer**, I want `Model:` and `Thinking:` labels recognised by a rule with a
defined answer for every input, so that a mistyped label is never silently ignored and an
unrelated label in my repo is never mistaken for a dial.

*Delivery:* the label-reading layer for the dial family — detection, matching, ambiguity counting,
and refusal replies — with `lf:tools=` untouched.

- **AC-MS-2.1** A label whose key is not in the dial vocabulary is **ignored in silence**, not refused. `priority:high` (already exercised at `internal/daemon/tools_internal_test.go:92-94`) still starts a run with no reply, under every casing.
- **AC-MS-2.2** Each of `Model: Opus`, `model:opus`, `Model:␣␣Opus`, `MODEL:OPUS` and `Model:Opus` has a **defined, tested disposition** of exactly one of {resolves, refused-malformed}. No input in this set is silently ignored — a near-miss on a dial key is loud (D-6). The assignment of dispositions is the architect's (see *Open design choices*); its **totality and its test coverage are not**.
- **AC-MS-2.3** The canonical form the owner specified — `Model: Opus`, `Thinking: High` — resolves.
- **AC-MS-2.4** Two labels on the same dial (e.g. two `Model:` labels, in any casings) are **ambiguous and refuse the run**. The count is taken **before** any of them is judged; one is never picked and two are never unioned (copying `internal/daemon/tools.go:141-163`, reasoning included).
- **AC-MS-2.5** A `Model:` label and a `Thinking:` label together are **two different dials, not an ambiguity**, and both resolve.
- **AC-MS-2.6** A dial value that is not in the vocabulary refuses the run, rather than being passed through to `claude` (F-5: the CLI fails open, so litefactory must not).
- **AC-MS-2.7** No refusal echoes label text that has not passed a shape check. The malformed and ambiguous refusals carry **no field for label text to travel in** (S-9), mirroring `report.Reply`'s existing discipline.
- **AC-MS-2.8** Labels are resolved **once per tick**, from the snapshot already on the issue payload — no extra GitHub API call. Two comments drained in one tick share one snapshot; a bare relabel with no following comment starts nothing (mirroring `TestRelabelThenComment`, AC-2.10).
- **AC-MS-2.9** Every existing `lf:tools=` behaviour is unchanged: the same three refusal kinds, the same reply text, the same tests green, with no `lf:tools=` label migrated to the new grammar.
- **AC-MS-2.10** A dial refusal on the issue-body path (trigger comment id `0`) **posts nothing**, refuses the run and logs — the existing `tick.go:1505-1511` branch's behaviour.

---

### MS-3 — Model and effort vocabularies resolve to values litefactory authored
**Size: M · Depends on: MS-1**

As a **daemon operator**, I want the label value to be a lookup key into a table litefactory or I
authored, so that no string originating on the forge ever reaches argv.

*Delivery:* the alias→model-ID table, the effort level set, and the per-model `effort`-capability
matrix, plus validation.

- **AC-MS-3.1** The label value is used **only as a lookup key**. The string joined into argv comes from the table, never from the label (C2, `internal/turn/execute.go:221-226`).
- **AC-MS-3.2** The model table maps to **full model IDs** (e.g. `claude-opus-5`), not to CLI aliases, because alias resolution is provider-dependent (`opus` → `claude-opus-5` first-party, `claude-opus-4-7` via gateway; R-10).
- **AC-MS-3.3** The model vocabulary covers at least Opus, Sonnet and Haiku, each pinned to a full ID.
- **AC-MS-3.4** The effort vocabulary is exactly `low`, `medium`, `high`, `xhigh`, `max` — the five values `claude --help` lists on 2.1.251.
- **AC-MS-3.5** A per-model `effort`-capability matrix exists and is correct as of 2.1.251: carrying `effort` — `claude-sonnet-4-6`, `claude-sonnet-5`, `claude-opus-4-6`, `claude-opus-4-7`, `claude-opus-4-8`, `claude-opus-5`, `claude-fable-5`; **not** carrying it — `claude-3-5-haiku`, `claude-3-5-sonnet`, `claude-3-7-sonnet`, `claude-haiku-4-5`, `claude-sonnet-4-0`, `claude-sonnet-4-5`, `claude-opus-4-0`, `claude-opus-4-1`, `claude-opus-4-5`, `claude-mythos-5`.
- **AC-MS-3.6** If any part of the table is operator-configurable, **every configurable string that can reach argv is added to `validateNoForbiddenFlag`** (`internal/config/policy.go:155-184`), and a test proves a configured value smuggling the permission-skipping flag is refused at startup (R-5).
- **AC-MS-3.7** If any config key is added, it is absent from a fresh install, appears in `internal/config/template.go` as a commented example only, and `TestDefaultFileMatchesDefaults` stays green (the `tool_ceiling` / `tool_profiles` precedent).
- **AC-MS-3.8** The vocabulary layer names no Claude-specific concept in its own types or field names (D-MS-4/D-MS-5): a future harness's dial values must be expressible without renaming anything.

---

### MS-4 — The selected model and effort reach `claude`'s argv
**Size: M · Depends on: MS-1, MS-2, MS-3**

As a **repository maintainer**, I want the labels to actually change the `claude` invocation, so
that `Model: Opus` runs Opus.

*Delivery:* new request fields carried through `turn` → `harness`, and conditional argv
construction.

- **AC-MS-4.1** With **no dial label present**, argv is **byte-identical** to today. Both flags are omitted entirely — not emitted empty, not defaulted. `TestRunWithNoProfileArgvIsByteIdentical` (`internal/harness/runner_test.go:1200-1218`, AC-2.3) stays green unmodified (R-2).
- **AC-MS-4.2** There is **no configured default model and no configured default effort**. A config-level default would change argv for every existing run and could not be absent (R-2).
- **AC-MS-4.3** With a model selected, argv carries `--model <full-model-id>`; with an effort selected, `--effort <level>`; with both, both.
- **AC-MS-4.4** Cold, resume, and cold-after-resume-refused argv differ **only** by the session flag, with both new flags present identically on all three. `TestRunResumeSwapsOnlyTheSessionFlag` (`internal/harness/runner_test.go:1021-1048`) is extended to cover them (R-7). The cold-rerun path's in-place request mutation (`internal/turn/harvest.go:130-133`) preserves them.
- **AC-MS-4.5** **`RunResult.Model` continues to hold strictly the init event's value** and is never pre-populated from the request. A test asserts `resumeRefused` (`internal/harness/runner.go:724-729`) still fires on a resume refusal when a model was requested — otherwise `ErrSessionNotResumable` stops being returned and the task wedges permanently (R-1).
- **AC-MS-4.6** The requested model and effort live in **separate, unmistakably-named fields** from the resolved model, at every layer they cross (`harness.Request`, `turn.Result`, `harness.Meta`).
- **AC-MS-4.7** Slices and structs carrying the new values are copied at struct boundaries per the house rule, so a request mutated on the cold-rerun path cannot rewrite configuration shared with a sibling turn.
- **AC-MS-4.8** Neither flag is delivered via the shared settings file or via an environment variable. Both go in argv (R-12, R-5).

---

### MS-5 — An effort request that the model cannot honour is allowed and reported
**Size: M · Depends on: MS-3, MS-4**

As a **repository maintainer**, I want to be told when my `Thinking:` label had no effect because
of the model I chose, so that "my label did nothing" is never a silent outcome.

*Delivery:* the applicability check and the report sentence — D-MS-3 made concrete.

- **AC-MS-5.1** `Model: Haiku` + `Thinking: High` (or any model/effort pair where the model carries no `effort` capability) **runs**. The combination is never refused (D-MS-3).
- **AC-MS-5.2** The run report for such a run **states plainly** that effort was requested and was not applicable to the selected model, naming the requested level and the model. The phrasing follows `toolsEntry`/`notGranted` (`internal/report/run.go:614-669`) — a fact about the run, not a warning banner.
- **AC-MS-5.3** When effort **was** applicable, the report says so without editorialising, and never claims a level was applied that was not requested.
- **AC-MS-5.4** Applicability is judged against the **init event's resolved model** when one arrived, falling back to the requested model when no init event arrived. A run with a `Thinking:` label and no `Model:` label is therefore judged against whatever model `claude` resolved.
- **AC-MS-5.5** With no effort requested, no applicability sentence appears anywhere.
- **AC-MS-5.6** The applicability determination is unit-tested across the full capability matrix (AC-MS-3.5) in both directions, with no `claude` process involved.

---

### MS-6 — What was requested is durable and visible after the fact
**Size: M · Depends on: MS-4, MS-5**

As a **maintainer reading a run report a week later**, I want to see which model and thinking
level the run *asked for* beside the one it *got*, so that a relabel is detectable and a silent
divergence is not.

*Delivery:* `meta.json` fields plus the run report's provenance line. No schema migration.

- **AC-MS-6.1** `NNNN.meta.json` carries `model_requested` and `effort_requested` beside the existing `model`, mirroring `tools_requested` beside `tools` (`internal/harness/meta.go:66-73`).
- **AC-MS-6.2** **No store migration.** No column is added to `runs` or any other table (Q-12; precedent `.aiteam/AC_AMENDMENTS.md:1286-1312`, which rejected a `runs` column in a closely analogous case).
- **AC-MS-6.3** The run report shows requested-vs-resolved for the model: when the init event's model differs from the requested one, the report says so rather than showing only one of them.
- **AC-MS-6.4** **No new bullet appears between the `Model` bullet and the `Tools` bullet.** `TestToolsBulletFollowsModelImmediately` (`internal/report/report_test.go:1119-1145`, AC-4.2) stays green unmodified (R-6).
- **AC-MS-6.5** Every new provenance line sits in `secFixed` and **survives truncation** at the 32 KiB cap, tested the way the Tools bullet is (`report_test.go:1153-1165`). Labels are authenticated by nothing (C3); this line is the whole detection story for a relabel.
- **AC-MS-6.6** A run that never produced an init event still reports what was requested, and says plainly that no resolved model was recorded — it does not present the request as the result (the AC-4.3 shape).
- **AC-MS-6.7** No untrusted label text reaches the comment body. Only values that passed the vocabulary lookup, or litefactory's own table entries, are rendered.

---

### MS-7 — The dials are documented where the next engineer will look
**Size: S · Depends on: MS-6**

As the **next engineer to touch this**, I want the dial family, its matching rule and its rulings
recorded in the places this codebase keeps such things, so that the reasoning is not reconstructed
from a diff.

*Delivery:* config template comments (if a config key exists), package doc updates,
`.aiteam/APP_CONTEXT.md`, and `.aiteam/AC_AMENDMENTS.md`.

- **AC-MS-7.1** `.aiteam/APP_CONTEXT.md` describes the `Key: Value` dial family, names the two dials, and states that `lf:tools=` deliberately did **not** migrate and why (D-MS-6).
- **AC-MS-7.2** `.aiteam/AC_AMENDMENTS.md` carries a ruling entry for D-MS-1 (the label format departing from the house `lf:` form) and one for D-MS-3 (allow-and-report rather than refuse), each with its reason — so "what happened to D-6's house label format?" has exactly one place to look.
- **AC-MS-7.3** The matching rule chosen under AC-MS-2.2 is written out in a `doc.go` or a package-level comment, in the register of `internal/daemon/tools.go:10-28`: what is detected, what is accepted, and what breaks if it is changed the obvious way.
- **AC-MS-7.4** `.aiteam/APP_CONTEXT.md`'s "Where durable external-behaviour findings live" section lists the new probe document alongside the two existing ones, and notes that both of those are version-behind 2.1.251.
- **AC-MS-7.5** Any comment this feature makes untrue is updated as part of the change — in particular `internal/daemon/tools.go:10` ("the whole of the label vocabulary") if a second vocabulary now exists.
- **AC-MS-7.6** `make check` is green: `gofmt`, `go vet`, the pinned golangci-lint, and `go test -race ./...`.

---

## In scope

- Two GitHub-label dials in the owner's `Key: Value` notation: `Model:` and `Thinking:`.
- `Model:` → `claude --model <full-model-id>`; `Thinking:` → `claude --effort <level>`.
- A vocabulary/matching layer shaped so further dials and further harnesses join it without redesign.
- Refusal semantics for unknown / malformed / ambiguous dial labels, on the `lf:tools=` groove.
- The per-model `effort`-capability matrix, and reporting when a requested effort was not applicable.
- Requested-vs-resolved persistence in `meta.json` and rendering in the run report.
- A behavioural probe document for `claude 2.1.251` (MS-1), gating everything above.
- `validateNoForbiddenFlag` coverage for any new configured string that reaches argv.

## Out of scope

| Excluded | Why |
|---|---|
| **Any non-Anthropic backend** — OpenAI, Qwen, Gemini, and "Sol"/"Qwen 3.6" as literal targets | F-4: the CLI has no non-Anthropic provider path. Reaching one needs a translating proxy at `ANTHROPIC_BASE_URL` with its own auth story this product deliberately does not have. **The label/vocabulary layer must nonetheless carry no Claude-only assumption** (D-MS-4/D-MS-5) so a future harness can join. |
| **Migrating `lf:tools=` to `Tools:`** | D-MS-6. Build the mechanism for it; do not do it now, and do not break the existing label. |
| **A store migration** | Q-12. `meta.json` plus the report comment carry the selection. |
| **A per-repo model or effort override** | Q-10. `claude:` is global to the daemon; no per-repo override exists and none is invented here. |
| **A spend ceiling (`--max-budget-usd`)** | Q-14. It exists, it is "only works with `--print`" (which litefactory uses), and it is the natural bound if C3's cost exposure ever needs closing. **Recorded so it is not rediscovered from scratch** — not built. |
| **A configured default model or effort** | R-2. It would change argv for every existing run and could not be absent. |
| **Any prompt-injection "ultrathink" layer** | F-2: thinking is flag-controlled on 2.1.x. A prompt layer would be strictly worse. |
| **`--thinking`, `--thinking-display`, `--max-thinking-tokens`, `--fallback-model`** | `--thinking` is binary enabled/disabled; the graded control the owner described is `--effort`. `--max-thinking-tokens` is deprecated. `--fallback-model` is a separate resilience feature. |
| **Effort values `auto` and integers** | Accepted by the binary but outside the five values `--help` documents. Scope to the five (AC-MS-3.4). |
| **Any change to `RunResult.Model`'s meaning** | R-1. The closest thing to a hard invariant in this work. |
| **A per-run settings-file or environment-variable delivery channel** | R-12, R-5. Both flags go in argv. |
| **A `model_requested` column in `litefactory logs` output** | Not asked for; the report comment and `meta.json` carry it. Revisit only if the report proves insufficient. |
| **A committed test that spawns a real `claude`** | R-13, `.aiteam/APP_CONTEXT.md:405-424`. Every CLI fact comes from the MS-1 probe document. |

---

## Open design choices for the architect

Genuinely open. Each is stated with its trade-offs and **is not settled here.**

### O-1 — The exact `Key: Value` matching rule (D-MS-2 makes the requirements binding; the rule is not chosen)

The rule must be **total** over AC-MS-2.2's input set and must never silently ignore a near-miss on
a dial key. Three shapes, with their costs:

| Option | Rule | Gains | Costs |
|---|---|---|---|
| **(i) Byte-exact canonical** | Key must be exactly `Model`/`Thinking`, one `: ` separator, value byte-exact from a display-cased table (`Opus`). Anything folding to a dial key but not matching byte-for-byte → malformed refusal. Anything not folding to a dial key → ignored. | Maximum D-6 fidelity: detect leniently, accept strictly, transplanted unchanged. One canonical string per dial value, so the GitHub label list stays tidy. | `MODEL:OPUS` and `model: opus` both draw a refusal, which reads as pedantic to a maintainer who typed a reasonable thing. A repo's own unrelated label literally named `Model: <anything>` draws a refusal on every comment. |
| **(ii) Fully normalising** | Trim, collapse internal whitespace, fold case on both key and value; accept every variant that normalises to a vocabulary entry. | Most forgiving; no maintainer is ever refused for capitalisation. Reads best in the GitHub UI. | Highest namespace-collision blast radius: any repo label whose key normalises to `model` **hijacks** the run rather than merely being refused. Loses the "seeing-and-refusing turns a typo into a loud refusal" property entirely — there is no near-miss left to refuse. |
| **(iii) Hybrid** | Detect the key case- and whitespace-insensitively; accept the key only byte-exact (refuse otherwise); match the value case-insensitively against the vocabulary. | Keeps D-6 on the key (where collision risk lives) and relaxes it on the value (where it does not). `Thinking: high` and `Thinking: High` both work; `thinking: High` refuses loudly. | Two rules to explain instead of one. Value-side leniency means the label list can accumulate visually-different labels that mean the same thing. |

Also open within O-1, and coupled to it:

- Whether the separator is `":"`, `": "`, or either.
- Whether an unknown *value* on a known dial key refuses (assumed yes, A-S8) or is reported like an inapplicable effort. The owner settled only the **compatibility** case (D-MS-3), not this one.
- Whether the dial key set is closed in Go or extensible from config — a closed set bounds the collision surface; an open one is what makes a future harness's dial free to add.

### O-2 — Where the model table and the capability matrix live (Q-4)

| | Config (`claude.models:` map) | Hardcoded in Go | Hybrid |
|---|---|---|---|
| New model IDs | No release needed; the catalog holds 17 IDs and churns faster than this daemon ships | Needs a litefactory release | Built-ins ship; operator can add |
| C2 | Preserved — label is a lookup key into operator-authored config, exactly like `tool_profiles` | Preserved — the argv string is litefactory-authored | Preserved either way |
| Operator control over reach | Operator decides which models are reachable — the structural analogue of `tool_ceiling` (R-11) | None: every alias litefactory knows is reachable by anyone who can label an issue | Operator can restrict as well as extend |
| The capability matrix (F-3) | Operator must hand-maintain it in YAML and **will get it wrong silently** | litefactory ships it and can keep it correct | Matrix in Go, table extensible |
| Reproducibility (R-10) | Operator can pin full IDs | litefactory pins them | Both |
| Cost | Another `Defaults()` / template / validation / `validateNoForbiddenFlag` quadruple | Zero config surface | The full quadruple, plus merge semantics |

**Weight to carry into the decision:** F-3 tilts against pure config — the effort-capability matrix
is knowledge about *models*, not about *this operator's preferences*. R-11 tilts against pure
hardcoding — labels are authenticated by nothing (C3) and this dial's blast radius is **spend**
(`max` effort on Opus 5 is ~1.7× `high`; model choice moves cost by a larger factor again), so a
config table *is* the natural allowlist. The hybrid has direct precedent: `tool_ceiling` defaults
to a copy of `tools` when absent (`internal/config/config.go:155-163`).

### O-3 — Shared `Key: Value` dial resolver, or a third copy of `resolveTools` (Q-13)

| | Generalise into a shared dial resolver | Copy `resolveTools` per dial |
|---|---|---|
| Adding harness N+1 | One vocabulary entry | Another copy, another set of refusal kinds |
| D-MS-4 (many harnesses coming) | Directly serves it — this is the ruling's strongest pull | Works, but the redesign the ruling wants to avoid arrives at copy three or four |
| The existing comments | A generalisation **dilutes** them: `internal/daemon/tools.go`'s value is largely in very specific, heavily-reasoned prose about *this* dial | Each copy keeps its own reasoning, at the cost of nine copies of the same paragraph |
| Risk to a shipped boundary | Higher — the tools dial is the security-relevant one and D-MS-6 says do not break it | Zero: `lf:tools=` is not touched at all |
| Grammar mismatch | The shared resolver must handle `Key: Value` now and `lf:tools=` later, or be new-dials-only until the migration | No mismatch to handle |

**Coupled sub-question (R-8):** how many `report.ReplyKind`s. Two dials × {unknown, malformed,
ambiguous} is naively six new kinds, each needing an enum value, a case in `replyKind()`
(`internal/turn/report.go:612-625`), prose, and tests. A shared resolver could collapse these into
three dial-agnostic kinds carrying the dial name — but the dial name is then a **field that label
text could travel in**, which is exactly what S-9 removes from the malformed and ambiguous
refusals. Whichever is chosen must keep AC-MS-2.7 true.

**Also note:** `internal/report` cannot import `internal/turn`, so any new reply shape is a
`ReplyKind` **plus** a case in `replyKind()` — both, always.

---

## Assumptions taken

Recorded because this was a headless run and nobody could be asked.

- **A-S1 — Dial keys are the owner's words, internal names are the flag's.** The label keys are `Model` and `Thinking` verbatim; internal identifiers, field names and the meta key use `effort`, because `--effort` is the flag that exists and `--thinking` is a different, binary flag. The spec never promises a `--thinking` mode.
- **A-S2 — `Thinking: High` maps to `--effort high`.** Carried from brief A-3. The graded control the owner described is `--effort`; `--thinking` is enabled/disabled only.
- **A-S3 — Unknown *value* on a known dial key refuses the run** (Q-7's recommendation). Reason: F-5 — `--model` has no parse-time validation and unknown strings pass through, and `--effort` warns and continues, so if litefactory does not refuse, nobody does, and the run silently uses a different model than was asked for. **This is distinct from what the owner settled**: D-MS-3 settled the *model/effort compatibility* case toward allow-and-report; it did not settle unknown values. Flagged as a reversible choice in O-1.
- **A-S4 — Effort applicability is judged against the resolved model where one exists** (AC-MS-5.4). A `Thinking:` label with no `Model:` label cannot be judged before the run, so the report is the only honest place to state it.
- **A-S5 — Refusal shape mirrors the tools dial exactly**: three kinds (unknown / malformed / ambiguous), "no vocabulary configured at all" taking the unknown path, and the issue-body path (comment id `0`) posting nothing. Whether these are per-dial or shared kinds is O-3.
- **A-S6 — Per-tick snapshot semantics are identical to tools** (Q-9): resolved once in `Daemon.run`, never mid-run; a bare relabel starts nothing; the report names what was requested so the snapshot is auditable.
- **A-S7 — "Sol" and "Qwen 3.6" are not implemented.** Per D-MS-4 they read as forward-looking references to future non-Claude harnesses. The vocabulary layer stays harness-neutral; nothing is built for them. This reverses brief A-1's reading (friendly aliases for Claude models) on the owner's own input, and the practical scope is the same either way: Claude models only.
- **A-S8 — The initial model vocabulary is Opus / Sonnet / Haiku at minimum**, each pinned to a full model ID. The owner named only "Opus". A larger initial roster (Fable, Opusplan, the `[1m]` variants) is the architect's call under O-2 and costs one table row each.
- **A-S9 — `forward_anthropic_env` + `ANTHROPIC_MODEL` is documented, not refused.** AC-MS-1.7 establishes which wins; the daemon logs when both a label-selected model and a forwarded `ANTHROPIC_MODEL` are in play. Refusing the combination is not proposed — the setting is off by default and `internal/config/config.go:176-186` already says it should stay off.
- **A-S10 — The probe document lives in this session directory** (`MODEL_EFFORT_PROBE.md`), following the two existing probes' placement under their own session directories rather than a shared location.
- **A-S11 — No CLI surface changes.** `litefactory logs` keeps showing the resolved `model` row only; the requested values are readable in `meta.json` and the report comment.
- **A-S12 — Acceptance criteria carry the `AC-MS-` prefix** and stories the `MS-` prefix, following the `AC-PAR-` convention that `TestIntroducedCriteriaCarryTheParPrefix` (`internal/daemon/record_test.go`) established for a feature's own introduced criteria.
- **A-S13 — Story sizes are relative to each other**, not to a calendar. MS-2 is the L because the matching rule, the refusal surface and the O-3 structural decision all land in it.
