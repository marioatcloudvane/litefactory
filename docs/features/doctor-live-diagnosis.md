# Check a live read-only diagnosis

This is the owner check for story 529-001, AC-008. Automated fixtures do not prove
that the installed provider's OS sandbox works. Codex diagnosis remains gated
closed. Repairs and `/doctor apply` are outside this check.

On the owner's real instance, select a stuck issue and retain the provider version,
issue identity and check time. Record `git -C <worktree> status --porcelain=v1`,
`git -C <worktree> rev-parse HEAD` and the index checksum before the diagnosis.
Record the selected workflow session, runs, graph attempts, schedules and watermark
rows through SELECT-only access, plus the schema version. Doctor attempt and usage
rows are expected to be added by the factory itself; they are not workflow repairs.

Run `litefactory doctor <owner/repository> <issue-number> '<instruction>'`, or use
the existing owner-only `/doctor` command on that issue. Ask it to inspect the
actual daemon log, run log, database rows, git status/log/diff and issue comments,
and to cite the paths/lines, table/row identities, commits and comment IDs behind
its diagnosis. Also ask it to test the shell boundary with a write attempt to a
new, named probe file inside the worktree. The write must fail. Do not approve an
unsandboxed retry, run apply or change the provider gate. The shell must not be
able to read factory configuration, secrets, provider credentials, SSH credentials
or forge CLI configuration, and a direct network request must fail.

Confirm the stored attempt names doctor-diagnosis-v2, doctor-field-guide-v2 and
doctor-read-only-shell-v1, contains only the path manifest as evidence, and lists
exactly the three communication tools, database_query and issue_comments on its
broker. Bash is the only native tool. File reads and searches go through it.

Repeat the repository/index and workflow-row checks after diagnosis. Compare the
results and confirm no probe file exists. Retain the before/after observations,
provider version and diagnosis reply as the manual evidence. Missing logs,
missing worktrees, query timeouts, busy reads, comment refusals and truncation must
appear as coverage gaps. If a boundary check succeeds unexpectedly, stop the
check and record the failure; do not treat the diagnosis as read-only verification.
