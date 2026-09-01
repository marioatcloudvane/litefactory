# MS-3 — Model and effort vocabularies resolve to values litefactory authored

> As a daemon operator, I want the label value to be a lookup key into a table litefactory or I authored, so that no string originating on the forge ever reaches the command line.

Delivers the dial-value → full-model-ID roster, the effort level set, the per-model effort-capability matrix, the one optional config key that lets an operator bound which models are reachable, and the startup validation around all of it.

## Why This Matters

This is the story that decides which models anyone can actually pick, and it is the only place in the
feature where cost is bounded. Anyone who can put a label on an issue can pick a model, and nothing
authenticates that — so the difference between the most and least expensive choice is real money that
someone else can spend on your behalf. Writing down the list of allowed models is how an operator
puts a fence around that.

There is a second, subtler thing this story owns: knowing which models can actually honour a thinking
level at all. That is knowledge about the models themselves, not a preference, and if an operator had
to maintain it by hand they would get it wrong silently — producing exactly the "my label did
nothing" outcome the whole feature exists to prevent. So litefactory keeps that knowledge and takes
responsibility for it being right, while the operator keeps control over which models are on the
menu. Splitting it that way is the one genuinely hard call in this story, and the cost of the split
is that reaching a model litefactory has never heard of needs a release.

## Architecture Notes

- **The capability matrix is not configurable, and that is the decision.** It is knowledge about
  models, not operator preference. An operator maintaining it in YAML gets it wrong silently.
- **Every configured model ID must be a catalog key.** This is the load-bearing constraint. It
  removes the "effort capability unknown" third state that MS-5's binary wording has no room for, and
  it is what keeps the lookup-key invariant airtight: a configured value is a litefactory constant by
  the time it reaches argv.
- **`claude.models` present *replaces*, it does not merge.** One rule instead of two, and it gives
  both halves of what an operator needs — restrict and extend — simultaneously.
  `tool_ceiling`-defaults-to-`tools` (`internal/config/config.go:163`, resolved at `config.go:281`)
  is the mechanism to copy exactly.
- **Absent means the built-in roster is live, not that the feature is inert.** The `tool_profiles`
  precedent (absent → nil → every label refuses) was considered and rejected: model choice is not a
  permission grant, and a fresh install where `Model: Opus` does nothing is a broken feature. The
  residual spend exposure is answered by the template comment, not by inertness. *This is the
  assumption most worth the owner's eye.*
- **Platform:** `internal/catalog` is a leaf — stdlib only, no litefactory import. Three packages
  point into it and it points at nothing. Do **not** put it behind an interface. Note
  `dec.KnownFields(true)` at `internal/config/config.go:295` — the struct field must exist or real
  configs stop decoding. `internal/config/purity_test.go` forbids any third-party import in
  `internal/config` outside the one decode carve-out.
- **Risk:** seventeen model IDs typed by hand. The test must pin both directions *and* the total
  count, or a transcription slip ships silently.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-MS3.1 | New package `internal/catalog`: the 17 full model IDs from the 2.1.251 catalog, each with its `effort` capability (AC-MS-3.5's two lists verbatim); `EffortApplicable(modelID) bool`; `KnownModel(id) bool`; `doc.go` naming the exact `claude` version the table describes and pointing at `MODEL_EFFORT_PROBE.md`. | data | T-MS1.7 | S |
| T-MS3.2 | `internal/catalog`: the closed effort-level set `low medium high xhigh max` and `Effort(value) (string, bool)` doing the folded lookup. | data | T-MS3.1 | XS |
| T-MS3.3 | `internal/catalog`: `BuiltinModels() map[string]string` — `opus→claude-opus-5`, `sonnet→claude-sonnet-5`, `haiku→claude-haiku-4-5`, `fable→claude-fable-5`. Comment why `best`/`opusplan`/`[1m]` are excluded and why `haiku` is non-negotiable. | data | T-MS3.1 | XS |
| T-MS3.4 | `internal/config`: `Claude.Models map[string]string` with `yaml:"models"`; `resolveModels()` defaulting to `BuiltinModels()` when absent, called in `Load` beside `resolveToolCeiling()` at `config.go:281`. `Defaults()` leaves it **nil**. | infra | T-MS3.3 | S |
| T-MS3.5 | `internal/config/policy.go`: `validateModels` — each key matches the dial-value shape pattern; each value is a `catalog.KnownModel`; the map is non-empty when present. Wire into `Claude.Validate`. | infra | T-MS3.4 | S |
| T-MS3.6 | `internal/config/policy.go`: add `claude.models` keys **and** values to `validateNoForbiddenFlag` (`policy.go:155-184`). Belt-and-braces given the catalog constraint, which is what that function's discipline asks for. | infra | T-MS3.4 | XS |
| T-MS3.7 | `internal/config/template.go`: commented `models:` example in the register of `template.go:134-140` — say that a label is authenticated by nothing, that anyone who can label an issue can select **any** model in this list, and that writing the map is how an operator bounds it. | infra | T-MS3.4 | S |
| T-MS3.8 | `internal/daemon`: `Options.Models`, `Daemon.models` via `maps.Clone` in `New` (mirroring `daemon.go:314`); wiring from `config.Claude` in `internal/daemon/setup.go`. | infra | T-MS3.4 | S |
| T-MS3.9 | Tests: capability matrix pinned in both directions and by total count; a `models:` value smuggling the permission-skipping flag is refused at startup; `TestDefaultFileMatchesDefaults` and `TestDefaultFileValidates` green **unmodified**. | test | T-MS3.5, T-MS3.6, T-MS3.7 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-MS-3.1 | The label value is used **only as a lookup key**. The string joined into argv comes from the table, never from the label. |
| AC-MS-3.2 | The model table maps to **full model IDs** (e.g. `claude-opus-5`), not to CLI aliases, because alias resolution is provider-dependent. |
| AC-MS-3.3 | The model vocabulary covers at least Opus, Sonnet and Haiku, each pinned to a full ID. The shipped roster is `opus`, `sonnet`, `haiku`, `fable`. |
| AC-MS-3.4 | The effort vocabulary is exactly `low`, `medium`, `high`, `xhigh`, `max` — the five values `claude --help` lists on 2.1.251. |
| AC-MS-3.5 | A per-model `effort`-capability matrix exists and is correct as of 2.1.251: carrying `effort` — `claude-sonnet-4-6`, `claude-sonnet-5`, `claude-opus-4-6`, `claude-opus-4-7`, `claude-opus-4-8`, `claude-opus-5`, `claude-fable-5`; **not** carrying it — `claude-3-5-haiku`, `claude-3-5-sonnet`, `claude-3-7-sonnet`, `claude-haiku-4-5`, `claude-sonnet-4-0`, `claude-sonnet-4-5`, `claude-opus-4-0`, `claude-opus-4-1`, `claude-opus-4-5`, `claude-mythos-5`. |
| AC-MS-3.6 | **Every configurable string that can reach argv is added to `validateNoForbiddenFlag`** (`internal/config/policy.go:155-184`), and a test proves a configured value smuggling the permission-skipping flag is refused at startup. |
| AC-MS-3.7 | `claude.models` is absent from a fresh install, appears in `internal/config/template.go` as a commented example only, and `TestDefaultFileMatchesDefaults` stays green **unmodified**. |
| AC-MS-3.8 | The vocabulary layer names no Claude-specific concept in its own types or field names: a future harness's dial values must be expressible without renaming anything. `internal/dial` holds the grammar; `internal/catalog` holds the Claude knowledge; the split is what makes this structurally true rather than a naming convention. |
| AC-MS-3.9 | `claude.models` present **replaces** the built-in roster rather than merging with it, every configured value is a catalog key checked at startup, and every configured key matches the dial-value shape pattern. |
