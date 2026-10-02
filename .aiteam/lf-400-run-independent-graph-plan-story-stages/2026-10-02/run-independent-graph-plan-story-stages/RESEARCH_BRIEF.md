# Research brief

## Request

Investigate parallel execution of independent graph Plan story stages, including story, technical/architecture and acceptance drafts, using settings.max_concurrent_runs as the capacity boundary while preserving ordering, exact approvals, workspace safety and recovery.

## Decisions

- **D-1** — The owner's latest instruction makes the existing config.yaml global concurrency limit the capacity boundary; it supersedes the issue note's suggestion of an additional configurable per-parent cap.
- **D-2** — This shaping pass defines the proposed scope for owner review without implementing it or changing workflow mode.
- **D-3** — The primary user outcome is that approving several independent stories allows their next story, technical/architecture or acceptance stages to overlap whenever global capacity is available.
- **D-4** — Each active story has at most one running stage attempt, while story-set proposal, shared-scope changes and final Plan assembly remain coordinated parent operations.
- **D-5** — Every agent stage consumes one global run slot, and a parent may use all spare slots without an additional parent limit or permanently reserved idle capacity.
- **D-6** — At stage boundaries, ready parents and other issues receive fair rotating admission before a parent takes another slot, with stable ready-story order within each parent and no preemption of running agents.
- **D-7** — The retained model identifies the parent, stable story and child issue, stage, attempt, execution ownership generation, input revisions, candidate, approvals, review wait, consumed replies, effect receipts and isolated workspace.
- **D-8** — Story results are conditional updates of their own story and declared affected dependants, retaining whole-Plan revision history without overwriting sibling changes.
- **D-9** — An unrelated sibling update may trigger a short reload and revalidation but must not require rerunning an otherwise valid agent result.
- **D-10** — A change to the selected story, consumed dependency candidates, shared semantics or relevant issue revision rejects the stale result and queues only still-authorized affected work.
- **D-11** — Each concurrent attempt receives a private workspace and artifact area pinned to its admitted repository and Plan inputs, with parent branch writes and commits performed only by coordinated factory operations.
- **D-12** — Existing owner authentication, exact review approval commands, authority checks and child-to-parent routing remain required, and agents cannot approve work or acquire broader publication permissions.
- **D-13** — Replies are durably deduplicated and applied to their exact story, stage, wait and candidate, while replies to a running story are retained for ordered handling without blocking siblings.
- **D-14** — Restart and retry recovery retain attempt and effect identities, reject results from superseded execution owners and reconcile uncertain remote effects before repeating them.
- **D-15** — Failure, timeout, hold or cancellation of one story stops that story and affected dependants while independent siblings remain eligible.
- **D-16** — Shared-scope changes invalidate affected in-flight results and approvals before new work is admitted, while parent cancellation or loss of authority prevents further results and publication across the parent.
- **D-17** — Final assembly starts only after all required active stories have current approvals and confirmed effects, with no unresolved stage writes, and remains bound to the exact reviewed Plan set.
- **D-18** — The existing global configuration and story review/reply surfaces remain the user interface, with a parent summary listing every running story and stage plus queued, dependency-blocked, owner-waiting, failed and completed states.
- **D-19** — Existing graph Plan sessions retain candidates, approvals, waits, reply identities and effects when resumed under the new execution model, and an old active run must be reconciled before parallel admission.
- **D-20** — Validation must demonstrate actual overlapping independent agents, the global ceiling, same-story exclusion, preservation of concurrent sibling results, exact stale-result rejection, fair admission, isolated workspaces and restart-safe effects.
- **D-21** — Missing, cyclic, retired or unresolved dependencies prevent affected work and produce a specific reported reason while unrelated stories continue.
- **D-22** — Parallel Implement or Validate, overlapping stages of one story, new approval syntax, a new dashboard, extra capacity settings and changes to graph Plan admission defaults are outside scope.


## Assumptions

- **A-1** — Parallelism means independent stories may overlap; stages of the same story remain sequential. This follows the issue's explicit exclusion, and the owner's latest comment does not explicitly reverse it. Implement and Validate parallelism remain outside this request.
- **A-2** — The issue's reported setting of 5 and stage timings are owner observations, not measurements from this pass. No live config or daemon state was inspected. The repository default is 3, with supported values 1–16.
- **A-3** — Fairness is interpreted as rotating admission at stage boundaries without preemption or reserved idle slots, based on the issue's fairness requirement and the owner's rejection of an additional parent cap.
- **A-4** — The scope covers resuming existing graph Plan sessions as well as new ones because the owner describes current story-session delays; legacy non-graph Plan and deployment changes are not included.


## Open questions

### Q-1 — Should a dependent story’s first draft wait for owner approval of the upstream story text?

_Asked of: Product Owner Questions_

- **preserve** — Recommended: preserve the current first-draft rule—start after required upstream drafts exist, bind exact candidates, and invalidate affected downstream work if those drafts change; this keeps initial drafting fast without treating a draft as approved.  *(recommended)*
- **approved** — Require owner-approved upstream story text before drafting a dependant; this reduces work based on provisional text but adds an owner review wait at each dependency level.
