# Engineering notes


## Implement run 0007 engineering notes

Authoritative scope is the factory-supplied immutable US-001 revision ad2a5af7b7121f5ad770073abc6d22e46c499f0516d3c9998f25565a0a7a4df1 and US-002 revision 9c1be5ccfdc83bb4e2fa29b7ab59ccee742892744c704e8616c88c630aa8957e, workflow 2. No acceptance, code, architecture or approved-plan review was performed.

US-001 implementation bounds: 128 entries, 1 MiB body, 64 KiB retained headers per entry, 32 MiB aggregate. These finite bounds are implementation choices within the approved story. The transport wraps a copy of the HTTP client beneath SDK authentication; the architecture invariant permits only the wrapper's precise delegation statement. Unknown/stale quota probes can force an ordinary GET through a context option without suppressing observation.

The first aggregate lint attempt found ownership-analysis warnings in the test helper and three literal HTTP statuses. Test specialists corrected these; pinned lint subsequently reported zero issues. The default linter cache was outside writable roots, so checks use GOLANGCI_LINT_CACHE=/private/tmp/lf208-golangci-cache. No runtime state, daemon process, installed binary, credentials, persistent cache, settings or migrations changed. Offline fixtures establish local behavior only, not live endpoint support or production quota savings.


The earlier aggregate run passed format/vet/lint and all test packages except internal/turn, which reached the configured 20-minute package timeout while multiple short-running tests were active. No success is claimed for that run. Final gates will serialize package execution using GOFLAGS=-p=1 to reduce concurrent SQLite/race contention, retaining all tests, race instrumentation and the existing timeout. A separate stale broad daemon test (started before fixture corrections) was cancelled through its exact test exec session; no daemon/service process was touched.

US-002 fixture correction: tests now supply valid actual quota headers. The backoff test separately demonstrates expiry at 45 seconds and recovery after the independent one-minute observation cadence permits a fresh quota probe. Tick/worker summaries have overlapping scopes and must not be summed. Offline two-parent measurements were cold 6×200, warm 6×304, and changed 1×200 + 5×304.

Second-cycle aggregate diagnosis found fixture assumptions, with no production change needed: expired quota reset after large injected-clock advances, quota probe intercepted by candidate-only spies, and immediate bootstrap admission inconsistent with reserved list bounds. Fixtures now renew actual metadata, forward the unrelated probe and wait for durable bootstrap watermarks across admitted ticks; active concurrency/individual slot-refill assertions remain.

Final gate timed out in the gated mid-tick backoff test. The exact trace and bounded-cycle stop reason are retained in BUG_ESCALATION.md. No third fix cycle, aggregate success, acceptance or save is claimed.
