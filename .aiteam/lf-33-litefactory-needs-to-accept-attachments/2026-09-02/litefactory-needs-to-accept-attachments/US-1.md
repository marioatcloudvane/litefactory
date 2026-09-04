# US-1 — Rename tool label to `Tools: <Profile>`

> As an operator, I want the tool-selection label rewritten as friendly text like
> `Tools: Attachments` instead of raw config syntax like `lf:tools=web`, so that
> choosing a run's tool profile reads like a label a person wrote, not a
> command line.

Delivers the new `Tools: <Profile>` label vocabulary that every later story's
permission gate reads.

## Why This Matters

Today, turning on a capability for a run means typing a config-file key straight
onto a GitHub label — `lf:tools=web` — which only makes sense to someone who
already knows the YAML underneath. This story replaces that with a label that
reads in plain English, `Tools: Web`, matching the style the owner asked for and
setting the pattern `Tools: Attachments` will follow. It is a rename with real
stakes: every issue currently carrying the old label needs to keep behaving
sensibly the moment this ships, which is why the loud refusal in US-2 ships in the
same breath as this story.

## Architecture Notes

- **Flagged spec correction, adopted:** the feature spec's case-folding rule
  ("lowercase the first character only") is unshippable — it conflicts with
  `internal/config`'s profile-key pattern, which requires the whole key to be
  lowercase, and would turn a label value like `Research-Deep` into the illegal
  key `research-Deep`. The corrected rule, matching the precedent already shipped
  for `Coding Agent: ` at `internal/daemon/agent.go:141`: validate the label's
  display value against `^[A-Z][A-Za-z0-9_-]{0,39}$`, then lowercase the **whole**
  value before using it as a profile key. Implement the corrected rule, not the
  spec's literal wording.
- Reuses the exact detect-loosely/accept-strictly ladder already shipped for
  `Coding Agent: ` — case-insensitive prefix detection, byte-exact value
  acceptance, ambiguity counted before any label is judged. Do not invent a
  second parsing style for this prefix.
- Platform: no new dependency; this is string matching and lookup against
  `internal/config`'s existing profile map.
- Scope boundary: this story is the label-to-profile-key mapping only. Whether a
  resolved profile is honoured for anything — including attachments — is US-5's
  gate, not this story's concern.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Add `Tools: ` prefix detection to the label ladder, mirroring `Coding Agent: ` | code | — | M |
| T-1.2 | Apply the corrected case rule: validate `^[A-Z][A-Za-z0-9_-]{0,39}$`, then lowercase the whole value | code | T-1.1 | S |
| T-1.3 | Map the resolved lowercase value to a `config.ToolProfile` key lookup | code | T-1.2 | S |
| T-1.4 | Test coverage: no-label no-op, ambiguity counted before judged, byte-exact acceptance | test | T-1.1 | M |
| T-1.5 | Update shipped docs/config-template comments referencing `lf:tools=` to the new notation | docs | T-1.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | A label spelled `Tools: <Profile>` (case-insensitive on the word "Tools") whose value matches `^[A-Z][A-Za-z0-9_-]{0,39}$` is accepted; the value is lowercased in full before use as a profile key. |
| AC-1.2 | A label value that does not match the pattern is treated as absent, never partially accepted. |
| AC-1.3 | Existing `Coding Agent: ` label parsing is unchanged byte-for-byte. |
| AC-1.4 | Ambiguity (more than one candidate label) is counted before any label is judged, consistent with the shipped ladder. |
