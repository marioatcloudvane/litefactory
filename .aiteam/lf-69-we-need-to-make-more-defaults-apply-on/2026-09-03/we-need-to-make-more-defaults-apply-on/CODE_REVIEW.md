# Code Review
# Feature: we-need-to-make-more-defaults-apply-on
# Reviewed by: go-code-reviewer
# Date: 2026-09-04
# Status: ADVISORY_ONLY

---

## Scope confirmation

`git diff --stat` against the worktree shows exactly two files touched, matching
the two task reports precisely:

```
 CLAUDE.md                  | 11 +++++++++++
 internal/harness/prompt.go | 18 ++++++++++++++++++
 2 files changed, 29 insertions(+)
```

Both diffs are insertion-only (0 deletions), which independently confirms AC-6
(steps 1-3 and the "sounds like" table byte-for-byte unchanged), AC-7 (`## Gate`
byte-for-byte unchanged), and AC-8 (`AGENTS.md` zero changed lines — `git diff
--stat -- AGENTS.md` returns nothing). No ring-fenced file
(`internal/daemon/mode.go`, `internal/daemon/agent.go`, `internal/daemon/tools.go`,
the mode-label write-back at `tick.go`, any config key) was touched.

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-001: AC-10 citation attached to the wrong sentence in both comments
**File:** `internal/harness/prompt.go:486`, `internal/harness/prompt.go:634`
**Story:** US-2
**Category:** Standards / precision
**Finding:** In `writeHeader`'s comment, the sentence "...the header-trust
behavior in CLAUDE.md can silently stop matching with nothing here to catch it
(AC-10)" cites AC-10. But AC-10 is the build-cleanliness criterion ("no
executable line added/removed/reordered; `gofmt -l .` reports nothing; `go
build`/`go test`/`make check` green"), not the "no test catches CLAUDE.md-side
drift" property — that property is AC-9's closing clause ("stating no test
catches drift on that side") and is separately (and correctly) cited as AC-11
two sentences later in the same comment ("...they do not, and cannot, protect
the CLAUDE.md prose that reads it (AC-11)"). The same pattern repeats in
`writeResumeHeader`'s comment header ("AC-9/AC-10: same literal as writeHeader
above..."), where AC-10 is again invoked for a sentence about literal
restatement rather than about build-artifact cleanliness. This is a citation
mismatch, not a substantive gap — the actual content required by AC-9 and AC-11
is present and correctly worded elsewhere in the same comment block.
**Suggestion:** Drop the stray `(AC-10)` mid-sentence citation in `writeHeader`
and the `/AC-10` in `writeResumeHeader`'s lead-in, since AC-10 is a
test/build-cleanliness criterion satisfied by the diff shape itself (verified
externally by `gofmt`/`go build`/`go test`/`make check`, not by comment
prose) and citing it against unrelated sentences invites a future reader to
misread what AC-10 actually guards.

### CR-A-002: AC-3's "report the unrecognized value" not explicit in CLAUDE.md wording
**File:** `CLAUDE.md:21-24`
**Story:** US-1
**Category:** Standards / precision
**Finding:** AC-3 asks that an unrecognized header value cause the run to
"report the unrecognized value rather than invent a mode from it." The
committed text says to "note in your final report that an unrecognized mode
header was seen and ignored" — it instructs noting *that* an unrecognized
header was seen, but does not explicitly instruct including the actual value
that was seen. A reasonable agent would likely include the value anyway when
writing "an unrecognized header was seen," but the wording does not require it
the way AC-3's phrasing ("report the unrecognized value") suggests it should.
**Suggestion:** Consider tightening to "...note in your final report which
unrecognized value the header carried and that it was ignored" if the intent
is for the report to always surface the literal value, not just the fact of
the mismatch. Not blocking: the softer wording is a defensible reading of
AC-3's spirit and does not create a fallback-safety gap (steps 1-3 still run
unconditionally either way).

---

## Cross-story summary

The two stories integrate cleanly at their shared boundary. US-2's added
comments in `internal/harness/prompt.go` name US-1's exact heading ("CLAUDE.md's
`## Classify` section") and describe the mechanism ("parses to decide whether a
run can trust the header and skip re-classification, rather than running its
own steps 1-3") in terms that match the prose actually committed in `CLAUDE.md`
under that heading — there is no drift between what the comment claims
`## Classify` does and what it actually does. The comment's claim that "no test
catches drift on [the CLAUDE.md prose] side" was independently verified: no Go
test in the tree parses or asserts against `CLAUDE.md`'s prose (only
`internal/harness/prompt_test.go` and `internal/harness/prompt_body_test.go`
pin the Go-side literal, as claimed). `config.TestForbiddenFlagIsAbsentFromTheTree`
passes, confirming `--dangerously-skip-permissions` appears nowhere in either
diff. `gofmt -l internal/harness/prompt.go` is silent, `go build ./...`
succeeds, `go vet ./...` is silent, `golangci-lint` reports 0 issues, and
`go test -race ./...` (the full `make check` target) is green across every
package — matching the pre-edit state with no existing assertion touched. Both
changes are genuinely comment/documentation-only: the `prompt.go` diff is
comment-only (every added line in the diff starts with `//`), and no executable
line was added, removed, or reordered.

---

## Verdict

ADVISORY_ONLY — 2 precision suggestions noted (AC-number citation mismatch in
`prompt.go`'s new comments; softer-than-ideal wording for AC-3's "report the
unrecognized value" requirement in `CLAUDE.md`). Neither blocks correctness,
build, tests, or the fallback-safety guarantee (`## Gate` runs unconditionally
in every branch as required by AC-4/AC-5). Proceeding to environment setup.
