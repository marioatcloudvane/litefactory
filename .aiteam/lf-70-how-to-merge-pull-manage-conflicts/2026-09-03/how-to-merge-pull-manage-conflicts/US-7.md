# US-7 — `lf:sync=` label opt-out

> As the owner of a task, I want a label I can put on the issue to turn
> automatic syncing on or off for that one task, so that I can override the
> default when a merge would touch a false-positive conflict I don't want to
> stop the run over.

This story adds the escape hatch: an `lf:sync=on|off` label that overrides
the config default for one issue at a time.

## Why This Matters

Blocking a run over a conflict is the right default, but `MergeBase` merges
the *whole* base branch, not just the parts relevant to a given task — so a
conflict in an unrelated file, like a README both branches happened to touch,
can block real work that has nothing to do with that file. Without a way to
override that on a per-issue basis, the safety mechanism in US-4 becomes a
trap with no way out. This story is that way out, and it is not optional
polish: the plan's architect made shipping the block in US-2/US-4 without this
label a release-blocking condition, precisely because an un-overridable
refusal in front of real work is worse than the drift problem this whole
feature exists to fix.

## Architecture Notes

- Pattern: copy the detect-loosely/accept-strictly ladder used by the two
  **live** label levers in `internal/daemon` — `agentLabelPrefix`
  (`internal/daemon/agent.go:21`) and `modeLabelPrefix`
  (`internal/daemon/mode.go:14`) — rung for rung. Do **not** model this on
  `lf:tools=`: that label is dead code, kept only so `legacyToolsLabel` can
  detect and refuse it, and its value is deliberately never read or logged.
  Copying it would produce a label litefactory silently refuses — the exact
  opposite of what this story needs. (The research brief's citation of
  `lf:tools=` to `internal/config/policy.go` is also wrong on file location —
  the code lives in `internal/daemon`.)
- Package placement: `internal/daemon`, next to `agent.go` and `mode.go`.
- Ladder, copied exactly: no label with the `lf:sync=` prefix (case-
  insensitive) → silence, config default applies. More than one matching
  label → ambiguous, refused — never unioned, never first-wins. Prefix
  matched case-insensitively but not byte-exact → malformed, refused, flagged
  internally (e.g. a `MiscasedPrefix`-style signal), with **no field carrying
  the raw label text onward** (SEC-5/S-9 — this is the same "don't let
  untrusted text travel" discipline used elsewhere in the report package).
  Value present but not in `{on, off}` → malformed, refused. Otherwise:
  resolved, and the label's value beats the config default.
- `lf:sync=off` means "do not sync **and** do not block" — its outcome is
  `skipped`, disclosed exactly like any other skip (US-3). An override that
  turns off merging but leaves the refusal path active is not a real escape
  hatch and defeats the purpose of this story.
- Integration point: the resolved decision threads into `turn.Request`, then
  into `syncBase` (US-2), alongside the config default it can override.
- Hard sequencing constraint (see `plan_brief.md` Workflow): this story must
  ship no later than US-2/US-4 — never after. It is not an optional add-on to
  the auto-sync/block feature; it is a load-bearing part of shipping it
  responsibly.
- NOT in scope: any other label (`lf:base=` etc. is explicitly out of scope
  for the whole plan); reading or acting on `AutoSyncBase`/`BlockRunOnConflict`
  themselves (US-8 owns those); what `syncBase` does with the resolved
  decision (US-2).

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | Add `syncLabelPrefix` and `resolveSync` following the agent/mode ladder | Code | — | M |
| T-7.2 | Thread the resolved decision into `turn.Request` for `syncBase` to read | Code | T-7.1 | S |
| T-7.3 | Tests: absent, `on`, `off`, miscased prefix, duplicated label, invalid value | Test | T-7.1, T-7.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-7.1 | `lf:sync=off` overrides a config default of `auto_sync_base: true`, and `lf:sync=on` overrides a config default of `auto_sync_base: false`; a malformed, miscased, or duplicated label is refused and the config default applies, never a guessed value. |
