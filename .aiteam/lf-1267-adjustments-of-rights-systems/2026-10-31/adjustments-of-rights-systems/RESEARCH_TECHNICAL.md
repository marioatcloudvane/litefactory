# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

The existing implementation is an administration and checking foundation, not usable delegation: production references to rights.Service are confined to CLI setup; no runtime Check caller was found in internal. The CLI explicitly treats local machine access as owner authority.

Evidence:

- `internal/cli/cmd_rights.go:22`

## F-02

Grant granularity is exact repository ID + numeric GitHub user ID + workflow + graph node name + action, optionally one task and UTC expiry. Actions are answer, request_changes, approve; tags are legal only on lf_ask and forbidden in Implement. There is no workflow-start or general discussion permission in this vocabulary.

Evidence:

- `internal/graphexec/validate.go:219`

## F-03

Check denies unknown catalogue actions and store/catalogue errors, allows any matching active task-compatible unexpired grant, and bypasses catalogue/grants for the configured owner after validating repository and service readiness. Reviewer assignments confer no rights. Revocation affects subsequent checks.

Evidence:

- `internal/rights/check.go:47`

## F-04

No lf_rights tags were found under teams; documentation explicitly says shipped graphs do not use them. Research has separate ask, clarify and review waits that could declare different actions, but adding tags alone would not authorize replies. Current CLI: rights catalogue --repo owner/name, then rights grant LOGIN --repo owner/name --workflow W --stage S --action A [--task REF] [--expires RFC3339]. On shipped graphs no such grant can currently be issued.

Evidence:

- `docs/workflow/activities.md:157`

## F-05

Owner-only enforcement exists at ingress and persistence, not just UI: ticket comment intake rejects actors other than the appointed owner; graph reply storage requires AuthorID equal OwnerID. Delegation must integrate both layers while preserving current interaction, candidate, delivery, stale reply and replay checks.

Evidence:

- `internal/store/graph_owner_waits.go:82`

## F-06

Two MCP boundaries must be distinguished. Per-run worker MCP uses attempt authority and mode-specific tools; it is not human identity. The remote connector verifies GitHub login equals configured OwnerID. Its repository consent scopes authorize an owner client, not a collaborator or workflow-stage grant. Binding owner actions additionally use Discord approval.

Evidence:

- `internal/connector/browser.go:132`

## F-07

MCP integration implication: retain authenticated human subject plus client/token identity, intersect repository consent and stage/action rights at tool calls, and pass the same authorization through durable workflow admission. Discovery filtering alone is insufficient. Reusing owner tokens or rewriting collaborator identity as owner would defeat attribution and existing gates. This is a research recommendation, not a settled design.

Evidence:

- `internal/connector/mcp.go:49`

## F-08

No rights group, membership or group-policy model was found in rights service or rights migration. Current grants reference individual users. Repository-scoped groups could reuse the exact action checker, but membership evaluation, administration, audit, expiry and revocation need new behavior; reviewer routing must remain distinct from permission.

Evidence:

- `internal/store/migrations/0101_rights.sql:5`

## F-09

Repository flexibility already exists: workflow source resolves each repository selected installed team and version. However rights catalogue hardcodes Bug, Plan, Research, Implement and Spike, omitting Validate, and stops with an empty list on ErrNotFound. Workflow identities themselves are a closed set. Arbitrary collaborator workflows and actor/group-specific workflow selection are not current configuration features. Catalogue enumeration must be reconsidered for partial teams and added workflow identities.

Evidence:

- `internal/rights/catalogue.go:50`

## F-10

Owner compatibility requires attention: rights uses global numeric GitHub OwnerID and CLI only resolves GitHub repos, while ticket intake already uses project approver identities with provider and origin, including Linear. A numeric subject alone does not describe a cross-provider principal. Preserve repository/ticket owner authority rather than inferring it from collaborator groups or repository organization membership.

Evidence:

- `internal/daemon/ticket_ingress.go:151`

## F-11

Impact: rights service/store/migration/CLI, graph tag validation and selected team graphs, ticket ingestion and graph reply transactions, connector OAuth/consent/tools and owner-action admission, project identity/approver resolution, docs and corresponding tests. Callers affected include local owners, ticket participants, remote clients, Research/Plan/Validate review paths and saved workflow instances. Existing grant/history rows and selected team releases require compatibility treatment; node renames or tag removal make existing grants inactive without deleting them. No separate project/group rights migration was found.

Evidence:

- `internal/rights/rights_test.go:327`

## F-12

History explains the gap: prior rights brief described staged delegation, but only foundation story #502 was implemented; #503 Research, #504 shaping and #505 validation are currently CLOSED with stateReason NOT_PLANNED. Their text is intent, not proof of shipped behavior. Related issue/PR searches and recent rights commits were inspected; PR search for rights returned no matches.

Evidence:

- `.aiteam/lf-495-rights-system/2026-10-06/rights-system/RESEARCH_BRIEF.md:1`

## F-13

Focused tests passed: go test ./internal/rights ./internal/graphexec -run Test(Check|Grant|Reviewer|RepositorySource|RemovedTag|Revocation|Catalogue|.*Rights) -count=1. This establishes existing unit behavior only; no live installation, collaborator login or delegated workflow was exercised. Working tree was unchanged by investigation.

Evidence:

- `internal/rights/rights_test.go:219`
