# Research brief: file-based execution workflows

Status: incomplete — clarification before substantial investigation.

## Request and settled scope

Source: owner instruction for issue #230, run 0001, 2026-09-13. This is a shallow first Research pass; do not implement or start Plan.

- Explore file-defined execution graphs for every mode, including backward transitions where needed. Ship one standard workflow initially; allow other workflows later.
- Plan proceeds through interactive story descriptions and problems, prose acceptance intent followed by concrete acceptance criteria, then interactive architecture. Publish stories only after those stages are finished. Consider missing stages.
- Implement has no human gates. Move deterministic orchestration into litefactory, with implementation and test/fix iteration. Run tests tailored to stories here rather than the full suite.
- Validate starts with the full suite, then checks acceptance criteria.
- Explore workflow actions such as agent invocation and test execution, including how a test manager returns executable test instructions to litefactory.

## Unresolved decision

The instruction contains two headings named “Implement Mode.” The first describes a small interactive workflow for finding solutions; the second explicitly requires autonomous implementation without human gates. Does the first heading mean Research, or an interactive stage within Implement?

Recommendation: interpret the first heading as Research, preserving the explicitly autonomous Implement requirement. This is a proposed interpretation, not an approved assumption.

## Evidence and current behavior

Repository revision: 6e52c3e8c0953a60509e65a2a64d2b9259c7bbbb.
Only session/routing instructions were inspected. Current execution behavior and code owners have not yet been investigated. No architecture conclusions are asserted.

## Preliminary framing and risks

From the request alone, a workflow graph with explicit retry/backward transitions appears a better conceptual fit than requiring every mode to be a DAG. Acyclic story dependencies can coexist with test/fix loops. This is conceptual reasoning, not a repository finding or selected design.

The mode-label decision changes where human pauses and resumptions belong; silently resolving it would risk researching the wrong interaction contract. File syntax, execution state, action contracts, retry bounds and validation policy remain research topics, not additional questions for the owner at this stage.

## Continuation

After the owner resolves the heading, map current mode dispatch, agent orchestration, story publication and test execution at a high level. Compare a small built-in workflow interpreter with extending existing execution control; identify persistent state and action boundaries. Produce a surface-level recommendation with focused code references and tradeoffs. Do not start Plan or implement changes.
