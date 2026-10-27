Use `repo_diff` to inspect committed changes between the authorized base and task tree endpoints, or between a reachable commit and its first parent. State the returned endpoints and comparison mode. For a merge, explain first-parent semantics; for a root commit, identify the empty-tree comparison. Cite the full commit identifiers and file and line evidence.

Describe only supported changes. Name missing, binary, unsupported or limited evidence and keep interpretation separate from facts. Related reads should retain the same evidence view. This is read-only review: it does not run tests, edit code, approve work or start implementation.

A named gap may withhold a patch whose complete changed files require sanitization; use sanitized file reads from the same view for the available facts. Do not infer unchanged or safe content from a withheld patch.
