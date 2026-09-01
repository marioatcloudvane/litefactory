# Plan Brief — Model switching
# Branch: lf/9-model-switching
# Date: 2026-09-01 (session directory 2026-08-31 — one continuous piece of work)
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/9
# Citation base: f2b8900
# Research brief: ./RESEARCH_BRIEF.md
# Feature spec (APPROVED): ./feature-spec.md
# Architect review: ./ARCHITECT_REVIEW.md

Two GitHub-label dials — `Model: Opus` and `Thinking: High` — reach the `claude` invocation as
`--model` and `--effort`. Seven stories, MS-1..MS-7. This brief holds only what two or more stories
need. Each story's own tasks, acceptance criteria and architecture notes live in its sub-issue and
nowhere else.

---

## Shared Decisions

### Naming and literals

- **SD-1** — Canonical dial keys are the byte strings `Model` and `Thinking`. The canonical separator
  is `": "` — exactly one colon, exactly one ASCII space. Nothing else.
- **SD-2** — Internal identifiers use **`effort`**, never `thinking`. The flag that exists is
  `--effort`; `--thinking` is a different, binary flag this design does not use. The *label key*
  stays `Thinking` because that is the owner's word. Both facts are documented, not left implicit.
- **SD-3** — Request-side field names are `ModelRequested` and `EffortRequested`, spelled identically
  on `harness.Request`, `turn.Request`, `turn.Result`, `report.Run` and `harness.Meta`. The
  resolved-side name stays `Model` on `harness.RunResult` and `harness.Meta`. The `Requested` suffix
  is present on every requested field and absent from every resolved one — there is no second
  distinguishing scheme.
- **SD-4** — `meta.json` keys are `model_requested` and `effort_requested`, beside the existing
  `model`, mirroring `tools_requested` beside `tools`.
- **SD-5** — Three new `report.ReplyKind`s, and only three: `ReplyUnknownDialValue`,
  `ReplyMalformedDialLabel`, `ReplyAmbiguousDialLabels`. Their `String()` forms are
  `unknown_dial_value`, `malformed_dial_label`, `ambiguous_dial_labels`.
- **SD-6** — The one new config key is `claude.models`. There is no other.
- **SD-7** — Pinned model IDs, authored in MS-3 and consumed by MS-4/MS-5/MS-6:
  `opus → claude-opus-5`, `sonnet → claude-sonnet-5`, `haiku → claude-haiku-4-5`,
  `fable → claude-fable-5`. Effort levels: `low medium high xhigh max`. The effort-capability matrix
  is AC-MS-3.5's two lists verbatim — 7 model IDs carrying `effort`, 10 not, 17 total.

### The matching rule (O-1, settled: hybrid with a strict separator)

- **SD-8** — Four gates, in this order, total over every input:
  - **G1 detection, lenient.** Split at the **first** `:`. Trim ASCII space and tab from the key.
    Case-fold. If it does not fold to a member of the closed key set `{Model, Thinking}`, the label is
    **ignored in silence** — no reply, no log, the run proceeds.
  - **G2 ambiguity, counted per dial and before anything is judged.** Two or more labels naming one
    dial, in any casings and whatever their shape, refuse that dial. One is never picked and two are
    never unioned. A `Model:` label beside a `Thinking:` label is two dials, not an ambiguity.
  - **G3 key and separator, strict.** The label must begin with the canonical key byte for byte,
    followed by `": "`. Anything else is refused-malformed.
  - **G4 value.** Taken byte-for-byte with no trimming, then shape-checked: 1–40 characters,
    alphanumeric first and last, containing only ASCII alphanumerics, `.`, `_`, `-` and space. A
    failure is refused-malformed. A pass is lower-cased and looked up. A miss is refused-unknown.
- **SD-9** — A key that folds to a dial key but fails acceptance is refused **loudly**. A key that
  does not fold is ignored **silently**. There is no third answer.
- **SD-10** — An unknown *value* on a known dial key **refuses the run**. This is a different case
  from the one the owner settled: allow-and-report covers a request whose halves are individually
  well-formed and which litefactory can name exactly; an unknown value is a request litefactory
  cannot even name.
- **SD-11** — The dial key set is **closed in Go**, not config-extensible. A new dial is one table
  row in a litefactory release, because a new dial needs a new argv builder in that release anyway.

### Where the vocabulary lives (O-2, settled: hybrid, split three ways)

- **SD-12** — The effort-capability matrix, the model catalog and the effort-level set are **Go, not
  configurable at all**. They are knowledge about models, not operator preference; an operator
  hand-maintaining them in YAML gets them wrong silently.
- **SD-13** — The dial-value → full-model-ID roster is a **Go built-in overridable by
  `claude.models`**. Absent means the built-in roster is live. Present **replaces** the built-in
  roster entirely — it does not merge. Every configured value must be a key of the Go catalog,
  checked at startup.
- **SD-14** — There is **no configured default model and no configured default effort**, at any
  layer. A default would change argv for every existing run and could not be absent.
- **SD-15** — Model IDs are **full IDs**, never CLI aliases, because alias resolution is
  provider-dependent.

### Structure (O-3, settled: shared resolver, new-dials-only)

- **SD-16** — Two new leaf packages. `internal/dial` holds the `Key: Value` grammar and names no
  Claude concept anywhere in its types or fields. `internal/catalog` holds the Claude model catalog,
  the capability matrix and the effort vocabulary. **Neither may import any other litefactory
  package, ever** — that constraint is what lets `internal/report` import them without a cycle, and
  `internal/report` cannot import `internal/turn`.
- **SD-17** — The **daemon** does the vocabulary lookup, in `internal/daemon/dials.go`.
  `internal/dial` has no opinion about what a value means; it answers "which dial keys did these
  labels name, is each well-formed, is each unambiguous, what value did it carry" and stops.
- **SD-18** — `resolveTools` runs **first** and its refusal wins when an issue carries both a bad
  `lf:tools=` label and a bad dial label. `internal/daemon/tools.go` is edited for **one comment** in
  this entire feature and nothing else. `refuseDial` is a **sibling** of `refuseTools`, not a
  generalisation of it — ~15 duplicated lines bought against leaving the shipped tools refusal path
  byte-untouched.
- **SD-19** — Dial refusals do **not** go through `replyKind()`. Verified in the tree: tool refusals
  do not either — `replyKind()` maps command kinds, and a refusal sets `ReplyRequest.Refusal`
  directly. The real integration point is `ReplyKind.RefusesTools()`, which has exactly two callers,
  both in `internal/turn/report.go`. A new `RefusesLabel()` covering all six label-drawn kinds
  replaces it at both.
- **SD-20** — Model and effort are carried as **strings, never slices**, at every layer. Say so in
  the field doc comments, so the next reader knows the copy-at-struct-boundaries rule was considered
  rather than forgotten.
- **SD-21** — Nothing goes in `pkg/`. **No new interface is declared anywhere in this feature** — one
  concrete implementation, nothing needing substitution in tests. **No new third-party dependency.**

---

## Workflow

### Shipping order

```
MS-1  probe gate  (hard gate; nothing below may reach argv until it passes)
  │
  ├──► MS-3  vocabularies  ── ships BEFORE MS-2
  │       │
  ├──► MS-2  grammar + refusals ──┐
  │                               │
  └───────────────────────────────┴──► MS-4  argv
                                          │
                                          ├──► MS-5  applicability + report sentence
                                          │        │
                                          │        └──► MS-6  meta.json + provenance line
                                          │                     │
                                          └─────────────────────┴──► MS-7  documentation
```

1. **MS-1 first, and it is a hard gate.** `claude --help` confirmed the flags' existence and accepted
   values on 2.1.251. It confirmed nothing about composition with litefactory's argv shape, about
   whether the init event reflects `--model`, or about exit codes. This repo probes external-tool
   behaviour hands-on and writes it up before anything depends on it, and the two existing probe
   documents are both version-behind.
2. **MS-3 before MS-2** — a deliberate departure from the spec's listing order, legal because the
   spec has both depending on MS-1 only. `internal/catalog` is a leaf with no dependencies, and
   MS-2's `resolveDials` needs the vocabularies to look values up in. **Hard cross-story coupling:
   T-MS2.7 depends on T-MS3.3.** Building MS-3 first removes a stub.
3. **MS-4 after both**, because it needs a resolved model (MS-2's grammar) *and* a full model ID
   (MS-3's table) before it has anything to put in argv.
4. **MS-5 after MS-4**, because applicability is judged against the init event's resolved model,
   which only exists once a run has been invoked with `--model`.
5. **MS-6 after MS-5** — the provenance line and the applicability sentence share the render input
   and the same bullet-order and truncation constraints. Splitting them across two independent
   changes means two chances to move the Model/Tools pair.
6. **MS-7 last**, because it documents the matching rule *as built* and fixes every comment the
   feature made untrue — both need the code final.

### If a probe row fails

| Probe row | Consequence | Cost |
|---|---|---|
| AC-MS-1.2 — flags compose with litefactory's full argv | **Stop the feature, back to the owner.** The argv delivery shape is the whole design and both alternative channels are ruled out. | Feature |
| AC-MS-1.6 — `--model` alongside `--resume`, incl. a changed model | The row most likely to cost the plan. If a resumed conversation refuses a changed model, "identical on all three paths" is unshippable and a mid-conversation relabel wedges the task. Contingency: pin the model to the **segment**, ignore later relabels within it, and say so in the report. Must be known before MS-4 starts. | MS-4 redesign |
| AC-MS-1.5 — valid effort on a no-effort model is a silent no-op | If it is a hard error instead, the allow-and-report ruling is unshippable and the owner must be re-asked. Second stop condition. | Owner |
| AC-MS-1.3 — init event's `model` reflects `--model` | Applicability loses its evidence base and requested-vs-resolved becomes vacuous. Contingency: judge against the requested model always, and say plainly that no resolved model was recorded. | MS-5/MS-6 wording |
| AC-MS-1.4 — bogus model behaviour | Informational only. litefactory refuses unknown values before spawning. | None |
| AC-MS-1.7 — `ANTHROPIC_MODEL` vs `--model` | If the forwarded env var wins, the daemon log line becomes a warning and the report must not claim the label took effect. | One log line, one caveat |

---

## Cross-Story Constraints

### Test invariants that must stay green, unmodified

| Test | File | What breaks it |
|---|---|---|
| `TestRunWithNoProfileArgvIsByteIdentical` | `internal/harness/runner_test.go:1200-1218` | Emitting either flag unconditionally, or emitting it empty. Both must be **omitted entirely** when unselected. |
| `TestRunResumeSwapsOnlyTheSessionFlag` | `internal/harness/runner_test.go:1021-1048` | Building the flags differently on the resume path. It is *extended*, never relaxed. |
| `TestToolsBulletFollowsModelImmediately` | `internal/report/report_test.go:1119-1145` | Any new bullet between the `Model` and `Tools` bullets. New lines go **after** the Tools bullet. |
| Tools-bullet truncation survival | `internal/report/report_test.go:1153-1165` | A provenance line outside `secFixed`. |
| `TestRelabelThenComment` | `internal/daemon/tools_test.go:84-112` | Resolving dials anywhere but once, in `Daemon.run`, from the tick's snapshot. |
| `TestDefaultFileMatchesDefaults` | `internal/config/template_test.go:19-27` | Putting `Models` in `Defaults()`, or an uncommented `models:` in the template. It goes through `Decode`, not `Load`, so the default-resolution path never runs on it. |
| `TestForbiddenFlagIsAbsentFromTheTree` | `internal/config/purity_test.go:82+` | Writing the permission-skipping flag as a literal anywhere, including in the two new packages, which the walk covers. |
| Every `lf:tools=` test | `internal/daemon/{tools_test.go,tools_internal_test.go}` | All green, **none edited**. |

### Ring-fenced

**`RunResult.Model` and `resumeRefused` (`internal/harness/runner.go:715-729`).** An empty `Model` is
the structural test for "no init event arrived". The tempting shortcut — pre-populating it from the
request so the report always has a model to show — stops `ErrSessionNotResumable` being returned,
kills the cold rerun, and **wedges the task permanently**: every run asks to resume, is refused, and
reports the same failure forever. **`harness.RunResult` gains no field in this work.** This is the
single most dangerous edit in the feature.

**`internal/daemon/tools.go` and the whole `lf:tools=` path.** One comment change, in MS-7. Nothing
else. `git diff` on that file is the review's cheapest check.

### Platform

- Two new top-level packages, both under `internal/`, both leaves, both stdlib-only. No `pkg/`
  promotion — there is no external consumer.
- **No goroutine, channel, mutex or `context` change is introduced.** The one shared-state item is
  `Daemon.models`: `maps.Clone` at construction, read-only thereafter, exactly as `toolProfiles` is.
  `go test -race ./...` is part of `make check` and must stay green.
- A dial refusal is a **result, not an error** — a refusal owes the thread a comment, and an error
  return is the one shape that cannot carry one. No new sentinel error. Request-validation errors use
  the existing `invalidRequest(...)` wrapper.
- **Pattern triplication is deliberate and must be preserved.** The G4 value-shape pattern will exist
  **three times**: in `internal/dial` (the label-side gate), in `internal/report` (the render-side
  re-check), and in `internal/config` (validating `claude.models` keys). This mirrors the existing
  `profileKeyRE` / `toolProfileName` / `profileNamePattern` triple. **Each copy carries a comment
  saying why it is not imported.** A reviewer must not "fix" this into one shared regexp.
- Table-driven tests, plain `if got != want { t.Errorf }` plus `cmp.Diff` where the repo already uses
  it. Do not introduce `testify`.
- `log/slog` throughout, structured fields. **Label text is never logged.** A malformed-reason enum
  exists so the log can say *why* without carrying the text.
- No committed test may spawn a real `claude`. Every CLI fact comes from the MS-1 probe document.
- When this change makes a comment untrue, updating it is part of the change.

### Security

- **A label is a lookup key and never a value.** The dial value is only ever a map key; the string
  joined into argv comes from the Go catalog, or from a `claude.models` value that startup validation
  proved is a catalog key. The one way to break this is a "pass unrecognised model names through to
  `claude`" convenience — **forbidden**, and the reason is that the CLI would not refuse it either.
- **Labels are authenticated by nothing, and this dial's blast radius is spend.** This is the highest
  residual risk in the feature and it is accepted knowingly. Mitigation built: `claude.models` is the
  operator's model allowlist. Mitigation *not* built: there is no effort ceiling and no spend cap.
- **Refusals carry no field for label text to travel in.** The daemon-side decision struct's
  value-carrying field is **zero** on the malformed and ambiguous branches, constructed that way at
  the `return`. `Reply.Dial` is an **int** and is therefore structurally incapable of carrying label
  text. `Reply.DialValue` is read by one kind alone and re-checked in `internal/report` against its
  own copy of the shape pattern before rendering.
- **No untrusted text is echoed into a comment.** The only strings rendered are catalog constants,
  closed-set effort levels, and the init event's model — which is third-party text from `claude`, not
  from the forge, and must go through the same escaping helper the existing Model bullet uses. Do not
  invent a rawer path.
- **Every new configured string that can reach argv is added to `validateNoForbiddenFlag`**, and a
  test proves a configured value smuggling the permission-skipping flag is refused at startup.

---

## Story Index

Sub-issue links are filled in when litefactory creates the sub-issues from the story bodies written
beside this brief (`MS-1.md` … `MS-7.md`, one file per row, named for the story ID).

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| MS-1 | Behavioural probe of `--model` and `--effort` | M | — | TBD |
| MS-2 | `Key: Value` dial labels are detected, matched and refused deterministically | L | MS-1, MS-3 | TBD |
| MS-3 | Model and effort vocabularies resolve to values litefactory authored | M | MS-1 | TBD |
| MS-4 | The selected model and effort reach `claude`'s argv | M | MS-1, MS-2, MS-3 | TBD |
| MS-5 | An effort request the model cannot honour is allowed and reported | M | MS-3, MS-4 | TBD |
| MS-6 | What was requested is durable and visible after the fact | M | MS-4, MS-5 | TBD |
| MS-7 | The dials are documented where the next engineer will look | S | MS-6 | TBD |

---

## Out of Scope

| Excluded | Why |
|---|---|
| **Any non-Anthropic backend** — OpenAI, Qwen, Gemini, and "Sol"/"Qwen 3.6" as literal targets | The CLI has no non-Anthropic provider path. Reaching one needs a translating proxy with its own auth story this product deliberately does not have. The label layer nonetheless carries no Claude-only assumption, so a future harness can join. |
| **Migrating `lf:tools=` to `Tools:`** | Build the mechanism for it; do not do it now, and do not break the existing label. |
| **A store migration** | `meta.json` plus the report comment carry the selection. The ledger already rejected a `runs` column in a closely analogous case. |
| **A per-repo model or effort override** | The `claude:` block is global to the daemon; no per-repo override exists and none is invented here. |
| **A spend ceiling** | `--max-budget-usd` exists, works with `--print` (which litefactory uses), and is the natural bound if the cost exposure ever needs closing. **Recorded so it is not rediscovered** — not built. Recommended as a follow-up alongside an `effort_ceiling` key. |
| **A configured default model or effort** | It would change argv for every existing run and could not be absent. |
| **Any prompt-injection "ultrathink" layer** | Thinking is flag-controlled on 2.1.x. A prompt layer would be strictly worse. |
| **`--thinking`, `--thinking-display`, `--max-thinking-tokens`, `--fallback-model`** | `--thinking` is binary; the graded control is `--effort`. `--max-thinking-tokens` is deprecated. `--fallback-model` is a separate resilience feature. |
| **Effort values `auto` and integers** | Accepted by the binary but outside the five values `--help` documents. |
| **Any change to `RunResult.Model`'s meaning** | The closest thing to a hard invariant in this work. |
| **A per-run settings-file or environment-variable delivery channel** | Both flags go in argv. |
| **A `model_requested` column in `litefactory logs` output** | Not asked for. No CLI surface change at all. |
| **A committed test that spawns a real `claude`** | Every CLI fact comes from the MS-1 probe document. |
| **Fixing the `--allow-dangerously-skip-permissions` gap in `validateNoForbiddenFlag`** | **Found during this review, deferred deliberately.** The existing substring check does not catch the CLI's second permission-skipping variant. **This feature does not widen the exposure** — a configured model value must be a catalog key, so it can never be a free string. Fixing it changes a shipped, tested security boundary and mixes a second change with different review needs into this branch. Recommended as its own small issue; recorded in MS-7 so it is not rediscovered. |
| **`best`, `opusplan` and the `[1m]` model variants** | CLI aliases with no stable full-ID mapping — `opusplan` is a planning/execution strategy, not a model ID, and `[1m]` is a context-window modifier the full-ID namespace does not express. The pin-to-full-IDs rule cannot be honoured for them. |

---

## Assumptions

Recorded because this was a headless run and nobody could be asked. Story-local assumptions live in
their story's Architecture Notes; these are the ones binding on two or more stories.

- **AA-1 — The separator is `": "` and nothing else.** The owner wrote `Model: Opus`. Accepting `":"`
  as well would double the canonical label set for no stated benefit. Reversible: relaxing later
  turns refusals into resolutions, which is strictly widening and safe. Tightening later would not
  be.
- **AA-2 — Value matching folds case; key matching does not.** The owner did not speak to casing. The
  collision risk lives entirely on the key side, so the loud-refusal property is kept exactly there.
- **AA-3 — The value shape admits ASCII space and `.`** as well as alphanumerics, `-` and `_`, up to
  40 characters, alphanumeric first and last. Wider than the tool-profile pattern, which is
  lowercase-only and admits neither, because the dial family is explicitly built for values
  litefactory has not seen yet and version-shaped values are the obvious near-term case. Judged safe:
  the value is quoted inside a code span and the pattern excludes backticks, newlines and every
  markup character.
- **AA-4 — Unknown value on a known dial key refuses.** The owner settled only the compatibility
  case. Reversible: switching to allow-and-report later is a one-branch change.
- **AA-5 — The initial roster is four models**, with `haiku` included non-negotiably as the only
  reachable model carrying no `effort` capability — remove it and the allow-and-report ruling has
  nothing to exercise. Adding a row later costs one line.
- **AA-6 — `claude.models` absent means the built-in roster is live**, not that the feature is inert.
  The `tool_profiles` precedent (absent → inert) was considered and rejected: a fresh install where
  `Model: Opus` does nothing is a broken feature, and model choice is not a permission grant. The
  residual spend exposure is answered by the template comment, not by inertness. **This is the
  assumption most worth an owner's eye, and it is a one-line change if they rule the other way.**
- **AA-7 — `claude.models` present replaces rather than merges.** No merge semantics are defined
  anywhere in this design, and they are genuinely ambiguous (can an operator *remove* a model?).
  Replace answers both halves — extend and restrict — with one rule.
- **AA-8 — A sibling `refuseDial` rather than generalising `refuseTools`.**
- **AA-9 — `resolveTools` runs first and its refusal wins** when an issue carries both a bad tools
  label and a bad dial label. Not specified anywhere; chosen because the tools dial is the security
  boundary and its refusal must never be preempted.
- **AA-10 — Two new packages, not one.** Folding them together would put Claude model IDs inside the
  layer that is required to name no Claude concept.
- **AA-11 — `internal/report` asks `internal/catalog` for applicability**, rather than `internal/turn`
  computing a boolean upstream. Follows the existing `notGranted` precedent: a requested-vs-resolved
  comparison is a rendering-time fact derived from fields the render input already carries.
- **AA-12 — MS-3 ships before MS-2**, reordering the spec's listing. Legal because the spec has both
  depending on MS-1 only, and required because `resolveDials` needs the vocabulary to look values up
  in.
- **AA-13 — No separate product-owner decomposition pass was run.** The architect's review produced
  the per-story task tables directly, and the approved spec already carried the acceptance criteria,
  which the run instruction said to refine rather than reinvent. A second pass would have risked
  contradicting the architect for no added coverage.

---

## Provenance

- **Citation base: `f2b8900`.** Every `path:line` in this brief and in the seven story bodies was
  opened against that commit during the architect review on 2026-09-01.
- **Known-stale areas.** The two existing external-behaviour probe documents describe `claude`
  2.1.246 and 2.1.247; the installed binary is **2.1.251**. Their claims are inherited nowhere in
  this plan — MS-1 re-establishes everything this feature depends on, and MS-7 records that they are
  version-behind.
- **The effort-capability matrix is a snapshot of the 2.1.251 catalog** and will drift as the CLI
  ships new models. It is pinned in Go precisely so litefactory owns keeping it correct; a model the
  catalog does not know cannot be configured, which is what removes the "capability unknown" state.
- **Re-pinning before merge.** Re-verify the line numbers cited in the Cross-Story Constraints test
  table and in each story's tasks if the branch is rebased onto a moved `main`. Re-run the MS-1
  probes if the installed `claude` version changes between the probe and the merge, and update the
  version named in the probe document and in `internal/catalog`'s doc.
- **Evidence tags** carried through from the research brief: `[V]` verified in-repo, `[Vb]` extracted
  from the CLI binary, `[W]` inference, `[O]` owner opinion. Everything in the Shared Decisions above
  is `[V]` or an architect decision; nothing rests on a `[W]` that MS-1 does not re-establish.
