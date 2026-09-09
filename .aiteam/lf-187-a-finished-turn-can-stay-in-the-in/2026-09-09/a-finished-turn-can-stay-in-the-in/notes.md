# Implementation notes — run 0005

- Scope is the factory-selected generation-1 revisions: US-1 (#192), US-2 (#193), and US-3 (#194). The parent incident diagnosis is superseded by the approved observability stories.
- Execute dependent stories sequentially. Verify acceptance, scoped review and tests before publishing each immutable evidence event. Invalidate a previous snapshot before overlapping implementation changes, then reverify the stable result.
- Use local HTTP and migrated SQLite fixtures as explicitly approved. Environment-variable gates for externally hosted integration environments do not replace these repository tests.
- Preserve polling, concurrency admission, queue ordering, durable settlement, and existing diagnostics. Source metadata describes enrollment, and executor requests do not count actual process launches.
- Leave all work uncommitted. The factory owns forge access, publication, transport lifecycle, save and push.

## Progress

- US-1 implemented and verified: scoped race/coverage checks passed (5 functions / 7 cases), full daemon tests passed, independent review CLEAN. Event `000001.json` published the immutable evidence.
- Event `000002.json` invalidated US-1 evidence before US-2 edits overlapping worker launch files. Reverification is required against the changed snapshot; the already tested dispatch behavior remains the dependency baseline.
- US-2 implemented and verified after CLEAN production/test review and stable race checks. US-1 was reverified in event `000003.json`; US-2 evidence published as `000004.json`.
- Evidence bound for AC-4: normal ordinary polling currently yields after one spawning comment. Multiple-request accounting is verified through two actual private `d.run` calls in one constructed worker with sequential task copies, including a failed second call; normal yielding remains separately covered.
- Review advisory CR-A-002 was resolved before evidence: synchronous bounded Quiesce while terminal logging is held now proves the diagnostic is joined. Tests always unblock the logger on exit.
- US-3 implemented and verified. Events `000005.json` and `000006.json` invalidated earlier snapshots before the shared tick summary changed; passing combined race checks and compatibility review supported renewed US-1/US-2 events `000007.json` and `000008.json`.
- Event `000009.json` published US-3 acceptance, review and test evidence immediately after `make check` passed. Every criterion AC-1 through AC-8 is covered; final aggregate review is CLEAN.

## Validation

- US-1 scoped race/coverage suite: 5 functions / 7 cases passed; daemon coverage 42.1%. Full daemon tests also passed at the first story gate.
- US-2 stable combined dispatch/lifecycle race suite passed (29.219s); the final Quiesce-only test correction was followed by a passing lifecycle race rerun (16.621s).
- Final composed daemon acceptance/regression suite passed (26.194s, 50.4% daemon coverage): `go test ./internal/daemon -run 'TestQuietPollingAC|TestWorkerLifecycleAC|TestWorkerDispatchAC|TestStoryRefinement' -race -cover -count=1`.
- `make check` passed: format, vet, pinned golangci-lint v2.13.1 (0 issues), and all repository race tests. The sandbox denied only lint-cache persistence; analysis still succeeded. `MAKE_CHECK.txt` summarizes 2720 repetitive cache warnings and retains the actual gate output; unabridged output is available for this session in `/private/tmp/lf187-make-check-raw.txt`.
- `go build ./...` passed after each production story. `git diff --check` passed in aggregate review.

## Bounds and decisions

- Synthetic quiet-parent tests force forge discovery empty while retaining local per-issue HTTP and migrated SQLite behavior. They prove repository behavior, not the identities of the two production sessions or the deployed revision.
- No polling optimization, lock reconciliation, concurrency-policy change, runtime deployment or process-launch counter was added.
- Both review advisories were resolved before verification: use the shared `dispatch_sources` attribute name, and prove Quiesce joining with a bounded synchronous call rather than a short scheduling window.
- The worktree contains source, validation, documentation and session artifacts only. Event files remain ignored transient transport owned by the factory; no commits or pushes were made.
- Final context refresh updated the existing daemon reference in `APP_CONTEXT.md`; no new normative policy was needed, so `APP_CONSTRAINTS.md` remains unchanged. All nine event schemas/digests and current bytes of the three latest verified snapshots validated after this refresh.
