# Code Review
# Feature: Configurable Agent Teams (US-6, US-7, US-8 — Claude/Codex/opencode renderers)
# Reviewed by: go-code-reviewer
# Date: 2026-09-06 (re-review pass)
# Status: ADVISORY_ONLY

---

## Scope note (carried forward, not a finding)

Per the run brief: US-6 and US-8 deliberately implement only the standalone-file
output path this run (their marker-tagged-section path depends on US-18, not yet
landed). US-7 has no marker-section path ever, by permanent design. Both are
confirmed against US-6.md/US-7.md/US-8.md's own Architecture Notes and are not
flagged below.

---

## Re-review pass (2026-09-06)

Scope: only the files tied to CR-B-001 — `internal/render/errors.go`,
`internal/render/claude.go`, `internal/render/codex.go`,
`internal/render/opencode.go`, and their three `*_test.go` files. The rest of
the package was not re-litigated.

### CR-B-001 — RESOLVED

**Verification performed:**

1. **Single sentinel.** `internal/render/errors.go:36-43` declares exactly one
   `var ErrMissingAgentName = errors.New("frontmatter has no agent name")`,
   documented alongside `ErrUnsupportedField` and `ErrUnknownHarness`. A grep
   for `ErrMissingAgentName`/`ErrOpencodeMissingName`/any local variant across
   `internal/render/` found no duplicate declaration anywhere else in the
   package.
2. **Consistent wrapping.** All three renderers now wrap it with `%w`:
   - `claude.go:51` — `fmt.Errorf("render claude: cannot derive %s path: %w", claudeAgentsDir, ErrMissingAgentName)`
   - `codex.go:63` — `fmt.Errorf("codex: %w", ErrMissingAgentName)`
   - `opencode.go:74` — `fmt.Errorf("opencode: %w", ErrMissingAgentName)`
3. **Consistent test assertions.** `claude_test.go:201`, `codex_test.go:144`,
   and `opencode_test.go:157` all assert
   `errors.Is(err, render.ErrMissingAgentName)`, replacing the previous
   bare-nil-check (claude) and divergent-sentinel checks (codex, opencode).
4. **No dead code.** Codex's former local `ErrMissingAgentName` sentinel
   declaration and opencode's `ErrOpencodeMissingName` are both gone; nothing
   in either file references them anymore. The `"errors"` import in each test
   file is live (used by the `errors.Is`/`errors.As` calls already present in
   those files for the unsupported-field cases).
5. **Build/vet/test.** Ran `go build ./...` (clean), `go vet ./...` (clean),
   and `go test ./internal/render/... -v` — all 21 tests in the package pass,
   including the three missing-name tests and the pre-existing
   registry/unsupported-field tests.

No new issue was introduced by the fix. CR-B-001 is closed.

---

## BLOCKING findings

None remaining.

---

## ADVISORY findings

> Carried forward unchanged from the initial review — these files were not
> touched by the CR-B-001 fix and were not re-reviewed this pass.

### CR-A-001: Identical "sort Extra keys, report the first" block duplicated verbatim in all three renderers

**Files:** `internal/render/claude.go:54-61`, `codex.go:79-86`, `opencode.go:84-91`

**Story:** US-6 / T-14, US-7 / T-15, US-8 / T-16

**Category:** Standards / Idiom

**Finding:** Each renderer independently re-implements the same eight-line
pattern — collect `fm.Extra`'s keys into a slice, `sort.Strings` it, report
`keys[0]` in an `*UnsupportedFieldError` — byte-for-byte identical across all
three files. This is exactly the kind of parallel-authorship duplication the
package's shared-helper convention (`Register`/`Lookup`/`Harnesses` all live in
one place) was meant to avoid elsewhere in the package.

**Suggestion:** Factor this into one unexported helper (e.g. a function
returning the lexicographically-first key of a `map[string]any`, or one that
builds the `*UnsupportedFieldError` directly given `agent`, `harness`, `extra`)
shared by all three call sites. The related "trim body, skip the leading blank
line when empty" block in `codex.go:97-101` and `opencode.go:107-111` (also
identical) is a good candidate to fold into the same cleanup.

### CR-A-002: `codex.go` hardcodes its harness key as a repeated string literal instead of a named const

**File:** `internal/render/codex.go` (`"codex"` appears in `init()`'s `Register` call and in three separate `UnsupportedFieldError{Harness: "codex", ...}` literals)

**Story:** US-7 / T-15

**Category:** Idiom / Consistency

**Finding:** `claude.go` and `opencode.go` both define `claudeHarness`/`opencodeHarness` constants and use them everywhere the harness key is needed, so the key exists in exactly one place. `codex.go` is the odd one out — it spells `"codex"` out four separate times, which is both an inconsistency with its two siblings (evidence of the same parallel-authorship gap as CR-B-001/CR-A-001) and a latent risk if the key is ever renamed and one occurrence is missed.

**Suggestion:** Add `const codexHarness = "codex"` and use it at all four sites, matching the other two renderers' convention.

### CR-A-003: Claude's empty-body case produces a trailing blank line; siblings explicitly guard against it and are tested for it

**File:** `internal/render/claude.go:75-80`

**Story:** US-6 / T-14

**Category:** Consistency / doc.go's determinism obligation ("terminate every produced file with exactly one trailing newline")

**Finding:** `codex.go` and `opencode.go` both special-case an empty body (`if body := strings.TrimRight(...); body != "" { ... }`) so that an agent with no body content renders as a clean `"---\n...\n---\n"` with no dangling blank line, and each has a dedicated test (`TestCodexRenderEmptyBody`, `TestOpencodeRenderEmptyBody`) proving it. `claude.go` has no such guard:
```go
b.WriteString("---\n")
b.WriteString(strings.TrimRight(source.Body, "\n"))
b.WriteString("\n")
```
When `source.Body` is empty, `TrimRight("", "\n")` is `""`, and the unconditional trailing `b.WriteString("\n")` still runs, so the output ends `...---\n\n` — an extra blank line after the closing frontmatter delimiter that the two sibling renderers deliberately avoid. There is no test in `claude_test.go` exercising this case, so it went unnoticed.

**Suggestion:** Align Claude's body-writing with its siblings' empty-body handling (skip the trailing newline write when the trimmed body is empty) and add a `TestClaudeRenderEmptyBody` matching the pattern already established in `codex_test.go`/`opencode_test.go`.

### CR-A-004: Stylistic inconsistency in `yaml.Marshal` call — pointer in `claude.go`, value in `codex.go`/`opencode.go`

**Files:** `internal/render/claude.go:70` (`yaml.Marshal(&header)`) vs. `codex.go:88` and `opencode.go:93` (`yaml.Marshal(codexFrontmatter{...})` / `yaml.Marshal(opencodeFrontmatter{...})`)

**Story:** US-6 / T-14, US-7 / T-15, US-8 / T-16

**Category:** Idiom

**Finding:** Both forms work identically with `yaml.v3`, so this isn't a bug, but the mismatch has no functional motivation and is a small, visible sign of the same parallel-authorship-without-a-shared-convention pattern as the other findings above.

**Suggestion:** Pick one form (passing by value is simpler and matches two of the three files already) and use it consistently.

---

## Cross-story summary

The three renderers integrate cleanly at the boundaries this review can check:
registration keys (`"claude"`, `"codex"`, `"opencode"`) are distinct with no
duplicate-registration panic risk; none imports outside stdlib +
`gopkg.in/yaml.v3` + the `render` package itself, so the `internal/team`/
`internal/teamsource` dependency direction is respected; all three reuse the
package's existing `*UnsupportedFieldError`/`ErrUnsupportedField` correctly
(`errors.As`/`errors.Is`, no `==` comparisons, dotted field names, sorted-key
determinism for multi-key `Extra` maps); determinism is genuinely covered by a
render-twice-compare-bytes test in each of the three test files; there are no
`fmt.Print*`/`log.*` calls and no filesystem/network/clock access anywhere in
the three renderer files. `gofmt`, `go vet`, and `go test ./internal/render/...`
all pass clean.

The previously-BLOCKING "missing agent name" inconsistency (CR-B-001) is now
resolved: all three renderers converge on one shared sentinel,
`ErrMissingAgentName`, declared once in `errors.go` and wrapped consistently
with `%w`, with all three test files asserting `errors.Is` against it. The
remaining ADVISORY findings (CR-A-001 through CR-A-004) are smaller instances
of the same root cause — logic or naming conventions one file got right and the
other two didn't quite match — worth a pass before this package picks up more
renderers, but none of them block correctness and none were touched by the
CR-B-001 fix.

---

## Verdict

ADVISORY_ONLY — 4 suggestions noted (CR-A-001 through CR-A-004, unchanged from
the initial review). CR-B-001 is resolved and no new BLOCKING issue was found
in the re-reviewed files. Proceeding to environment setup.
