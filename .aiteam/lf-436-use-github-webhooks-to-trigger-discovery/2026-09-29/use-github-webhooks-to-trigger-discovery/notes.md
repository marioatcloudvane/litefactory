
## story-001 assumptions (poll interval minutes)
- Options.DiscoveryInterval zero means no gating (every heartbeat lists); daemon.Open always passes settings.poll_interval_minutes. Chosen so existing tick tests that Tick repeatedly keep listing each tick.
- Heartbeat is fixed at daemon.DefaultInterval (30s); Options.Interval/10..60s clamp now heartbeat-only. Startup.Interval = heartbeat, Startup.DiscoveryInterval new.
- Schedule marks ListingFinished at cursor-persist time (end of a completed tick) so a tick failing after discovery retries the listing next heartbeat.
- Heartbeat-only ticks persist LastTickAt but leave TasksUpdatedSince unchanged. Run loop now runs an immediate first tick when no injected tick source.
- TestDoctorCommand_Arguments/apply_unavailable/no_repair in internal/cli reads the real ~/.litefactory/config.yaml (which has the old poll_interval_seconds) and fails here; environmental, not caused by the code.

## story-002 assumptions (webhook listener)
- A missing X-Hub-Signature-256 header answers the identical 401 (AC-002 "bad/missing signature"), not the step-3 400; 400 is for a missing event or delivery header.
- A payload with no repository.full_name is an unknown repository: 401, not 400. 400 is only for undecodable JSON.
- webhook.listen validates host:port with a port 1..65535 and a non-empty host (":8787" is refused so an all-interfaces bind is always written out); port 0 is refused at config load (tests bind ephemeral ports through Setup/Options directly).
- The handler and dedup use their own clock (time.Now unless Options.Now is set): the daemon's clock may only be read from Tick (clock_test.go). Webhook package tests inject the clock.
- Fixed path lives in webhook.Path; config has no copy. Config section is top-level `webhook:`.
- Setup gains WebhookSecrets (secrets.Resolver, zero = real keychain); Startup gains WebhookAddr; `daemon run` prints a webhook line.
- Secret cache is refreshed at the top of every Tick (before the backoff check) from a fresh ListRepos; the Open-time load counts as the first attempt, so the first retry of a missing secret is 5 minutes after start.
- A hint on a heartbeat-only tick triggers the budget probe (rate limited to one a minute inside the budget) because background reads need a fresh quota observation. A hint whose read fails 10 times is dropped and forces a full listing; not-found and pull-request hints are dropped.
- Hints naming a registered story child go through markCommentedChild (no read), as the comment-listing path does.
- A deferred hint does not mark the tick deferred (the cursor/schedule still finish); the hint stays pending for the next heartbeat.
- Open binds the listener even when s.DoctorHost.Recovery is set (the replacement daemon owns the port once the old one released the lock; Close closes the listener before the lock). Doctor repair host objects themselves never open a listener.
- Tests: internal/daemon fixture gained Webhook override and forge onGetIssue hook; export_test.go exposes WakeForTest etc. The fixture setup() sets webhook.listen empty so Open never binds the fixed default port.

## story-003 assumptions (webhooks postpone the repo poll)
- Schedule keyed by repo id. A due repo is marked owed before its listing is attempted (cleared only by ListingFinished), which covers failed, backed-off, budget-deferred and mid-tick-aborted attempts; a backoff-skipped tick marks every due repo owed via owePendingListings.
- Webhook acceptance is stamped in webhookRuntime.Wake (called only after signature, dedup and issue-number checks) with its own clock (WebhookOptions.Now, default time.Now) because only Tick may read the daemon clock.
- ListingFinished is stamped at end-of-tick time; the repo cursor is written as the tick start time, forward-only. A deferred tick still finishes the repos that completed (cursor + schedule) but does not write last_tick_at.
- Repo with no cursor row lists from repo.CreatedAt without overlap. The global poll_cursor.tasks_updated_since is still planted by InitPollCursor but never advanced or read for listing.
- Removed repos: no repo delete exists in the store; Tick forgets schedule entries whose repo is no longer registered (FK cascade drops the cursor row). Not integration-tested for that reason.
- Paging through long windows: github.ListIssues/ListCommentedIssues already follow Link pagination (TestListIssuesFollowsLinkPagination), bounded at 100 pages; a window needing more fails the listing, leaving the repo owed and its cursor unmoved.
- Existing tests asserting the global cursor advances were changed to assert the per-repo cursor (crossrepo_test, discovery_schedule_test).

## story-004 assumptions (webhook registration)
- Migration is 0091_repo_webhooks.sql (0090 was taken); repo_webhooks classed ArchiveClassGlobal in the archive registry.
- `webhook.public_url` empty => registration skipped; in `repo add` this is reported on the stdout webhook line (not a stderr warning) because it is the default state and existing tests require empty stderr. Failed registrations are stderr warnings with a next step; exit stays 0.
- Stored hook ID 404 => clear record and create a new hook (no adoption by URL in that path, per design). Any other recorded hook GetHook error fails without changes.
- GitHub never returns a hook secret, so a hook is "already correct" only when this install recorded it (stored ID) and the keychain secret was not regenerated; an adopted hook (found by URL) or a regenerated secret is edited with the secret rewritten (reported as field `secret`).
- 403 and 404 on list/create/edit both map to the "bot account cannot manage webhooks" message; other API errors show status and GitHub's message.
- `security -i` can exit 0 on a failed command, so any stderr output from the write is treated as failure.
- The real `security -i` write path is not exercised by tests (keychain must not be touched); only its stdin command builder is unit-tested.
- Env gained KeychainWrite as the CLI test seam; webhook secrets are read through the existing Env.Keychain seam. The github package's Reveal-once guard now allows a second, marked site for the hook secret.
- `repo webhook` with no public_url prints "skipped: ..." per repo and exits non-zero (the repo could not be fixed). `repo webhook` exit is 1 for unfixed repos, 2 for bad arguments, 3 for an unregistered repo.

## story-005 assumptions (webhook and poll health in daemon status)
- Migration is 0092_daemon_health.sql; both tables classed ArchiveClassGlobal. Replace-snapshot deletes every repository row and reinserts the live ones in one transaction (equivalent to dropping removed repos).
- The publisher runs as a deferred call at the top of Tick (so it runs on every exit path, on the tick goroutine, with the tick's start time as written_at); first call after construction writes unconditionally, which is the "since start" reset, and daemon_started_at is that first tick's time. It compares a signature of everything but written_at and rewrites only on change or after a failed write.
- Mode ambiguity resolved as: listener off => polling only; delivery since start => webhook; hook registered => "webhook, no delivery yet"; listener on with no hook and nothing delivered => polling only.
- Secret column: "ok" when cached, "MISSING" only when the listener is on, "not needed (listener off)" otherwise.
- Unannounced-change warning uses the repo's cursor as "previous listing start", requires a finished listing in this daemon run (the announced set starts empty with the process), skips repos in polling-only mode and a set that hit its 1000 bound; webhook-mode there means mode != polling only (a registered hook with no delivery still warns). The set is cleared by ListingFinished, so a hint accepted between a listing's read and its finish is cleared with it.
- Warning line: one per repo per listing, names the lowest issue number (attr issue) and an others=N count.
- Env gained an optional Now clock used only for the "in 42m" text.
- story-006: guide check lives in internal/cli/docs_webhooks_test.go; daemon log line verified by reading internal/daemon/health.go (constant is unexported, no code change). Guide's nginx snippet and Docker listen advice are illustrative.
