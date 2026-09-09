# Codex initialization permission failure

The supplied [issue #183](https://github.com/marioatcloudvane/litefactory/issues/183)
reports this signature when a managed run tried to launch nested Codex:

```text
WARNING: proceeding, even though we could not create PATH aliases: Operation not permitted (os error 1)
Error: failed to initialize in-process app-server client: Operation not permitted (os error 1)
```

The PATH-alias warning is optional and is not enough to diagnose the failure.
The exact initialization error and permission cause indicate a possible sandbox
restriction. They do not establish that the process was nested. The reported
ordinary top-level invocation succeeded; this is not a claim that all coding
agents or all environments share the limitation. See the
[evidence decision](decisions/nested-agent-evidence.md) for attribution and bounds.

The adapter adds guidance only when its directly invoked Codex process exits
with a positive failure status, produces no stdout events, and its direct stderr
contains the exact error line above (with LF or CRLF line endings). A prefixed
quotation or a different initialization cause does not match. Successful runs,
terminal event failures, session quota interruptions, cancellation and timeouts
retain their existing classification. The failed outcome, exit status, raw local
diagnostics and resume policy remain unchanged.

For a nested command, inspect the captured tool output for the signature. The
outer adapter receives nested tool output inside its JSONL event stream; direct
stderr classification cannot automatically inspect arbitrary nested output or
infer nesting from generated quotations. Absence of the report hint does not
rule out a nested failure.

Use the [external capture and fixture provenance procedure](agent-evidence-capture.md)
to plan bounded offline evidence. Operator-controlled acquisition happens outside
managed runs; implementation and CI do not require launching a live coding agent.
Record unavailable authentic evidence as an unsatisfied obligation. A historical
fixture cannot alone prove present-day runtime delivery, conversation lifecycle
or sandbox compatibility. Do not retry with broader permissions or substitute a
different agent to turn this diagnostic into a success claim.

This guidance neither revalidates nor proves recovery of the supplied historical
[#73](https://github.com/marioatcloudvane/litefactory/issues/73) and
[#125](https://github.com/marioatcloudvane/litefactory/issues/125) reports. Their
frozen criteria require separate evidence review and factory-mediated replanning
where applicable. Synthetic test emissions of the signature test recognition and
rendering only; they are not new authentic capability evidence.
