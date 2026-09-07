# Spike — config.yaml credential reference

2026-09-04. Issue #50's Implement run left `TestInitCreatesTheHomeConfigAndDatabase`
failing outside the Codex sandbox, and the failure looked like a reversed security
posture. This spike was asked to find out what actually changed, why, and whether
it could be fixed cheaply.

## What was done

Traced the failure to its origin, then made three changes in `internal/cli` and one
comment change in `internal/config`. No production behaviour changed except one
warning message, described below.

- `internal/cli/command.go` — `Env` gains a `Keychain` test seam, mirroring the one
  `secrets.Resolver` already documents on itself.
- `internal/cli/cmd_init.go` — `resolveBotIdentity` uses it, and a missing keychain
  item now gets its own warning naming the `security add-generic-password` command.
- `internal/cli/cmd_init_test.go` — the fresh-machine test is hermetic again and
  asserts the AC-0.5 warning; a new test pins the other branch.
- `internal/config/template_test.go` — comment only, explaining why four values in
  the generated file are legitimately not defaults.

## What was found

**The change was mandated, not improvised.** US-8 (2026-09-03) asked for it in as
many words: *"live YAML represents the canonical structure rather than commented
dead placeholders"*, and AC-8.3 required the file to *"contain references rather
than credential values"* and to never enable two credential methods at once. The
implementer followed its story. The generated `config.yaml` therefore ships a live
`token_keychain` reference where main ships two commented-out ones.

**The test defect was a genuine consequence nobody accounted for.** Because the
generated file now names a reference, `init` reaches the keychain on the ordinary
path instead of only when an operator configured one. The test that lets `init`
generate a config therefore read the real login keychain of whoever ran the suite.
It passed on the author's machine and failed everywhere else — here it reported
`the login keychain is locked`. That is now stubbed.

**A user-visible regression was hiding behind it, and it is the more important
find.** `init` has two warnings. `initNoTokenWarning` carries the
`security add-generic-password` remedy; `initTokenFailedWarning` says only "fix the
reference in config.yaml". Before US-8 a fresh machine ended on the first. After
US-8 it ends on the second — so a brand-new operator was told to fix a reference
that is already correct, and the one message telling them how to store a token had
become unreachable on the path that needs it. `initNoTokenWarning` now fires only
if an operator deletes the reference. Added `initKeychainMissingWarning` for the
"reference is right, item is not there yet" case; a locked keychain or an empty
value keeps the generic wording, because for those "look at what you configured"
is genuinely the right advice.

## Decisions worth remembering

**The credential reference must not go into `Defaults()`.** This was the obvious
fix and it is wrong. `Decode` starts from `Defaults()` and unmarshals over it, and
validation rejects `token_keychain` and `token_env` both being set — so defaulting
the keychain in would make every `token_env`-only config.yaml fail to start. Proven
directly before writing it down:

    BEFORE keychain={Service: Account:} env="LITEFACTORY_GITHUB_TOKEN"
    AFTER  validate=providers.github.token_keychain and providers.github.token_env
           are both set; configure exactly one bot token source

So the generated file's reference is a starter suggestion in a file the operator
edits, not a runtime default, and `TestDefaultFileMatchesDefaults` patching `want`
is correct in kind. What was wrong is that it was undocumented: a test whose whole
job is catching drift had an unexplained exception list that a real drift could
hide in. The exceptions are now named, with the reason each one cannot be a default.

## Not done

- `cmd_daemon.go:140` builds `secrets.Resolver{}` with no seam. Threading one there
  means changing `resolveDaemonToken`'s signature and its caller. No test fails on
  it today, so it was left alone rather than grown into this spike.
- The two macOS harness tests #50's run escalated (`TestDescendantsWalksMoreThanOneLevel`,
  `TestRunTimeoutKillsTheWholeSubtree`) were not touched. That escalation was
  correct: both pass here outside the sandbox. They need no fix, only a gate that
  can see the process table.
- Issue #50's remaining problem is untouched and needs a human: the 2026-09-04 plan's
  Story Index binds US-1..US-7 to issues #51..#57, which are closed and still carry
  the 2026-09-03 titles for different work. #58 is orphaned.

## Recommendation

The three findings above are settled and the full gate is green here — `go build`,
`go vet`, `gofmt` and `go test ./...` all clean, including the two tests that fail
only inside the Codex sandbox.

Do not close #50 on this. The sub-issue binding is still wrong, and that is a
reporting problem a spike should not decide unilaterally: either the closed issues
get reopened and retitled to the 2026-09-04 stories, or the plan is re-bound to
fresh sub-issues. That is a call for the issue's owner.
