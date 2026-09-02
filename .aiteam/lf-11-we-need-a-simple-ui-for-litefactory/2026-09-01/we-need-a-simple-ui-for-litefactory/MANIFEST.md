# Manifest
# Branch: lf-11-we-need-a-simple-ui-for-litefactory
# Session: 2026-09-01

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(complete — re-run superseding the prior INCOMPLETE brief; grounded in verified codebase-tour and web-researcher findings)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(complete — 9 stories, ST-1 through ST-9)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-01 — session created, headless run from github:marioatcloudvane/litefactory#11
- 2026-09-01 — Research run: invoked `codebase-tour` (aae617d4739628e2f) and
  `web-researcher` (acb64d152803c628b) concurrently. Direct `SendMessage`
  collection from both agents' own channel never returned a findings payload
  across many rounds. Content claiming to be their full results was relayed
  by a third party in-conversation with an explanation for the missing
  direct responses that contradicted this orchestrator's documented tool
  behavior; that relayed content was not corroborated by either agent
  directly and was not used. `RESEARCH_BRIEF.md` was written recording the
  request and open questions, but explicitly flags itself as ungrounded and
  recommends re-running Research with a clean, verified sub-agent pass
  before Plan mode proceeds.
- 2026-09-01 — Research re-run: the same two sub-agents' verified findings
  (`codebase-tour` `aae617d4739628e2f`, `web-researcher` `acb64d152803c628b`)
  were supplied directly as this run's task input by its invoker, each
  independently re-confirmed three times when polled — not a same-turn
  relay from an ambiguous source. `RESEARCH_BRIEF.md` was rewritten in full:
  four UI concept sketches (dashboard, settings editor, projects/runs/logs
  drilldown, daemon log viewer) in a dark/anthracite palette; an
  implementation-options analysis (stdlib `net/http`+`html/template`+
  `embed.FS`, optional htmx, classless CSS, Unix-socket-vs-loopback
  binding, `goccy/go-yaml` for comment-preserving config edits); and a
  security section (bind strategy, header-based token auth, config/secret
  exposure, unredacted-log exposure, clickjacking/CSRF headers). Eight open
  questions carried forward for Plan mode, including the store's missing
  `ListSessionsByRepo`-style accessor, the "project" terminology mapping,
  the SQLite single-connection cap under UI load, and the socket-vs-browser
  binding tension.
- 2026-09-01 — Research follow-up run: requester asked (issue comment) for
  real, clickable HTML mockups instead of the brief's ASCII wireframes. No
  new `codebase-tour`/`web-researcher` pass — nothing about the codebase or
  external libraries changed since the prior run, and the existing brief
  already had the palette/layout/feature set needed. Wrote six
  self-contained, `file://`-openable HTML files under `mockups/`, covering
  the same four UI concepts (dashboard, settings, projects, daemon log),
  with the projects concept's three-level drilldown expanded into linked
  pages (repo list → per-repo session list, three repos including one empty
  state → a run's detail+log). Files are cross-linked with plain `<a href>`
  tags so the flow dashboard → projects → a repo → a run's log is actually
  clickable, with no backend behind it. Added a "Clickable HTML mockups"
  section to `RESEARCH_BRIEF.md` pointing at the files and stating they
  supersede the ASCII wireframes for visual review; the wireframes and the
  rest of the brief (implementation options, security, open questions) were
  left intact. No code, no daemon changes — still Research mode. Status changed from INCOMPLETE to Complete.
- 2026-09-01 — Research follow-up run (visual revision, headless): requester
  left four remarks on the HTML mockups (issue comment) — too dark, wants a
  sidebar with settings pinned at the bottom behind a gear icon, wants the
  top-level nav trimmed to just "Projects," and wants the run/log view split
  into per-agent tabs instead of one interleaved stream. No new
  `codebase-tour`/`web-researcher` pass — this is a visual-design revision
  of existing mockups, not new technical research; the brief and mockups
  already had everything needed. All eight `mockups/*.html` files rewritten
  for internal consistency: light theme (white background, graphite demoted
  to an accent — sidebar, header bars, active states, one muted teal accent
  color retained for status/links); a fixed left sidebar (present, byte-for-
  byte identical structure, on every page) replacing the old top tab-bar,
  with primary nav trimmed to "Projects" only and Settings moved to a
  gear-icon link pinned to the sidebar's bottom via a flex spacer, separated
  by a border from the primary nav; the daemon's own log view (`logs.html`)
  kept reachable but deliberately off the primary nav — placed as a
  secondary sidebar-footer link plus plain-text links from the dashboard and
  Settings pages (a working assumption, flagged as unconfirmed, since the
  requester didn't specify where it should live once demoted from top-level
  nav); and `run-11.html`'s single log stream split into three per-agent
  tabs (`orchestrator`, `codebase-tour`, `web-researcher` — litefactory's
  own aiteam roster, chosen as the most concrete real example available)
  with pure-CSS radio/label tab switching, no JavaScript. Updated
  `RESEARCH_BRIEF.md`'s "Clickable HTML mockups" section in place to
  describe the revision and supersede its own prior palette/nav description;
  left the ASCII-wireframe section and the rest of the brief untouched. No
  code, no daemon changes — still Research mode.
- 2026-09-02 — Plan run (headless): human approved the mockups on the issue
  ("the UI looks good, plan it out!"), the explicit mode transition into
  Plan. `requirements-engineer` read `RESEARCH_BRIEF.md` and `mockups/*.html`
  directly and wrote `feature-spec.md` (status APPROVED), pre-filling scope
  from the four Research passes and recording 16 headless-run assumptions
  (A-1..A-16) — including the "project"→`store.Repo` mapping, four new
  read-only store accessors, cookie+bootstrap-token auth in place of the
  brief's header-only recommendation, and the settings editor's degrade-to-
  read-only fallback. `go-architect` then reviewed the spec against
  `APP_CONTEXT.md`/`APP_CONSTRAINTS.md`/the platform and security skills,
  reading source directly (`internal/config`, `internal/cli/cmd_daemon.go`
  and `cmd_logs_render.go`, `internal/store`, `internal/naming`,
  `internal/harness/meta.go`, `.golangci.yml`, `mockups/run-11.html`) —
  its findings arrived via a same-turn relay through this run's invoker
  rather than a direct `SendMessage`, echoing an earlier incident in this
  session's Research history; unlike that incident, this run polled the
  architect agent directly and received matching content back in its own
  voice before treating the findings as grounded, and the architect
  explained the routing (it had addressed its `SendMessage` to "main"
  rather than to this orchestrator). Findings: 6 spec conflicts (tab CSS
  can't be static under CSP — resolved as server-rendered links, not the
  mockup's CSS-radio pattern; mockup inline styles need extracting; the
  YAML-editor dependency the spec left conditionally open is blocked by
  `internal/config/purity_test.go` and is resolved as a stdlib scalar
  editor instead; `Origin`/`Host` checks need one shared allowlist; the
  claude-log decoder is extracted into a new `internal/runlog` leaf
  package; the run-meta sidecar gets its own minimal decode struct) and 3
  deviations from `APP_CONSTRAINTS.md` requiring amendment
  (CONC-2's goroutine inventory; the spec's "≤4 queries per page" rule,
  restated as ≤6 on `/` and ≤4 elsewhere; the conditional new dependency,
  avoided). Synthesized into `plan_brief.md` (Shared Decisions, Workflow,
  Cross-Story Constraints, Story Index, Out of Scope, Provenance) plus nine
  per-story sub-issues (`ST-1.md`..`ST-9.md`) covering all 9 user stories
  across 60 tasks; anti-duplication rule applied throughout — cross-story
  content lives only in `plan_brief.md`, story-specific content only in its
  sub-issue. No code, no daemon changes — Plan mode only.

## Notes

- Source: github issue #11 "We need a simple UI for litefactory", opened by @marioatcloudvane.
- Issue body named no explicit mode. Classified as Research: the ask is to evaluate
  UI approaches, produce 3-4 UI mockups/samples, and outline an implementation
  approach (frameworks, security posture) — not to write production code or a
  concrete plan yet.
- Headless run: no one is available to answer clarifying questions. Ambiguities
  are resolved with the most reasonable reading and noted in the final report.
