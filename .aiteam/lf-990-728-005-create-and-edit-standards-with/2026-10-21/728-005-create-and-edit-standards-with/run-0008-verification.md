# Run 0008 verification

This run inspected the current branch and source and used three parallel subagents for store, service and CLI review/repairs. Earlier attempt reports were not used as proof.

Repairs verified in this run:

- Registered authoring records as retained global archive state alongside the shared standards book.
- Kept removed context filters unresolved through validation and completion after rebase.
- Preserved original saved input when context exceeds the prompt limit, without recording unsent bodies or coverage.
- Raised the bounded serialized session allowance to 64 MiB so a valid retained book and large original edit target/draft can be saved together; documented the limit.
- Exposed saved attempt diagnostics and provenance in text inspection.

The initial complete run found four archive-registry failures in store/daemon. Those failures were repaired and the final complete runs passed. No failing tests were removed or weakened.

Final checks against the unchanged 28-file source manifest:

- Complete package checks: 27 packages, 25 with tests and two compiled with no test files. Store, CLI, turn and daemon ran without race instrumentation; the other 23 used race instrumentation. No test-name filters were used.
- The single CLI end-to-end test passed for create/edit, reopened question/answer state, operator edits, exact proposal handoff, repeated completion and absence of authoring approval/publication.
- The retained-context/rebase regressions passed, including an 11 MiB original edit target and oversized-context narrowing recovery.
- Repository compilation passed. The pinned linter passed on all five changed Go packages with zero issues. Formatting and diff whitespace checks passed.
- Source hashes were checked again after all runs. Full-suite testing and acceptance review were not performed.

Evidence contains exact commands, complete compressed final logs, package results, initial failure names and source hashes. Checks used temporary local fixtures, not production. No commit, stage, push, approval or publication was performed by this worker. Acceptance remains for Validate.
