# ST-2 — Shell: layout, assets, shared helpers

> As the operator, I want every page in the UI to share one consistent
> layout, stylesheet and safety baseline, so the product feels coherent and
> every page — including ones added later — is safe by default.

Builds the base HTML layout, the extracted stylesheet, the error pages and
the small set of rendering helpers (bounded tail reader, time formatting,
render-to-buffer) that every page story reuses rather than reimplements.

## Why This Matters

Five different pages are coming (dashboard, projects, run detail, daemon
log, settings), and every one of them needs the same sidebar, the same
styling, the same "what happens on an error" behaviour, and the same care
around not leaking a half-rendered page or an open file handle. Building
that once, here, is what keeps the other stories small and consistent; if it
does not land first, every one of them ends up solving the same problems
slightly differently, which is exactly how a five-page product stops looking
like one product. The mockups define the finished look, but they were built
as static HTML with no security policy in mind, so turning them into
something a strict content-security policy will actually allow to render is
real, non-cosmetic work.

## Architecture Notes

- `app.css` is the mockups' stylesheet extracted, not redesigned — visual
  design is settled and out of scope to re-open. All 10 inline `style=`
  attributes and all 8 per-file `<style>` blocks across the mockups become
  classes here.
- Per the plan brief's shared tab-link decision: `app.css` must express tab
  panel visibility with a plain `.tab-panel` / `.tab-panel.active` class
  pair, not per-id `:checked ~` selectors — it must not need to know how
  many agent tabs a run has.
- `/static/app.css` sits behind the auth middleware like every other route
  (per the plan brief's shared decision); the `401` page is deliberately
  left unstyled rather than exempting the stylesheet from auth. Do not try
  to special-case `/static/*` out of the auth chain.
- Static handler serves the stylesheet as a single hardcoded `embed.FS`
  read, never `http.FileServer` — that removes path traversal and directory
  listing as categories rather than defending against them.
- Templates parse once, at `New(opts)` time, from `embed.FS` — a broken
  template must fail at startup, not at first render.
- Render helper: build into a `bytes.Buffer`, copy to the `ResponseWriter`
  only on success — a template error mid-execution must never leave a
  half-page behind an already-committed `200`.
- Bounded tail reader (last N bytes, discard a leading partial line, missing
  file → sentinel error) is shared by both log views (ST-6's 4 MiB run-log
  tail, ST-7's 256 KiB daemon-log tail); it lives here, not in `runlog` (it
  is not claude-specific) and not duplicated per story.
- Time-dependent template funcs (relative time, greeting-by-hour, the
  liveness dot) must be pure functions of the injected `Now` clock from
  ST-1 — no direct `time.Now()` calls in template funcs.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Extract mockups' `<style>` blocks into `internal/webui/static/app.css`; convert all 10 inline `style=` attributes to classes; add class-based tab show/hide rules | infra | — | M |
| T-2.2 | `embed.FS` for templates + `app.css`; parse all templates once in `New` | api | ST-1's T-1.4 | S |
| T-2.3 | Base layout: sidebar, brand, Projects nav, daemon-log + Settings footer links, liveness dot from `GetPollCursor` | api | T-2.2 | M |
| T-2.4 | Static handler: single hardcoded `embed.FS` file, no `http.FileServer`, `no-store` | api | T-2.2 | XS |
| T-2.5 | Error pages in-layout: 401 (plain/unstyled), 404 with a link back to `/projects`, 500 generic + logged detail | api | T-2.3 | S |
| T-2.6 | Render-to-buffer-then-copy helper | api | T-2.2 | XS |
| T-2.7 | Template funcs: relative time + RFC 3339 title in daemon-local tz, run/session status badge classes, greeting-by-hour — all pure functions of the injected clock | logic | T-2.2 | S |
| T-2.8 | Bounded tail reader: last N bytes, discard leading partial line, missing file → sentinel error, truncated-tail flag | logic | — | S |
| T-2.9 | Tests: tail reader table — empty, smaller than window, larger than window, no trailing newline, missing file, file with no newline at all | test | T-2.8 | S |
| T-2.10 | Test: every rendered page contains no `<script>`, no `on*=` attribute and no inline `style=`/`<style>` | test | T-2.3 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | With `PollCursor.LastTickAt` older than 2× `poll_interval_seconds`, the sidebar liveness dot renders amber; within it, green. |
| AC-2.2 | Rendering with JavaScript fully disabled works for navigation and every other page's interaction; the served HTML for every page contains no `<script>` tag and no `on*=` attribute. |
| AC-2.3 | A template error during rendering never produces a half-page after a committed `200` — verified by a handler test that forces a mid-render error. |
