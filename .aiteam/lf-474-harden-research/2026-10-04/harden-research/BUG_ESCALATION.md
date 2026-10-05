# Unfinished engineering gate

The final aggregate race suite failed. The concrete reminder defect is missing archive-registry classification and lifecycle handling for research_wait_reminders, whose foreign keys reference archived graph_owner_waits and report_deliveries. The test location is internal/store/archive_integration_test.go:54. Story-003 completion was invalidated under request bb62ae1486958ad0fae2e35caedbec2e; its receipt reports valid input, rejected engineering, not-applicable acceptance and pending effects.

Other failures: CLI/daemon/store/turn package timeouts at eight minutes, Git fake executable timeout at internal/git/push_test.go:146, MCP response timeout at internal/mcpadapter/server_test.go:381. The observed stacks show running SQLite work; resource contention is plausible but has not been established as the sole cause.

Cycle one fixed 16 lint diagnostics, constant SQL query construction and removed unused route readers. Cycle two fixed the continuation update schema edit matrix. Both allowed fix/retest cycles are spent. No third cycle or timeout increase was attempted.

Next engineering run: classify and preserve/archive reminder rows consistently with their wait and report-delivery dependencies; run the archive registry test and reminder tests; diagnose the timed-out aggregate packages using bounded smaller selections and complete the aggregate gate. Keep already retained story evidence and frozen revisions; real provider observations remain operator work.
