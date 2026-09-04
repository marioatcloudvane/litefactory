# US-5 — Gate attachment ingestion by tool profile

> As an operator, I want attachment fetching to happen only for runs whose tool
> profile explicitly grants it, so that a turn without that permission behaves
> exactly as it does today — no new network calls, no new prompt content, no
> surprise.

Delivers the config keys and the permission check that decide, before any fetch
is attempted, whether a turn is allowed to ingest attachments at all.

## Why This Matters

Downloading a reporter's files and stitching them into a prompt is new behaviour,
and new behaviour needs an off switch that is off by default. This story is that
switch: attachment ingestion only runs for a turn whose resolved `Tools:
<Profile>` label — the one this plan renames in US-1 — explicitly grants it, and
every operator who does not opt in sees no change at all, not even a new log
line. Getting the off-by-default path exactly right here is what makes the rest
of this feature safe to turn on gradually, profile by profile, instead of
all at once.

## Architecture Notes

- Five new config keys — `attachments`, `attachment_max_image_bytes`,
  `attachment_max_document_bytes`, `attachment_max_total_bytes`,
  `attachment_max_count` — live in the policy stanza and inside `ToolProfile`.
  Default them via decoding, mirror the defaults into `config.Defaults()` and the
  shipped config template in the same commit (config-key changes are a hard
  coupling per `plan_brief.md`'s Workflow).
- Boot-time validation via `errors.Join` aggregation; an out-of-range value is a
  startup error, never a runtime clamp.
- The gate itself lives in `tick.go`'s per-agent narrowing stage: resolved
  `Tools: <Profile>` label present **and** the resolved profile's `attachments`
  key is `true`. Deliberately not implemented inside `resolveTools`, which stays
  a pure label-to-profile-key ladder with no side effects.
- This is a permission, not a capability: passing the gate grants the agent no
  new tool (no `WebFetch`/`WebSearch` implied) — it only decides whether
  `internal/attach`'s pre-step runs before the subprocess is spawned.
- Depends on US-1 — there is no profile to resolve a permission from until the
  new label vocabulary exists.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Add the five new config keys to `ToolProfile` and the policy stanza | code | — | S |
| T-5.2 | Default via decoding; mirror into `config.Defaults()` and the shipped template | code | T-5.1 | S |
| T-5.3 | Boot-time validation via `errors.Join`; out-of-range is a startup error | code | T-5.1 | M |
| T-5.4 | Gate check in `tick.go`'s per-agent narrowing stage (label present AND `attachments: true`) | code | T-5.1 | M |
| T-5.5 | Regression test: `resolveTools` stays a pure label ladder with no gating side effect | test | T-5.4 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | A turn with no `Tools:` label present performs zero fetch attempts and produces prompt/argv bytes identical to a build without this feature. |
| AC-5.2 | A turn whose resolved profile has `attachments: false` performs zero fetch attempts even when GitHub-hosted attachments are present. |
| AC-5.3 | A turn whose resolved profile has `attachments: true` is the only case in which `internal/attach`'s pre-step runs. |
| AC-5.4 | An out-of-range value for any of the five new keys fails at boot with an aggregated error, not a silent clamp. |
