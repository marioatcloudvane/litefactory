# Standards exceptions

`litefactory standards exception` records temporary departures from one published
standard revision for one exact task in a registered repository. These records
never change factory permissions, enforcement, owner waivers, gates or retained
run guidance. The CLI records and inspects agreements; it does not apply them to
agent runs. Task references are checked locally, without checking remote issue
existence.

The referenced published standard must explicitly permit task exceptions for the
repository and task, with a positive maximum lifetime. Missing, denied or
unsupported policies grant no permission. An exception retains both the database
repository identity and its forge/owner/name; a rename does not silently retarget
it. Later standard revisions and related tasks inherit nothing.

A standard may optionally include a structured policy in its normal reviewed
book proposal. For example, this field permits separately approved exact-task
exceptions in any registered repository for at most one day:

```json
{
  "exception_policy": {
    "version": 1,
    "mode": "allow-task",
    "all_registered_repositories": true,
    "max_lifetime_seconds": 86400
  }
}
```

Use `repository_ids` instead of `all_registered_repositories` for an exact
repository allowlist, and optional `task_refs` to narrow eligible tasks further.
An explicit `{"version":1,"mode":"deny"}` or an absent policy denies exceptions.
Policy changes require the standards book's separate review, approval and
publication; they never rewrite an existing revision or exception agreement.

Create a JSON file containing the complete terms:

```json
{
  "book_revision": 3,
  "standard_id": "checks",
  "repository_id": 1,
  "task_ref": "github:example/project#42",
  "departure": "Defer the optional slow check.",
  "reason": "The check infrastructure is unavailable.",
  "expires_at": "2026-11-01T15:00:00Z"
}
```

Choose a future absolute RFC3339 expiry within the published policy lifetime,
measured from proposal creation. Times are normalized to UTC milliseconds.
Replace the example identities and date with your actual terms.

```sh
litefactory standards exception propose --operation-key checks-outage-42 --file exception.json --json
litefactory standards exception review EXCEPTION_ID
litefactory standards exception approve EXCEPTION_ID --digest REVIEW_DIGEST
litefactory standards exception show EXCEPTION_ID --json
litefactory standards exception list --limit 50 --offset 0 --json
litefactory standards exception revoke EXCEPTION_ID --reason 'Check infrastructure restored.'
```

Use `--file -` for stdin. Commands are noninteractive; inspection also supports
text output. Mutations use the configured numeric owner, with local machine
access implying that owner. There is no actor override. Proposal creation grants
nothing. Review the complete standard, policy, task, departure, justification,
expiry and digest before separately approving the exact ID and digest.

Terms cannot be edited or extended. Changed terms need a new proposal and fresh
approval. An optional `replaces` proposal reference records lineage only; revoke
the previous agreement explicitly. Approval never extends expiry. Revocation is
terminal and can also be recorded after expiry.

Inspection retains approval and revocation facts separately from current
effectiveness and reports its evaluation time. Unapproved, expired, revoked or
unverifiable agreements grant no permission. Missing or changed repository
identity and failed revision verification leave retained history inspectable,
with effectiveness unavailable rather than using current policy as a fallback.

After lost output, inspect the same ID or retry the same operation. Reusing a
proposal operation key with identical terms recovers the same proposal; different
terms conflict. Approval and revocation retries recover retained events and
current status, without reactivating an exception. Success is returned only after
commit. On a storage conflict or uncertain response, inspect before retrying.

SQLite WAL with `synchronous=NORMAL` preserves consistency, but power loss can
lose recent committed approvals or revocations. Ordinary process recovery does
not protect against disk loss or administrator edits. Correct host UTC clock
operation is required; clock or database manipulation is outside these guarantees.
