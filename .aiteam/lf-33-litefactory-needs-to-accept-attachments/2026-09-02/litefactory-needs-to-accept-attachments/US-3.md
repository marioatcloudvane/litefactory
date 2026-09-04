# US-3 — Ingest and hand off attached images

> As a run triggered from an issue or comment carrying image attachments, I want
> litefactory to fetch those images and tell the agent where to find them, so
> that the agent can see the mockup, screenshot or diagram the reporter attached
> instead of working blind.

Delivers the first end-to-end ingestion path — fetch, sniff, size, land, and hand
off a GitHub-hosted image to the invoked agent through the prompt.

## Why This Matters

Right now, a screenshot or mockup dropped onto a GitHub issue is invisible to the
agent litefactory runs — the reporter sees it, litefactory does not pass it on.
This story closes that gap for images: litefactory downloads the file itself
(never the agent, never a new tool grant), puts it in the run's own working
folder, and tells the agent in plain text where to look. The owner was clear
this had to be litefactory doing the fetching, not the agent, so interception and
logging stay possible later — this story is where that boundary gets built, and
it is the largest, riskiest piece of the whole feature because everything else
(documents, audit trail, retry hardening) extends the pipeline it establishes.

## Architecture Notes

- New leaf package `internal/attach`: imports `internal/config`/`internal/naming`
  only, is imported only by `internal/turn`, and must not import
  `internal/daemon`, `internal/harness`, `internal/report`, or `internal/store`.
- The one HTTP hop is a new, single-shot `FetchAsset` method on the concrete
  `*github.Client` in `internal/github` — the only package holding the
  installation token. It returns a typed `ErrAssetUnauthorized`, retries nothing,
  sleeps nowhere. This is a deliberate, documented amendment to
  `internal/github/surface_test.go`'s pinned "exactly seven endpoints, no
  interface in this package" test — update the pin in the same commit, with a
  comment explaining why. `internal/attach` consumes `FetchAsset` through an
  interface it declares at its own point of use.
- Insertion point: `internal/turn/prepare.go`, after the worktree exists, before
  `BuildPrompt` is called. Files land under `$session/attachments/` (gitignored,
  path minted by a new `config.Paths` method).
- Handoff is exactly one new field, `harness.PromptInput.Attachments string`,
  rendered by `BuildPrompt` into its own `## Attachments` section — no change to
  `agent.Request`, no change to any `Runner` implementation, no per-attachment
  struct crosses into `internal/harness`.
- SSRF: the allow-list matches GitHub asset host/path patterns only, checked
  before every dial and re-checked on every redirect hop. A non-matching URL in
  the body is ignored, never dialed.
- Size ceilings are enforced during the stream (`io.LimitReader` + abort), never
  by trusting `Content-Length`; total-bytes and count ceilings (across the whole
  turn) are evaluated before per-file ceilings.
- Sniffed content type is authoritative over the declared `Content-Type` header
  and the file extension.
- Depends on US-5 (the gate must exist before any fetch is attempted) and US-6
  (a rejection needs somewhere to report before ingestion can safely run).
- **Highest-risk regression:** the pinned byte-exact cold-prompt test — a turn
  with zero attachments must produce identical prompt bytes to today. Update the
  test alongside the new rendering path, not after.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | `internal/attach` package skeleton and import-boundary enforcement | code | — | S |
| T-3.2 | GitHub asset-URL allow-list matcher, applied pre-dial and on every redirect hop | code | T-3.1 | M |
| T-3.3 | `*github.Client.FetchAsset` — single-shot GET, typed `ErrAssetUnauthorized`, no retry | code | — | M |
| T-3.4 | `internal/attach` fetcher interface consuming `FetchAsset` | code | T-3.1, T-3.3 | S |
| T-3.5 | Stream-time size enforcement (bounded reader + abort); total/count ceilings before per-file | code | T-3.1 | M |
| T-3.6 | Content-type sniffing, authoritative over declared type/extension | code | T-3.1 | S |
| T-3.7 | litefactory-derived filename minting/sanitisation + `config.Paths` method for the landing dir | code | T-3.1 | S |
| T-3.8 | Wire ingestion into `internal/turn/prepare.go`, after worktree exists, before `BuildPrompt` | code | T-3.2, T-3.4, T-3.5, T-3.6, T-3.7 | M |
| T-3.9 | Add `harness.PromptInput.Attachments` field; render `## Attachments` section in `BuildPrompt` | code | T-3.8 | S |
| T-3.10 | Update the pinned byte-exact cold-prompt test for both the no-attachment and rendered cases | test | T-3.9 | M |
| T-3.11 | Bounded, context-cancellable concurrent fetch pool for multi-attachment turns | code | T-3.4 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | A GitHub-hosted image attachment is fetched, sniffed, sized, landed under `$session/attachments/`, and referenced in the rendered `## Attachments` prompt section. |
| AC-3.2 | A non-GitHub URL present in the same body is never dialed. |
| AC-3.3 | A turn with zero attachments produces byte-identical prompt output to the pre-feature baseline. |
| AC-3.4 | A fetch exceeding the per-file or total byte ceiling aborts mid-stream; no oversized bytes are ever written to disk. |
| AC-3.5 | Sniffed content type, not the declared header or extension, decides whether a file is treated as a supported image. |
