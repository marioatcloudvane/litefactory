# Factory application ownership

The factory remains one Go module and one process with the existing SQLite
schema. `pkg/execution` supplies neutral execution mechanics. Factory decisions,
permissions, captures and durable results belong above that library. Optional
Discord and remote access do not supply authority for ordinary ticket execution.

## Contracts and composition

`internal/factory/contracts` contains shared request and read values without
storage or transport imports. Focused implementations hold their database and
existing domain services privately. CLI handlers retain parsing, rendering and
exit mapping; local services remain callable with the daemon stopped.

| Operation family | Decision owner | Consumer contract |
| --- | --- | --- |
| Published health | Factory health service | Freshness, heartbeat and worker snapshot |
| Discovery cadence and in-flight sessions | Factory runtime | Atomic admission, cancellation, settlement and due-time observations |
| Initialization and service lifecycle | Factory local administration | Local bootstrap/start/stop/status operations and safe results |
| Worktree cleanup | Factory maintenance | Exact task selector, removal result and typed refusal |
| Rights and reviewers | Factory local rights | Opaque local repository scope with bounded operations |
| Teams and repository bindings | Factory local teams | Package operations and locked binding updates |
| Project connections and approvers | Factory local projects | Immutable connection versions, bindings and owner appointments |
| Reviewed owner actions | Factory owner-action domains | Resolve, recheck, execute and receipt recovery using verified principals |
| Action schemas and availability | Factory owner-action catalogue | Fixed argument validation and discovery from trusted installed-capability facts |
| Feature drafts | Factory feature draft service | Protected preview generation, identity, hash, expiry, replay and creation fences |
| Remote authentication and consent | Connector state adapter | Browser, token, grant and protocol transitions only |
| Remote factory target access | Trusted connector access adapter | Current grant checks and purpose-specific retained targets |
| Remote read projections | Existing overview, task/code evidence and usage services | Shared factory read contracts, redacted bounded pages |

Daemon composition supplies providers, verified identities, clocks and primitive
effect hosts. The remote server receives bounded owner-action, workflow, stage
and advisory interfaces; it cannot run workers, register domains or recover
arbitrary effects through those interfaces. Authentication, access and local grant
control must bind to the same backing store. Independent adapter instances for
that same store remain compatible.

The registered CLI handlers now delegate factory decisions and durable effects
to the local administration, project, team, rights, usage, standards, analysis,
diagnosis and maintenance services. Parsing, output and dependency composition
remain in the CLI. Daemon discovery and in-flight wrappers call factory runtime
operations, including verified project ticket admission and owner-authority claims;
existing turn and daemon workflow mechanics remain trusted core
implementations hosted by the daemon.

Messaging resolves binding owner actions through factory owner-action domains
and reads task status and project context through factory projections. Ask's
project view contains identity and base-branch presentation metadata, with no
workspace paths or connection authority. Its production dependency guard rejects
direct storage, daemon, connector and live-attempt execution dependencies.
Messaging receives its own persistence port for conversation and delivery state;
remote authentication and consent use the connector persistence adapter.
These are source ownership boundaries. The acceptance review must assess the
remaining trusted host wiring and behavior evidence independently.

Changing-action validation and discovery use fixed schemas in
`internal/factory/owneractions`. Messaging supplies installed-command facts;
it cannot replace required fields, scalar types or input budgets. Resolve and
recheck call the factory validator directly, without callbacks into messaging.
Remote action composition constructs the same factory domains without creating
a messaging service. Existing authentication and approval checks still apply.

`internal/factory/featuredraft` owns project selection, pending-preview checks,
isolated response validation, protected preview identity and content hash,
confirmation code, expiry and durable save. Composition supplies registered
projects, instruction content, isolated generation, protection, history and
persistence capabilities. The messaging draft adapter translates those values
and presents the factory result; it makes no preview admission or save decision.

## Persistence and transactions

No schema, migration, connection revision, capture digest, authorization generation
or pending-delivery encoding changes are intended. Existing store transactions
remain the coordinator for conversation/capture creation, provider replacement
with drain checks, ordered delivery acknowledgment, owner outcome plus notices,
grant-checked action/advisory admission, and report completion plus watermarks.
Network effects remain outside those transactions. Port extraction must never
replace one compound transaction with sequential independent writes.

Messaging retains conversation sessions, channel bindings and delivery
acknowledgments. Connector state retains authentication, consent and protocol
sessions. Factory workflow state, schedules, locks, attempts, results,
publication intent, retained routing and binding decisions remain factory-owned.
A shared physical database does not grant a consumer general table access.

## Compatibility bridges

| Bridge | Owner and purpose | Removal condition |
| --- | --- | --- |
| Daemon discovery and in-flight wrappers | Factory runtime; preserve existing host calls while retiring the old policy body | All host callers use the runtime contract directly |
| Messaging health value aliases | Factory health; preserve existing presentation type names over neutral health contracts | Consumers use neutral contract names directly |
| Pending owner-action payload structures | Factory owner-action domains; preserve exact saved natural/domain JSON | Supported proposals drain, or a reviewed compatible conversion exists |
| Messaging invocation and command aliases | Factory owner-action catalogue; preserve existing command registration and saved invocation field names | Consumers use factory catalogue values directly |
| Messaging feature draft adapter and helpers | Factory feature draft service; adapt instruction/history/model capabilities and preserve existing presentation calls | All callers use factory drafting and helper names directly |
| Store and messaging preview aliases | Factory preview contract; preserve saved fields, confirmation identity and creation fences | Persistence callers use the factory contract directly with recovery coverage |
| Ask project and task lookup adapters | Factory target selection and task facts; project and task views preserve conversation presentation without storage rows | Legacy Ask entry names and workflow compatibility values are no longer needed |
| Store conversation and inbox value aliases | Messaging owns conversation/delivery values; factory owns inbox views; aliases preserve existing store callers and encodings | Internal persistence callers adopt owned value names with unchanged-state recovery coverage |
| GitHub workflow shapes and Linear local slots | Factory ticket bridge; retain current workflow serializers and qualified provenance | Workflow serializers use qualified ticket values with recovery proof |
| Store connector value aliases | Connector state; retain existing source compatibility and serialized fields | Storage callers use connector-owned state names directly |
| CLI helper wrappers | Local administration services; retain existing local host/test composition | All callers use the owned local operations directly |

These bridges translate values or compose dependencies. A service refusal must
never fall back to a previous policy implementation.

## Availability, receipts and recovery

The [complete registered CLI inventory](factory-cli-inventory.md) records the
stopped-daemon behavior and existing compatibility fixtures. Installation,
configuration, grant administration and lifecycle commands remain local.
Live-attempt submission requires its own descriptor and broker capability;
remote authentication or consent cannot replace it. Actions requiring confirmed
Discord approval remain unavailable or pending when Discord is disabled.

A durable result receipt and an external publication acknowledgment are separate
facts. On a lost reply, recover the existing request and receipt before retrying.
An uncertain send remains owed until authenticated evidence confirms the effect;
recovery does not authorize another paid execution. Retained ticket and code
connections must be used rather than current configuration. Ticket owner approval
never implies GitHub code-delivery authority.

For rollback, use the existing process ownership and service lifecycle to stop
admission and drain owned work before selecting compatible previous code against
the unchanged state. Do not delete pending records, alter captures or substitute
approval channels. Restart existing reconciliation to recover interrupted runs
and pending deliveries. The retained pre-change fixture was written by the pinned baseline store before
the ownership cutover. Its test checks protected proposals, grant revocation,
connection versions, captures and uncertain report completion. The offline
rehearsal opens a temporary copy with baseline, current, then baseline code,
checking the saved values each time. Commands and scope are documented beside
[the fixture](../internal/store/testdata/factory-pending-state/README.md).
This exercises storage compatibility; it does not rehearse a live process
stop/drain or establish recovery for every historical pending-state shape.

Provider deduplication is bounded. Preserve the current per-channel ordering,
stable nonces, authenticated delivery observations and retry/refusal rules. An
ambiguous approval send authorizes nothing. Controlled provider fixtures cannot
prove uninterrupted live delivery or indefinite exactly-once publication.

## Evidence scope

`TestFactoryCoreTicketMainPathWithoutOptionalTransports` runs the same main
path for GitHub and Linear using controlled HTTP providers and a controlled paid-execution boundary.
It exercises verified ticket admission, qualified owner identity propagation,
live-attempt broker preflight/submission/receipt, restart with an uncertain
publication, and recovery using retained ticket/code connection versions after
current configuration changes. Recovery must confirm the existing publication
and watermark without another execution or provider post. The receipt's request
identity and accepted payload must survive the reopen.

The fixture applies the Plan owner-response state transition directly; it does
not run the graph runtime, full daemon setup or a paid provider. Existing graph,
authority, scheduling, lock and restart suites supply separate coverage. Tests
under controlled providers cannot certify live service delivery guarantees.
Source fixtures and reproducible commands are not claims that a particular run
passed; the worker's recorded test results supply executed evidence. The full
repository check remains a Validate obligation.
