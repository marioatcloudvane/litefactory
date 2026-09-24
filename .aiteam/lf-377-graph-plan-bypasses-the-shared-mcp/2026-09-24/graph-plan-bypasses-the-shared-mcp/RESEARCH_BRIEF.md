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


## Assumptions

- **a1** — The 'every workflow uses the shared result service' guard is structural: a workflow policy must carry a shared-result decoder to register, the private-protocol escape is removed, and a test walks the full registered policy list (Research, Plan, later graphs). Rests on the policy struct being the single registration point today.
- **a2** — 'Nine sections' means the exact Research headings: Role, Voice, Goal, How You Are Invoked, When You Are Done, Must Do, Must Not Do, Writing the Report, Output. The Plan technical role keeps its extra Skills section as a tenth.
- **a3** — Failure visibility reuses Research's failure-comment mechanism, and covers conflict, uncertain and failed outcomes of the story effect as well as agent failures. What happens to the owner's comment is question q2 (recommended: consumed with a visible failure comment).
- **a4** — The #367 create case is refused at preflight with a diagnostic naming the rule; this needs the single create error to be split per rule.
- **a5** — The legacy-route hold-back (new Plan sessions go to legacy Plan) stays until this issue is complete; when it lifts is question q6. Rests on the store comment naming #377.
- **a6** — Sessions already on graph Plan keep their route. A session that already has stories skips the set stage and adds stories through the existing new-story path. Saved Plan state stays compatible. Old retained private Plan outputs are only untrusted prompt context; no migration.
- **a7** — The set agent reads the parent issue thread and the saved research scope as its input, as the legacy product-owner role read the reviewed specification. More than 12 stories is refused with advice to split the parent.
- **a8** — Plan skills are corrected only where they contradict the new contract; they are not rewritten to Research depth. The role files and the contract are.
- **a9** — I did not read the implementation-constraint documents for this stage, as instructed. The written rule is added to the two architecture documents in build, where the designer will read them.


## Open questions

### q1 — Deliver this as one issue in three ordered parts, or split it into separate issues?

_Asked of: Product Owner Questions_

- **a** — One issue, three ordered parts: (1) shared route, visible failures, guards; (2) roles, contract and written rules; (3) set stage and decomposition. Plan stage turns them into stories. Recommended: the parts depend on each other and the hold-back lifts only when all three are done.  *(recommended)*
- **b** — Three separate issues, each shipped alone. Smaller reviews, but graph Plan stays held back until the last one and the parts must be tracked by hand.
- **c** — Two issues: standard (parts 1 and 2) and decomposition (part 3).

### q2 — When a Plan step fails, what happens to the owner's comment?

_Asked of: Product Owner Questions_

- **a** — Post a failure comment with the cause and how to retry, and mark the owner's comment handled. The owner replies to try again. Same as Research. Recommended: clear, and no risk of endless automatic retries.  *(recommended)*
- **b** — Post the failure comment and leave the owner's comment open so the step reruns on its own next tick. Risks a loop on a fault that will not fix itself.
- **c** — Post nothing new and only log it. This is today's silent failure.

### q3 — After the owner promotes the story set, how do the stories get their sub-issues?

_Asked of: Product Owner Questions_

- **a** — One graph loop walks the stories in dependency order and creates each sub-issue with a first draft, then waits for reviews. Recommended: one owner approval gives the whole set of sub-issues, and each story is then reviewed on its own.  *(recommended)*
- **b** — Only the first story is drafted; the owner must nudge the factory for each next story. Simple, but slow and easy to stall.
- **c** — Create empty shell sub-issues at promotion and fill them later. Needs a new create path with no draft, which the current rules refuse.

### q6 — When is the graph Plan hold-back lifted?

_Asked of: Product Owner Questions_

- **a** — Last, as its own final step, after all parts are merged and one owner-run tryout on a multi-deliverable parent yields a reviewed set and one sub-issue per story. Recommended: this is the check #362 never had.  *(recommended)*
- **b** — As soon as the shared route and roles are done, before decomposition. Risks repeating the one-story failure.
- **c** — Never automatically; the owner flips it by hand after the tryout.

### q4 — How does the Plan result ride inside the shared result?

_Asked of: Technical Questions_

- **a** — Extend the shared Plan payload with two exclusive parts, story request and story set. Old planning fields stay for legacy Plan only and are refused in graph results. ask maps to an incomplete result with questions, review-ready maps to a completed result. Recommended: one format, no new mode, legacy untouched.  *(recommended)*
- **b** — Add a separate payload type for graph Plan. Cleaner names, but the shared result has a closed list of five modes, so this touches the mode list and every consumer.
- **c** — Keep the private Plan JSON as a string inside the shared result. Fastest, but keeps a second format and defeats the point.

### q5 — Should agents stop authoring fields the factory already knows?

_Asked of: Technical Questions_

- **a** — Yes. The factory fills candidate, request identity, upstream references and shared-decision revision. The agent supplies only the draft, the operation, sections changed, expected revision and replacements. Recommended: removes the fields agents most often got wrong, and the factory-side checks become a safety net.  *(recommended)*
- **b** — No. Agents keep echoing them and the preflight checks they match. Keeps today's contract but leaves more room for guesswork.
