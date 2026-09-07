# US-5 — Render conflicts in plain language, safely

> As the owner of a task, I want the list of conflicting files shown to me
> clearly and without surprises, so that I can trust what the comment tells me
> even though the file names in it come from code I didn't write.

This story makes the conflict-file list in US-4's refusal comment both
readable and safe to display, given that the file names in it originate on
the base branch and are not under litefactory's control.

## Why This Matters

The list of files that conflict has to come from somewhere, and that
somewhere is the base branch's own file tree — which, on most repositories,
anyone who can land a commit is able to influence. A file name is just a
string, and strings can be built to look like markdown, like an HTML comment,
or like litefactory's own status markers, once they're echoed into a comment
litefactory signs its own name to. This story is not about making the list
look nicer; it is about making sure litefactory never prints something it
didn't mean to say, no matter what someone names a file on the other branch.

## Architecture Notes

- Pattern: reject-don't-salvage, following the two existing precedents in
  `internal/report/reply.go` — `agentNamePattern` (re-checked locally at the
  point of echo, not trusted from upstream) and `commandSpan` (stops at the
  first disallowed rune rather than skipping past it, because skipping lets
  fragments be reassembled into something that looks like a real command).
  This story applies the same local-revalidation discipline (SEC-5) to
  conflict paths.
- Package placement: `internal/report`, alongside `reply.go`'s existing
  pattern constants.
- Key interface: a local path pattern plus a reject-span helper. Any path
  failing the pattern is **rejected outright and replaced by a count** — never
  sanitized, never printed as a truncated prefix. A truncated file name is
  misinformation (it names a different, real file that might not even be in
  conflict), not a safety improvement.
- Truncation and rejection are two different rules and both are needed: the
  full list (after rejection) is truncated at 20 entries plus an "and N more"
  remainder count — this is a display rule, not a security rule, and doesn't
  substitute for the rejection check.
- Do not rely on git's own path quoting (`core.quotePath`) for markdown
  safety — quoting solves a different problem (shell/argv safety) than
  markdown/HTML-comment/bidi-override injection into a rendered comment.
- Depends on US-4 (this sanitizer feeds the refusal rendering that story
  introduces; there is no separate display surface for conflict paths).
- NOT in scope: capping the list where it's *parsed* inside `internal/git`
  (S-9 — that's a memory-safety concern belonging to US-2's `MergePreview`,
  distinct from this story's markdown-safety concern at render time); the
  refusal shell itself, its wording, or its `ReplyKind` (US-4).

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Add local path pattern and reject-span helper in `internal/report` | Code | US-4 T-4.1 | M |
| T-5.2 | Truncate accepted paths at 20 plus rejected-count and remainder-count reporting | Code | T-5.1 | S |
| T-5.3 | Plain-language wording for the conflict list section | Code | T-5.2 | S |
| T-5.4 | Injection-payload tests: markdown, HTML comment shaped like `lf:status`, bidi-override runes | Test | T-5.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | A conflicting path containing markdown, an HTML-comment marker, or a bidi-override rune is rejected outright and replaced by a count — never printed verbatim, and never printed as a partial/truncated string. |
| AC-5.2 | A conflict list with more than 20 accepted entries renders exactly 20 plus an "and N more" remainder count. |
| AC-5.3 | The rejection/refusal wording for this story's conflict-list handling lives in the same package-level constant grouping as the other refusal sentences, so a fourth account of a refusal rule does not drift from the other three. |
