# US-002 unit-test evidence

Frozen revision `c4ce0a3f1b6da01dace95d5b9961aa1cd9f5897cd65b514b8dda99a2404577ff`.
Production phase was followed by an explicitly authorized bounded test phase in
the same worker because the factory tool could not allocate another test thread.
No acceptance/code/architecture/plan review was performed.

- AC-001: strict malformed, null, duplicate, unknown, wrong-type, pointer/index
  cases; exact/first-rejected byte, depth and operation bounds; schema vocabulary,
  edit matrix, byte-copy parity and parser tests.
- AC-002: all six operations; recursive omission preservation; explicit clearing;
  missing/required/duplicate/conflicting identities; reorder; retirement via item,
  full collection and parent replacement, including retired nested child IDs.
- AC-003: atomic coordinated story and criterion/task graphs, cycles and dangling
  unchanged siblings; complete-merge rollback including retirement; copied result,
  projection and retirement boundaries; typed operation diagnostics.
- AC-004: 15 mode/outcome round trips; five versioned sparse-update fixtures;
  rough Plan criteria then architecture preserving sibling identity/content;
  valid draft completion and invalid incomplete/completed merges; stale revision
  expected/current fields with only authorized target disclosure.
- AC-005: nil grants deny, narrowed item grants cannot read siblings/collections,
  wrong-mode and immutable targets reject, reference proof is revalidated,
  new Validate revisions preserve the old assessment byte representation.

All targeted tests passed, zero failures. Final full-package race/coverage run:
89.8% of package statements; retained output `US-002-unit-tests.log`.
Synthetic pure inputs establish offline contract behavior only. Durable history,
real evidence access, production adoption and lifecycle decisions are not claimed.
