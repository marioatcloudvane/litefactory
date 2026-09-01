# Manifest
# Branch: lf-11-we-need-a-simple-ui-for-litefactory
# Session: 2026-09-01

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(complete — re-run superseding the prior INCOMPLETE brief; grounded in verified codebase-tour and web-researcher findings)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
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
  binding tension. Status changed from INCOMPLETE to Complete.

## Notes

- Source: github issue #11 "We need a simple UI for litefactory", opened by @marioatcloudvane.
- Issue body named no explicit mode. Classified as Research: the ask is to evaluate
  UI approaches, produce 3-4 UI mockups/samples, and outline an implementation
  approach (frameworks, security posture) — not to write production code or a
  concrete plan yet.
- Headless run: no one is available to answer clarifying questions. Ambiguities
  are resolved with the most reasonable reading and noted in the final report.
