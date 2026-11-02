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

## F-09

Further contract inspection: proposal fields are defect-based, and finished write results require a defect plus a regression-test path listed among changed files; therefore changing investigator prose alone cannot admit truthful documentation-only edits. The gate already parses file ceilings from one to five, but the captured graph currently chooses three. The proposed scope changes admission/report semantics, not just guidance.

Evidence:

- `internal/agentresult/types.go:259`
- `internal/agentresult/validate.go:420`
- `internal/turn/spike_gate.go:63`
- `teams/source/base-team/spike/spike.dot:32`

## F-10

Compatibility inspection: graph Spike admission preserves recorded provenance and refuses legacy or ambiguous sessions; runtime steps bind the profile digest and load retained source results through the Spike policy. New result semantics must not invalidate replay of old graph results. Before/after: defect-only editing becomes one bounded change with appropriate evidence; text routing remains advisory and repository-backed eligibility remains authoritative; old captures retain old behavior, new captures adopt the extension. No public command rename or application-data migration is proposed.

Evidence:

- `internal/store/spike_admission.go:10`
- `internal/turn/spike_gate.go:147`
- `internal/agentresult/validate.go:385`

## F-11

Shared machinery already exposes one retained result per graph instance, ask/done routing, question support and default access inherited from the run; Spike opts into per-node results, read-only default access, specialized effects, annotation and handoff. Inference: a single conversational role can reuse shared mechanisms, but the current Spike policy is not already that design.

Evidence:

- `internal/turn/workflow_policy.go:25`
- `internal/turn/workflow_policy.go:76`
- `internal/turn/workflow_policy.go:185`
- `internal/turn/spike_workflow.go:41`

## F-12

Replacement impact supersedes the extension impact recommendation in F-08: remove Spike-exclusive gate/check/note/issue actions, their statuses/proposal/fix report/check fields, stage handoffs and graph branches; replace investigator/fixer guidance with one role and a minimal result contract. Update result validation, policy/renderer, admission retirement handling, team captures, routing examples, owner docs and affected tests. Shared turn handling currently references a Spike-named time helper, so deleting Spike files requires preserving genuinely shared utilities rather than removing callers. Other modes and inherited teams depend on the shared runtime and must retain their behavior.

Evidence:

- `internal/turn/spike_workflow.go:19`
- `internal/turn/spike_note.go:21`
- `internal/turn/spike_handoff.go:68`
- `internal/agentresult/types.go:188`
- `internal/agentresult/validate.go:157`
- `internal/turn/research_agent.go:165`
- `teams/source/python-fastapi/inherit.json:1`

## F-13

The observed provenance/profile-binding behavior in F-10 still holds, but its proposed compatibility guarantee is superseded by O-13: old executable paths are retired, while history is preserved. Spike admission currently loads and checks captured definitions before resuming, providing an existing boundary at which unsupported old workflows can be refused. Old installations/captures need the normal updated team adoption; pending old work cannot transparently continue on a replacement graph. No migration of business data was found in the inspected Spike admission and result surfaces; the change concerns workflow state and results.

Evidence:

- `internal/turn/spike_admission.go:106`
- `internal/store/spike_admission.go:15`
- `internal/agentresult/types.go:188`
