# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

The current mode vocabulary is research, plan, implement, validate and spike; Bug is a separate label-selected workflow. Owner commands describe Spike as an ad-hoc small change. The classifier sees only title, body and instruction, without repository inspection; it already selects Spike for small tweaks, renames, dependency bumps and prototypes. A new decider must distinguish this text-only initial routing from repository-backed eligibility checks.

Evidence:

- `internal/agent/mode.go:11`
- `internal/instructionpackage/embedded/agents/mode-classifier.md:1`
- `docs/guides/owner-commands.md:49`

## F-02

Spike's investigator permits a proposal only when exactly one defect explains the request. SpikeGate requires nonempty defects, files, change kinds and a regression test. The source graph limits a proposal to three files including tests and forbids package, dependency, schema and API changes. Consequently, classifier examples such as a Go-version bump cannot pass that graph's dependency gate. Intentional changes and documentation-only edits do not naturally fit its defect/test contract.

Evidence:

- `teams/source/base-team/spike/spike-investigator.md:25`
- `internal/turn/spike_gate.go:43`
- `teams/source/base-team/spike/spike.dot:32`
- `internal/agentresult/types.go:249`
- `internal/agentresult/validate.go:385`

## F-03

Reusable Spike machinery already separates read-only investigation, one question round, proposal measurement, write-capable fixing, real-change measurement, pinned focused/full checks and outcome reporting. The source graph routes failed checks through one additional fixer and recheck, then reports unverified on another failure. It has no pre-fix owner approval step. This is substantially closer to a short path than the multi-stage Plan process.

Evidence:

- `teams/source/base-team/spike/spike.dot:1`
- `internal/turn/spike_gate.go:80`
- `internal/turn/spike_check.go:168`
- `docs/guides/owner-commands.md:109`

## F-04

Documentation is inconsistent with current graph source: the Spike guide says no repair retry, while the source graph has a refix path; general workflow and owner-command guides say Spike never asks, while the graph and investigator allow one question round. Current code/graph takes precedence over these summaries. Documentation-only changes would improve discoverability but would leave the intentional-change contract mismatch intact.

Evidence:

- `docs/workflow/spike.md:72`
- `docs/guides/workflow.md:38`
- `docs/guides/owner-commands.md:72`
- `teams/source/base-team/spike/spike.dot:16`

## F-05

Measured checks count changed paths outside the current session's artifact directory. Dependency manifests and lockfiles, and migration paths, are recognized from paths; new packages and API changes depend on declared change kinds. File count alone does not establish low impact. The repository has focused and full phases in its check recipe, but this research did not establish a live sandbox configuration or run those phases.

Evidence:

- `internal/turn/spike_check.go:115`
- `internal/turn/spike_check.go:141`
- `.litefactory/bug-checks.yaml:1`

## F-06

Existing alternatives differ in outcome: Plan creates and approves a story set plus story, technical and acceptance stages before Implement; Bug diagnoses a defect, waits for exact approval and then repairs and automatically delivers. Bug cannot simply represent intentional changes without altering its meaning and task-lock behavior. Spike leaves edits for factory inspection/commit and does not itself open a PR; passing checks is explicitly not owner review or merge safety. A short path's delivery promise is therefore a separate choice from reducing planning.

Evidence:

- `docs/guides/owner-commands.md:109`
- `docs/workflow/bug-runtime.md:7`
- `docs/workflow/bug-runtime.md:25`
- `docs/workflow/spike.md:133`

## F-07

Historical context: closed #406 and its #414–418 stories introduced graph Spike. Its retained research records explicit owner choices for one defect, three files including regression test, forbidden package/dependency/schema/API changes and no approval step. Those were deliberate bounds, not accidental missing implementation. The earlier brief's no-retry assumption is superseded by the present graph. Recent merged #1019 deliberately routes small changes to Spike; #1054 expanded read/run access. Searches of open/recent issues and PRs found #1359 as the direct current request and no separate implemented shorttrack identity.

Evidence:

- `.aiteam/lf-406-graph-driven-spike/2026-09-26/graph-driven-spike/RESEARCH_BRIEF.md:7`
- `.aiteam/lf-406-graph-driven-spike/2026-09-26/graph-driven-spike/plan_brief.md:1`
- `internal/instructionpackage/embedded/agents/mode-classifier.md:15`

## F-08

Impact — extending Spike touches base-team Spike roles, result skill/schema/validation, DOT gate/check attributes, runtime gate/check activities, classifier guidance, owner docs and focused gate/check/result tests. Callers include issue command/label routing, graph admission, result retention/reporting and all stack teams inheriting base-team. Adding a distinct mode also touches the closed mode vocabulary, command/label parsing, classifier and workflow/profile wiring. Source changes need the team release/capture process; edits to source do not alter retained admitted workflows. No application-data migration is established for a guidance-only extension; schema and retained-result compatibility require analysis if fields change. No existing path is selected for retirement; retained Spike sessions and published team captures are compatibility surfaces.

Evidence:

- `internal/agent/mode.go:11`
- `internal/turn/spike_gate.go:43`
- `internal/agentresult/types.go:249`
- `internal/agentresult/validate.go:385`
- `teams/source/python-fastapi/inherit.json:1`
- `docs/guides/workflow.md:74`
- `internal/store/spike_admission.go:1`
- `internal/turn/spike_fix_units_test.go:1`
