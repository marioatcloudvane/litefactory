# Conversation factory review evidence

The main-path test is `TestConversationFactoryOwnerJourney` in
`internal/messaging/conversation_factory_e2e_test.go`. It exercises Claude and
Codex through their production conversation adapters and controlled executable
fixtures. Intake, SQLite, the ongoing coordinator, proposal delivery,
deterministic confirmation and the existing answer handler are production code.
The external research-answer operation is a handwritten fake that records the
exact authorized target, revision and answer.

The journey asks for current factory status, continues with “For that issue,
answer CSV output.”, reads the current question, and proposes the exact answer.
Only the terminal answer or protected proposal reaches the sender. The fixture
uses the current factory-issued target reference and retained owner source span;
it cannot supply executable text from its own memory. The test confirms through
the delivered prompt identity, checks that no effect occurred before confirmation,
checks the saved execution and outcome link, and checks that outcome narration
receives the saved result. Every adapter call resumes the same recorded native
session after the first call.

The fixture records private prompts under its temporary conversation directory.
These files allow the test to compare actual handoff bytes and native session
identities. Fixture capability responses and fixture session persistence are
controlled test contracts, not evidence that a real provider obeys isolation or
remembers context.

The tolerant-conversation main path is
`TestConversationTolerantCorrectionRestartJourney` in
`internal/messaging/conversation_tolerant_recovery_test.go`. It uses the existing
provider/store seams to stage a malformed response and a saved protected
correction, reopen SQLite and the coordinator after lease expiry, then continue
through a second correction, a lookup with exact quoted argument bytes and a
plain-prose answer delivered from immutable saved parts. The controlled provider
records native session and prompt identity; assertions check two correction
records, unchanged invocation limits and refusal of the stale worker. This is a
local restart fixture, not live-provider or live-Discord evidence. Its execution
result belongs to the current story worker's final report.

## Review scenarios

The following remain human review guidance. They are not additional automatic
acceptance tests or claims that live demonstrations ran.

| Area | Controlled scenario and expected observation |
| --- | --- |
| Authorization | Disable a command or revoke channel access; no lookup or effect starts. References from another owner/channel, expired references, and changed question revisions are rejected or require clarification. |
| Exact content | Supply quoted follow-up data with whitespace, literal escapes and Unicode; argument bytes match the selected interior source span. Remove the retained owner span, split a UTF-8 character, forward or fence the content, or ask for oversized content; no reconstructed answer is proposed. The complete action, target and protected content appear in the factory-rendered confirmation. |
| Confirmation | Quote agreement or confirm an undelivered, cancelled, expired or replaced proposal; no effect starts. Repeat delivery or confirmation; it does not create another action identity. |
| Saved outcome | Make narration unavailable after the effect is saved; the fixed fallback reports the saved outcome. Retrying delivery sends saved bytes without executing the action again. |
| Bounded recovery | Return malformed provider output, unavailable evidence or repeated lookups; at most two corrections across the turn share the unchanged four outer calls (five with consult), three lookups and 16 exchange slots. Count each saved correction boundary once, including across lookups, restart and affirmative pre-start retries. Interrupted provider input and uncertain effects are not blindly replayed. |
| Safe restart | Interrupt at a complete saved correction boundary; prove the previous process exited, reclaim fenced ownership and compare resumed prompt, native session, captured instructions and reconstructed counters. Missing session identity, invalid journals or uncertain launches stop continuation. Revalidate current access, sources and targets. |
| Prose and grounding | Retain safe answer-only prose and unknown-reference wording with visible warnings and incomplete-source caveats across saved parts. Malformed, ambiguous, duplicate-key and action-shaped payloads still fail strict validation. Prior answers and fallback text never become current evidence. |
| Compatibility | Explicit slash commands, `/details`, and unbound channels use their existing handlers. A conversational draft only prepares a feature preview, which keeps its separate confirmation gate. |
| Evidence | Lookup request, normalized arguments, redacted result, source, capture time and coverage gaps remain linked to the owner turn. An unavailable result does not mean zero. |

## Native demonstrations

No live Claude, Codex, Discord or external factory mutation demonstration is
claimed by this document. The tests use isolated local fixtures and temporary
SQLite databases. Actual provider-memory behavior and live transport behavior
need a separately authorized test environment. Any service demonstration must
first identify a test or staging `TEST_BASE_URL` and, where needed, a dedicated
`TEST_DATABASE_URL`; production is not a test target.

The story worker executed the scoped main-path test and reported passing Claude
and Codex subtests. It also executed the strict-envelope tests and reported that
they passed. Those are local fixture results; no live provider, transport or forge
effect was exercised.

Complete package checks and lint results are reported by the story worker after
its final verification runs. This document grants no acceptance and makes no
claim that the manual scenarios above were demonstrated.

The coordinator also retains queued diagnosis results for the next owner turn,
keeps provider switching behind live proposals and pending outcomes, and issues
references only from saved current previews. Focused tests exercised these
recovery and revision checks. Native token/cost measurements are unavailable from
the current conversation interface; each invocation is recorded in the existing
factory-wide ledger with unobserved usage and its actual completion outcome.

## Prior conversation baseline engineering checks

The following records the earlier conversation implementation's checks. It does
not certify the tolerant-conversation changes or their current snapshot. The
current story worker's final result records only checks actually executed for
those changes.

The final code snapshot passed `go build ./...`, `gofmt -l .` (no findings),
`git diff --check`, and the pinned golangci-lint v2.13.1 on messaging, store and
daemon (zero findings). Complete package runs used `-count=1 -timeout=0`, with
`-race` except for store, turn, daemon and CLI. No package filters or whole
repository test/check target were used in these final runs.

| Package | Instrumentation | Result |
| --- | --- | --- |
| cli | Without race | Passed |
| daemon | Without race | Passed |
| doctor | Race | Passed |
| messaging | Race | Passed |
| messaging/ask | Race | Passed |
| messaging/discord | Race | Passed |
| messaging/feature | Race | Passed |
| messaging/response | Race | Compiled; no test files |
| meterview | Race | Passed |
| projectconnection | Race | Passed |
| repos | Race | Passed |
| resultservice | Race | Compiled; no test files |
| rights | Race | Passed |
| store | Without race | Passed |
| store/storetest | Race | Compiled; no test files |
| turn | Without race | Passed |
| webhookreg | Race | Passed |

The main-path fixture passed for both Claude and Codex in the final complete
messaging run. These checks establish engineering results only; manual
acceptance and live/native demonstrations remain unclaimed.
