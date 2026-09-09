# Implement run 0012 notes

## Severe environment gate

US-1 AC-1.4 is blocked. Installed and authenticated codex-cli 0.153.4 fails before model output when initializing its in-process app-server client: Operation not permitted (os error 1). Two bounded read-only scratch probes, including one without ephemeral mode, produced the same failure. See CAPABILITY_REPORT.md for sanitized commands and observations. Claude Code 2.1.266 yielded some real child/model evidence, but this does not establish the complete hierarchy and model capability contract.

The approved plan selects exact provider surfaces only after this proof and places US-2/3/4 after US-1, with US-5/6/7 downstream. No production schema, collector, CLI or test implementation was started against an unproven contract. Scope remains unchanged; full subagent support was not reduced to root-only support.

## Managed evidence

Published seven atomic blocked events in .litefactory-story-events/245, one for each selected frozen story, sequences 000001 through 000007. No verified event or completed acceptance claim was emitted. Factory owns publication and transport lifecycle.

## Resumption requirement

Provide a managed execution environment where normal authenticated Codex CLI startup succeeds under its standard permission checks, or provide factory-approved version-pinned sanitized real-execution evidence covering all required hierarchy/model scenarios. Then repeat the capability gate and continue the existing dependency order. No permission bypass or credential change is proposed.

## Validation

Safe go build ./... passed. Baseline make check terminal result is recorded below and in MANIFEST.md. Pinned lint reported zero issues but warned that its default cache directory is not writable in this sandbox. These baseline checks are not evidence that the requested feature exists.

## Final baseline validation result

- go build ./...: passed, exit 0.
- make check: failed, exit 2. Format and go vet passed; pinned golangci-lint v2.13.1 reported 0 issues (default cache-write permission warnings).
- go test -race ./...: internal/daemon and internal/turn exceeded the default 10-minute package timeout, failing after 605.286s and 606.989s respectively. Daemon was running TestTheFirstEverTickDoesNotReachBack during fixture migration; turn had multiple ApplyConfirmedResolution and other tests active. This output does not establish one test as the root cause. Other reported packages passed, including cli, codex, harness and store.
- No production or test source changed; these are baseline results, not feature acceptance. No repeated full suite or unrelated repair was attempted after the required provider capability gate was found blocked.
- Scoped review: CODE_REVIEW.md is BLOCKING (CR-B-001, missing mandatory provider capability evidence). No acceptance completion was claimed.
