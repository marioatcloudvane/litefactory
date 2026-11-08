# Factory consumer compatibility inventory

This records the CLI compatibility baseline and implemented ownership cutover
for story 1269-001. The registration owner is
[`Commands()`](../internal/cli/commands.go). It currently registers 113 commands.
The existing [command reference](cli/command-reference.md) records invocation,
arguments, flags, defaults, output and exit codes. Each registered command's
`Usage`, `Args` and flag constructor remain the precise executable baseline;
this document groups registrations without replacing that reference.

## Registered surface and stopped-daemon availability

A slash below separates complete subcommand alternatives. Each alternative is a
registration, including the bare help-oriented commands. “Local” means it does
not require a running factory daemon; credentials, initialized storage, Git or a
provider may still be required. Existing approval gates apply independently.

| Prefix | Registered suffixes | Availability and observable effects | Existing characterization |
| --- | --- | --- | --- |
| `init` | bare | Local; configuration, schema, identity bootstrap; progress and warnings | `internal/cli/cmd_init_test.go` |
| `standards` | bare; `proposal create/edit/list/show/validate/review/approve/publish`; `category add/replace/remove/list`; `book current/history/show/diff/render` | Local; retained proposals, exact approvals and immutable book revisions; JSON | `standards_test.go`, `standards_endtoend_test.go` |
| `standards exception` | `propose/list/show/review/approve/revoke` | Local; exact-task exception records and approval; JSON | `standards_exceptions_endtoend_test.go` |
| `standards run` | bare | Local; selected run's retained standards evidence | `standards_run_test.go` |
| `standards author` | bare; `start/list/show/questions/answer/edit/continue/validate/complete/rebase` | Local; resumable author sessions and paid author attempts where requested; JSON | `standards_author_endtoend_test.go` |
| `standards analyze drift` | bare | Local; pinned code analysis and retained evidence | `drift_endtoend_test.go` |
| `standards analysis` | `list/show/evidence/expire-evidence` | Local; retained analysis inspection and evidence expiration | `drift_endtoend_test.go` |
| `standards analyze logs` | bare (constructor action `start`); `list/show/reread/recover/file` | Local; bounded log acquisition, analysis, recovery and explicitly requested ticket filing | `log_analysis_endtoend_test.go`, `log_analysis_render_test.go` |
| `team` | `install/clone/update/list/inspect/remove/link/unlink` | Local; installed immutable team versions and repository bindings | `cmd_team_test.go`, `cmd_team_binding_test.go`, `team_binding_process_test.go`, `team_retirement_io_test.go` |
| `submission` | `preflight/submit/receipt` | Live attempt descriptor and broker required, rather than remote connector consent; correlation journal and durable result receipt | `cmd_submission_test.go`, `internal/submission/client_test.go`, `internal/submission/broker_test.go` |
| `mcp` | `serve/logs` | `serve` needs live attempt capability; `logs` uses bounded local evidence; stdio protocol | `cmd_mcp_test.go`, `cmd_mcp_logs_test.go` |
| `repo` | `add/list/delivery/webhook` | Local; repository registration, delivery settings and legacy webhook setup | `cmd_repo_test.go`, `cmd_repo_webhook_test.go` |
| `repo approver` | `inspect/set/replace/refresh` | Local; verified provider owner binding, protected replacement | `cmd_repo_approver_test.go` |
| `repo connection` | `list/set/disable/check` | Local; immutable ticket/code connections, checks and disable effects | `cmd_repo_connections_test.go`, `project_connections_e2e_test.go` |
| `repo binding` | `list/add/correct/disable` | Local; qualified ticket-to-code bindings | `cmd_repo_connections_test.go`, `project_connections_e2e_test.go` |
| `repo webhook` | `list/status/create/repair/update/disable` | Local; owned provider subscription operations (distinct from bare legacy `repo webhook`) | `cmd_repo_subscriptions_test.go` |
| `daemon` | `run/start/stop/status` | Local lifecycle composition; `run` becomes the daemon, `start` installs/starts service, `stop` stops it, `status` observes it | `cmd_daemon_test.go`, `cmd_daemon_agent_test.go`, `cmd_daemon_run_test.go`, `cmd_daemon_health_test.go` |
| `logs` | bare | Local; application/run log inspection; following is explicit | `cmd_logs_test.go`, `cmd_logs_app_test.go`, `cmd_logs_tail_test.go`, `cmd_logs_app_follow_test.go` |
| `doctor` | bare; `stop` | Doctor state of a task; stop a task's running invocation | `maintenance.DoctorState`, `maintenance.StopTask` |
| `meter` | bare; `export/recover/prices`; `prices list/set` | Local; measured/unknown usage, CSV export, explicit recovery apply and immutable price revisions | `meter_test.go`, `meter_endtoend_test.go`, `meter_recovery_test.go`, `meter_prices_test.go`, `research_meter_csv_test.go` |
| `task` | `clean/retire` | Local; clean Git worktree/state or explicit task retirement; retain dirty/missing/no-op refusals | `cmd_task_test.go`, `retirement_preview_unit_test.go` |
| `connector grants` | `list/inspect/revoke/revoke-all` | Local, including with connector disabled; safe summaries and durable grant revocation | `cmd_connector_test.go`, `connector_e2e_test.go` |
| `rights` | `catalogue/list/grant/revoke/history` | Local; exact repository rights, retained revocation/history; table or explicit JSON | `cmd_rights_test.go` |
| `reviewer` | `assign/remove/list` | Local; reviewer assignments and retained removal/history | `cmd_rights_test.go` |

Test names in this table without a directory are under `internal/cli`.
General routing preserves exit 0 (success/help), 1 (operation failure), 2 (syntax)
and 3 (precondition), stdout results and stderr diagnostics. Individual command
exceptions and defaults remain in the command reference and the named tests.
`TestFactoryCommandInventoryOfflineParsing` adds every-registration coverage for
help, unknown flags and required argument refusal without factory initialization.
It does not claim semantic compatibility of every successful handler.

## Research and invariant coverage map

These are retained source fixtures, not claims that this attempt ran them.
The main automated path and structural guards below complement the existing
behavior fixtures. Executed commands and results are reported separately.

| Research / invariant | Existing baseline fixtures and behavior |
| --- | --- |
| F-01 | `pkg/execution` dependency/behavior tests; `examples/execution`; neutral mechanics remain separate from application policy |
| F-02 | `internal/turn` lock, worktree and result tests; `internal/daemon/daemon_test.go`, `schedules_internal_test.go`, `report_recovery_db_test.go` |
| F-03 | Registered inventory above; `internal/cli/router_test.go`, `parse_test.go`, `cmd_task_test.go`, `cmd_init_test.go`, lifecycle tests |
| F-04 | `internal/ticket/identity_test.go`, `internal/projectconnection/approver_test.go`, connection factory tests |
| F-05 | `internal/daemon/ticket_recovery_e2e_test.go`, `schedules_internal_test.go`; retained Linear slots and authenticated delivery |
| F-06 | `internal/messaging` status/service tests; `internal/connector` construction tests; consumer dependency checks in `internal/connector/boundary_test.go`, `internal/factory/boundary_test.go` and `internal/messaging/ask/boundary_test.go` |
| F-07 | `internal/connector/actions_test.go`, advisory/discussion tests; `internal/owneraction` lifecycle tests |
| F-08 | `internal/submission/broker_test.go`, `client_test.go`; CLI submission/MCP tests; descriptor scopes, journal and receipt recovery |
| F-09 | Daemon chat and remote connector tests; `TestFactoryCoreTicketMainPathWithoutOptionalTransports` covers both providers with transports absent, durable receipts and publication recovery |
| F-10 | `internal/store` workflow, connector, conversation and delivery tests; existing schema/serializer unchanged-state baseline |
| F-11 | `cmd_task_test.go`, projectconnection approver tests, `internal/daemon/linear_delivery_request_test.go`, report recovery, broker and connector security tests |
| F-12 | Historical Research execution evidence only; current worker must run affected suites, while full check belongs to Validate |
| F-13 | Consumer retirement and documentation inventory are reviewed after each cutover; wrapper invocation alone is insufficient |
| F-14 | `internal/connector/connector_security_test.go`, `actions_test.go`, owner-action approval-delivery tests; authentication never substitutes approval |
| F-15 | `internal/store/conversations_test.go`, `conversation_answers_test.go`, `conversation_turns_test.go`; immutable capture creation, provider drain fence and ordered acknowledgment |
| F-16 | Unchanged-state rollback precedent in `docs/execution.md`; no migration or live-upgrade proof inferred |
| F-17 | `TestReportRecoverySQLiteRestartKeepsCadenceAndEscalation`, `TestTicketRecoveryAuthenticatedDeliveryThenRestart`, `TestScheduledLinearStoryRetainsIdentityAndPublishesOnce`; CLI connector process tests |
| K-01 | CLI inventory, ticket/projectconnection and Linear code-delivery-authority fixtures |
| K-02 | Turn locks/worktrees, broker receipt recovery, daemon interrupted dispatch/report recovery, immutable team capture fixtures |
| K-03 | Connector scopes/actions/advisory/discussion and grant-revocation fixtures |
| K-04 | Discord confirmed approval delivery and disabled-chat startup fixtures; unavailable approval remains unavailable |
| K-05 | Store serialization, immutable connection versions, authorization generation and capture/delivery fixtures; no schema conversion planned |
| K-06 | Store transaction rollback/order fixtures and daemon retry/lock tests; provider exactly-once delivery is not assumed |

## Reproducible pending-state baseline

The restart tests create isolated SQLite databases using existing serializers,
write pending work, close and reopen them, then drive recovery. In particular,
report-recovery database tests retain publication state, ticket recovery retains
authenticated ticket identity and routing, schedule tests retain Linear identity
and due work, store owner-action/connector tests retain protected payloads and
authorization generations, and conversation tests retain captures and delivery
parts. Keep these fixture builders and persisted encodings intact during the
refactor. A new-schema fixture alone cannot prove old-state compatibility.

The checked-in [pre-change fixture](../internal/store/testdata/factory-pending-state/README.md)
adds a database written by pinned baseline code, with an immutable capture,
running attempt, interrupted run, uncertain delivery, protected proposal,
consent grant, owner generation and superseded connection version.
`TestFactoryPrechangePendingStateRecovery` opens its copy with current code,
checks retained values, rejects unconfirmed completion without consuming input,
then confirms the original report and verifies idempotent completion.
`python3 scripts/factory-pending-state.py rehearse` checks a temporary database
with baseline, current, then baseline code. This is an offline storage reopen
rehearsal; live daemon stop/drain and every historical state shape are outside
that fixture's proof.

## Delivered consumer cutover

The CLI now uses owned local services for initialization/lifecycle, diagnosis,
project connections and subscriptions, standards, teams, rights, usage,
log analysis, maintenance and story upgrades. Live-attempt submission and MCP
retain their separate descriptor/broker authority. CLI dependency composition
can open local dependencies; handlers preserve parsing, rendering and exit
mapping. This does not introduce daemon RPC or change stopped-daemon access.

Factory task/status and owner-action services supply the messaging consumer;
Ask consumes project/task projections without store rows. Remote consumers use
narrow service and authentication/consent persistence interfaces. Trusted daemon
composition and existing turn implementations retain workflow execution and
recovery mechanics. The [ownership and bridge inventory](factory-core.md)
records these remaining implementation and serializer seams. Source movement,
command parsing coverage and structural guards do not alone prove successful
semantic compatibility for all 113 registrations.

