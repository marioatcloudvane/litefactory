# Discord extraction recovery fixture

`pending.db.gz` was written by the storage implementation at commit
`d41b68afd2f722003d0d12efe396801be4195367`, the story's starting commit. The
current implementation did not generate the saved database. The manifest records
values read back from that baseline, including the entire SQLite schema.

The fixture contains two Discord conversations, immutable fixture instruction
captures, native session handles and private directory identities, a completed
answer with the first of three parts acknowledged, a separate unfinished native
turn, and a protected owner action awaiting confirmation. Fixture instruction
bytes are synthetic; no shipped agent wording is pinned.

Reproduce with `python3 scripts/discord-component-recovery.py generate`.
Run `python3 scripts/discord-component-recovery.py rehearse` to perform:

1. Baseline, current and baseline reopen of the same isolated database, comparing
   schema, captures, conversation identities, turn fences, pending approval,
   saved answer bytes and acknowledgment metadata.
2. Current-code recovery of only the two remaining answer parts, rejecting
   duplicate acknowledgments and preserving the unfinished native identity and
   pending approval without creating an execution.
3. Baseline reopen of that recovered database against the recorded current
   state, proving that compatible previous code can read the recovery outcome.
4. Current component process stop/drain/restart, followed by baseline reopen of
   that same process database and comparison of its saved recovery outcome.

Each command runs a separate bounded process. Baseline code is extracted with
`git archive` into a temporary directory; the branch and worktree never change.
The regular `TestDiscordComponentBaselineRecovery` test reads this same fixture
and exercises the saved-byte recovery assertions without needing git history.

`TestDiscordComponentProcessStopDrainRestart` separately exercises the extracted
component in two real child processes against an isolated database and a local
fake Discord HTTP server. It observes the first reply acknowledgment, a 429 on
the second reply, cancellation and worker join before database close, then a new
process delivering the remaining saved bytes with the same nonce. It also checks
that admission is unavailable after drain and that native identity and captured
instructions survive. No model provider is installed, and no live credentials
or services are used.

These checks passed during implementation on 2026-11-03. They cover one known
baseline and constructed pending states, not all historical installations or
native provider state formats. The process rehearsal database is reopened by the baseline storage implementation
after both component processes exit; it does not launch a previous Discord
transport or simulate a running native subprocess. The unfinished-turn fixture checks
its recovery identity after the baseline process exits; a lease alone never
proves process exit. Provider deduplication outside its bounded nonce window and
uncertain remote effects remain subject to the existing delivery guarantees.
