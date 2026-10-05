# Story 003 engineering

Frozen story: story-003, issue #484, revision d116e97ddc39a8688ba9b941d2c7ef64fed354a9bd4aed6a42f9121c9bf3a211. Dependencies: none.

Implemented a once-per-published-wait Research reminder using the existing daemon tick, per-session exclusion and report journal. The verified original GitHub CreatedAt starts the 168-hour delay. Exact numeric bot identity, body, comment ID and destination establish that original publication. Unknown publication time defers with a diagnostic. Prepared notices have a fixed identity bound to the original wait; the local POST claim rechecks wait eligibility and permanently suppresses an ineligible unsent notice. Possibly-sent notices recover the original receipt without a second POST. Completion changes only the reminder delivery, leaving owner input, the original wait, reply cutoff and findings intact.

Files: internal/store/migrations/0097_research_wait_reminders.sql; internal/store/research_reminders.go; internal/store/report_deliveries.go; internal/turn/research_reminder.go; internal/turn/report_delivery.go; internal/daemon/research_reminders.go; internal/daemon/research_reminders_internal_test.go; internal/daemon/tick.go.

Executed checks:

- go test ./internal/daemon -run '^TestResearchReminder' -count=1: passed, output `ok github.com/cloudvane/litefactory/internal/daemon 0.667s` (initial four test functions).
- go test -race ./internal/daemon -run '^TestResearchReminder' -count=1: passed after final six test functions, output `ok github.com/cloudvane/litefactory/internal/daemon 10.068s`.
- go vet ./internal/daemon ./internal/turn ./internal/store: passed with no output after final changes.

Focused tests exercise original publication age and immutable verification, the exact boundary, question/review notices, one POST, unchanged waits/watermarks, late replies and duplicate refusal, observation failure, wrong original author, unavailable age, closed/inactive issues, fresh owner comments, prepared notice suppression, original source recovery exclusion, lost POST receipt, missing receipt and SQLite/Reporter restart recovery.

No acceptance, code, architecture or plan review was performed. Aggregate make check remains the root orchestrator's final gate. No real providers or forge mutations were used. A remote reply arriving after the fresh observation can race an already-started POST; the existing owner reply admission remains available and there is no subsequent reminder. An uncertain POST with no trustworthy receipt remains unresolved rather than reposting.
