# Conversation grounding and answer details review evidence

`TestConversationGroundingDetailsOwnerJourney` is the single main-path test for
this story. It runs the same owner journey through the production Claude and
Codex adapters with controlled executable fixtures. Production intake, ongoing
session coordination, typed question lookup, temporary SQLite persistence,
ordered answer delivery and the explicit `/details` command are exercised.
The research reader and Discord sender are controlled local fixtures.

The reader returns an issue number, a number written as a word, an explicit
capture caveat and a private fixture value. Redaction replaces that private value
before the evidence is saved or supplied to either provider. The provider selects
the complete current fact by its factory-issued handle, then returns a long
unsupported explanation containing a task identifier and Unicode text. The
journey checks multipart size, one stable answer selector, visible uncertainty,
source identity and preserved source caveat. It then selects that answer with
`/details answer:<reference>` and compares its export with the saved result,
including metadata and redaction. The reader is called once and the provider
exactly twice: details adds neither a fresh lookup nor a model call. Native
invocation traces check that the second invocation resumes the first session.

This fixture proves local wiring and saved-result association. Its fabricated
capability responses and native event streams do not establish real provider
isolation, model memory, or Discord remote delivery guarantees.

## Reproducible manual review scenarios

These are review instructions, not claims that live demonstrations ran. Use a
dedicated authorized test channel and temporary database. Never exercise these
scenarios against production.

| Area | Demonstration and observations to retain |
| --- | --- |
| Conservative grounding | Supply a complete current result, a failed result, and a partial result with gaps. Compare exact current reference-only blocks against source bytes. Return a paraphrase, substring, foreign handle, prior-turn handle, matching digits and spelled-out numbers. Keep screenshots showing adjacent Unverified warnings and retained readable explanation. Markdown and mentions must not obscure warnings. |
| Selectors and access | Deliver two answers and select the older reference explicitly. Admit omitted-selector details, deliver a newer answer before processing the command, then show the command still selects the admitted answer. Repeat after an empty-evidence answer, another channel, target access loss, and expiry; retain explicit empty/denied/expired responses and show no substitution of another answer's evidence. |
| Export completeness | Compare each raw export result against its saved redacted body, arguments, source/target identity, capture time, status, completeness and gaps. Exercise exports spanning attachments; preserve the manifest and numbered continuations, with each attachment within 64 KiB. Definite attachment rejection should persist ordered inline text. Uncertain delivery retries its saved payload. Exports must exclude provider prompts, native transcripts, credentials and private reasoning. |
| Ordered recovery | Fail a middle answer part, retain saved payload and nonce, restart the local coordinator, then acknowledge it. Reconstruct content from numbered parts and compare accepted explanation bytes. Later parts and conversation answers wait; a different channel can progress. Remote duplicates remain possible and carry the same local part identity. |
| Pending retention | Advance local test time beyond 24 hours while delivery is incomplete, run cleanup, then resume. Pending evidence and remaining parts survive. Advance 24 hours after complete delivery and show an explicit expiry response while the answer reference remains distinguishable. |
| Protected proposal | Compare canonical proposal bytes with the proposal-bearing saved part. Confirm from another part and show no action. Confirm from the acknowledged proposal part and show the original action identity. Repeat delivery/confirmation and exercise an oversized protected block; no split or reconstructed proposal may authorize an action. |
| Compatibility and refusal | Compare unrelated slash commands, legacy source-text details and unbound channels with their existing routes. Return a permanent Discord refusal or revoke access during multipart delivery; preserve remaining parts and retain the visible unresolved channel-health reason. |

## Demonstration limits

No live Claude, Codex, Discord or forge demonstration has been performed for this
story. No dedicated staging URL or external test database was supplied for a
running-service demonstration. Native provider behavior and real transport
throttling therefore remain unclaimed. If a separately authorized service test
is later run, first establish `TEST_BASE_URL` and, when an external database is
needed, a dedicated `TEST_DATABASE_URL`; neither may point to production.

The scoped main-path test was executed locally for both adapters and passed.
The final focused run used `go test ./internal/messaging
-run '^TestConversationGroundingDetailsOwnerJourney$' -count=1 -timeout=3m`.
Complete package test and lint results are reported by the story worker after
the final verification runs. This document does not grant manual acceptance or
claim execution of the review scenarios above.
