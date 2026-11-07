topic: workflows
needs: brainstorm_open brainstorm_get brainstorm_entries brainstorm_scans brainstorm_add brainstorm_ask brainstorm_scan_get brainstorm_draft brainstorm_drafts_get
job: Work out an idea in a brainstorm
chain: brainstorm_open brainstorm_add brainstorm_ask brainstorm_scan_get brainstorm_draft brainstorm_drafts_get

## Workflow: work out an idea in a brainstorm

Use this when the owner wants to think a feature through. A brainstorm only reads: it writes no code, edits no issues and approves nothing.

1. Talk first: the goal and who it helps, the scope and the non-goals, then the open questions, one at a time.
2. Call brainstorm_open for the repository. It opens a new brainstorm or resumes the open one. On a resume, page brainstorm_entries and brainstorm_scans to the end so you forget nothing, and do not ask again what is still waiting or running.
3. Keep what the conversation settles with brainstorm_add: ideas, decisions, open questions, related issues, and notes from a shared document (always with the document's name).
4. Send every factual question about the repository to brainstorm_ask yourself, one question per call, without asking the owner first. Each one is a paid read-only run on the owner's standing consent and needs no yes: say briefly that you are checking and what it costs when known. Read the answer aloud with brainstorm_scan_get.
5. When the owner says it is ready, call brainstorm_draft. It is one more paid read-only run with no yes needed; say so first. Then read the drafts with brainstorm_drafts_get: title, size, impact, any possible duplicate (a warning, never proof).
6. Ask the drafter's open questions one at a time and keep each answer with brainstorm_add. The owner may drop any draft.

Nothing is created from a brainstorm alone. Turning drafts into tasks is a separate step, described in the feature request workflow when this connection can do it.
