# Research brief

## Request

Continue Research at shaping for issue #349, mcp-iverview. Settle the intended MCP deliverable and its scope.

## Decisions

- **d1** — Continue in Research shaping. Retain prior source findings; no implementation, stories, mode change or direct publication.
- **d2** — Proposed goal and user story, pending q1=A: an operator configuring or troubleshooting Litefactory can find what its MCP server exposes, when a tool is available, and what a successful response proves.
- **d3** — Proposed scope and interface, pending q1=A: a repository document at docs/mcp-overview.md, linked from the main README. Cover existing tool families, request/response contracts, Claude and Codex setup, availability gates, issue lookup boundaries and receipt recovery. No new CLI, UI, MCP tool, resource or prompt.
- **d4** — Proposed data model, pending q1=A: documentation only; no new persisted entities or schema. Each catalog entry records tool name, purpose, input fields and requiredness, output meaning, availability conditions, side effects, limits and source reference. Distinguish the full implemented catalog from the tools exposed to a particular attempt.
- **d5** — Proposed permissions, pending q1=A: describe current repository and attempt boundaries and publication/creation gates. Documentation grants no new access or ability to publish. Examples use fictional values and contain no credentials or private session identifiers.
- **d6** — Proposed validation and edge cases, pending q1=A: every documented tool and field must match current source contracts. Explain unavailable handlers, permission denial, invalid input, reserved server-name collisions, summary-only issue lookup, and lost-reply recovery by known request identity. Distinguish pending, failed and uncertain receipts from accepted input, engineering completion, acceptance and remote publication. Mark authentic provider behavior unverified unless separately evidenced.
- **d7** — Proposed acceptance and exclusions, pending q1=A: the overview covers every currently implemented tool family, identifies exposure gates and response limits, gives a valid read-only lookup example and a result/receipt sequence, and links to source owners. Exclude runtime changes, richer issue retrieval, external integrations, new transports, permission expansion, performance work and provider certification.


## Assumptions

- **a1** — Treat MCP as Litefactory's existing service because the issue is in this repository and that service exists. External integration remains an owner-selectable alternative.
- **a2** — The overview interpretation is a recommendation inferred from the title, not an owner requirement. The phrase 'extend mcp’s' prevents treating it as agreed; q1 settles this.
- **a3** — The previous pass reported no earlier artifacts and an empty baseline. This continuation has saved research artifacts and authorized result-17 revision 1; reuse them rather than treating the session as fresh.
- **a4** — Retained source observations are pinned to revision 7d5799834b5031c84286080708aaff6091ea63ee. This shaping pass has not reverified live behavior, run tests or obtained authentic host observations.
- **a5** — For the proposed overview, a repository Markdown document serves operators and agents without requiring a runtime interface. File placement and organization are shaping choices, not findings or owner decisions.


## Open questions

### q1 — What should issue #349 deliver? I recommend option A and the proposed scope below. Choosing A approves that scope. If choosing B, name the user, the missing action, and one example input with its expected result; also say whether it only reads data or changes anything.

_Asked of: Product Owner Questions_

- **a** — An operator-facing overview of Litefactory's existing MCP support. Document current tools, contracts, availability, provider setup and limitations; add no runtime capability. Recommended.  *(recommended)*
- **b** — A functional MCP extension. Specify the missing capability, such as full issue context, a new action, or an external MCP integration, and its expected result.
