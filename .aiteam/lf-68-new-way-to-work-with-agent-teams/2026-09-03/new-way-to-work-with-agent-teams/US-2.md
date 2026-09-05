# US-2 — Three-layer config resolution (embedded → org → repo-local)

> **RECONCILIATION NOTE (2026-09-05).** `plan_brief.md`'s Story Index now titles this story
> "Index source resolution (embedded default → static path override → registered-repo override)" —
> the corrected design resolves *where the index JSON comes from*, not a three-layer per-field
> merge of full agent definitions. **Everything below this note still describes the REJECTED
> design** (Chain A / Chain B, per-field override merge into a typed `ResolvedAgent`) and has not
> been rewritten. The general shape of "more than one source, precedence between them" likely
> survives; the specific merge semantics almost certainly do not. A full body rewrite is deferred
> to a follow-up planning pass. See `plan_brief.md`'s top-of-file Reconciliation Note.

> As a platform operator, I want litefactory to resolve an agent's configuration across a shipped
> fallback, an optional organization-supplied file, and the repo's own overrides, so that a team
> can customize one field of an agent without forking its entire definition.

This story implements the merge logic that turns three separate JSON layers into the single
resolved agent definition every later story (rendering, hashing, `team list`) reads from.

## Why This Matters

Every team that uses litefactory will eventually want to change something small about a shared
agent — a different model, a stricter tool list — without maintaining a full fork of that agent's
definition. This story is what makes that possible: it defines exactly how a value set in the
repo overrides one set by the organization, which in turn overrides litefactory's own shipped
default, field by field rather than whole-file. The genuinely tricky part, already resolved by
design review but easy to get wrong in code, is that there are two different questions hiding
inside "where does the config come from" — which file supplies the organization's layer at all,
versus how three layers' worth of content actually combine — and conflating them is how a
customization silently stops taking effect the moment someone adds an organization-wide file.

## Architecture Notes

- Two independent precedence chains — implement them as two separate pieces of logic, not one:
  **Chain A** (which file supplies the org layer: `--team-config` flag, then `team_config` key in
  `$LF_HOME/config.yaml`, then none) and **Chain B** (how three layers' content composes:
  embedded → org → repo-local, later overrides earlier per field, lists replaced wholesale, an
  agent can be added but never deleted by a higher layer — removal is `"disabled": true`). Full
  definitions are in `plan_brief.md`'s Shared Decisions §2 — implement against that text, it is
  not repeated here.
- `team_config` must be an absolute path, enforced in `config.validate` (CFG-8: path-joining
  against `$LF_HOME` stays confined to `internal/config`). A configured-but-missing or relative
  `team_config` is a load error, never a silent fallback to embedded-only.
- This story's merge logic (T-08) depends on US-3's migration existing — merge always operates on
  already-current-schema config from every layer, never on a not-yet-migrated one.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-05 | `//go:embed` fallback config assets + loader | setup | US-1 T-02 | S |
| T-08 | Three-layer merge into `ResolvedAgent` (Chain B: per-field override, wholesale list replace, `disabled: true` removal) | logic | T-05, US-3 T-06 | M |
| T-09 | Tests: three-layer precedence; `disabled: true` removes the agent from every team and from generated output | test | T-08 | M |
| T-10 | Org-layer location resolution (Chain A: `--team-config` flag, then `team_config` config key, then none) | logic | T-08 | S |
| T-11 | `config.TeamConfig` key: `Defaults()` + `template.go` entry, absolute-path validation in `config.validate` | data | — | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | A value set in all three layers resolves to the repo-local one; a value set only in the fallback layer survives an org layer that sets a different field on the same agent. |
| AC-2 | Setting `"disabled": true` on an agent removes it from every team it belongs to and from all generated output. |
