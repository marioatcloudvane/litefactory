# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The status command uses a paginated scan of registered GitHub repositories. It includes active runs, published owner waits and parked implementation work, truncates the factual output at 16000 bytes with an omission count, and reports read failures. It is not an owner-only cross-source inbox.

Evidence:

- `internal/messaging/service.go:409`

## f2

The task projection reads latest run, Plan stages, latest graph instance, published unanswered wait and parked implementation reason. A wait is visible only on a running latest graph, with state waiting and a completed delivery carrying a positive comment ID. Claimed/allocated waits and unpublished deliveries are excluded. No generic failed-step collector is present in this projection.

Evidence:

- `internal/messaging/ask/store_reader.go:42`
- `internal/messaging/ask/store_reader.go:98`

## f3

The existing general reasoner is separately configurable by provider/model/effort. It has a four-minute consult budget, up to six read-only lookups, one output correction, and no proposal/write authority. Results become evidence for the conversation model; failures become explicit gaps.

Evidence:

- `internal/messaging/conversation_reasoner.go:20`
- `internal/messaging/conversation_reasoner.go:55`
- `internal/messaging/conversation.go:34`

## f4

Current reasoner instructions explicitly demonstrate cost reasoning through meter breakdowns. The conversation model delegates difficult cost questions using consult, and the factory executes meter reads with totals and breakdowns. This is a verified adjacent agent pattern, not proof of the historical agent the owner meant.

Evidence:

- `internal/instructionpackage/embedded/agents/conversation-reasoner.md:25`
- `internal/instructionpackage/embedded/agents/conversation-factory.md:28`
- `internal/messaging/conversation_factory.go:612`

## f5

Validate retains its own live report revision, owner verdicts, finding choices and remote-effect states. Failed or uncertain effects require an owner to repeat the command. These are additional facts outside the current status task projection; exact inbox eligibility must follow Validate’s existing state/command rules.

Evidence:

- `internal/store/validate_delivery.go:23`
- `internal/store/validate_delivery.go:47`
- `internal/store/validate_delivery.go:103`
- `internal/store/validate_delivery.go:196`

## f6

Ticket sessions can already be admitted for Linear with canonical task references and separate ticket/code publishing identities. Existing Linear owner-wait and Validate tests provide authentic repository integration seams. The GitHub-only status filter therefore limits chat coverage despite existing Linear workflow support.

Evidence:

- `internal/store/linear_extensions_test.go:133`
- `internal/daemon/graph_issue_closure_internal_test.go:70`
- `internal/turn/validate_owner_test.go:1139`
- `internal/messaging/service.go:418`

## f7

Chat Research question/answer commands already exist, but target numeric GitHub issues and require a current question revision token. Store admission checks require the current Research ask and preserve ownership/publication fences. Inbox next actions cannot assume one universal chat reply mechanism for every workflow/source.

Evidence:

- `internal/messaging/research.go:53`
- `internal/messaging/research.go:70`
- `internal/store/chat_research.go:16`

## f8

Conversation presentation persists answer blocks against current facts and renders them through the existing presentation layer. Discord has a 2000 UTF-16-unit message budget. This is an existing presentation constraint and reuse point for manageable inbox summaries/details.

Evidence:

- `internal/messaging/conversation_presentation.go:77`
- `internal/messaging/presentation/render.go:5`

## f9

Report delivery records distinguish possibly-sent, completed and retained comment identity. A stored report intent alone does not establish that an owner received it. Inbox collection must preserve that distinction rather than treating every delivery failure as an unanswered owner question.

Evidence:

- `internal/store/report_deliveries.go:10`
