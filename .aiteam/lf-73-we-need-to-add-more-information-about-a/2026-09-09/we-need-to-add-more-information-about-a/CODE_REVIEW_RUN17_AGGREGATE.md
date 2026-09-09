# Run 17 aggregate review

Reviewed the integrated dependency direction and ownership: provider decoding stays in harness/codex; normalization and price arithmetic stay in meter; SQL and snapshots stay in store; turn allocates attempts and maps records; report receives prepared values; CLI validates selectors and serializes output. No additional production findings remain after the scoped corrections documented in CODE_REVIEW_RUN17_SCOPED.md.

The same transaction pins catalog revisions before launch and later freezes each measurement valuation. Export reads matching measurement/valuation revisions into a bounded TEMP snapshot and releases cursors before writer calls. Public complete totals are suppressed when evidence is incomplete. Inclusive provider observations and reasoning/cache-duration subsets are not additive. The manual provider validation deferral has not been used to claim complete provider support.

The added transport test uses temporary shell executables, exercises the actual runner-to-turn sink and store/report projection, verifies Claude replay deduplication and its observed priced subtotal, and preserves Codex unknown/non-additive coverage. It launches no paid agent. APP_CONTEXT and APP_CONSTRAINTS describe the implemented ownership and conservative provider limitations. No historical-log recovery script, runtime-state edit, forge mutation, permission bypass, or dist write was introduced by this run.

Independent integrated scoped command passed:

`go test -race ./internal/store ./internal/turn ./internal/meter ./internal/cli ./internal/report -run 'TestMeter|TestPrice|TestTurnImposesNoAmbientDeadline' -count=1`

Results: store 265.760s, turn 37.971s, meter 1.770s, CLI 211.001s, report 2.495s. Earlier independent adapter race tests also passed. Final aggregate make check and safe build results are recorded separately; this review does not replace those gates.

Independently validated published verification events 000006, 000007 and 000008: all 52 scoped snapshots match current bytes, content hashes and manifest hashes are correct, and acceptance/review/test entries bind digest 0998046f467105890c43ad251d9f0e897051645b42b006ef1e941a1c0b0614ab. Criteria counts are 7 for US-4, 6 for US-5 and 10 for US-6. These are local evidence publications; the factory owns acceptance and forge state changes. US-1/2/3/7 remain incomplete or deferred as documented.

## Mechanical lint correction and refreshed evidence

The aggregate linter found one report test variable shadowing the predeclared max function. US-4/5/6 events were invalidated before changing the captured test. Re-review confirmed the change only renames max to maximumTokens and its reference; test behavior is unchanged. The focused report test passed after the rename.

Independently validated replacement events 000013, 000014 and 000015 against current file bytes and recomputed manifest hashes. The final scoped digest is 28b760b6687af8df7c3a5d98c8bafbe1a292ed68471bd6a3bf8f0c201b8efa9f. The earlier digest above is historical and its verification was invalidated. Aggregate checks restarted after this correction.

## Final gate result

Final aggregate `GOFLAGS=-p=1 GOLANGCI_LINT_CACHE=/tmp/lf73-run261-golangci-cache make check` passed, including the full race suite. Safe `GOFLAGS=-p=1 go build ./...` passed. TEST_REPORT_RUN17.md records results. Final diff whitespace check passed. No further production or captured validation files changed after replacement verification events 000013–000015.
