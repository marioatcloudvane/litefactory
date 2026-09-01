# Architect Review — Model switching (issue #9)
# Architect: go-architect
# Date: 2026-09-01
# Branch: lf/9-model-switching
# Input: feature-spec.md (APPROVED), RESEARCH_BRIEF.md, owner rulings D-MS-1..D-MS-7
# Headless run. Nobody could be asked. Every gap is decided and the reasoning recorded in §7.

Read against the real tree, not the brief's summary of it. Every `path:line` below was opened this
run.

---

## 1. Decisions

### O-1 — The `Key: Value` matching rule

**Chosen: (iii) Hybrid, with a strict separator.** Detect the key case- and whitespace-insensitively;
accept the key only byte-exact; accept exactly one canonical separator; shape-check then case-fold the
value.

#### The rule, stated normatively

A GitHub label is put through four gates in this order. The gates are total: every string reaches
exactly one of *ignored*, *resolved*, *refused-malformed*, *refused-unknown*, *refused-ambiguous*.

**G1 — Detection (lenient).** Split the label at the **first** `:`. Let `rawKey` be everything
before it and `rawValue` everything after. Trim ASCII space and tab from both ends of `rawKey`. The
label is *detected as a dial label* iff `strings.EqualFold(trimmedRawKey, k)` holds for one `k` in the
**closed** dial-key set `{"Model", "Thinking"}`. A label with no `:` is not detected. A label whose
folded key is not a dial key is **ignored in silence** — no reply, no log, run proceeds. This is
AC-MS-2.1: `priority:high` folds to `priority`, which is not a dial key, under every casing.

G1 is the direct transplant of `internal/daemon/tools.go:130-139` — `strings.EqualFold` on the
prefix, nothing else — and it exists for the reason `tools.go:14-23` gives: GitHub preserves label
case, so a case-sensitive scan would not *see* the mis-typed label, and the run would proceed having
silently ignored the request.

**G2 — Ambiguity, counted before anything is judged, per dial.** Group the detected labels by the
dial key they folded to. If any one dial key was named by **two or more** labels, in any casings and
whatever their shape, that dial is **refused-ambiguous**. One is never picked and two are never
unioned. This is `tools.go:141-163` transplanted with its reasoning intact, and the *per-dial*
grouping is what makes AC-MS-2.5 true: a `Model:` label beside a `Thinking:` label is two dials, not
an ambiguity.

**G3 — Key and separator acceptance (strict).** For a dial named by exactly one label, the label must
begin with the canonical key **byte for byte** (`Model` or `Thinking`) followed by the canonical
separator **`": "` — exactly one colon and exactly one ASCII space**. Anything else is
**refused-malformed**. This is the accept-strictly half of D-6, and it is the only reason G1 is
lenient at all.

**G4 — Value.** The remainder after `": "` is taken byte-for-byte with no trimming and put through a
**shape check** before it is used as anything:

> the value must be 1–40 characters, must begin and end with an ASCII alphanumeric, and may contain
> only ASCII alphanumerics, `.`, `_`, `-` and space.

A value that fails the shape check is **refused-malformed** and is never used as a map key, never
logged, and never rendered. A value that passes is lower-cased (ASCII only — the shape check already
excludes everything else) and looked up in the dial's vocabulary. A hit **resolves**. A miss is
**refused-unknown**, and that refusal is the one allowed to name what it was asked for, exactly as
`ReplyUnknownToolProfile` is today.

The shape check is the direct analogue of `toolProfileName` (`tools.go:40`) and carries the same job
description from `tools.go:31-39`: it is deciding whether a string that arrived from the forge is even
the *shape* of a key, before it is used as one and long before `internal/report` is asked to print it
back.

#### Dispositions — total over AC-MS-2.2's input set and beyond

| Label | Detected? | Disposition | Gate that decided it |
|---|---|---|---|
| `Model: Opus` | yes | **resolves** | G3 key+separator exact, G4 shape ok, `opus` in vocabulary |
| `model:opus` | yes | **refused-malformed** | G3 — key `model` is not `Model` byte-for-byte |
| `Model:␣␣Opus` | yes | **refused-malformed** | G3 — separator is `":  "`, not `": "` |
| `MODEL:OPUS` | yes | **refused-malformed** | G3 — key `MODEL` is not `Model` byte-for-byte |
| `Model:Opus` | yes | **refused-malformed** | G3 — separator is `":"`, not `": "` |
| `priority:high`, any casing | **no** | **ignored in silence** | G1 — `priority` is not a dial key (AC-MS-2.1) |
| `Thinking: High` | yes | **resolves** | canonical (AC-MS-2.3) |
| `Thinking: high` / `Thinking: HIGH` / `Thinking: XHigh` | yes | **resolves** | G4 folds the value |
| `Model: Sol` | yes | **refused-unknown** | G4 — shape ok, not in the vocabulary |
| `Model:` | yes | **refused-malformed** | G3 — separator is `":"` |
| `Model: ` (trailing nothing) | yes | **refused-malformed** | G4 — empty value fails the shape check |
| `Model: a:b`, `Model: <b>x</b>`, 41+ chars | yes | **refused-malformed** | G4 — shape check |
| `␣Model: Opus` (leading space in the label name) | yes | **refused-malformed** | G3 — `rawKey` is `␣Model` |
| two `Model:` labels, any casings | yes | **refused-ambiguous** | G2 (AC-MS-2.4) |
| one `Model:` + one `Thinking:` | yes | **both resolve** | G2 groups per dial (AC-MS-2.5) |

Only `Model: Opus` resolves out of AC-MS-2.2's five. That is intentional and it is what AC-MS-2.2
anticipates: it demands a *defined, tested* disposition from `{resolves, refused-malformed}` for each
of the five, and AC-MS-2.3 pins only the canonical one as resolving. Four loud refusals and one
resolution is D-6 transplanted without dilution: `LF:Tools=research` is malformed *today*, for exactly
this reason, and nobody is left thinking their label worked.

#### The three sub-questions, settled explicitly

**Separator: `": "` only.** Not `":"`, not "either". Reasons: (a) it is the form the owner wrote and
the form AC-MS-2.3 pins; (b) "either" produces two canonical strings per dial value, which doubles the
label list in the GitHub UI and destroys the one tidiness argument option (i) had; (c) a strict
separator *narrows* the collision surface — a repo's own `Model:v2` is refused rather than parsed,
and a repo's own `Models: x` or `model_type: x` is never detected at all; (d) it makes G3 one rule
("the label starts with `Model: `") instead of two.

**Unknown value on a known dial key: refuses.** A-S3 is confirmed. Reasoning: F-5 — `--model` has no
parse-time validation and passes unknown strings through, and `--effort` warns on stderr and
continues. If litefactory does not refuse, **nobody does**, and the run silently uses a different
model than was asked for. That is precisely the "my label did nothing" outcome `tools.go:14-23` names
as the one this whole label mechanism exists to prevent. This is a *different* case from the one the
owner settled: D-MS-3's allow-and-report covers a request where both halves are individually
well-formed and litefactory can state exactly what happened; an unknown value is a request litefactory
cannot even name. Counter-argument recorded, not accepted: a model is not a permission grant, so
refusing is harsher than the tools case strictly requires — mitigated by the refusal being loud,
immediate and one click to fix.

**Dial key set: closed in Go.** Not config-extensible. Reasons: (a) a closed set bounds the collision
surface, and the collision surface *is* D-MS-2(a); an operator-extensible key set lets an operator add
a key that collides with their own repo's labels and hijacks runs; (b) the dial keys are litefactory's
own vocabulary, exactly as `lf:tools=` is one constant, not operator content; (c) D-MS-4 ("many
harnesses coming") is served by making a new dial **one table row in Go**, not by making the key set
editable — a new harness needs a new argv builder in a new litefactory release regardless, so a
config-only dial key would have nothing to bind to. The asymmetry with O-2 is deliberate and is the
`tool_profiles` shape exactly: **the mechanism is Go, the content is partly the operator's**.

#### Why not (i) or (ii)

**(i) byte-exact canonical** loses on the value side for no gain. Refusing `Thinking: high` is
pedantry: the value is not a namespace, it is a closed vocabulary, and C2 is preserved either way
because the folded value is a *lookup key* and the string that reaches argv comes from the table.

**(ii) fully normalising** is rejected outright and would be rejected even if it were the only option
left. It is **fail-open on the collision case**: a repo whose own label normalises to `model` does not
draw a refusal, it *hijacks the run*. Labels are authenticated by nothing (C3) and this dial's blast
radius is spend. Fail-open is the wrong direction on both counts, and it destroys the near-miss
property — there is no near-miss left to refuse.

---

### O-2 — Where the model table and the capability matrix live

**Chosen: Hybrid, split three ways.** The split is finer than the spec's table and each piece is
decided on its own evidence.

| Thing | Where | Why |
|---|---|---|
| **Effort-capability matrix** (which model IDs carry `effort`) | **Go, not configurable at all** | F-3: this is knowledge about *models*, not about this operator's preferences. An operator hand-maintaining it in YAML will get it wrong silently and produce exactly the "my label did nothing" half-failure this feature exists to prevent. There is no configuration case for it. |
| **Effort level vocabulary** (`low medium high xhigh max`) | **Go, closed** | AC-MS-3.4 fixes it to the five values `--help` documents on 2.1.251. `auto` and integers are explicitly out of scope. Nothing here is an operator preference. |
| **Model catalog** (which full model IDs exist) | **Go, closed** | It is the same knowledge as the capability matrix, keyed the same way. Keeping them one table means they cannot drift. |
| **Dial-value → full-model-ID roster** (what `Model: Opus` means, and which models are reachable) | **Go built-in, overridable by one optional config key** | R-11: labels are authenticated by nothing and the blast radius is spend, so a config table *is* the natural allowlist. Direct precedent: `tool_ceiling` defaults to a copy of `tools` when absent (`internal/config/config.go:163`, resolved at `config.go:281`). |

#### The exact config surface — one key

```yaml
claude:
  models:            # optional; absent on a fresh install
    opus:   claude-opus-5
    sonnet: claude-sonnet-5
```

- Go field: `Claude.Models map[string]string`, YAML tag `models`.
- **Absent means the built-in roster**, resolved once in `Load` by a `resolveModels()` sitting beside
  `cfg.Claude.resolveToolCeiling()` at `internal/config/config.go:281` — after decode, before
  `Validate()`. This is the `tool_ceiling` groove, applied faithfully.
- **Present replaces the built-in roster entirely.** Not merged. Merge semantics are the extra cost
  the spec's O-2 table flags, and they are genuinely ambiguous (can an operator *remove* `haiku`?).
  Replace-semantics answers both halves of R-11 — extend and restrict — with one rule: the map you
  wrote is the map that exists.
- **Every configured value must be a key of the Go catalog**, checked at startup. This is the load-
  bearing constraint: it makes the catalog the closed model universe, makes `models:` a pure allowlist
  with naming freedom over it, and **removes the "effort capability unknown" third state entirely**,
  which AC-MS-5.2/5.3's binary wording has no room for. Cost: reaching a model litefactory has never
  heard of needs a release. That is the right trade — F-3 says the matrix is knowledge litefactory
  owns, and a model with unknown effort capability produces the exact silent half-failure at issue.
- Keys are validated against the same value-shape pattern G4 uses, so a configured key is always a key
  a label can actually name.

**No other config key.** No `claude.efforts`, no `claude.model_capabilities`, no `claude.default_model`
(AC-MS-4.2 forbids the last one and R-2 explains why).

#### AC-MS-3.6 and AC-MS-3.7 are satisfiable — confirmed against the tree

**AC-MS-3.6.** `validateNoForbiddenFlag(c *Claude)` at `internal/config/policy.go:155-184` enumerates
by hand every config string that reaches argv. It gains a loop over `c.Models` checking **both** the
key (as `claude.models` key text) and the value (as `claude.models[%q]`). The value check is
belt-and-braces given the catalog constraint above — a forbidden flag could never survive
`validateModels` — and belt-and-braces is precisely what that function's discipline asks for. Two
lines. The startup-refusal test AC-MS-3.6 demands writes a config with a smuggled flag in `models:` and
asserts `Load` fails.

**AC-MS-3.7.** Verified against `internal/config/template_test.go:19-27`: `TestDefaultFileMatchesDefaults`
does `cmp.Diff(Defaults(), *Decode(DefaultFileContents()))` — it goes through **`Decode`, not `Load`**,
so the `resolveModels()` default never runs on that path. Therefore: `Defaults()` leaves `Models` nil,
the template carries `models:` as a **commented example only**, `Decode` of the template yields nil,
the diff is empty, and the test stays green **unmodified**. Exactly the `tool_ceiling` / `tool_profiles`
precedent. Note `internal/config/config.go:295` sets `dec.KnownFields(true)`, so the struct field must
exist for any real config to decode — adding it is required, not optional.

#### The initial roster (A-S8 settled)

| Dial value | Canonical label | Full model ID | Carries `effort`? |
|---|---|---|---|
| `opus` | `Model: Opus` | `claude-opus-5` | yes |
| `sonnet` | `Model: Sonnet` | `claude-sonnet-5` | yes |
| `haiku` | `Model: Haiku` | `claude-haiku-4-5` | **no** |
| `fable` | `Model: Fable` | `claude-fable-5` | yes |

Four rows, pinned to full IDs per AC-MS-3.2/R-10 (alias resolution is provider-dependent: `opus` is
`claude-opus-5` first-party but `claude-opus-4-7` via gateway).

`haiku` is in the roster **deliberately and non-negotiably**: it is the only no-effort model in the
roster, so it is the sole reachable path to AC-MS-5.1's `Model: Haiku` + `Thinking: High` case. Remove
it and D-MS-3's allow-and-report ruling has nothing to exercise.

Excluded from the initial roster, recorded so it is not rediscovered: `best` and `opusplan` are CLI
*aliases* with no stable full-ID mapping (`opusplan` is a planning/execution strategy, not a model ID,
so R-10's "pin to full IDs" cannot be honoured for it), and the `[1m]` variants are context-window
modifiers the full-ID namespace does not express. `mythos` and the older Opus/Sonnet generations are in
the catalog (so an operator can reach them via `models:`) but not in the default roster.

Effort dial values: `low`, `medium`, `high`, `xhigh`, `max`. Canonical display forms for documentation
are `Thinking: Low` … `Thinking: XHigh`, `Thinking: Max`; because G4 folds the value, any casing
resolves, so the display form is a documentation choice only.

Capability matrix content: exactly AC-MS-3.5's two lists — 7 carrying `effort`, 10 not, 17 total,
matching the 2.1.251 catalog.

---

### O-3 — Shared dial resolver vs. a third copy of `resolveTools`

**Chosen: a shared `Key: Value` dial resolver, new-dials-only. `internal/daemon/tools.go` and every
`lf:tools=` path are not touched.**

**Chosen: 3 new `report.ReplyKind`s, dial-agnostic, carrying an integer dial identifier.**

#### Where the code lives — two new leaf packages

**`internal/dial` — the grammar, and nothing else.**
Owns G1–G4's *shape* rules, the closed dial-key set, and the count-before-judge ambiguity rule. Exports:

- `type Key int` with `KeyModel`, `KeyThinking`, a `String()` for logs and a `Label() string` returning
  the canonical key literal (`"Model"`, `"Thinking"`).
- `type Outcome int` — `OutcomeResolved`, `OutcomeMalformed`, `OutcomeAmbiguous`.
- `type Result struct { Key Key; Outcome Outcome; Value string; Reason MalformedReason }` where `Value`
  is set on `OutcomeResolved` **only** and `Reason` is a litefactory-authored enum for the log line.
- `func Resolve(labels []string) []Result`.

It does **not** do vocabulary lookup. It answers "which dial keys did these labels name, is each one
well-formed, is each one unambiguous, and what value did it carry" — and stops. That split is what makes
AC-MS-3.8 structurally true: this package names no Claude concept in any type or field, so a future
harness's dial joins it by adding one `Key` constant.

It imports **stdlib only** — no litefactory package. That is what lets `internal/report` import it
without a cycle.

**`internal/catalog` — the Claude model catalog and the effort vocabulary.**
Owns the 17 model IDs and their `effort` capability, the five effort levels, and `BuiltinModels()`.
Exports the lookups `internal/config` (validation), `internal/daemon` (resolution) and `internal/report`
(the MS-5 applicability sentence) all need. It imports nothing internal either.

Two packages rather than one because the Claude-specificity has to sit somewhere that is *not* the
harness-agnostic grammar layer (D-MS-4, AC-MS-3.8). Both are `internal/`, both are leaves, neither
declares an interface.

**`internal/daemon/dials.go` — the join.** `(*Daemon).resolveDials(labels []string) dialDecision` calls
`dial.Resolve`, then does the vocabulary lookups against `d.models` (config-derived) and
`catalog.Effort`, and produces the decision the tick acts on. This is the direct sibling of
`resolveTools`, and it stays a method on `*Daemon` for the same reason: it reads daemon-held config.

#### Why shared and not copied

- **D-MS-4 is the owner's stated reason for the whole `Key: Value` scheme.** Adding harness N+1 must
  cost one vocabulary entry. A copy-per-dial design pays the redesign at copy three or four, which is
  the outcome the ruling exists to avoid.
- **The dilution objection does not apply here.** The spec's O-3 table warns that a generalisation
  dilutes `internal/daemon/tools.go`'s heavily-reasoned prose. It cannot, because `tools.go` is not
  touched: its prose stays where it is, and `internal/dial/doc.go` writes new prose about the new
  grammar (AC-MS-7.3). The one edit to `tools.go` is amending line 10's "the whole of the label
  vocabulary", which AC-MS-7.5 requires anyway.
- **The risk objection is answered by scope.** The shared resolver is new-dials-only. It never handles
  `lf:tools=`; the grammar mismatch the O-3 table worries about is simply not taken on. D-MS-6 is
  honoured exactly: the mechanism exists for `Tools:` to migrate onto later, and nothing migrates now.
  AC-MS-2.9 becomes trivially true and trivially reviewable — `git diff internal/daemon/tools.go`
  shows one comment change.

#### The ReplyKinds — exact count and names

**Three new kinds**, added to `internal/report/reply.go`:

| Kind | Answers | Names what it was asked for? |
|---|---|---|
| `ReplyUnknownDialValue` | a well-formed dial label whose value is not in that dial's vocabulary | **yes** — the dial (as an int) and the value (re-checked) |
| `ReplyMalformedDialLabel` | a label detected as a dial but failing G3 or G4 | the dial only |
| `ReplyAmbiguousDialLabels` | two or more labels naming one dial | the dial only |

Not six. Collapsing from six to three is what makes harness N+1 cost zero new kinds, and it is the whole
practical payoff of the shared resolver.

**`report.Reply` gains exactly two fields:**

- `Dial dial.Key` — an **integer**, read by all three new kinds. Because it is an int, it is
  *structurally* incapable of carrying label text. `internal/report` owns the rendered literal for each
  `Key` and an out-of-range value falls back to wording that names no dial, which is the `profileSpan`
  discipline (`reply.go:170-175`) applied to a second field.
- `DialValue string` — read by `ReplyUnknownDialValue` **alone**, re-checked in `internal/report`
  against its own copy of the G4 shape pattern by a `dialValueSpan` before rendering. Empty or failing
  produces a reply that quotes nothing. This mirrors `Reply.Profile` (`reply.go:140-148`) exactly.

**AC-MS-2.7 stays true, and at the layer where it is true today.** Read `tools.go:60-64` carefully: the
"no field for the text to travel in" mechanism lives on the **daemon-side decision struct**, not on
`report.Reply` — `report.Reply` does carry `Profile` for every kind and relies on the producer leaving
it empty plus the renderer re-checking it. So the binding requirement is: **`dialDecision`'s
value-carrying field is zero on the malformed and ambiguous branches**, constructed that way in the two
`return` statements, exactly as `toolDecision{Refusal: report.ReplyMalformedToolLabel}` is today. The
dial identifier is not label text and is not a string; it cannot violate S-9 by construction.

#### `replyKind()` — the spec's premise is narrower than stated

Verified in the tree: **tool refusals do not go through `replyKind()`.** `internal/turn/report.go:612-625`
maps `CommandKind` → `ReplyKind` for *command* replies. A refusal sets `ReplyRequest.Refusal` directly
(`internal/daemon/tick.go:1519`) and `ReplyRequest.kind()` (`report.go:239-245`) branches on that first.
So a dial refusal needs **no case in `replyKind()`**.

What it does need is the guard those two call sites use. `report.ReplyKind.RefusesTools()`
(`reply.go:114`) has exactly **two callers**, both in `internal/turn/report.go` — line 242 (`kind()`)
and line 256 (`validate()`). Decision:

- Add `func (k ReplyKind) RefusesLabel() bool` covering all six label-drawn kinds.
- Switch those two call sites to it.
- Leave `RefusesTools()` in place and correct (still exactly the three tools kinds). If nothing needs
  the tools-only distinction after the switch, delete it in the same change rather than leaving a
  method whose name is a lie (AC-MS-7.5).

That is the **entire** touch to shipped `internal/report` / `internal/turn` refusal machinery: one new
method, two call-site edits, three new enum values with their `String()` and `valid()` cases.

#### The refusal path in the daemon: a sibling, not a generalisation

`refuseTools` (`tick.go:1487-1533`) gets a **sibling** `refuseDial`, not a rename or a generalisation.
It duplicates ~15 lines including the comment-id-0 branch (AC-MS-2.10) and cross-references
`refuseTools`'s prose rather than restating it. Trade recorded: ~15 duplicated lines bought against
leaving the shipped tools refusal path byte-untouched, which is what makes AC-MS-2.9 reviewable at a
glance. D-MS-6 ("do not break it") and R-9 ("the triplication is deliberate") both point this way.

**Order in `Daemon.run`: `resolveTools` first, unchanged, then `resolveDials`.** If an issue carries
both a bad `lf:tools=` label and a bad `Model:` label, the tools refusal wins. Reason: the tools dial is
the security boundary and its refusal must never be preempted; and running the existing check first
keeps every existing test's behaviour byte-identical.

---

## 2. System overview

### The dial family, end to end

```
GitHub issue labels  (t.issue.Labels — the tick's snapshot, no extra API call)
        │
        ▼
internal/daemon/tick.go  Daemon.run
        ├─ resolveTools(labels)   ── unchanged, first, wins ties ──► refuseTools ──► report.Reply
        │
        └─ resolveDials(labels)  [internal/daemon/dials.go]
                 │  grammar ─────────────────► internal/dial.Resolve   (leaf, stdlib only)
                 │  vocabulary ──────────────► d.models  +  internal/catalog
                 │
                 ├─ refused ──► refuseDial ──► turn.ReplyRequest{Refusal, Dial, DialValue}
                 │                                    └─► internal/report.Reply  (renders, re-checks)
                 │
                 └─ resolved ──► turn.Request{ModelRequested, EffortRequested}
                                        │
                                        ▼
                          internal/turn/prepare.go  ──► harness.Request{ModelRequested, EffortRequested}
                                        │                        │
                                        │                        ├─► runner.args()  ──► --model / --effort
                                        │                        └─► harness.NewMeta ──► NNNN.meta.json
                                        ▼
                          internal/turn/execute.go  ──► turn.Result{ModelRequested, EffortRequested}
                                        │
                                        ▼
                          internal/turn/report.go   ──► report.Run{ModelRequested, EffortRequested}
                                        │
                                        ▼
                          internal/report/run.go    ──► the Model bullet (unchanged)
                                                        the Tools bullet (unchanged, still immediately after)
                                                        the new provenance line (after both, in secFixed)
                                                        the new effort-applicability line
```

### New packages

| Package | Responsibility | Imports |
|---|---|---|
| `internal/dial` | The `Key: Value` grammar: detection, key/separator acceptance, value shape, per-dial ambiguity. Harness-agnostic; names no Claude concept. | stdlib only |
| `internal/catalog` | The `claude` model catalog as of 2.1.251 — which full model IDs exist, which carry the `effort` capability — plus the five effort levels and the built-in dial roster. | stdlib only |

Both are leaves. Nothing new goes in `pkg/`. No new interface is declared anywhere in this feature.

### New types and fields, by package

| Package | Addition |
|---|---|
| `internal/dial` | `Key`, `Outcome`, `MalformedReason`, `Result`, `Resolve()` |
| `internal/catalog` | model catalog table, `EffortApplicable(modelID) bool`, `Effort(value) (string, bool)`, `BuiltinModels() map[string]string`, `KnownModel(id) bool` |
| `internal/config` | `Claude.Models map[string]string`; `resolveModels()`; `validateModels()`; two lines in `validateNoForbiddenFlag` |
| `internal/daemon` | `Options.Models`, `Daemon.models`; `dialDecision`; `resolveDials`; `refuseDial` |
| `internal/report` | 3 `ReplyKind`s; `Reply.Dial`, `Reply.DialValue`; `dialValueSpan`; `RefusesLabel()`; `Run.ModelRequested`, `Run.EffortRequested`; `effortEntry`, `modelProvenanceEntry` |
| `internal/turn` | `ReplyRequest.Dial`, `.DialValue`; `Request.ModelRequested`, `.EffortRequested`; `Result.ModelRequested`, `.EffortRequested` |
| `internal/harness` | `Request.ModelRequested`, `.EffortRequested`; `Meta.ModelRequested`, `.EffortRequested`; two conditional appends in `args()` |

### Data flow facts worth stating once

- **The dials are resolved exactly once per tick**, from `t.issue.Labels` — the snapshot already on the
  payload (`internal/github/issues.go:67-77`). No extra API call. Two comments drained in one tick share
  one snapshot. A bare relabel with no following comment starts nothing. This is not new behaviour to
  design; it falls out of putting `resolveDials` beside `resolveTools` at `tick.go:1382` (AC-MS-2.8).
- **The label value never becomes an argv string.** The folded value is a map key; the string joined
  into argv is the map's *value*, which is a `catalog` constant (or a `claude.models` value that startup
  validation proved is a catalog key). C2 holds all the way to `exec.CommandContext`.
- **`RunResult.Model` is not touched.** It remains strictly the init event's value. The requested values
  travel in separate fields at every layer.

### Shared state and side effects

- `Daemon.models` is read concurrently by parallel turn workers. It must be `maps.Clone`d in `New`
  and never written after, exactly as `toolProfiles` is (`internal/daemon/daemon.go:314`).
- No goroutine is started by this feature. No channel. No mutex. The concurrency surface is entirely
  "a map read by N workers, cloned at construction and immutable thereafter".
- No database write. No migration. No new table, no new column (AC-MS-6.2).
- One new file per run: nothing — the two new `meta.json` keys go in the existing document.

---

## 3. Shared decisions binding on two or more stories

These feed `plan_brief.md`. They are the things an engineer must not re-decide.

**Naming and literals**

1. Canonical dial keys are the byte strings `Model` and `Thinking`. Canonical separator is `": "`.
2. Internal identifiers use **`effort`**, never `thinking` (A-S1): the flag is `--effort`; `--thinking`
   is a different, binary flag this design does not use. The *label key* stays `Thinking` because that
   is the owner's word. Both facts belong in `internal/dial`'s doc.
3. Request-side field names are `ModelRequested` and `EffortRequested`, spelled identically on
   `harness.Request`, `turn.Request`, `turn.Result`, `report.Run` and `harness.Meta`. Resolved-side stays
   `Model` on `harness.RunResult` and `harness.Meta`. AC-MS-4.6 is satisfied by the suffix being present
   on every requested field and absent from every resolved one — no other distinguishing scheme.
4. `meta.json` keys are `model_requested` and `effort_requested`, beside the existing `model` —
   mirroring `tools_requested` beside `tools` (`internal/harness/meta.go:66-73`).
5. New `ReplyKind`s: `ReplyUnknownDialValue`, `ReplyMalformedDialLabel`, `ReplyAmbiguousDialLabels`.
   Their `String()` forms are `unknown_dial_value`, `malformed_dial_label`, `ambiguous_dial_labels`.
6. The one new config key is `claude.models`. There is no other.

**Pinned model IDs** (MS-3 authors them, MS-4/MS-5/MS-6 consume them)

`opus → claude-opus-5`, `sonnet → claude-sonnet-5`, `haiku → claude-haiku-4-5`,
`fable → claude-fable-5`. Effort levels: `low medium high xhigh max`.
Capability matrix: AC-MS-3.5's lists verbatim (7 with, 10 without).

**Package-placement rules**

7. Grammar goes in `internal/dial`. Claude knowledge goes in `internal/catalog`. Neither may import any
   other litefactory package, ever — that constraint is what lets `internal/report` import them.
8. The daemon does the vocabulary lookup, not `internal/dial`. `internal/dial` has no opinion about what
   a value means.
9. Nothing goes in `pkg/`. No new interface is declared. No new third-party dependency
   (`internal/config/purity_test.go` forbids one in `internal/config` outright).

**Behavioural rules**

10. `resolveTools` runs first and its refusal wins. `internal/daemon/tools.go` is edited for one comment
    and nothing else.
11. Ambiguity is counted **per dial** and **before** anything is judged.
12. A key that does not fold to a dial key is ignored in silence. A key that does fold but fails
    acceptance is refused loudly. There is no third answer.
13. No configured default model and no configured default effort exist, at any layer (AC-MS-4.2).
14. Model and effort are carried as **strings**, never slices — which is why AC-MS-4.7's slice-copying
    rule barely bites (see §4).

---

## 4. Cross-story constraints

### Test invariants that must stay green, unmodified

| Test | File | What breaks it |
|---|---|---|
| `TestRunWithNoProfileArgvIsByteIdentical` | `internal/harness/runner_test.go:1200-1218` | Emitting either flag unconditionally, or emitting it empty. Both must be **omitted entirely** when unselected — the `--allowedTools` shape at `runner.go:488-493`. |
| `TestRunResumeSwapsOnlyTheSessionFlag` | `internal/harness/runner_test.go:1021-1048` | Building the flags differently on the resume path. It is *extended* (AC-MS-4.4), never relaxed. Note it also pins `PathColdAfterResumeRefused` argv equal to cold argv. |
| `TestToolsBulletFollowsModelImmediately` | `internal/report/report_test.go:1119-1145` | Any new bullet between `Model` and `Tools`. New lines go **after** the Tools bullet. |
| Tools-bullet truncation survival | `internal/report/report_test.go:1153-1165` | A provenance line outside `secFixed` (index 0, `run.go:232-238`, "never cut"). AC-MS-6.5. |
| `TestRelabelThenComment` | `internal/daemon/tools_test.go:84-112` | Resolving dials anywhere but once, in `Daemon.run`, from the tick's snapshot. |
| `TestDefaultFileMatchesDefaults` | `internal/config/template_test.go:19-27` | Putting `Models` in `Defaults()` or an uncommented `models:` in the template. |
| `TestForbiddenFlagIsAbsentFromTheTree` | `internal/config/purity_test.go:82+` | Writing the permission-skipping flag as a literal anywhere — including in the two new packages, which the walk covers. |
| Every `lf:tools=` test | `internal/daemon/tools_test.go`, `tools_internal_test.go` | AC-MS-2.9: all green, none edited. |

### Ring-fenced: `RunResult.Model` and `resumeRefused`

`internal/harness/runner.go:715-729`. `resumeRefused` uses `result.Model == ""` as the structural test
for "no init event arrived". The tempting shortcut — prefilling `RunResult.Model` from the request so the
report always has a model to show — makes `ErrSessionNotResumable` stop being returned, kills the
cold-rerun at `internal/turn/harvest.go:88-96`, and **wedges the task permanently**: every run asks to
resume, is refused, and reports the same failure forever. This is the single most dangerous edit in the
feature. It is ring-fenced: `harness.RunResult` gains **no field** in this work, and AC-MS-4.5's test is
mandatory, not optional.

### Platform constraints (from `.claude/skills/go/platform-constraints.md`)

- **Project layout** — applies. Two new top-level packages, both correctly under `internal/`. No `pkg/`
  promotion; there is no external consumer. No shared-across-`cmd/`-binaries concern.
- **Error handling** — applies narrowly. A dial refusal is a **result, not an error**, following
  `refuseTools`'s stated reasoning (`tick.go:1469-1471`): a refusal owes the thread a comment, and an
  error return is the one shape that cannot carry one. No new sentinel error is warranted. Where
  `harness.Request.validate` gains checks, they use `invalidRequest(...)`, which wraps
  `ErrInvalidRequest` — follow the existing groove, do not invent a second one. Config validation
  returns `[]error` and joins, unchanged.
- **Concurrency** — applies, weakly, and that is worth saying out loud. **No goroutine, channel, mutex
  or `context` change is introduced.** The one shared-state item is `Daemon.models`: `maps.Clone` at
  construction, read-only thereafter, exactly as `toolProfiles` is. `go test -race ./...` is part of
  `make check` (AC-MS-7.6) and must stay green.
- **Interfaces** — applies as a prohibition. Do **not** introduce an interface for the catalog or the
  resolver. One concrete implementation, no test fake needing substitution: premature abstraction.
- **Dependency management** — applies as a prohibition. No new third-party package. `internal/config`
  may import exactly one third-party path, in one file, for decoding only
  (`internal/config/purity_test.go`), and this feature does not change that.
- **HTTP / API layer** — not applicable (no listener).
- **Database access** — not applicable, **and that is a decision**: AC-MS-6.2 forbids a migration, with
  the ledger precedent at `.aiteam/AC_AMENDMENTS.md:1286-1312`. Reviewers should expect the absence.
- **Testing conventions** — applies strongly. AC-MS-2.2's disposition set and AC-MS-5.6's capability
  matrix are both table-driven or they are wrong. The repo uses plain `if got != want { t.Errorf }` plus
  `cmp.Diff` in `internal/config`; do not introduce `testify`. R-13: no committed test spawns a real
  `claude` — the harness tests use a script fixture (`successScript(t)`), which is the existing groove
  and is where every argv test belongs.
- **Logging** — applies. `log/slog` throughout, structured fields, never `fmt.Println`. And: **label
  text is never logged**, which `tick.go:1495-1503` establishes explicitly for the tools path. The
  malformed-reason enum exists so the log can say *why* without carrying the text.
- **Linting** — applies. Two new packages are new lint surface under the pinned `.golangci.yml`. Run
  `make check` before any task is called done.

### House rules this feature collides with

- **Copy slices at struct boundaries (AC-MS-4.7, `APP_CONTEXT.md:329-337`).** Model and effort are
  carried as **strings**, so the `append`-into-spare-capacity hazard does not arise for them, and the
  cold-rerun in-place mutation at `internal/turn/harvest.go:130-133` is safe for them by construction.
  Where the rule *does* bite: `Claude.Models` and `Daemon.models` are maps and must be `maps.Clone`d at
  every ownership boundary. State the string-ness in the field doc comments so the next reader knows
  the rule was considered rather than forgotten.
- **Pattern triplication is deliberate (`internal/report/reply.go:151-161`).** The G4 value-shape
  pattern will exist **three times**: in `internal/dial` (the label-side gate), in `internal/report` (the
  render-side re-check, for the reason reply.go states verbatim — "a rule that is only enforced by
  whoever remembers to enforce it upstream is not a rule"), and in `internal/config` (validating
  `claude.models` keys). That mirrors the existing `profileKeyRE` / `toolProfileName` /
  `profileNamePattern` triple exactly. **Each copy carries a comment saying why it is not imported.** A
  reviewer must not "fix" this into one shared regexp.
- **Comments that this change makes untrue are updated as part of the change**
  (`APP_CONTEXT.md:326-328`). Named collisions: `internal/daemon/tools.go:10` ("the whole of the label
  vocabulary") and `report.ReplyKind.RefusesTools`'s doc if it survives.

### Security constraints, forward-looking

Read against `.claude/skills/shared/security-scanner.md` in forward-looking mode. This is what the
*plan* carries, not an audit of what exists.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation (C2) | ✅ by construction | The dial value is only ever a lookup key. The string joined into argv comes from `internal/catalog` or from a `claude.models` value that startup validation proved is a catalog key. **The one way to break this is a "pass unrecognised model names through to `claude`" convenience — forbidden, and F-5 is why: the CLI would not refuse it either.** |
| 2 | Input validation (shape) | ✅ | G4 gates the value before it is used as a map key and long before it is rendered; `internal/report` re-checks. Two independent gates, matching `Profile`. |
| 3 | Authorisation (C3) | ⚠️ **highest residual risk** | Labels are authenticated by nothing. Anyone with triage permission on a watched repo can put `Model: Opus` + `Thinking: Max` on an issue. The blast radius is **spend**: `max` effort on Opus 5 is ~1.7× `high` on the effort axis alone, and model choice moves cost by a larger factor again. **This feature ships a spend amplifier with no ceiling.** Mitigation built: `claude.models` is the allowlist — an operator who does not want Opus reachable writes a `models:` map without it. Mitigation *not* built: there is no effort ceiling and no `--max-budget-usd` (Q-14, explicitly out of scope). See §7 for the follow-up recommendation. |
| 4 | Secrets | ✅ n/a | No credential, token or key is introduced. `claude.models` is not secret-shaped; `TestDefaultFileHasNoCredential` (`template_test.go:43`) covers the template. |
| 5 | Argv injection (R-5) | ⚠️ **existing gap found, not widened** | `validateNoForbiddenFlag` (`policy.go:155-184`) matches `strings.Contains(value, "--dangerously-"+"skip-permissions")`. **The CLI's second variant, `--allow-dangerously-skip-permissions`, does not contain that substring** — the leading `--` is not present in it. The existing check therefore does not catch it. **This feature does not widen the exposure** (a `models:` value must be a catalog key, so it cannot be a free string), so the gap is recorded and deferred rather than fixed here — see §7. The new `claude.models` entries are added to the function regardless. |
| 6 | Data handling / echo (S-9, AC-MS-6.7) | ✅ by construction | `dialDecision`'s value field is zero on the malformed and ambiguous branches. `Reply.Dial` is an `int` and cannot carry text. `Reply.DialValue` is set on one kind and re-checked by `dialValueSpan`. Label text never reaches a log line either. |
| 7 | Data handling / echo (third-party, not forge) | ⚠️ worth naming | The **init event's `res.Model`** is third-party text from `claude`, and MS-6's provenance line renders it a *second* time alongside the existing Model bullet. It is not label text so AC-MS-6.7 is literally satisfied, but the new line must go through the same `codeSpan` helper `run.go:568` already uses and must not invent a rawer path. |
| 8 | Error handling / leakage | ✅ | Refusal prose describes the rule, never the input. Config validation errors name the config key, which is operator-authored. |
| 9 | Dependencies | ✅ | No new module. `go.sum` unchanged. |

**Rulebook:** `.aiteam/rulebook.md` does not exist. The project's own invariant vocabulary (C2, C3, C9,
D-6, S-8, S-9, P-3), which is documented in-tree and in `APP_CONTEXT.md`, was used in its place — it is
stricter and more specific than the OWASP fallback would have been.

---

## 5. Shipping order and dependency graph

```
MS-1  probe gate  (hard gate; nothing below it may reach argv until it passes)
  │
  ├──► MS-3  vocabularies  ◄── ship BEFORE MS-2, see below
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

**MS-1 stays first, and stays a hard gate.** D-MS-7 is a ruling, not a preference. `claude --help`
confirmed the flags' *existence and accepted values*; it confirmed nothing about **composition** with
litefactory's argv, about whether the init event reflects `--model`, or about exit codes. The repo has a
standing convention for this (`APP_CONTEXT.md:403-424`) and two prior probe documents, both of which are
now version-behind 2.1.251.

**MS-3 before MS-2 — a departure from the spec's listing order, and it is legal.** The spec has both
depending on MS-1 only, so the order between them is free. The code says MS-3 goes first: MS-2's
`resolveDials` needs the vocabularies to look values up in, and `internal/catalog` is a leaf with no
dependencies at all. Building MS-3's three catalog tasks first unblocks MS-2's last three tasks and
removes a stub. Cross-story dependency, stated explicitly: **T-MS2.7 depends on T-MS3.3.**

**Why each story sits where it does**

- **MS-4 after both** because it needs a resolved model (MS-2's grammar) *and* a full model ID (MS-3's
  table) before it has anything to put in argv.
- **MS-5 after MS-4** because applicability is judged against the init event's resolved model
  (AC-MS-5.4), which only exists once a run has actually been invoked with `--model`.
- **MS-6 after MS-5** because the provenance line and the applicability sentence share the render
  input and the `secFixed` / bullet-order constraints; splitting them across two independent changes
  means two chances to move the Model/Tools pair.
- **MS-7 last** because AC-MS-7.3 documents the matching rule *as built* and AC-MS-7.5 requires every
  comment this feature made untrue to be fixed — both of which need the code to be final.

### What happens to the plan if a probe row fails

| Probe row | If it fails | Cost |
|---|---|---|
| **AC-MS-1.2** — flags compose with litefactory's full argv | **Stop the feature. Back to the owner.** The spec says so and it is right: the argv delivery shape is the whole design, and R-12/R-5 rule out both alternative channels. | Feature |
| **AC-MS-1.6** — `--model` alongside `--resume`, incl. a *changed* model | **The row most likely to cost the plan.** If a resumed conversation refuses a changed model with a non-zero exit, AC-MS-4.4's "identical on all three paths" is unshippable and a mid-conversation relabel wedges the task. Contingency: pin the model to the **segment** — record it when the segment is created and keep passing that one, ignoring later relabels within the segment — and say so in the report. That is a real design change and it must be known before MS-4 starts. | MS-4 redesign |
| **AC-MS-1.5** — valid effort on a no-effort model is a silent no-op | If it is a **hard error** instead of a no-op, D-MS-3's allow-and-report ruling is unshippable and the owner must be re-asked. Second stop condition. | Owner |
| **AC-MS-1.3** — init event's `model` reflects `--model` | If not, AC-MS-5.4 loses its evidence base and AC-MS-6.3's requested-vs-resolved comparison is vacuous. Contingency: judge applicability against the **requested** model always, and have the report say plainly that no resolved model was recorded — which is AC-MS-6.6's shape, applied universally. Contained to MS-5/MS-6 wording. | Two stories' wording |
| **AC-MS-1.4** — bogus model behaviour | Informational only. litefactory refuses unknown values *before* spawning (O-1), so nothing structural depends on it. If a bogus model turns out to fail locally and fast, the argument for refusing weakens slightly and does not reverse. | None |
| **AC-MS-1.7** — `ANTHROPIC_MODEL` vs `--model` | If the forwarded env var **wins**, the daemon log line becomes a warning rather than an info, and MS-6's report must not claim the label took effect. A-S9 stands: documented, not refused. | One log line, one report caveat |

### Where the plan is most likely to go wrong, ranked

1. **The byte-identical argv (AC-MS-4.1).** Any tidying refactor of `args()` risks reordering. The two
   appends go at a fixed position and only when the field is non-empty.
2. **`resumeRefused` / R-1.** See §4's ring-fence. Silent, permanent, and looks like a convenience.
3. **Per-dial ambiguity counting.** A naive "count all detected dial labels" makes
   `Model: Opus` + `Thinking: High` ambiguous, breaking AC-MS-2.5. Group by `dial.Key` first.
4. **`RefusesTools()` not covering the new kinds.** `ReplyRequest.validate` (`turn/report.go:256`) would
   reject every dial refusal as "not a tool-permission refusal" and the refusal would never post. This
   fails loudly in tests, but only if a test posts a dial refusal end to end — MS-2 must have one.
5. **The report line landing between the Model and Tools bullets.** `TestToolsBulletFollowsModelImmediately`
   catches it; the trap is "the provenance line is *about* the model, so it belongs next to the Model
   bullet", which is the one place it cannot go.
6. **The capability matrix drifting from AC-MS-3.5.** Seventeen IDs typed by hand. The test must pin
   both directions and the total count.

---

## 6. Per-story task tables and architecture notes

Task types per `.claude/skills/shared/task-decomposer.md`: `data`, `api`, `logic`, `infra`, `test`.
Every task goes to `go-implementation-engineer` (test tasks to the test engineers). No task exceeds `L`.

---

### MS-1 — Behavioural probe of `--model` and `--effort` (Size M, depends on —)

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-MS1.1 | Create `MODEL_EFFORT_PROBE.md` in this session dir: exact `claude` version header, re-verification checklist, and the probe/contingency table shape copied from `.aiteam/main/2026-08-30/tool-enforcement/IMPLEMENTATION_PLAN.md:58-91`. Match the two existing probe docs' register. | infra | — | S |
| T-MS1.2 | Probe P1 — litefactory's **full** argv (`-p --output-format stream-json --verbose --session-id … --tools … --settings … --strict-mcp-config`) with `--model` added, with `--effort` added, and with both. Record exit code and whether the run completed. (AC-MS-1.2) | infra | T-MS1.1 | M |
| T-MS1.3 | Probe P2 — does the init event's `model` field reflect the value passed to `--model`, verbatim, for one alias and one full ID. (AC-MS-1.3) | infra | T-MS1.2 | S |
| T-MS1.4 | Probe P3 — bogus model value (exit code, stderr, init event arrival, local vs API round-trip); bogus effort value; valid effort on a no-`effort` model. (AC-MS-1.4, AC-MS-1.5) | infra | T-MS1.2 | S |
| T-MS1.5 | Probe P4 — `--model` alongside `--resume`, including whether a resumed conversation honours a *changed* model. (AC-MS-1.6) | infra | T-MS1.2 | S |
| T-MS1.6 | Probe P5 — `forward_anthropic_env: true` puts `ANTHROPIC_MODEL` in the child env and `--model` is also passed: which wins. (AC-MS-1.7) | infra | T-MS1.2 | S |
| T-MS1.7 | Fill the contingency column for every row, record the gate verdict, and sign off AC-MS-1.8 (read-only, scratch dir outside the repo, nothing committed that spawns `claude`) and AC-MS-1.9. | infra | T-MS1.2–T-MS1.6 | S |

**Architecture notes**

- **This is a gate, not a document task.** T-MS1.7's verdict is what unblocks MS-2 through MS-7. A
  probe row that invalidates AC-MS-1.2 stops the feature and goes back to the owner — do not design
  around it.
- **Two rows are stop conditions and one is a redesign.** AC-MS-1.2 failing stops the feature; AC-MS-1.5
  turning out to be a hard error rather than a no-op stops D-MS-3 and needs the owner; AC-MS-1.6 turning
  out to refuse a changed model on resume forces the model to be pinned per *segment*, which is an MS-4
  redesign. Write those three contingencies out in full — they are the reason the table exists.
- **Platform: no committed test may spawn a real `claude`** (R-13, `APP_CONTEXT.md:405-424`). Every probe
  runs read-only in a scratch directory outside the repo. Nothing from this story reaches `_test.go`.
- **Forward-looking risk:** the two existing probe documents are already version-behind (2.1.246,
  2.1.247 vs the installed 2.1.251). Record that in this document rather than silently inheriting their
  claims — AC-MS-7.4 will need it.

---

### MS-2 — `Key: Value` dial labels detected, matched, refused (Size L, depends on MS-1)

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-MS2.1 | New package `internal/dial`: `Key` enum (`KeyModel`, `KeyThinking`), `String()`, `Label()` returning the canonical key literal; `Outcome` and `MalformedReason` enums. Closed key set. | data | T-MS1.7 | S |
| T-MS2.2 | `internal/dial`: `Resolve(labels []string) []Result` — G1 detection (first colon, trimmed key, `EqualFold`), G2 per-dial count-before-judge, G3 byte-exact key + `": "` separator, G4 value shape check. Stdlib imports only. | logic | T-MS2.1 | M |
| T-MS2.3 | Table-driven unit tests for `dial.Resolve`: AC-MS-2.2's five inputs with their §1 dispositions, `priority:high` under every casing (AC-MS-2.1), two `Model:` labels in mixed casings (AC-MS-2.4), `Model:`+`Thinking:` together (AC-MS-2.5), plus the empty/oversized/markup/multi-colon value cases. | test | T-MS2.2 | M |
| T-MS2.4 | `internal/report/reply.go`: three new `ReplyKind`s with their `String()` and `valid()` cases; `Reply.Dial dial.Key` and `Reply.DialValue string`; `dialValueSpan` re-checking its own copy of the G4 pattern; new `RefusesLabel()` method. | data | T-MS2.1 | M |
| T-MS2.5 | `internal/report/reply.go`: the `body()` cases for the three new kinds, sharing one `dialRefusalSentence` constant for the reason `toolsRefusalSentence` (`reply.go:299-308`) exists. Malformed and ambiguous quote nothing. | logic | T-MS2.4 | M |
| T-MS2.6 | `internal/turn/report.go`: `ReplyRequest.Dial` and `.DialValue`; switch `kind()` (:242) and `validate()` (:256) from `RefusesTools()` to `RefusesLabel()`; retire `RefusesTools()` if nothing else needs it. | api | T-MS2.4 | S |
| T-MS2.7 | `internal/daemon/dials.go`: `dialDecision` struct (value field zero on malformed/ambiguous — S-9) and `(*Daemon).resolveDials`, joining `dial.Resolve` to `d.models` and `catalog.Effort`. | logic | T-MS2.2, **T-MS3.3** | M |
| T-MS2.8 | `internal/daemon/tick.go`: `refuseDial` as a sibling of `refuseTools`, including the comment-id-`0` branch that posts nothing (AC-MS-2.10); call site in `Daemon.run` **after** `resolveTools`. | logic | T-MS2.7, T-MS2.6 | M |
| T-MS2.9 | Tests: each refusal renders end to end and posts; the issue-body path posts nothing and logs (AC-MS-2.10); the per-tick snapshot behaves like `TestRelabelThenComment` — a bare relabel starts nothing, two comments in one tick share one snapshot (AC-MS-2.8). | test | T-MS2.8 | M |
| T-MS2.10 | Regression pass: every existing `lf:tools=` test green and **unedited**; `git diff internal/daemon/tools.go` shows nothing but the AC-MS-7.5 comment change. (AC-MS-2.9) | test | T-MS2.8 | S |

**Architecture notes**

- **The grammar package is a leaf and must stay one.** `internal/dial` imports stdlib only. That is not
  tidiness — it is what lets `internal/report` import it for the `Key` type without a cycle, and
  `internal/report` cannot import `internal/turn` (`reply.go:11-18`).
- **`internal/dial` does no vocabulary lookup.** It answers "is this a well-formed, unambiguous dial
  label and what value did it carry", and stops. The lookup is the daemon's, against config-derived
  state, exactly as `d.toolProfiles[name]` is (`tools.go:173`). This split is what makes AC-MS-3.8
  structurally true rather than a naming convention.
- **Count per dial, before judging.** `map[dial.Key][]detected`, then judge. A single global count makes
  `Model:` + `Thinking:` ambiguous and breaks AC-MS-2.5. The ordering (count first) is `tools.go:141-163`
  transplanted with its reasoning, and the reasoning transfers verbatim: judging first would make the
  refusal depend on the order GitHub returned the labels.
- **AC-MS-2.7 is a property of `dialDecision`, not of `report.Reply`.** `report.Reply` carries
  `DialValue` for every kind, exactly as it carries `Profile` today; the mechanism is that the malformed
  and ambiguous branches *construct* the decision with that field zero, and `internal/report` re-checks
  before rendering. `Reply.Dial` is an `int` and so is structurally incapable of carrying label text —
  that is the whole reason it is not a string.
- **Platform / security:** label text never reaches a log line either. The `MalformedReason` enum exists
  so the log can say *why* a label was refused without carrying it — the discipline
  `tick.go:1495-1503` already establishes for `MiscasedPrefix`.
- **Do not touch `resolveTools`.** D-MS-6. The one edit to `internal/daemon/tools.go` in this whole
  feature is line 10's comment, and it belongs to MS-7.

---

### MS-3 — Model and effort vocabularies (Size M, depends on MS-1)

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-MS3.1 | New package `internal/catalog`: the 17 full model IDs from the 2.1.251 catalog, each with its `effort` capability (AC-MS-3.5's two lists verbatim); `EffortApplicable(modelID) bool`; `KnownModel(id) bool`; `doc.go` naming the exact `claude` version the table describes and pointing at `MODEL_EFFORT_PROBE.md`. | data | T-MS1.7 | S |
| T-MS3.2 | `internal/catalog`: the closed effort-level set `low medium high xhigh max` and `Effort(value) (string, bool)` doing the folded lookup. (AC-MS-3.4) | data | T-MS3.1 | XS |
| T-MS3.3 | `internal/catalog`: `BuiltinModels() map[string]string` — `opus→claude-opus-5`, `sonnet→claude-sonnet-5`, `haiku→claude-haiku-4-5`, `fable→claude-fable-5`. Comment why `best`/`opusplan`/`[1m]` are excluded and why `haiku` is non-negotiable. (AC-MS-3.2, AC-MS-3.3) | data | T-MS3.1 | XS |
| T-MS3.4 | `internal/config`: `Claude.Models map[string]string` with `yaml:"models"`; `resolveModels()` defaulting to `BuiltinModels()` when absent, called in `Load` beside `resolveToolCeiling()` at `config.go:281`. `Defaults()` leaves it **nil**. | infra | T-MS3.3 | S |
| T-MS3.5 | `internal/config/policy.go`: `validateModels` — each key matches the dial-value shape; each value is a `catalog.KnownModel`; the map is non-empty when present. Wire into `Claude.Validate`. | infra | T-MS3.4 | S |
| T-MS3.6 | `internal/config/policy.go`: add `claude.models` keys and values to `validateNoForbiddenFlag` (`policy.go:155-184`). (AC-MS-3.6) | infra | T-MS3.4 | XS |
| T-MS3.7 | `internal/config/template.go`: commented `models:` example, in the register of `template.go:134-140` — say that a label is authenticated by nothing, that anyone who can label an issue can select **any** model in this list, and that writing the map is how an operator bounds it. (AC-MS-3.7) | infra | T-MS3.4 | S |
| T-MS3.8 | `internal/daemon`: `Options.Models`, `Daemon.models` via `maps.Clone` in `New` (mirroring `daemon.go:314`); wiring from `config.Claude` in `internal/daemon/setup.go`. | infra | T-MS3.4 | S |
| T-MS3.9 | Tests: capability matrix pinned in both directions and by total count; a `models:` value smuggling the permission-skipping flag is refused at startup (AC-MS-3.6); `TestDefaultFileMatchesDefaults` and `TestDefaultFileValidates` green **unmodified** (AC-MS-3.7). | test | T-MS3.5, T-MS3.6, T-MS3.7 | M |

**Architecture notes**

- **The capability matrix is not configurable, and that is the decision.** F-3: it is knowledge about
  models, not operator preference. An operator maintaining it in YAML gets it wrong silently and produces
  the exact "my label did nothing" half-failure this feature exists to prevent.
- **Every configured model ID must be a catalog key.** This is the constraint that removes the
  "effort capability unknown" third state, which AC-MS-5.2/5.3's binary wording has no room for. It is
  also what keeps C2 airtight: a `models:` value is a litefactory constant by the time it reaches argv.
- **`claude.models` present *replaces*, it does not merge.** One rule instead of two, and it gives
  R-11's restrict and extend simultaneously. `tool_ceiling`-defaults-to-`tools` (`config.go:163`,
  resolved at `config.go:281`) is the mechanism to copy exactly.
- **Absent means the built-in roster is live, not that the feature is inert.** The alternative
  (`tool_profiles`-style, absent → nil → every `Model:` label refuses) was considered and rejected: model
  choice is not a permission grant, and a fresh install where `Model: Opus` does not work is a broken
  feature. The residual C3/spend exposure is answered by T-MS3.7's template comment, not by inertness.
- **Platform:** `internal/catalog` is a leaf — stdlib only, no litefactory import. Three packages will
  point into it (`config`, `daemon`, `report`) and it points at nothing. Do **not** put it behind an
  interface: one concrete implementation, no substitution needed.
- **Platform:** `internal/config/purity_test.go` forbids any third-party import in `internal/config`
  outside the one decode carve-out. `claude.models` decodes with the existing YAML package and nothing
  else. Note `dec.KnownFields(true)` at `config.go:295` — the struct field must exist or real configs
  stop decoding.

---

### MS-4 — Model and effort reach argv (Size M, depends on MS-1, MS-2, MS-3)

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-MS4.1 | `internal/harness/runner.go`: `Request.ModelRequested` and `.EffortRequested` (strings), with doc comments stating explicitly that neither is `RunResult.Model` and why that separation is load-bearing. (AC-MS-4.6) | data | T-MS2.7, T-MS3.3 | S |
| T-MS4.2 | `Request.validate`: reject a `ModelRequested` that is not a catalog model ID and an `EffortRequested` that is not one of the five levels, via `invalidRequest`. Defence in depth at the argv boundary, mirroring the ceiling check. | logic | T-MS4.1 | S |
| T-MS4.3 | `Runner.args` (`runner.go:468-495`): append `--model <id>` and `--effort <level>` conditionally, at a fixed position, omitted entirely when empty — the `--allowedTools` shape at `:488-493`. Extend the `args` doc comment (`:425-467`) with both flags. (AC-MS-4.1, AC-MS-4.3, AC-MS-4.8) | logic | T-MS4.1 | S |
| T-MS4.4 | `internal/turn`: `Request` and `Result` gain both fields; `prepare.go:218-235` builds them into `harness.Request`; `execute.go:326-330` sets them on the first `Result` so they survive a turn that never started a subprocess. | api | T-MS4.1 | M |
| T-MS4.5 | `internal/daemon/tick.go`: `Daemon.run` passes the resolved values into `turn.Request`; add the log line naming the selected model and effort before the turn starts, beside the existing tool-profile line (`tick.go:1393-1396`). | api | T-MS4.4, T-MS2.7 | S |
| T-MS4.6 | Tests: `TestRunWithNoProfileArgvIsByteIdentical` green and **unmodified**; new argv assertions for model-only, effort-only, and both. (AC-MS-4.1, AC-MS-4.3) | test | T-MS4.3 | M |
| T-MS4.7 | Test: extend `TestRunResumeSwapsOnlyTheSessionFlag` (`runner_test.go:1021-1048`) so cold, resume and `PathColdAfterResumeRefused` all carry both flags identically. (AC-MS-4.4) | test | T-MS4.3 | S |
| T-MS4.8 | Test: `resumeRefused` still fires on a resume refusal when a model was requested; `RunResult.Model` is never pre-populated from the request. (AC-MS-4.5, R-1) | test | T-MS4.3 | S |
| T-MS4.9 | Test: the cold-rerun path (`harvest.go:130-133`) preserves both fields across the in-place mutation; two sibling turns cannot rewrite each other's configuration. (AC-MS-4.4, AC-MS-4.7) | test | T-MS4.4 | S |

**Architecture notes**

- **Ring-fenced: `RunResult.Model` and `resumeRefused` (`runner.go:715-729`).** An empty `Model` is the
  structural test for "no init event arrived". Pre-populating it from the request — which looks like a
  convenience for the report — stops `ErrSessionNotResumable` being returned, kills the cold rerun at
  `harvest.go:88-96`, and **wedges the task permanently**. `harness.RunResult` gains no field in this
  work. T-MS4.8 is mandatory.
- **Both flags are omitted, never emptied.** `TestRunWithNoProfileArgvIsByteIdentical` compares argv
  whole with `slices.Equal` against a literal list. An unconditional element — even an empty one —
  breaks it, and R-2 is also why AC-MS-4.2 forbids a configured default.
- **Carried as strings, not slices.** This is deliberate and it is why AC-MS-4.7's copy-at-struct-
  boundaries rule barely bites for this feature: there is no `append`-into-spare-capacity hazard on the
  cold-rerun path's in-place mutation. Say so in the field doc comments so the next reader knows the rule
  was considered. Where it *does* bite is `Daemon.models`, which is MS-3's `maps.Clone`.
- **Integration point:** `harness.New` already receives the whole `config.Claude` block
  (`internal/daemon/setup.go:157-162`), but the runner needs **nothing** new from config here — both
  values arrive on the request. Do not add a runner field.
- **Platform:** new validation errors use `invalidRequest(...)`, which wraps `ErrInvalidRequest`
  (`runner.go:289-291`). Do not introduce a new sentinel.

---

### MS-5 — Inapplicable effort is allowed and reported (Size M, depends on MS-3, MS-4)

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-MS5.1 | `internal/report`: `Run.ModelRequested` and `.EffortRequested`; `internal/turn/report.go:552-576` maps them from `turn.Result` alongside `ToolsRequested`. | data | T-MS4.4 | S |
| T-MS5.2 | `internal/report/run.go`: `effortEntry(run Run) (string, bool)` — applicability judged against `run.Result.Model` when an init event arrived, falling back to `run.ModelRequested` when none did (AC-MS-5.4), asking `catalog.EffortApplicable`. Phrasing follows `toolsEntry` (`run.go:614-657`): a fact about the run, not a warning banner. | logic | T-MS5.1, T-MS3.1 | M |
| T-MS5.3 | Wire `effortEntry` into `metadataList` **after** the Tools bullet, inside `secFixed`. Do not move the `Model`/`Tools` pair. (AC-MS-6.4) | logic | T-MS5.2 | S |
| T-MS5.4 | Tests: the full AC-MS-3.5 capability matrix in both directions, table-driven, with no `claude` process anywhere. (AC-MS-5.6) | test | T-MS5.2 | M |
| T-MS5.5 | Tests: `Model: Haiku` + `Thinking: High` **runs** and is reported (AC-MS-5.1, AC-MS-5.2); the applicable case says so without editorialising and never claims an unrequested level (AC-MS-5.3); no effort requested → no sentence anywhere (AC-MS-5.5). | test | T-MS5.2 | S |

**Architecture notes**

- **`internal/report` asks `internal/catalog` directly; `internal/turn` computes nothing.** This is the
  shipped precedent: `toolsEntry` computes `notGranted` (`run.go:659-669`) in the render layer rather than
  receiving it, because a requested-vs-resolved comparison is a rendering-time fact derived from fields
  the render input already carries. Computing it upstream means threading a nil-vs-false distinction
  through two more structs for a turn that never produced a result.
- **AC-MS-5.4's fallback is the whole subtlety.** A `Thinking:` label with **no** `Model:` label is
  judged against whatever `claude` resolved — which means applicability cannot be decided before the run,
  and the report is the only honest place to state it. This is also why D-MS-3's allow-and-report is the
  only coherent ruling: there is nothing to refuse at label-resolution time.
- **The precedent to copy word-for-word is `toolsEntry`/`notGranted`.** D-MS-3 names it. Match its
  register: `requested X, but …` rather than a warning.
- **Platform:** table-driven across 17 models × applicable/not is the Go-idiomatic default and the only
  sane shape for AC-MS-5.6. Plain `if got != want { t.Errorf }` — do not introduce `testify`.
- **Forward-looking risk:** the tempting shortcut is a third state ("capability unknown") for a model ID
  the catalog does not know. MS-3's "every configured model must be a catalog key" rule removes that state
  by construction. If a reviewer proposes relaxing that config validation, this is the story that breaks.

---

### MS-6 — What was requested is durable and visible (Size M, depends on MS-4, MS-5)

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-MS6.1 | `internal/harness/meta.go`: `Meta.ModelRequested` / `.EffortRequested` with `json:"model_requested"` / `"effort_requested"`, beside the existing `model`; `NewMeta` fills them from `req` (`meta.go:121-136`), mirroring `ToolsRequested`. (AC-MS-6.1) | data | T-MS4.1 | S |
| T-MS6.2 | `internal/report/run.go`: the requested-vs-resolved provenance line, in `secFixed`, **after** the Tools bullet. When the init event's model differs from the requested one, say so rather than showing one of them. (AC-MS-6.3, AC-MS-6.4, AC-MS-6.5) | logic | T-MS5.3 | M |
| T-MS6.3 | Wording for the no-init-event case: state what was requested and that no resolved model was recorded — never present the request as the result. Follow `toolsEntry`'s AC-4.3 shape (`run.go:629-634`). (AC-MS-6.6) | logic | T-MS6.2 | S |
| T-MS6.4 | Tests: `meta.json` round-trips both keys; `TestToolsBulletFollowsModelImmediately` green and **unmodified**; a truncation-survival test in the `report_test.go:1153-1165` shape for the new line. (AC-MS-6.4, AC-MS-6.5) | test | T-MS6.2 | M |
| T-MS6.5 | Test: only catalog/table values and `codeSpan`-escaped init-event text reach the comment body — no label text, no unescaped third-party string. (AC-MS-6.7) | test | T-MS6.2 | S |
| T-MS6.6 | Confirm no store migration: no new column on `runs`, no third file in `internal/store/migrations/`. (AC-MS-6.2) | test | T-MS6.1 | XS |

**Architecture notes**

- **This line is the detection story for a relabel, not decoration.** `run.go:600-613` states it for the
  Tools bullet and every word transfers: dials are resolved per tick, two comments in one tick share one
  snapshot, labels are authenticated by nothing (C3), and this line is the only after-the-fact evidence of
  which snapshot the run in front of you used. It therefore lives in `secFixed` (index 0, `run.go:232-238`,
  "never cut") and survives the 32 KiB truncation at `maxResultBytes` (`run.go:25`).
- **Nothing goes between the Model and Tools bullets.** `TestToolsBulletFollowsModelImmediately`
  (`report_test.go:1119-1145`) pins the adjacency. The trap is that the provenance line is *about* the
  model, so it feels like it belongs beside the Model bullet — which is the one position it cannot take.
- **No migration, and that is a decision with a ledger precedent.** `.aiteam/AC_AMENDMENTS.md:1286-1312`
  rejected a `runs` column in a closely analogous case: "the migration budget went to
  `sessions.needs_poll`, which is load-bearing." `meta.json` plus the comment carry the selection. T-MS6.6
  exists so a reviewer reads the absence as intentional.
- **Security (AC-MS-6.7):** the only strings rendered are catalog/table constants, closed-set effort
  levels, and the init event's `res.Model` — which is third-party text from `claude` (not from the forge),
  is already rendered via `codeSpan` at `run.go:568`, and must go through the same helper here. Do not
  invent a rawer path for the comparison line.
- **Platform:** `Meta` is a JSON document read by `litefactory logs` and by humans. Adding two keys is
  additive and needs no version field — but A-S11 stands: **no CLI surface change**, `litefactory logs`
  keeps showing the resolved `model` row only.

---

### MS-7 — Documentation where the next engineer will look (Size S, depends on MS-6)

| ID | Title | Type | Depends On | Est |
|---|---|---|---|---|
| T-MS7.1 | `internal/dial/doc.go`: write the matching rule out in full — what is detected, what is accepted, and what breaks if it is changed the obvious way — in the register of `internal/daemon/tools.go:10-28`. Include the dispositions table. (AC-MS-7.3) | infra | T-MS2.2 | S |
| T-MS7.2 | Amend `internal/daemon/tools.go:10`: `toolLabelPrefix` is no longer "the whole of the label vocabulary". State that a second vocabulary now exists in `internal/dial` and that `lf:tools=` deliberately did not migrate. (AC-MS-7.5) | infra | T-MS2.8 | XS |
| T-MS7.3 | `.aiteam/APP_CONTEXT.md`: describe the `Key: Value` dial family, name both dials, state that `lf:tools=` deliberately did not migrate and why (D-MS-6); add `MODEL_EFFORT_PROBE.md` to "Where durable external-behaviour findings live" (:403-424) and note that the two existing documents are version-behind 2.1.251. (AC-MS-7.1, AC-MS-7.4) | infra | T-MS6.4 | S |
| T-MS7.4 | `.aiteam/AC_AMENDMENTS.md`: a ruling entry for D-MS-1 (label format departing from the house `lf:` form) and one for D-MS-3 (allow-and-report rather than refuse), each with its reason — so "what happened to D-6's house label format?" has exactly one place to look. (AC-MS-7.2) | infra | T-MS6.4 | S |
| T-MS7.5 | Sweep every comment this feature made untrue (`RefusesTools`'s doc if it survived, `runner.args`'s flag list, `config.Claude`'s block doc); confirm the `claude.models` template comment reads correctly; `make check` green — `gofmt`, `go vet`, pinned `golangci-lint`, `go test -race ./...`. (AC-MS-7.5, AC-MS-7.6) | test | all above | S |

**Architecture notes**

- **"When your change makes a comment untrue, updating it is part of the change"** (`APP_CONTEXT.md:326-328`).
  Named collisions, so nobody has to hunt: `internal/daemon/tools.go:10`, `report.ReplyKind.RefusesTools`'s
  doc, `Runner.args`'s flag list (`runner.go:425-467`), and `config.Claude`'s block doc
  (`config.go:133-140`) if `models` belongs in its summary.
- **Match the register.** `APP_CONTEXT.md:326-328` is explicit: "a one-line restatement of the code is
  worse than nothing here." `tools.go:10-28` is the standard `internal/dial/doc.go` is held to — it
  explains *why the rule is what it is* and *what breaks if it changes*, not what the code does.
- **The one place to record what this feature did not do.** T-MS7.3 should also carry the three recorded-
  not-built items so they are not rediscovered: `--max-budget-usd` as the spend bound (Q-14), the
  `--allow-dangerously-skip-permissions` gap in `validateNoForbiddenFlag` (§7), and the `Tools:` migration
  (D-MS-6).
- **Platform:** `make check` includes `go test -race ./...`. Two new packages are new lint surface under
  the pinned `.golangci.yml`. This story is where that is confirmed, not assumed.
- **`TestIntroducedCriteriaCarryTheParPrefix` is pinned to a different session directory**
  (`internal/daemon/record_test.go:278`) and does **not** scan this feature's documents. A-S12's `AC-MS-`
  convention is therefore a convention, not an enforced invariant, and no sibling test is required. Noted
  so nobody adds one under the impression it is mandatory.

---

## 7. Assumptions taken

Recorded because this was a headless run and nobody could be asked.

- **AA-1 — The separator is `": "` and nothing else.** The owner wrote `Model: Opus`. Accepting `":"` as
  well would double the canonical label set for no stated benefit. Reversible: relaxing later turns
  refusals into resolutions, which is a strictly widening change and safe. Tightening later would not be.
- **AA-2 — Value matching folds case; key matching does not.** The owner did not speak to casing. The
  split is chosen because the collision risk lives entirely on the key side, and D-6's loud-refusal
  property is worth keeping exactly there.
- **AA-3 — The value shape admits ASCII space and `.`** as well as alphanumerics, `-` and `_`, up to 40
  characters, with alphanumeric first and last. Wider than `toolProfileName` (`tools.go:40`), which is
  lowercase-only and admits neither. Reason: the dial family is explicitly built for values litefactory has
  not seen yet (D-MS-4), and version-shaped values are the obvious near-term case. Counter-argument
  recorded: it slightly widens what `ReplyUnknownDialValue` can quote back. Judged safe — the value is
  quoted inside a code span and the pattern excludes backticks, newlines and every markup character.
- **AA-4 — Unknown *value* on a known dial key refuses** (A-S3 confirmed). The owner settled only the
  compatibility case. Reversible: switching to allow-and-report later is a one-branch change.
- **AA-5 — The initial roster is four models**, `haiku` included non-negotiably as the only reachable
  no-`effort` model. Adding a row later costs one line.
- **AA-6 — `claude.models` absent means the built-in roster is live**, not that the feature is inert. The
  `tool_profiles` precedent (absent → inert) was considered and rejected: a fresh install where
  `Model: Opus` does nothing is a broken feature, and model choice is not a permission grant, so C9's
  "the only safe answer is no" does not transfer. The residual spend exposure is answered by the template
  comment, not by inertness. **This is the assumption most worth an owner's eye.**
- **AA-7 — `claude.models` present replaces rather than merges.** No merge semantics are defined anywhere
  in this design.
- **AA-8 — A sibling `refuseDial` rather than generalising `refuseTools`.** ~15 duplicated lines bought
  against leaving the shipped tools refusal path byte-untouched. D-MS-6 and R-9 both point this way.
- **AA-9 — `resolveTools` runs first and its refusal wins** when an issue carries both a bad tools label
  and a bad dial label. Not specified anywhere; chosen because the tools dial is the security boundary.
- **AA-10 — Two new packages, not one.** `internal/dial` (harness-agnostic grammar) and
  `internal/catalog` (Claude knowledge). Folding them together would put Claude model IDs inside the layer
  AC-MS-3.8 requires to name no Claude concept.
- **AA-11 — `internal/report` asks `internal/catalog` for applicability**, rather than `internal/turn`
  computing a boolean upstream. Follows `notGranted`'s precedent.

### Flagged for the owner — decisions above my authority, none of which block the plan

1. **⚠️ The `--allow-dangerously-skip-permissions` gap in `validateNoForbiddenFlag`.** `policy.go:155-184`
   matches the substring `--dangerously-skip-permissions`. The CLI's second variant,
   `--allow-dangerously-skip-permissions`, **does not contain that substring** — the leading `--` is not
   present in it — so the existing check does not catch it. **This feature does not widen the exposure**
   (a `models:` value must be a catalog key, so it can never be a free string), so the fix is **deferred,
   not taken on**: it changes a shipped, tested security boundary, it mixes a second change with different
   review needs into this branch, and D-MS-6's reasoning points the same way. Recommended as its own
   small issue: `forbiddenFlags() []string` returning both fragment-assembled variants, a loop in
   `validateNoForbiddenFlag`, and both needles in `TestForbiddenFlagIsAbsentFromTheTree`. Recorded in
   T-MS7.3 so it is not rediscovered.
2. **⚠️ This feature ships a spend amplifier with no ceiling.** C3 plus a dial whose blast radius is cost.
   `claude.models` is a *model* allowlist; there is no effort ceiling, and `Thinking: Max` is reachable
   on every model that carries the capability. `--max-budget-usd` exists and is the natural bound
   (Q-14) — recorded as out of scope by the spec, and I am not adding it. Recommended as a follow-up
   alongside an `effort_ceiling` config key if the exposure ever needs closing.
3. **AA-6** (absent-means-built-ins) is the one design assumption where a reasonable owner might rule the
   other way, and it is a one-line change if they do.

### Gaps outside this architect's specialisation

None. Every task in MS-1..MS-7 is Go backend work — package boundaries, argv construction, config
validation, report rendering, and one external-binary probe. Nothing in this feature touches frontend,
infrastructure topology, or data-platform concerns, so no gap needs to be flagged against this team's
lack of a sibling architect.
