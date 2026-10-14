# Challenge saved work

An issue owner can comment `/judge` to challenge the latest eligible saved research result or Plan for that ticket. Saved substantive review drafts are eligible before approval. The source revision is pinned when the request is admitted; newer work does not change a pending challenge.

Use the target reference printed in a previous advisory report to select that exact local saved source:

```
/judge --target <saved-reference> --agent other --focus "Check the evidence behind the main conclusion"
```

Options are `--target`, `--agent claude|codex|other`, `--model <tier|alias|model-id>`, `--thinking <provider-level>` and `--focus <text>`. Duplicate and unknown options are refused. An invalid target is explained without substituting a different source. Cross-ticket sources are not eligible.

Request settings override shared judge defaults, then provider defaults. `other` selects the opposite of the source's recorded provider. One availability fallback to the source provider is permitted; authorization, invalid output, unsupported explicit settings and an exhausted budget do not trigger fallback. Reports disclose actual settings and any fallback.

The report identifies its source, captured evidence, uncertainty and concerns with impact and suggested correction. An essential evidence gap produces “unable to assess.” “No material concerns” is an advisory assessment, not approval. Judging leaves saved work, mode labels, source writers and workflow gates unchanged.

The factory retains the pinned request and saved report across recovery. Publication retries reuse saved report bytes. Ambiguous remote publication remains pending in the delivery journal; remote APIs do not guarantee exactly-once publication.

The current Claude adapter disables native tools and exposes only captured evidence and shared result tools. The current Codex client cannot disable every native execution tool, so Codex judging refuses before launch and explains that isolation is unavailable. This isolation refusal does not permit an availability fallback.

Evidence reads use a retained snapshot, with a 1 MiB per-file and 64 MiB repository bound. The discussion has an 8 MiB bound. Files outside the authorized ceiling and unavailable evidence are disclosed; essential omissions prevent a clean assessment. Catalog reads page in batches of at most 100 entries, and line reads return at most 200 lines or 64 KiB without clipping; byte windows allow long lines to be read in successive chunks. One 30-minute total budget covers both possible provider launches; provider-specific timeouts can shorten it.

Older saves that lack verifiable shared commit ordering require an explicit reference when selecting latest would be ambiguous. The explanation lists available local references. Model and thinking settings describe the native request; the report separately states an observed model when the client supplies one. Thinking does not guarantee reasoning depth or report quality.

Explicit thinking is checked against exact supported model identities. Unknown literal model IDs can be used with thinking unset; they do not inherit effort support from a name prefix. Provider-scoped aliases resolve before this check. The native Claude aliases `opus`, `sonnet` and `haiku` use their documented current model capabilities; the isolated invocation strips ambient model overrides and records an observed model separately when available. Requested effort remains subject to provider and organization limits.

The capability table was checked on 2026-10-14 against the [Claude model effort table](https://code.claude.com/docs/en/model-config#adjust-effort-level) and the installed Codex 0.160.0 model catalog. Claude judging with explicit thinking also performs a bounded local `--help` check before inference: the installed client must advertise `--effort` and, when it lists levels, the requested level. Missing executables count as availability failures; unsupported client capabilities fail visibly without fallback. This inspection does not make a paid model call or guarantee reasoning depth.

If a successful native response identifies an observed model whose explicit effort capability is unsupported or unknown, the assessment fails visibly. An alias resolving to an older model cannot produce a clean assessment through that mismatch.
