# Reusing graph and agent execution

The execution packages are part of the existing
`github.com/cloudvane/litefactory` Go module. They can be used without factory
configuration, tickets, storage or credentials. They share the repository's
release; there is no independent API compatibility or version promise.

Run the independent example:

```sh
go run ./examples/execution
go test -run '^TestExecutionExample$' ./examples/execution
```

The example parses inline DOT using application-owned action and attribute names.
Its `collect` handler creates shipment data. The returned `ready` outcome selects
an edge to the application's `summarize` handler, which invokes a deterministic
fake agent. Its `written` outcome selects the terminal node. No provider process
starts, and no factory setup is needed.

## Package boundaries

| Package | Owns |
| --- | --- |
| `pkg/execution/graph` | DOT declarations and effective defaults, neutral action descriptions, structural checks, deterministic edge selection |
| `pkg/execution/agent` | Prepared request and result values, launch and session callbacks, failure evidence, process coordination seam |
| `pkg/execution/providers/claude` | Claude arguments, native events and execution |
| `pkg/execution/providers/codex` | Codex arguments, native events and execution |
| `pkg/execution/internal/process` | Private process lifecycle, cancellation, environment isolation, output handling and cleanup |

The graph package does not assign application meaning to DOT attributes. Parse
with `graph.Parse`, interpret your own attributes into `graph.Graph`, register
your action descriptions in `graph.Registry`, and call `graph.Validate` before
navigation. Raw declarations are retained alongside effective attributes, so an
application can check earlier values even when a later declaration overrides
them. `graph.Decide` selects a unique edge for a declared outcome; it does not
invoke handlers or persist progress.

The example's handler map and dispatch loop belong to the application. Factory
activity registration and durable dispatch remain in factory core. This library
does not offer a plugin loader, activity registration service or workflow engine.

## Prepared execution

A prepared `agent.Request` carries prompt text, a working directory, an optional
native session handle, timeout, event sink, and launch/session callbacks. Each
provider owns its typed options, including native permission controls. Applications
choose their own prompts, policy and response validation.

Construct either provider explicitly; construction validates the executable but
does not start a process or check credentials:

```go
claudeRunner, err := claude.New(claude.Options{
    Binary: "/absolute/path/to/claude",
    Environment: providerEnvironment,
    Tools: []string{}, // Explicitly empty native tool grant.
})
// Check err before using claudeRunner.
codexRunner, err := codex.New(codex.Options{
    Binary: "/absolute/path/to/codex",
    Environment: providerEnvironment,
    Permission: &codex.Sandbox{Mode: "read-only"},
})
// Check err before using codexRunner.
```

Import these as `github.com/cloudvane/litefactory/pkg/execution/providers/claude`
and `github.com/cloudvane/litefactory/pkg/execution/providers/codex`. Supply an
explicit environment selected by the application. Claude distinguishes an omitted
`Tools` slice from an explicitly empty one; Codex distinguishes an omitted
permission from a supplied native sandbox selection. Factory defaults and
permission ceilings are resolved by factory adapters before execution.

Both runners accept `Run(ctx, agent.Request{Prompt: preparedText,
WorkingDirectory: absoluteDirectory, Timeout: time.Minute})`. The caller owns
session selection and any decision to retry; native model, effort and permission
options remain provider-specific. The example declares the small runner interface
at its consumer boundary, so its fake can be replaced without factory types.

`BeforeLaunch` can refuse a launch immediately before process start. Factory
adapters use it to recheck immutable delivery authorization, including retry
attempts. `OnSession` lets the caller acknowledge the exact native session before
accepting a conversation answer. Durable ownership, PID recording and the launch
barrier remain the caller's responsibility; process helpers do not replace a
persistent coordinator.

Results retain native session, text, events, usage, bounded native stderr and
terminal/exit evidence. Event sinks receive newline-delimited native records.
A failure can carry evidence that execution started. Inspect both the returned
result and error, and use `errors.Is` or `errors.As` to preserve cancellation and
failure causes, including a caller-supplied context cancellation cause. A provider process is not guaranteed to execute exactly once;
usage can be incomplete and native sandbox capabilities can vary by CLI version.

Set `Request.Usage` to a neutral `agent.UsageCapture` to receive source-qualified
invocations, observations and coverage updates synchronously. `Result.Coverage`
reports final collection coverage independently of process success. Raw native
usage remains available in `Result.Usage`; it is not automatically additive.
Optional counters preserve unknown versus measured zero. Observations identify
incremental versus cumulative counters, exclusive versus descendant-inclusive
scope, the observed model and its source, and diagnostics for incomplete evidence.
The consumer owns persistence and deduplication.

Claude collection reads only the selected session under the caller-supplied
`Options.ConfigDirectory`, capturing transcript baselines before a resumed run.
Codex collection uses the explicit environment's `HOME` and captures rollout byte
boundaries before launch. Native descendants must be associated with the selected
session or root; inherited and replayed observations cannot become fresh charges.
Source reads and final drains retain their existing file, byte and time bounds.
`UsageCapture.EvidenceContext` optionally supplies the caller's existing bounded
finalization policy. The factory adapter supplies its worker-deadline policy and
adds attempt identity only when forwarding events into existing metering storage.
Applications can also construct the provider's `NewUsageCollector` directly when
they already own a native stream and its launch lifecycle.

## Factory integration and rollback

Factory adapters keep workflow prompts and result schemas, permission ceilings,
configuration, captures, logs and usage attribution. Factory core retains graph
policy and digests, action bindings, approvals, worktrees, attempt reservation,
result receipts and durable recovery. Public execution packages do not import
factory internals.

The extraction proceeds through graph mechanics, process contracts, Claude and
Codex transport, followed by the independent example and compatibility checks.
Existing entry points delegate through the extracted packages; remaining wrappers
retain factory preparation, policy, translation or durable coordination. Graph
wire shapes, semantic and byte digests, native session artifacts and saved factory
state remain owned by their existing serializers. There is no new database,
capture identity or state conversion. Rollback uses the previous code with the
same state, subject to the existing process ownership checks.

Factory operation names do not belong to the library. Classification, scheduling,
conflict advice, conversations/chat, doctor/repair, judge, drift and log analysis
continue to use factory-owned prompts, validation and operation-specific limits.
Existing unsupported operations and provider isolation refusals remain in the
adapters; extracting transport does not enable additional capabilities.

Factory support remains operation-specific:

| Operation | Claude adapter | Codex adapter |
| --- | --- | --- |
| Ordinary execution, classification, scheduling, conflict advice | Existing supported paths | Existing supported paths |
| Conversation | Native isolation probes and retained session ownership | Native isolation probes and retained session ownership |
| Response-only chat | Existing restricted path | Refuses unverified diagnosis isolation |
| Doctor diagnosis | Existing approved read-only shell profile | Refuses unverified diagnosis isolation |
| Doctor repair and judge | Existing scoped broker controls and capability checks | Existing scoped broker controls and capability checks |
| Drift | Existing restricted capability checks | No implementation |
| Log analysis | Existing restricted capability checks | Refuses unverified log-analysis isolation |

The final call-site split is:

| Factory entry points | Extracted mechanics | Factory responsibility retained |
| --- | --- | --- |
| Graph parse, validation and navigation | `graph.Parse`, `Validate`, `Decide` | Factory attribute decoding, workflow policy, profile serialization and digests |
| Ordinary Claude and Codex runs | Provider `Execute` and native stream decoding | Prepared prompts, captures, authorization per launch, retry decisions and result translation |
| Classification, schedule and conflict advice | Provider native argument builders and `Buffered` transport | Role capture, operation prompts, output limits and response validation |
| Conversations | Native argument builders, capability probes and `agent.RunOwnedProcess` | Ownership lock, PID journal acknowledgment, session acknowledgment and durable recovery |
| Supported doctor/repair, judge, chat, drift and log-analysis transport | Provider bounded transport or `agent.RunProcess`, and native argument builders | Capability admission, permitted tools, operation schemas and diagnostics |
| Native usage and source reads | Provider usage collectors, transcripts and rollouts; neutral token math | Factory attempt attribution, persistence, reporting and worker evidence deadlines |

Factory provider adapters contain no direct `exec.Command`, `cmd.Start` or
`cmd.Run` launch bodies. The retained `internal/providerprocess` functions validate
factory request contracts and coordinate ownership before delegating to extracted
process primitives. Codex chat and log analysis retain their existing isolation
refusals; no Codex drift implementation is introduced.

## Credential-free checks and limits

The example package contains its main-path end-to-end test, constructor checks
that do not launch either provider, an external-consumer compile check and a
transitive production/test dependency check. The dependency check rejects all
imports under the module's factory `internal` directory, including indirect
imports. Private support under `pkg/execution/internal` is part of the library.

Fake-process and compile checks establish local contracts, not live-provider
availability, provider authentication, complete usage accounting or future CLI
compatibility. Live execution requires an explicitly configured provider binary
and appropriate credentials selected by the consuming application.

The extraction leaves existing agent guidance in factory prompt builders in
place. The previously recorded Go-side guidance violation is separate operator
work, not a new library instruction source.
