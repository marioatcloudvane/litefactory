# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Reply rendering is centralised in Service.Step: handler text, then Protect redaction, then SaveChatReply, then sender.Deliver. Only ask, draft and confirm take a separate agent path; others call registered handlers (help, health, status, meter, prices, diagnose, question, answer).

Evidence:

- `internal/messaging/service.go:243`
- `internal/messaging/service.go:331`
- `internal/messaging/service.go:348`

## f2

Health and status output is hard-coded diagnostic text; meter and prices text comes from meterview lines, redacted per line then paged by buildPages.

Evidence:

- `internal/messaging/service.go:341`
- `internal/messaging/meter.go:66`
- `internal/messaging/meter.go:128`

## f3

/diagnose queues a daemon job and defers the reply (errDeferredDiagnosis); the text is completed later via CompleteChatDiagnosis, so the agent's output is structured DiagnosisSummary rendered by RenderDiagnosis, not by the chat Step.

Evidence:

- `internal/messaging/research.go:19`
- `internal/messaging/research.go:99`

## f4

/ask uses ask.Model (response-only). NativeModel picks the first configured provider that passes DoctorIsolation, uses TierBalanced, runs in a temp scratch dir, and meters every call via StartChatMeterAttempt/RecordMeterEvent/FinishMeterAttempt.

Evidence:

- `internal/messaging/ask/native.go:37`
- `internal/messaging/ask/service.go:26`
- `internal/daemon/setup.go:318`

## f5

The /ask prompt marks all JSON context as untrusted data and requires fact-handle citations, with one correction retry. A free-text narrator has no handles, so number/link validation is new work.

Evidence:

- `internal/messaging/ask/service.go:96`

## f6

Chat meter attempts require repoID >= 1; project-less commands (health, status, prices, help) cannot be metered without a store change.

Evidence:

- `internal/store/chat_ask.go:84`

## f7

Step caps ask, draft and confirm at 5 minutes; Discord interaction handles expire after 15 minutes and the adapter falls back to a channel POST. Adapter redacts again and replaces text over 2000 UTF-16 units with 'report unavailable'.

Evidence:

- `internal/messaging/service.go:254`
- `internal/messaging/discord/adapter.go:204`
- `internal/messaging/discord/adapter.go:257`

## f8

Fast tier exists in config but chat does not use it; ChatRequest has Prompt, Model and WorkingDirectory only, no tool grant, so a no-tools profile already holds.

Evidence:

- `internal/config/tier.go:37`
- `internal/agent/chat.go:12`

## f9

Issue #630 reports replies not delivered (row confirmed in 3 ms, deferred message never edited). This feature depends on it and its reply wording is explicitly out of #630's scope.

Evidence:

- `internal/messaging/discord/adapter.go:249`
