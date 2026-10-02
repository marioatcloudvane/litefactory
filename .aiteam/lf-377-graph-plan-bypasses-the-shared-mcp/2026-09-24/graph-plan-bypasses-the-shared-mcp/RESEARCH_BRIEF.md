# Research brief

## Request

Widen scope for #377: make graph Plan deliver results through the shared MCP result route (preflight, submit, receipt) and remove the NodeProtocol bypass; raise the three Plan role files and the Plan node-result contract to the Research standard; make a rejected Plan result visible on the issue without silently consuming the owner's comment; add a story decomposition step so a multi-deliverable scope yields a reviewed story set and one sub-issue per story; write and test the agent standard for every workflow.

## Decisions

- **d1** — The request is one issue with three layers: (1) route Plan node results through preflight, submit and receipt and remove the bypass, (2) raise the Plan roles and contract to the Research standard, make failures visible, and write and test the standard for every workflow, (3) add story decomposition and a story-set review gate to graph Plan. Source: findings and both owner scope comments.
- **d2** — The agent-facing standard applies to Research, Plan and the planned Bug workflow (#367); Implement, Validate and Spike graphs (#319, #321, #322) must use it from the start. Source: findings.
- **d3** — Scope covers both owner scope comments. Graph topology for decomposition belongs in the graph definition file, not in Go. Source: owner comment.
- **d4** — Shaping decision: decomposition is a dedicated first stage with its own role and skill, so the story agent keeps a single job (one story). The set holds 1 to 12 stories with temporary ids, name, one-line intent (280 characters) and dependencies; the factory reserves story keys on promotion. Owner reviews and promotes the set on the parent issue, bound to the displayed revision.
- **d5** — Shaping decision: the terminal message stays as in Research (version, response ask or done, result) and must match the last accepted result; the retained graph output becomes the shared result, which removes the private Plan format and its parser. Roles never return approved; only the factory-bound owner path promotes.
- **d6** — Shaping decision: preflight names each create rule separately (no section list, no expected revision, story stage only) and carries the Plan-only binding checks; conversation stays a valid section name.
- **d7** — Shaping decision: 'workflow role' means a role named by a shipped graph definition's agent entries; the nine-section guard walks those, so a new workflow cannot dodge it. Tests also run each worked contract example through the real validator with factory-bound fields filled.
- **d8** — Settled by owner: the Plan result extends the shared Plan payload with a story request and a story set (Q4 = a), and the factory fills stage, candidate, request identity, upstream references and shared-decision revision (Q5 = a). Owner asked to see the JSON differences; they are in the comment.
- **d9** — Settled by owner (Q3 = a): after set promotion, a graph cycle over the existing story stage creates each sub-issue, one agent run per story, rather than a new loop activity or a bulk create. Rests on findings f17 and f18.
- **d10** — Settled by owner (Q1 = a): #377 stays one main issue. The Plan stage splits it into ordered sub-issues under #377: (1) shared route, failures, guards; (2) roles, contract, written rules; (3) set stage and decomposition.


## Assumptions

- **a1** — The 'every workflow uses the shared result service' guard is structural: a workflow policy must carry a shared-result decoder to register, the private-protocol escape is removed, and a test walks the full registered policy list (Research, Plan, later graphs). Rests on the policy struct being the single registration point today.
- **a2** — 'Nine sections' means the exact Research headings: Role, Voice, Goal, How You Are Invoked, When You Are Done, Must Do, Must Not Do, Writing the Report, Output. The Plan technical role keeps its extra Skills section as a tenth.
- **a3** — Failure visibility reuses Research's failure-comment mechanism and covers conflict, uncertain and failed outcomes of the story effect as well as agent failures. The owner's comment is marked handled together with the visible failure comment, and the owner replies to retry. Settled by owner (Q2 = a).
- **a4** — The #367 create case is refused at preflight with a diagnostic naming the rule; this needs the single create error to be split per rule.
- **a5** — The legacy-route hold-back (new Plan sessions go to legacy Plan) stays until this issue is complete. It is lifted last, as its own final step, after one owner-run tryout on a multi-deliverable parent gives a reviewed set and one sub-issue per story. Settled by owner (Q6 = a).
- **a6** — Sessions already on graph Plan keep their route. A session that already has stories skips the set stage and adds stories through the existing new-story path. Saved Plan state stays compatible. Old retained private Plan outputs are only untrusted prompt context; no migration.
- **a7** — The set agent reads the parent issue thread and the saved research scope as its input, as the legacy product-owner role read the reviewed specification. More than 12 stories is refused with advice to split the parent.
- **a8** — Plan skills are corrected only where they contradict the new contract; they are not rewritten to Research depth. The role files and the contract are.
- **a9** — I did not read the implementation-constraint documents for this stage, as instructed. The written rule is added to the two architecture documents in build, where the designer will read them.
