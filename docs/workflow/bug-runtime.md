# Simplified Bug workflow

The supported Bug package contains one graph: an owner conversation with the
configured read-only flagship analyst, explicit diagnosis approval, one fixer
invocation, then factory finalization. DOT declares each route. The fixer repairs
and performs required validation inside its invocation; the factory does not run
a test child or a validation recipe. Agents return application data through the
shared preflight, submit and receipt route. Terminal text supplies no authority.

Every stopped origin retains its reason and a durable owner question. An
authenticated ordinary reply resumes that origin once with guidance, including
one further bounded invocation after exhaustion. Guidance grants no repair
approval. Only the exact `/approve-bug analysis <revision>` command for the
current confirmed diagnosis review authorizes repair. Normal explicit task
cancellation remains available; special Bug grant and test-retry commands are
retired.

Eligible agent preparation preserves unfinished edits in a local unvalidated
checkpoint when needed, then uses the configured synchronization policy.
Validation identifies approved scope, complete source, target main, relevant
inputs, commands, outcomes and stable logs. Factory finalization saves only that
tested content, confirms non-force push, correlates or creates one PR, merges
with the expected-head guard, independently confirms integration, then confirms
issue closure. There is no second Bug owner review. Uncertain effects are
observed before retry; merged but unverified work cannot authorize closure,
another merge or repair. Stale work first parks at finalization; a claimed owner
reply releases reconciliation before its DOT-declared revalidation route.

## Upgrade and retained history

Migration 0096 makes `bug_tasks`, `bug_analyses`, `bug_repairs`, `bug_checks`,
`bug_delivery` and `bug_events` read-only in place. Rows, columns, keys,
relationships and embedded evidence survive unchanged. INSERT, UPDATE and DELETE
are refused; ancestor cleanup must preserve these records. The normal startup
migration boundary applies each SQL file transactionally, and a failed archive
migration remains unapplied. Historical migration files and applied-version
records are retained.

Fresh databases and empty older installations follow the unchanged historical
migration chain. Current installations where 0079 already applied preserve every
surviving record. A populated older installation awaiting destructive migration
0079 is refused before that migration executes and requires a separately reviewed
preservation upgrade. This does not restore history previously deleted by 0079.

Pinned unsupported legacy Bug definitions refuse dispatch without upgrading,
restarting, cancelling or reclassifying them. Their recorded runs, graph attempts
and held session state are excluded from automatic orphan repair. Issues,
discussions, branches, edits, PRs and prior effects remain owner work; owners may
repost issues separately. Historical stored evidence is inspectable, never new
approval or delivery authority.

Spike retains the shared check isolation and pinned recipe configuration.
Checkpoint, canonical source observation, graph waits, atomic attempts,
validation generations and effect reconciliation remain active shared services.
