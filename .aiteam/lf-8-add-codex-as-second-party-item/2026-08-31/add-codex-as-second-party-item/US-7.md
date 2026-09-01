# US-7 — Per-tool permission profiles

> As an operator, I want a `lf:tools=` permission profile to carry an explicit
> per-tool permission mapping and to refuse a run under a tool it has no mapping
> for, so that a grant written for Claude's `--tools` is never silently
> reinterpreted as a Codex sandbox mode.

Extends `lf:tools=` profiles with per-tool stanzas (a `claude:` stanza and a
`codex:` stanza), narrows a resolved profile to the routed agent's own
permission shape, and refuses the run outright when the routed agent has no
stanza in the profile.

## Why This Matters

Claude's permission model and Codex's are not the same kind of thing — one is a
named list of allowed tools, the other is a coarse sandbox mode — and there is
no honest way to translate an operator's Claude grant into a Codex sandbox
setting or vice versa without guessing in one direction or the other. This
codebase has already been burned by a permission mechanism that looked
restrictive on paper and quietly admitted far more than intended in practice.
The safe answer here is to refuse rather than guess: an operator's existing
`lf:tools=` profiles keep working exactly as before for Claude, and only start
refusing once someone explicitly routes a run to Codex under a profile that was
never written with Codex in mind — at which point "refused, write a codex:
stanza" is a much better outcome than "silently ran with the wrong permissions."

## Architecture Notes

- A `lf:tools=<profile>` label resolves against the profile map as today, then
  is narrowed to the resolved agent: `claude` reads the profile's `claude:`
  stanza, `codex` reads its `codex:` stanza.
- A profile with no stanza for the resolved agent refuses the run with
  `ReplyProfileNotAvailableForAgent` — this is stage 3 of the four-stage
  refusal precedence in `plan_brief.md`'s Shared Decisions, evaluated only
  after the agent-label ladder (stage 1) has already resolved. A grant is never
  translated across tools and never falls back to the other tool's default
  surface.
- No `lf:tools=` label at all → each implementation's own configured default
  surface, unchanged for Claude, and `agents.codex.default_sandbox` for Codex.
  A configured `agents.codex` with no `default_sandbox` is a **startup**
  validation error, not a per-tick refusal — that default must be a named
  constant fixed by US-1, present in both `Defaults()` and `template.go`.
- The forbidden-value guard has two halves, both required: the existing
  `TestForbiddenFlagIsAbsentFromTheTree` tree-grep test gains a Codex clause,
  and a separate config-load-time refusal mirrors `validateNoForbiddenFlag` over
  `agents.codex.default_sandbox` and every profile's `codex.sandbox` stanza —
  the tree-grep alone does not catch an operator writing the forbidden value
  directly into a config file.
- Profile `Tools`/`AllowedTools` slices are still copied, not aliased, when they
  reach a request — and this now happens across **two** slice-copy hops
  (config → `agent.Permissions` → `agent.Request`), both load-bearing for
  isolation between concurrently running sibling turns. Cover both hops in
  tests, not just one.
- Config additions here (`agents:`, `agents.codex:`, per-profile `codex:`
  stanzas) must be reflected in `config/template.go`, or
  `config/template_test.go` (which asserts the shipped template decodes back to
  `Defaults()`) will fail. A validation error is required if both the
  deprecated top-level `claude:` key and `agents.claude:` are set
  simultaneously — not silent precedence either way.
- `agents.codex.timeout_seconds` needs explicit bounds (matching
  `claude.timeout_seconds`'s existing bounds), refused at config load rather
  than silently clamped.
- `internal/config`'s stdlib-only purity invariant is preserved: all
  litefactory-side Codex configuration is expressed in the existing YAML
  config; litefactory never parses or writes Codex's own `config.toml`.
- This story needs `agent.Permissions` and the factory from US-5, not merely
  US-1 — it cannot start before US-5's core lands. Two tasks (the Codex sandbox
  enum and the config-value forbidden guard) are additionally gated on US-1's
  measured sandbox values.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | Per-tool `ToolProfile` stanzas (`claude:`, `codex:`) + `agents.codex` config shape; validation error when both `claude:` and `agents.claude:` are set; `agents.codex.timeout_seconds` bounds, refused not clamped | Config | US-5 | M |
| T-7.2 | Profile→`Permissions` narrowing with missing-stanza refusal (`ReplyProfileNotAvailableForAgent`); both slice-copy hops (config → `Permissions` → `Request`) | Code | T-7.1, US-5 (`ReplyKind`s) | M |
| T-7.3 | 🔒 Codex sandbox enum (from US-1's measured values) + most-restrictive default in `Defaults()`/`template.go`; startup validation error if `agents.codex` is configured without `default_sandbox` | Config | US-1, T-7.1 | M |
| T-7.4 | 🔒 Forbidden-value guard, both halves: `TestForbiddenFlagIsAbsentFromTheTree` Codex clause (tree-grep) + config-load-time refusal mirroring `validateNoForbiddenFlag` over `agents.codex.default_sandbox` and every profile's `codex.sandbox` | Code/Test | T-7.3 | M |
| T-7.5 | Tests: cross-derivation refusal (Claude stanza never derives a Codex sandbox value and vice versa); `TestValidationHasNoThirdPartyImports` still passes | Test | T-7.2 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-7.1 | A profile with only a `claude:` stanza refuses a Codex-routed run with `ReplyProfileNotAvailableForAgent` |
| AC-7.2 | A profile's Claude stanza is never used to derive a Codex sandbox value, and vice versa |
| AC-7.3 | Profile `Tools`/`AllowedTools` slices are copied, not aliased, across both hops (config → `Permissions` → `Request`) |
| AC-7.4 | `TestForbiddenFlagIsAbsentFromTheTree` gains a Codex clause and passes |
| AC-7.5 | A config-load-time refusal rejects the forbidden sandbox value written directly into `agents.codex.default_sandbox` or any profile's `codex.sandbox` stanza |
| AC-7.6 | `TestValidationHasNoThirdPartyImports` still passes: `internal/config` gains no decoder beyond `gopkg.in/yaml.v3` |
| AC-7.7 | With no `lf:tools=` label, each implementation uses its own configured default surface, and the Codex default is the most restrictive measured sandbox value |
| AC-7.8 | A configured `agents.codex` with no `default_sandbox` fails daemon startup, not a per-tick refusal |
| AC-7.9 | Setting both `claude:` (deprecated top-level) and `agents.claude:` simultaneously produces a config validation error |
| AC-7.10 | `config/template_test.go` passes after the config additions in this story |
