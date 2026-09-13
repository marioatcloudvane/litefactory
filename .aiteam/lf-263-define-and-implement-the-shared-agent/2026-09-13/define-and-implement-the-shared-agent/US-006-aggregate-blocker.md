# US-006 final aggregate blocker

Frozen story #271 revision
`86f049513951db118b266e66a35d9205ba7256e20434e0dbcdfabd7b2abf839b`.

The actual final `make check` exited 2. Formatting, vet and pinned lint passed;
all test packages passed except internal/teamsource. The final output is retained
in `aggregate-make-check-final.log`; its four failing top-level tests are:

- `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`
- `TestCodexSkills_SharedSourceParity`
- `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`
- `TestCodexSkills_AC003GuardClausesPreserved`

These are existing installed-instruction parity checks, not the new source-skill
tests. The new source capture/render, all-mode/outcome/question examples and pinned
exchange tests passed; see US-006-focused-tests.log. No further test was required
to diagnose the deterministic installed-byte mismatch.

## Unchanged baseline evidence

Read-only `git rev-parse HEAD` returned:
`82bd0dea62dc10d286f037126bd7f6b845424b6c`.

Executed:

```sh
git diff --exit-code HEAD -- .codex .claude teams/source/skills/planning-contracts/SKILL.md internal/teamsource/aggregate_instruction_parity_test.go internal/teamsource/codex_instruction_parity_test.go
```

Exit code 0; output empty. All failed assertion definitions, source text and
installed target trees therefore remain unchanged from the pre-implementation
HEAD. Representative targets and tests were also confirmed tracked using
`git ls-files`.

The test helper `readRepoFile` in
`internal/teamsource/codex_instruction_parity_test.go:25` reads fixed repository
paths with `os.ReadFile`; the assertions normalize whitespace and compare literal
text. None reads the new agent-finalization skill. Identical assertion/input bytes
establish that the mismatches already exist in that HEAD; this is a file-based
baseline diagnosis, not a separately executed full baseline checkout test.

## Concrete mismatches

- `internal/teamsource/codex_instruction_parity_test.go:151` expects the exact
  handoff beginning “for the persisted content/submission” and continuing with
  “absolute preflight/submit/receipt helper” and further detailed sentences.
  `.codex/skills/requirements-engineer/SKILL.md:22` and
  `.claude/agents/requirements-engineer.md:9` instead contain the shorter handoff
  beginning “for the lifecycle” and referring to the same instruction manifest.
  The two handoff tests report this across nine roles in each applicable tree.
- `TestCodexSkills_SharedSourceParity` at
  `internal/teamsource/codex_instruction_parity_test.go:98` requires verbatim
  planning-contracts paragraphs in four installed planning roles. For example,
  `.codex/skills/requirements-engineer/SKILL.md:33` rewrites the scope paragraph
  and omits the full typed serialization paragraphs expected from
  `teams/source/skills/planning-contracts/SKILL.md`.
- The guard test expects “Successful process exit alone is not acceptance.”
  `.codex/skills/go-code-reviewer/SKILL.md:39` instead says
  “passed, and successful process exit is not acceptance.” The literal sentence
  check fails despite that nearby instruction expressing the related boundary.

The final aggregate log lines 40–118 retains the concrete failure output;
lines 120–125 show the failed package and final exit. Long package runtimes were
not the final failure: turn passed in 1086.073s and daemon in approximately 666s.
The earlier 540s orchestration wrapper timeout was separately superseded by this
actual completed make check run. Makefile permits 20 minutes per race-test package.

## Scope and authority limit

Frozen US-006 explicitly limits work to shared source and temporary rendering,
excluding installed .claude/.codex trees, active-role dependencies and installed
parity obligations. The current sandbox additionally exposes .codex as read-only.
Repairing the installed trees or changing existing parity policy is a separate
scope/permission decision. No authorization exists to weaken tests or reinterpret
this failed aggregate gate as passing. No code, installed instructions or tests
were changed during diagnosis, and no further aggregate rerun was attempted.

US-002 through US-005 have recorded engineering checkpoints. US-006 source work
and scoped checks are delivered, but its required aggregate completion criterion
is not met. Parent reports blocked contribution durably retained under request
`0ce19b664e702c1fafaa756d19bc14ec`: input valid, engineering rejected, acceptance
not applicable, remote effects pending. This is a blocker receipt, not engineering
completion, acceptance or publication.
