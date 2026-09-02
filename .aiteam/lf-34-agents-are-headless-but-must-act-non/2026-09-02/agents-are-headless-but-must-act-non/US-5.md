# US-5 — implement-orchestrator.md Phase 2: env-var-based test setup

> As the implement-orchestrator running Phase 2 (test-environment setup), I
> want to resolve the test environment from environment variables instead of
> scripting a live conversation with the user, so that Phase 2 does something
> an unattended run can actually do.

This story rewrites implement-orchestrator.md's Phase 2 from a scripted
back-and-forth over which test environment to use into environment-variable
resolution, preserving the existing production-URL refusal.

## Why This Matters

Phase 2 was one of the two clearest offenders the research audit found: it
currently reads as a live dialogue, with quoted example lines asking the user
which test environment to use and waiting for an answer — something that
cannot happen mid-run. The fix is not just wording, because the phase's whole
premise (a conversation) has to become something else (reading configuration
that was already set before the run started). The risk in getting this wrong
is real: a test environment that resolves incorrectly could point integration
tests at a production system, or a poorly worded fallback could cause the
agent to quietly invent a URL. The rewrite keeps the safety property the
current phase has — never test against production — while making the
resolution itself something a headless run can actually perform.

## Architecture Notes

- **Must execute last in the serialized chain** (US-4 → US-3 → US-5) against
  `implement-orchestrator.md`.
- Resolve the test environment from `TEST_BASE_URL` and `TEST_DATABASE_URL`.
  If both are set and neither matches a production-shaped pattern, proceed.
- **Apply the existing production-pattern refusal and safe-pattern allowance
  to env-supplied values exactly as they applied to typed ones** — same
  lists, same refusal, no "the operator set it deliberately so it must be
  fine" reasoning. An unsafe env value is refused and escalated per US-4, not
  accepted with a warning.
- If no usable non-production environment is available, skip integration
  tests, continue with unit tests, and record the skip and its reason via
  US-4's `## Escalating` block — **name the variable that was unset or
  unsafe, never print its value.** `TEST_DATABASE_URL` conventionally embeds
  credentials, and the final message becomes a public issue comment.
- Preserve the fresh-database setup-only path and the existing pointer to
  the environment-validation hook script and skill doc — this story removes
  the human-conversation layer, it does not remove or duplicate the
  independent environment-variable-based enforcement that already exists
  alongside it.
- Note for whoever implements this: the phase's production-pattern list and
  the enforcement hook's pattern list are not identical today (the hook also
  catches a couple of patterns the phase's prose list doesn't, and vice
  versa). This story does not reconcile them — retain the phase's list
  verbatim — but record the observation in the final report; reconciling the
  two is separate work.
- Platform: none — no Go file in scope; the hook script referenced by this
  phase is read, not modified.
- Risk (security): the primary risk in this story is a credential leaking
  into a public comment via the escalation path — treat AC-4 below as
  load-bearing, not optional phrasing.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Replace the scripted dialogue with `TEST_BASE_URL` / `TEST_DATABASE_URL` resolution and its resolution order | infra | US-3 complete | M |
| T-5.2 | Apply the existing production-pattern refusal and safe-pattern allowance identically to env-supplied values | logic | T-5.1 | S |
| T-5.3 | Define the no-usable-environment path: skip integration tests, continue with unit tests, escalate per US-4's `## Escalating`, naming the variable but never its value | logic | T-5.1, US-4's block shape | S |
| T-5.4 | Preserve the fresh-database setup-only path and the pointer to the environment-validation hook/skill doc | infra | T-5.1 | XS |
| T-5.5 | Verify no credential-shaped value can reach the report; record the phase-vs-hook pattern-list divergence as an observation in the story's own notes, not as a fix | test | T-5.3, T-5.4 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | Phase 2 no longer contains a scripted example dialogue asking the user which environment to use. |
| AC-2 | Phase 2 resolves the test environment from `TEST_BASE_URL` and `TEST_DATABASE_URL`. |
| AC-3 | The existing production-pattern refusal and safe-pattern allowance apply to env-supplied values exactly as they applied to typed ones — no weakening for env-sourced input. |
| AC-4 | When no usable non-production environment is available, Phase 2 skips integration tests, continues with unit tests, and records the skip via the `## Escalating` block, naming only the variable — never its value. |
| AC-5 | The fresh-database setup-only path and the pointer to the environment-validation hook/skill doc are preserved. |
| AC-6 | The story's final report notes the divergence between the phase's production-pattern list and the hook's pattern list as an observation, without attempting to reconcile them. |
| AC-7 | This story's edit lands last in the implement-orchestrator.md chain, after US-4 and US-3. |
| AC-8 | `make check` passes; `git status --short` for this story's commit shows only `.claude/agents/implement-orchestrator.md` changed. |
