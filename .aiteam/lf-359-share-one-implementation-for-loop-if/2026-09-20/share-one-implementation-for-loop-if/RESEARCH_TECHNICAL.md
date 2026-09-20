# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Research already runs multiple Markdown roles through one graph and shared handlers, with durable dispatch between agent stages.

Evidence:

- `internal/turn/research_workflow.go:69` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f2

The graph engine is largely generic, but action names, control-policy selection and failure handling still contain mode-specific boundaries.

Evidence:

- `internal/graphexec/validate.go:101` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f3

Controls validate source attempt identity and retained output; initial defaults apply only before a source attempt exists.

Evidence:

- `internal/turn/research_control.go:51` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f4

Runner delivery, request validation and private-result handling also require generalization beyond the activity handlers.

Evidence:

- `internal/agent/request.go:143` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f5

Package loading, launch capture and owner-reply admission currently specialize in Research.

Evidence:

- `internal/store/graph_owner_waits.go:60` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f6

Comment retention and recovery already exist, but persisted comment kinds and effect identities constrain compatibility.

Evidence:

- `internal/store/graph_comments.go:118` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f7

Plan already owns typed story validation, generation checks, recoverable publication and readiness; a graph must invoke that existing completion boundary deliberately.

Evidence:

- `internal/turn/plan_handoff.go:113` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f8

The Research decoder validates the shape of an approved response without itself proving owner approval, so stage completion and effect authorization need separate contracts.

Evidence:

- `internal/agentresult/research_node.go:69` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f9

The earlier assessment supports shared package delivery, shared runtime activities and a narrow typed mode adapter rather than unrestricted graph scripting.

Evidence:

- `internal/teamsource/research.go:106` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f10

Pinned definitions and durable captures constrain upgrades; the findings establish neither migration-free adoption nor live provider compatibility.

Evidence:

- `internal/turn/research_admission.go:150` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`

## f11

The earlier pass reported focused offline tests passing, with no matching decoder tests; this shaping pass made no implementation changes and ran no new tests.

Evidence:

- `internal/turn/research_shaping_graph_test.go:33` @ `86d3fedffa6d43fc490bffdb4c551cb79cdf9e8c`
