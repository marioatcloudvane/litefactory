# go-implementation-engineer (placeholder)

This is a placeholder agent definition shipped as part of litefactory's
embedded default team index (US-2, tier 1 of the index-source-selection
precedence: embedded < static-path override < registered-repo override).

It exists so the `//go:embed` snapshot under `internal/team/embedded/` has a
real, non-empty markdown file to embed and so US-2's own precedence logic can
be exercised end to end without waiting on US-17.

US-17 ("bootstrap go-team index") replaces this file's content with the real
worked-example agent definition. It does not change how this file is loaded:
the embedding mechanism in `internal/team/embedded.go` is already the
permanent one.
