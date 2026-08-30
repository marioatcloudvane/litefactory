# Manifest
# Branch: main
# Session: 2026-08-30

## Modes

- [ ] **Research** — `RESEARCH_BRIEF.md` *(satisfied by reference — see Notes)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(done)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-30 — session created; Plan mode entered

- 2026-08-30 — Plan complete; `IMPLEMENTATION_PLAN.md` written. 6 user stories,
  33 tasks, 11 probe cases. `feature-spec.md` written by requirements-engineer
  after the user answered six scoping questions; go-architect performed a
  four-lens review and materially revised the spec.

  **Headline correction, now settled:** the research brief's own recommendation
  (`--setting-sources user`) was never a fix — the measured leak came from the
  operator's `~/.claude/settings.json`, which *is* user scope. Only dropping user
  scope closes it. The user's decision is an empty `--setting-sources` value.

  Eight planning decisions recorded as D-A1..D-A8. Four are reversals of the
  spec the architect reviewed: the `cli → harness` edge is accepted but narrowed
  to `cmd_init.go` and pinned by a policy test (D-A1); the spec's `repo add` /
  `repo remove` settings rewrite is replaced by a daemon tick refresh (D-A2);
  the absolute whole-argv pin is kept rather than replaced by a differential
  test (D-A4); `--bare` is rejected outright rather than held as a fallback,
  because it changes the auth path and would break Keychain auth on this
  deployment (D-A5).

  One security gap found and verified by inspection: `--allow-dangerously-skip-
  permissions` is a real flag on 2.1.251 and slips both today's
  `validateNoForbiddenFlag` and the extension the spec proposed, because the
  allow-variant contains only one hyphen before `dangerously` (D-A7).

  **D-A8 is left open and needs an owner answer before T-5 concludes:** whether
  to point `GOCACHE`/`GOMODCACHE` at a per-run directory rather than
  write-allowing the shared Go caches. It is contingent on probe P-4 and would
  require adding two names to the `harness/env.go` allowlist.

  Nothing implemented, nothing committed.

## Notes

Successor to `.aiteam/main/2026-08-26/tool-permissions/`, whose label→profile
feature shipped in merge commit a21d89a. This session covers the residue that
plan deferred under decision D-8: **enforcement**.

The Research gate is satisfied by reference to
`.aiteam/main/2026-08-26/tool-permissions/RESEARCH_BRIEF.md` (incl. its
2026-08-27 addendum). No new research was run; the scope here is the part of
that brief which was planned as OPTIONAL and then skipped.

Decisions carried in from the user, 2026-08-30:
- New session rather than re-planning tool-permissions.
- Option D (comment directive) remains rejected — the 2026-08-27 addendum stands.
- Bash sandbox settings are IN scope, gated on the WriteSettings fix.
