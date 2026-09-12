# S-4 aggregate-gate failure analysis

Analysis only. No source/test changes or additional Go tests were executed.
Input: `/tmp/lf229-make-check.log`.

## Baseline instruction mismatch

The teamsource failures predate S-4. Read-only comparison used `git show
HEAD:<path>` at `6b40dcde316a2b9bca7fd3b61fe05d83db784aea`, reconstructed
the exact expected handoff strings from the two Go test constants, and applied
the tests' blockquote/whitespace normalization to both HEAD and current files.

Observed results:

- All ten Claude and all ten Codex role files lack the exact expected handoff
  at HEAD and in the current worktree: 20 identical predicate failures.
- The reviewer guard sentence `Successful process exit alone is not acceptance.`
  is absent from the Codex reviewer at HEAD and in the current worktree.
- Both failing test files, the planning-contract shared source, and all ten
  Codex target files are byte-identical to HEAD. Therefore the reported Codex
  planning-contract paragraph failures also have unchanged inputs.
- Removing only the newly inserted Completion message section from each of
  the two Claude targets produces its exact HEAD bytes.

The exact-handoff tests expect the older expanded paragraph beginning with
`persisted content/submission`; the installed files already use shorter
lifecycle/protocol wording. For example, Claude Plan begins `Read ... for the
lifecycle and submission contract`; Codex Implement uses a `Protocol` heading
and a short workflow reference. This analysis establishes baseline literal
mismatches, not an acceptance judgment about semantic equivalence.

Fixing these failures requires changing unrelated protocol instruction content
across many roles, protected Codex files, or the existing contract tests. That
exceeds frozen S-4's two-file completion-message scope. Do not relax tests or
rewrite unrelated instructions to make this story's gate pass.

## Aggregate deadline failures

Daemon and turn each exceeded the package-wide eight-minute test deadline.
Daemon's active test at the deadline had run for four seconds
(`TestLegacyToolsLabelsAlwaysRefuseBeforeDispatch`); turn's had run for seventeen
seconds (`TestPlanRejectsInvalidCurrentHandoff`). These facts show the reported
deadline is aggregate package time, not proof that either named test hung for
eight minutes. No baseline timing reproduction was executed, so their timing
cause remains unverified. The failed aggregate gate remains failed.
