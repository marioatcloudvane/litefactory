# Session manifest

- Branch: lf/265-add-a-scoped-mcp-interface-for-in-flight
- Session date: 2026-09-13
- Feature: add-a-scoped-mcp-interface-for-in-flight

## Modes

- [x] Research
- [ ] Plan
- [ ] Implement
- [ ] Validate
- [ ] Spike

## History

- 2026-09-13: Research session initialized for issue #265, run 0001.
- 2026-09-13: Research completed at revision 6e52c3e8c0953a60509e65a2a64d2b9259c7bbbb. RESEARCH_BRIEF.md records the communication matrix, scoped MCP recommendation, current dependency gaps and verification limits. No implementation or issue publication performed.

## Notes

- Default communication is the end-of-run JSON response; investigate justified in-flight MCP needs.

## Run 0002 continuation

- 2026-09-13: Bounded Research complete at 20ff75952c1253bca4db739406f607821a5248f2; focused eight-file evidence unchanged from run 0001. RESEARCH_BRIEF.md now leads with five positive MCP needs and discussion tradeoffs; the earlier evidence remains retained.
- Settled: no real MCP implementation before JSON rework is complete; terminal JSON remains the default.
- Open product preferences: owner-facing status presentation and primary benefit of early issue/substory proposals. These are discussion choices, not blockers to completed bounded research. Suggested next discussion is the proposal scenario; no implementation or Plan was started.

## Run 0003 continuation

- 2026-09-13: Bounded Research complete at 5028d988a9187383cd67136d4d8eab5f37bde9f3. Checked official MCP/Claude/Codex documentation and focused unchanged repository owners. Added request/result explanation, local stdio setup diagram, publication batch semantics and five tool families to the brief.
- Settled: Research can request actual creation of smaller issues, exemplified by owner-reported #258 to #263/#264/#265. Supersedes draft-only framing and resolves run 0002's question about the primary proposal benefit. Litefactory publishes; no real MCP implementation before JSON rework completes.
- Tentative: bugs/follow-ups from additional modes. Open details: taxonomy, eligible mode/type combinations, relationship defaults, names and activity presentation. No required research questions; no issues created or implementation performed.

## Run 0004 continuation

- 2026-09-13: Bounded Research complete at c6efb21fce3152bc2a15cb3a85e9a98dcf14b448. Added Issues list (open/closed), get by ID and comment by ID; both reads initially return title and optional mode labels. Recorded future Research relation discovery as motivation.
- Recommended, not silently added to accepted content: a list targeting ID/opaque handle, bounded pagination with open default, and current-repository scope with explicit cross-issue comment capability. Distinguished repository issue number from database ID and durable comment receipts from remote confirmation.
- JSON-before-implementation prerequisite and prior actual-publication direction preserved. No clarification required; no live issue reads, comments, publication, implementation or tests performed.

## Run 0005 approval

- 2026-09-13: Owner approved the run 0004 proposal (“yes, approved!”). Research complete. Approved Issues list/get/comment, open/closed filtering with open default and pagination, repository-local issue-number targeting in list/get/comment, title plus optional mode labels for reads, and factory comment publication with durable receipts/retry reconciliation.
- Current-repository Research relation discovery and authorized target comments recorded as approved direction. Tentative other-mode eligibility and bug/follow-up taxonomy remain open; no broader approval inferred.
- JSON-before-MCP-implementation prerequisite remains. Artifact-only continuation; no implementation, tests, fresh source/web investigation, issue creation or commenting performed.

## Run 0006 sanity check

- 2026-09-13: Owner asked to sanity-check the approved brief against current repository state. Research complete; result is a confirmation, not a correction.
- Verified no source drift: this branch's HEAD (3d39ca200edf7348ff7efa856fc51c4b1fd4620a) is five commits ahead of origin/main (still at 6e52c3e), and those five commits touch only this session's `.aiteam/` files — `git diff` for `internal/`, `docs/` and `.aiteam/APP_CONSTRAINTS.md` between the run 0001 revision and HEAD is empty.
- Re-read every load-bearing file:line citation (issues.go, comments.go, followups.go, agent/mode.go, daemon/mode.go, submission/broker.go, submission/client.go, cli/cmd_submission.go, agent/story_progress.go, turn/typed_plan.go, turn/submissions.go, store/submission_progress.go, store/followups.go, harness/runner.go, codex/runner.go, APP_CONSTRAINTS.md CONC-9) against the live files; all match the brief's description.
- Confirmed no MCP-adapter code and no JSON-rework/terminal-envelope groundwork exists anywhere in the tree, so the owner's implementation-blocking prerequisite is still correctly unmet.
- No corrections were needed to the approved direction; no clarification required; no source modified, no issues read/created/commented on, no tests run, no implementation performed.
