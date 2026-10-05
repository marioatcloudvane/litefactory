# Research graphs

Research uses the shipped graph exclusively. Repository-local Research route
configuration no longer selects execution. Existing legacy tasks require manual
owner/operator migration before cutover; no legacy fallback is available.

The application embeds `internal/teamsource/research/research.dot`, investigation
and shaping Markdown roles, and their evidence skill. Both stages use the shared
`lf_agent`, `lf_loop`, `lf_if`, `lf_comment` and `lf_ask` implementations. Explicit
factory mode policy supplies result vocabulary, validation and presentation;
generic spelling grants no mode authority. New roles using an existing result
contract and permissions need Markdown and DOT, without another handler. A second
mode is exercised only by offline fixtures; no production Plan graph is registered.

The ask default enters each stage only before its source has any attempt. Loop,
agent, branch, brief and question limits are eight dispatches per instance; final
publication permits one. Counters survive owner continuations, and existing
subprocess timeouts remain in effect. Controls always read named agent output.

`lf_agent` retains validated private output without generating documents. Each
stage then runs the Research-only `lf_research_brief`, which renders both
`RESEARCH_BRIEF.md` and `RESEARCH_TECHNICAL.md` from the same retained candidate.
Declarations and existing artifacts are checked before private output retention;
only these two generated files defer existence checks. The effect validates both
destinations before writing, refuses unsafe paths, and retains successful
candidate/attempt-bound generation only after document and artifact validation.
Replay produces the same content. Partial files never satisfy the generation gate.

Successful reports, questions, reviews and stage handoff require matching retained
generation and current document bytes. Old files or another candidate's success
cannot authorize them. Failed-agent diagnostics can be reported without generation.
`lf_comment` publishes reports; only `lf_ask` opens a durable owner wait. Investigation
hands on to shaping after its report. Shaping shows the current candidate for
owner review, then waits for an authenticated owner reply. An explicit `done`
settles the displayed retained review in the factory and follows its approval
branch without an agent reproduction pass. Ordinary feedback resumes the source
stage. The reply must be for the correct task and mode, newer than the confirmed
publication, and consumed once. Waiting has no expiry and releases the worker.
Graph completion does not select Plan.

Each authorized launch captures current application-shipped bytes. Running
invocations retain their captured instructions. Identical same-node instructions
may resume; changed instructions rotate the provider session with saved findings.
A refused resume allows one separate cold invocation with full role, skill,
helper and saved context. Other failures never automatically retry an agent.
Each later dispatch uses the shipped graph. A parked node with the same action
and source contract uses its current edges; an unmapped node creates a linked
successor at the current start with retained findings and authenticated context.
The existing stage attempt allowance spans that successor chain. An unanswered
wait retains its original identity until answered; approval never transfers to a
different review. Prior uncertain publications reconcile using their
original identities before successor execution.

Recovery preserves actual failures and authored findings. Report-only recovery
reuses the same candidate and delivery identity after lost acknowledgments;
publication cannot change failed process history. Closure, retirement and an
explicit mode switch cancel execution while preserving findings and owed effects.
Validated successful output and confirmed publication precede success-only
artifact saving. Failed work remains in the worktree and durable graph output.

Existing usage/log inspection links execution path, graph instance, node,
attempt, actual run and provider invocation. Refused resume and cold fallback
have separate meter attempts. Missing counters remain unknown. There is no new
pricing pipeline, automated benchmark or claimed savings.

Authentic Claude and Codex host evidence remains outstanding. An operator-authorized
host session must record the shipped binary snapshot, provider CLI versions,
question/reply/review/final comment identities, retained results and delivery
receipts for both providers. Repository fixtures establish offline mechanics only.
Do not launch paid nested agents, alter a running daemon, or claim those fixtures
as host observations. Cutover requires manual migration of old Research tasks and
removal of obsolete Research route configuration. The referenced
`docs/decisions/nested-agent-evidence.md` is absent; use the explicit host-evidence
boundary in APP_CONSTRAINTS until the operator supplies that decision or records
its disposition.
