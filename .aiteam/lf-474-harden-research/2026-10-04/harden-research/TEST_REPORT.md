# Engineering test report

No acceptance, code, architecture or approved-plan review was performed.

Final scoped engineering checks passed against digest 2b6642148f82a5e4f073c6521d9e9962948853b4f576e306d49701b5ffdb8614.

- Story-001: focused turn tests 96.881s; store recovery tests 19.811s.
- Story-002: focused turn tests 77.433s; agentresult contract/schema tests 19.679s. The teamsource selection ran no tests; earlier resource-specific race checks passed before final capture.
- Story-003: reminder tests 21.009s. Earlier eight reminder test functions also passed with race checking.

Commands and actual outputs are preserved in story001-final-tests.log, story002-final-tests.log and story003-final-tests.log.

Aggregate go build ./... passed before lint corrections. Initial make check passed format/vet but failed 16 lint findings; cycle one corrected them. The next run passed pinned lint with zero issues but exposed schema parity; cycle two corrected it and its targeted test passed. That unbounded suite was interrupted (exit 130), so it is not passing evidence.

Final GOLANGCI_LINT_CACHE=/private/tmp/litefactory-474-lint-cache make check TEST_TIMEOUT=8m failed (exit 2). Format/vet and pinned lint v2.13.1 passed with zero lint issues. The race suite failed in six packages:

- cli, daemon, store and turn: eight-minute package timeout.
- git: TestPushFailsWhenGitDoesNotRecordTheUpstream; fake git --version timed out after 15s.
- mcpadapter: TestMCPServer_SizeBounds/full_contribution_plus_envelope; protocol response timeout.
- store additionally: TestArchiveIntegrationRegistryIsCompleteOnMigratedDatabase; research_wait_reminders is not classified and references archived graph_owner_waits/report_deliveries.

Full final output is make-check-final.log. Aggregate success is not claimed. Real Claude/Codex host observations were not performed; HOST_EVIDENCE.md records prerequisites.


## Run 0016 — final suite rerun

Run 0016 reran go build ./... (passed) and make check TEST_TIMEOUT=8m (failed, exit 2). Formatting, vet and pinned lint passed. Store failed TestArchiveIntegrationRegistryIsCompleteOnMigratedDatabase and TestArchiveRegistryProblems_MigratedDatabaseIsClean: research_wait_reminders is unclassified and references archived graph_owner_waits/report_deliveries. Daemon timed out in TestRecoveryAnnouncementCannotCreateScheduleOrRun; turn timed out in TestPlanGraphBoundary_ChildAuthorityAndMixedFeedback, both after 8m. No code changes, acceptance checks, commits or pushes. Reminder implementation was left untouched as requested.
