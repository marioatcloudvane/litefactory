# US-2 — Cross-reference comment protects the mode header format

> As a maintainer of `internal/harness/prompt.go`, I want the mode-header writes to document that `CLAUDE.md` depends on their exact literal format, so that a future change to that literal doesn't silently break a prose contract with no test on the other end.

Adds a comment at each of the two places the `- Mode: <value>` header line is written, explaining that `CLAUDE.md`'s `## Classify` section (added by US-1) parses this exact literal and has no compiler or test to catch drift if it changes.

## Why This Matters
The new trust behavior in US-1 depends on one exact line of text staying exactly as it is today. Nothing enforces that on the Go side — the compiler has no idea a markdown file cares what a string literal looks like. If someone later reformats that line for an unrelated reason, the router's new "trust the header" behavior would quietly stop working, and nothing would fail loudly to say so. This adds a note at the two places that string is written, so the next person touching that code knows there's a second consumer they can't see from the code alone, and what breaks if they change the format without checking.

## Architecture Notes
- Read `internal/harness/doc.go` first (STYLE-7) — it is the package's design record and already documents the prompt/stdin boundary this comment sits inside.
- Two sites, both required: `internal/harness/prompt.go:423` (`writeHeader`, cold path) and `:566` (`writeResumeHeader`, resume path). `writeResumeHeader`'s existing doc comment already explains why mode is restated on resume ("it can change between runs") — the new comment adds to that, it does not restate it.
- Content must explain *why*, per STYLE-1: the exact literal `"- Mode: %s\n"` is parsed by `CLAUDE.md`'s `## Classify` section to decide whether to skip re-classification; that consumer lives outside the compile boundary and has no test of its own, so a format change here would silently break it with nothing to catch the drift. A bare restatement like "CLAUDE.md depends on this format" fails STYLE-1's own stated bar ("worse than nothing here").
- Cite identifiers the way the surrounding code in this file already does (it densely cites D-3, AC-A1.3, F-9, F-10, AC-A1.10, Q9(b), AC-2.25) — reference this story's AC and the `CLAUDE.md` section by name so the cross-reference is traceable, not just descriptive.
- Naming the three existing test pins on the literal (`internal/harness/prompt_test.go:340`, `:427`; `internal/harness/prompt_body_test.go:152`) is useful: it tells the next engineer what will fail on a format change (the emitter side) and, more importantly, what will not (the prose side — this comment is the only signal there).
- Do not overstate the guarantee: the comment must not claim tests protect the `CLAUDE.md` side — they don't, they only pin the Go string literal.
- Platform: comment-only edit; no executable line may be added, removed, or reordered. Must be `gofmt`-clean; if attached directly to a function declaration it must follow Go doc-comment convention (start with the identifier name).
- Risk: this comment mitigates format drift only, not prose drift — nothing in this codebase can pin the wording inside `CLAUDE.md` itself, only the literal that feeds it. This story does not close that broader risk (see US-1's Architecture Notes for the wording-adherence risk); it closes only the narrower, code-side half of it.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2 | Add a STYLE-1-compliant comment at `internal/harness/prompt.go:423` and `:566` naming `CLAUDE.md`'s `## Classify` section as a consumer of the `- Mode: %s\n` literal | logic | US-1's task T-1 (soft — names the section by its final heading) | XS |
| T-3b | Verify: no executable line added/removed/reordered in `prompt.go`; `gofmt -l .` silent; `go build ./...`, `go test ./...`, `make check` all green and unchanged from pre-edit state | test | T-2 | XS |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-9 | A comment is present at both `internal/harness/prompt.go:423` (`writeHeader`) and `:566` (`writeResumeHeader`), each naming `CLAUDE.md`'s `## Classify` section as a consumer of the exact `- Mode: %s\n` literal and stating that no test catches drift on that side. |
| AC-10 | No executable line in `prompt.go` is added, removed, or reordered; `gofmt -l .` reports nothing; `go build ./...`, `go test ./...`, and `make check` are all green, matching their pre-edit state exactly (no existing assertion edited, per TEST-10). |
| AC-11 | The comment text does not overstate its guarantee — it does not claim any test protects the `CLAUDE.md` prose side. |
