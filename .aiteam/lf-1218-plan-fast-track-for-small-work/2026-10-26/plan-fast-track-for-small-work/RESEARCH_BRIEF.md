# Research brief

## Request

Shape a Plan-only fast track per S-sized subissue: three planning agents run in parallel with deliberately smaller outputs, M/L subissues retain every owner stage stop, mixed-size sets are supported, and growth causes a durable story-local fallback while set promotion, assembled approval and outcome coverage remain intact.

## Decisions

- **S-01** — mode=feature, size=large — Parallel per-story reasoning adds concurrent result retention, reconciliation, stale-result handling and mixed-path scheduling beyond the earlier medium routing-only scope. (agent's call)
- **O-01** — Each S-sized subissue runs the story, technical and acceptance agents in parallel with deliberately smaller scope and output, replacing the earlier whole-request eligibility proposal. (owner said: comment 6474304051, q1 and subissue clarification)
- **O-02** — A story that grows beyond S returns to normal owner gates for the remainder of its planning cycle without reopening unaffected approvals or changing sibling stories' routes. (owner said: comment 6474304051, q3: a and subissue clarification)
- **K-01** — Set promotion and final assembled plan approval remain owner decisions. (owner said: issue #1218 and retained #984 agreement)
- **K-02** — Stories retain Covers: O-… and any narrowing has a Ruling: line. (owner said: issue #1218)
- **N-01** — Sizing itself belongs to #1217; Implement and Validate are outside this work. (owner said: issue #1218, Not in this story)
- **O-03** — Set review shows each story's size and route, growth reports identify the affected story and reason, and final review shows per-story route history and graph-approved stages. (agent's call)
- **O-04** — An eligible S story advances automatically after coherent parallel results are retained, while questions and guarded approval failures retain the existing review or failure behavior across resumes. (agent's call)
- **K-03** — Existing approval identity, upstream dependencies, holds, retirement review and change invalidation rules remain effective on both planning paths. (agent's call)
- **K-04** — Existing saved plans and captured team versions retain their behavior through the established release and capture process. (agent's call)
- **N-02** — New commands, manual fast-track overrides, automatic plan migration and changes to team release authority are outside this scope. (agent's call)
- **O-05** — The feature consumes the sizing contract supplied by #1217 without creating a competing size calculation or weakening normal planning when size evidence is unavailable. (agent's call)
- **A-01** — Answers to q1, q2 and q3 with the routing-unit correction — owner: "q1: for S sizing, the 3 agents should run in parallel but deliberately with smaller scope / output. 
q2: yes, M and L should have the traditional path - waiting at every step
q3: a

important: this is for the subissues, so not for the overall issue. in an issue it can be that we have different sizes, e.g. two S and one L!" (owner said: comment 6474304051)
- **O-06** — M and L subissues retain sequential story, technical and acceptance planning with an owner review stop at every step. (owner said: comment 6474304051, q2)
- **O-07** — One promoted set can contain S, M and L stories whose routing is independent of sibling sizes and the overall Research size. (owner said: comment 6474304051, subissue clarification)
- **O-08** — Parallel agents work from the same current story scope and shared decisions, and their outputs must agree before automatic stage approval or assembled review includes them. (agent's call)
- **O-09** — An S story retains concise intent and boundaries, only necessary technical decisions, and the minimum tasks and observable criteria needed to cover its outcomes. (agent's call)
- **O-10** — Parallel completion order cannot overwrite another role's sections, apply an obsolete result or approve a partly completed story after a question, failure, scope change or growth fallback. (agent's call)
- **O-11** — Fast-track reasoning runs concurrently rather than executing the three agents one after another, while applying approvals may remain ordered to preserve existing approval guards. (agent's call)
- **K-05** — The parent set stage and final assembled approval retain their existing owner authority and are never routed through the S-story parallel path. (owner said: issue #1218 and comment 6474304051, subissue clarification)
- **K-06** — Shorter S outputs preserve required typed content, Covers and Ruling notes, relevant non-goals and invariants, task-to-criterion proof links and review visibility. (agent's call)
- **K-07** — Existing inter-story dependency approvals remain effective, so independent siblings can progress while genuine dependency waits remain visible. (agent's call)
- **K-08** — Existing advisory judges remain advisory and retain their review purpose on both paths without becoming owner approvals. (agent's call)
- **N-03** — Changing M/L planning depth, bypassing inter-story dependencies or building a general parallel-execution feature for other workflow modes is outside this scope. (agent's call)
- **O-12** — Missing or invalid per-story size evidence uses normal planning rather than inferring S from the parent Research size. (agent's call)
- **A-02** — Implementation-surface question — owner: "this would only change the graph files, right? no code changes needed, or?
" (owner said: comment 6474699964)
- **A-03** — Graph comparison proposal — owner: "the size is in the user story, not somewhere in code. in the graph we would \"only\" compare with an if activity and then go on a different path! i think this could work, right?" (owner said: comment 6474822940)
- **O-13** — The graph declares the comparison against the selected story's size and the resulting fast or normal paths, using the existing story size as its source of truth. (owner said: comment 6474822940)
- **K-09** — Runtime support must preserve graph ownership of the size comparison and route selection without adding another sizing source or embedding a separate fast-track routing policy in Go. (agent's call)


## Assumptions

- **AS-01** — Superseded by O-01 and O-11: keeping sequential agent execution with fewer stops was rejected in favor of three parallel planning agents for each S subissue. (agent's call)
- **AS-02** — Superseded by O-02 and O-07: eligibility and sticky growth fallback are story-local, not whole-plan policies, and mixed-size sets are supported. (agent's call)
- **AS-03** — Previously captured teams retain their graph behavior; source changes apply through the established release/capture process. No automatic migration of saved plans or captures is assumed. (agent's call)
- **AS-04** — Use the active promoted story's explicit S/M/L and reason from the existing sizing contract; missing or invalid size evidence takes normal planning, and overall Research-small never substitutes for a story size. (agent's call)
- **AS-05** — Retain each story's routing basis, sizing revision, parallel-result identities and growth fallback so resume preserves its route and rejects obsolete results; storage layout belongs to Plan. (agent's call)
- **AS-06** — Success means actual overlap of the three S agents, concise complete outputs, independent routing in a two-S/one-L set, coherent-result approval, safe question/failure/change/resume behavior, story-local sticky growth fallback and unchanged owner set/final approval. (agent's call)
- **AS-07** — Existing owner commands and story/final reviews remain the interface; internal result handling gains distinct concurrent contributions and retains graph approval provenance without granting agents new owner authority. (agent's call)
- **AS-08** — Superseded by S-01: the owner's requirement for concurrent planning and coherent result assembly expands this feature from medium to large. (agent's call)
- **AS-09** — Parallel agents refine the same promoted story intent within its settled boundaries rather than independently inventing new scope; material intent changes require the owner. (agent's call)
- **AS-10** — Reconcile compatible contributions automatically within existing bounded continuation limits and rerun only affected work when necessary; unresolved material contradictions or questions use the existing owner question/review path instead of a new mandatory reconciliation stop. (agent's call)
- **AS-11** — If any parallel result identifies growth beyond S, hold automatic advancement, retain the growth reason and resume the affected story on its earliest required normal stage under existing approval-invalidation rules. (agent's call)
- **AS-12** — Failures retain valid contributions for supported resume but do not count missing or failed contributions as complete; after scope or size changes, reuse only contributions demonstrably current with the selected story revision. (agent's call)
- **AS-13** — Sticky fallback lasts through the affected story's current planning cycle, including ordinary resumes and revisions; a later size reduction does not automatically re-enter fast track. (agent's call)
- **AS-14** — Smaller output means omitting optional elaboration and unnecessary tasks, not numeric limits or fewer required outcomes; the existing sizing guidance remains authoritative for depth. (agent's call)
- **AS-15** — The owner's questions preserve parallel planning while clarifying graph-owned routing from the story's size; runtime changes should supply only the missing supported capabilities, with no second sizing system or independent Go routing policy. (agent's call)
- **AS-16** — Graph-owned branching and runtime field access are separate concerns: expose the existing story size to supported graph comparison rather than hard-coding the workflow branch in Go; exact comparison syntax and execution mechanisms remain technical planning decisions. (agent's call)
- **AS-17** — S-01's large estimate applies to the complete concurrent-planning scope and does not imply that exposing size to an if activity is itself a large change; technical planning must establish the smallest necessary runtime extension and reuse existing approval mechanisms. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
