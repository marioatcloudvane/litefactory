package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ArchiveClass says what the daily archive does with a table.
type ArchiveClass string

const (
	// ArchiveClassArchived rows of a closed task's family are written to the
	// archive file and then deleted.
	ArchiveClassArchived ArchiveClass = "archived"
	// ArchiveClassSkeleton tables keep the family's identity and structure and
	// are never touched by the archive.
	ArchiveClassSkeleton ArchiveClass = "skeleton"
	// ArchiveClassStoryTwoOwned is the class the Plan tables had before they were
	// moved into the archived class. No table carries it now; it stays as the
	// extension point for a table that is classified before it is archivable.
	ArchiveClassStoryTwoOwned ArchiveClass = "story-002-owned"
	// ArchiveClassGlobal tables are not keyed by one task family.
	ArchiveClassGlobal ArchiveClass = "global"
)

// ArchiveTable is one registry entry. Where selects the family's rows and is
// written in terms of the family CTEs fs (session ids), fr (run ids) and ft
// (task refs). Selectors may depend on tables deleted *later* than this one
// (parents) but never on tables deleted earlier, because the ordered deletes
// re-evaluate them against the shrinking database.
type ArchiveTable struct {
	Name  string
	Class ArchiveClass
	Where string
}

// archiveCTE defines the family: the owner session plus every session whose
// issue is one of the owner's managed child stories, their runs and task refs.
const archiveCTE = `WITH fs(id) AS (SELECT id FROM sessions WHERE id=:sid OR (repo_id=:repo AND issue_number IN (SELECT issue_number FROM managed_stories WHERE parent_task=:task))), ` +
	`fr(id) AS (SELECT id FROM runs WHERE session_id IN (SELECT id FROM fs)), ` +
	`ft(task_ref) AS (SELECT task_ref FROM sessions WHERE id IN (SELECT id FROM fs)) `

const (
	inFS = `(SELECT id FROM fs)`
	inFR = `(SELECT id FROM fr)`
	inFT = `(SELECT task_ref FROM ft)`

	subSPR     = `(SELECT run_id FROM story_progress_runs WHERE run_id IN ` + inFR + ` OR parent_task IN ` + inFT + `)`
	subMeterAt = `(SELECT id FROM meter_attempts WHERE run_id IN ` + inFR + `)`
	subMeasure = `(SELECT id FROM meter_measurements WHERE attempt_id IN ` + subMeterAt + `)`
	subValuate = `(SELECT id FROM meter_valuations WHERE measurement_id IN ` + subMeasure + `)`
	subInst    = `(SELECT id FROM graph_instances WHERE session_id IN ` + inFS + `)`
	subNodeAt  = `(SELECT id FROM graph_node_attempts WHERE instance_id IN ` + subInst + ` OR run_id IN ` + inFR + `)`
	subSched   = `(SELECT id FROM schedules WHERE session_id IN ` + inFS + ` OR run_id IN ` + inFR + `)`
	subSchedPr = `(SELECT id FROM schedule_proposals WHERE session_id IN ` + inFS + `)`
	subConfl   = `(SELECT id FROM conflict_proposals WHERE session_id IN ` + inFS + `)`
	subProps   = `(SELECT hash FROM mcp_creation_proposals WHERE source_task IN ` + inFT + `)`
	subIntents = `(SELECT id FROM mcp_creation_intents WHERE source_task IN ` + inFT + `)`
	subGrants  = `(SELECT id FROM mcp_creation_grants WHERE intent_id IN ` + subIntents + ` OR run_id IN ` + inFR + `)`
	subOps     = `(SELECT id FROM mcp_creation_operations WHERE grant_id IN ` + subGrants + ` OR proposal_hash IN ` + subProps + `)`
	subResults = `(SELECT id FROM application_results WHERE task IN ` + inFT + `)`
	subSubAtt  = `(SELECT run_id FROM submission_attempts WHERE run_id IN ` + inFR + ` OR parent_task IN ` + inFT + `)`
	subSubSnap = `(SELECT run_id FROM submission_snapshots WHERE run_id IN ` + inFR + ` OR parent_task IN ` + inFT + `)`

	byRun     = `run_id IN ` + inFR
	bySession = `session_id IN ` + inFS
	byTask    = `parent_task IN ` + inFT
	byParent  = `parent IN ` + inFT
	byRunTask = `(run_id IN ` + inFR + ` OR parent_task IN ` + inFT + `)`
	byRunSess = `(session_id IN ` + inFS + ` OR run_id IN ` + inFR + `)`
	notDoctor = `doctor_attempt_id IS NULL`
)

func archived(name, where string) ArchiveTable {
	return ArchiveTable{Name: name, Class: ArchiveClassArchived, Where: where}
}

// archiveRegistry lists every archived table in DELETE order: children before
// the parents they reference. Streaming uses the reverse, parents first. A
// later story adds Plan tables by inserting them here in foreign-key order.
var archiveRegistry = []ArchiveTable{
	// Story progress.
	archived("story_progress_effects", `run_id IN `+subSPR),
	archived("story_progress_events", `run_id IN `+subSPR),
	archived("story_progress_scope", `run_id IN `+subSPR),
	archived("story_progress_runs", `run_id IN `+subSPR),
	// Validation and acceptance.
	archived("acceptance_decisions", byRunTask),
	archived("followup_materializations", byRun),
	archived("followup_proposals", byRunTask),
	archived("validation_assessments", byRunTask),
	archived("owner_exceptions", byTask),
	// Delivery.
	archived("delivery_proposals", byTask),
	archived("delivery_readiness", byTask),
	// Metering.
	archived("meter_valuation_dimensions", `valuation_id IN `+subValuate),
	archived("meter_valuations", `measurement_id IN `+subMeasure),
	archived("meter_checkpoints", `attempt_id IN `+subMeterAt),
	archived("meter_measurements", `attempt_id IN `+subMeterAt),
	archived("meter_observations", `attempt_id IN `+subMeterAt),
	archived("meter_invocations", `attempt_id IN `+subMeterAt),
	archived("meter_attempts", `id IN `+subMeterAt),
	// MCP creation.
	archived("creation_publication_requests", `(run_id IN `+inFR+` OR operation_id IN `+subOps+`)`),
	archived("creation_publication_artifacts", `operation_id IN `+subOps),
	archived("creation_owner_artifacts", `intent_id IN `+subIntents),
	archived("creation_owner_publications", `intent_id IN `+subIntents),
	archived("mcp_creation_operations", `id IN `+subOps),
	archived("mcp_creation_grants", `id IN `+subGrants),
	archived("mcp_creation_presentations", byRunSess),
	archived("mcp_creation_current", `source_task IN `+inFT),
	archived("creation_preparations", bySession),
	archived("creation_rejections", bySession),
	archived("mcp_creation_intents", `id IN `+subIntents),
	archived("mcp_creation_proposals", `hash IN `+subProps),
	// Comment operations.
	archived("comment_operations", byRun),
	archived("family_requests", byRun),
	archived("implement_progress_reports", byParent),
	// Bug tasks.
	archived("bug_analyses", bySession),
	archived("bug_checks", bySession),
	archived("bug_delivery", bySession),
	archived("bug_events", bySession),
	archived("bug_repairs", bySession),
	archived("bug_tasks", bySession),
	// Graph.
	archived("graph_effects", `instance_id IN `+subInst),
	archived("graph_outputs", `node_attempt_id IN `+subNodeAt),
	archived("research_wait_reminders", `wait_delivery_key IN (SELECT delivery_key FROM graph_owner_waits WHERE instance_id IN `+subInst+`)`),
	archived("graph_owner_waits", `instance_id IN `+subInst),
	archived("graph_comment_intents", `instance_id IN `+subInst),
	archived("graph_transitions", `instance_id IN `+subInst),
	archived("graph_dispatches", `(session_id IN `+inFS+` OR instance_id IN `+subInst+`)`),
	archived("spike_records", `instance_id IN `+subInst),
	archived("validate_outcomes", `instance_id IN `+subInst),
	archived("validate_reports", `instance_id IN `+subInst),
	archived("validate_verdicts", `instance_id IN `+subInst),
	archived("validate_choices", `instance_id IN `+subInst),
	archived("validate_effects", `instance_id IN `+subInst),
	archived("validate_reviews", `instance_id IN `+subInst),
	archived("validate_repair_rounds", `instance_id IN `+subInst),
	archived("graph_attempt_timeouts", `node_attempt_id IN `+subNodeAt),
	archived("implement_stories", byParent),
	archived("validate_entries", bySession),
	archived("research_successors", `(instance_id IN `+subInst+` OR predecessor_id IN `+subInst+`)`),
	archived("graph_node_attempts", `id IN `+subNodeAt),
	archived("research_launch_definitions", `(session_id IN `+inFS+` OR instance_id IN `+subInst+`)`),
	archived("research_execution_paths", bySession),
	archived("graph_instances", `id IN `+subInst),
	// Application results.
	archived("application_result_observations", byRun+` AND `+notDoctor),
	archived("application_result_aggregate_checks", `(run_id IN `+inFR+` OR result_id IN `+subResults+`)`),
	archived("application_result_aggregates", `(run_id IN `+inFR+` OR result_id IN `+subResults+`)`),
	archived("application_result_revisions", `(run_id IN `+inFR+` OR result_id IN `+subResults+`) AND `+notDoctor),
	archived("application_result_requests", `(run_id IN `+inFR+` OR result_id IN `+subResults+`) AND `+notDoctor),
	archived("mcp_attempt_leases", byRun),
	archived("application_result_attempts", `(run_id IN `+inFR+` OR result_id IN `+subResults+`) AND `+notDoctor),
	archived("application_results", `id IN `+subResults),
	// Submission.
	archived("submission_checks", `run_id IN `+subSubSnap),
	archived("submission_snapshots", `run_id IN `+subSubSnap),
	archived("submission_projections", byTask),
	archived("submission_scope", `run_id IN `+subSubAtt),
	archived("submission_receipts", `run_id IN `+subSubAtt),
	archived("submission_run_counters", byRun),
	archived("submission_attempts", `run_id IN `+subSubAtt),
	archived("submission_criterion_history", byTask),
	archived("submission_generations", byTask),
	// Reports.
	archived("report_deliveries", byRunSess),
	archived("report_delivery_legacy_runs", byRun),
	archived("report_recoveries", byRunSess),
	// Conflicts.
	archived("conflict_proposal_files", `proposal_id IN `+subConfl),
	archived("conflict_proposals", `id IN `+subConfl),
	// Schedules.
	archived("automatic_continuations", bySession),
	archived("schedule_block_replies", `schedule_id IN `+subSched),
	archived("schedule_prerequisites", `(schedule_id IN `+subSched+` OR prerequisite_id IN `+subSched+`)`),
	archived("schedule_proposal_items", `proposal_id IN `+subSchedPr),
	archived("schedule_proposals", `id IN `+subSchedPr),
	archived("schedules", `id IN `+subSched),
	// Comments.
	archived("comments", bySession),
	// Plan history, children first. The triggers that protect these tables let a
	// delete through only while the delete transaction holds a grant.
	archived("plan_story_effects", byParent),
	archived("plan_story_authorities", byParent),
	archived("plan_story_completions", byParent),
	archived("plan_story_owner_edits", byParent),
	archived("plan_story_candidates", byParent),
	archived("plan_story_notices", byParent),
	archived("plan_story_notice_states", byParent),
	archived("plan_reply_explanations", byParent),
	archived("plan_story_recovery_cursors", byParent),
	archived("plan_story_drafts", byParent),
	archived("plan_workflow_history", byParent),
	archived("plan_workflow_states", byParent),
	archived("plan_execution_paths", bySession),
}

// isPlanArchiveTable reports whether the delete transaction must hold a grant
// before deleting from the table.
func isPlanArchiveTable(name string) bool { return strings.HasPrefix(name, "plan_") }

// archiveRetained classifies every table the archive never deletes from.
var archiveRetained = map[string]ArchiveClass{
	// The family skeleton and the per-task structure that identifies it.
	"sessions": ArchiveClassSkeleton, "segments": ArchiveClassSkeleton, "runs": ArchiveClassSkeleton,
	"watermarks": ArchiveClassSkeleton, "retirement_intents": ArchiveClassSkeleton,
	"session_ticket_identities": ArchiveClassSkeleton, "ticket_effect_identities": ArchiveClassSkeleton,
	"managed_features": ArchiveClassSkeleton, "managed_stories": ArchiveClassSkeleton,
	"managed_parent_closures": ArchiveClassSkeleton, "approved_story_revisions": ArchiveClassSkeleton,
	"story_publications": ArchiveClassSkeleton, "story_revision_operations": ArchiveClassSkeleton,
	"story_scope_notices": ArchiveClassSkeleton, "story_contract_upgrades": ArchiveClassSkeleton,
	"story_contract_upgrade_items": ArchiveClassSkeleton, "workflow_admissions": ArchiveClassSkeleton,
	"mode_selections": ArchiveClassSkeleton, "story_refinements": ArchiveClassSkeleton,
	"story_refinement_acks": ArchiveClassSkeleton, "story_discovery": ArchiveClassSkeleton,
	"story_discovery_pending": ArchiveClassSkeleton, "story_discovery_rotation": ArchiveClassSkeleton,
	"segment_deliveries": ArchiveClassSkeleton,
	// Finished suite runs and commands have no-delete triggers; they keep only
	// the instance id and need no graph_instances row.
	"suite_runs": ArchiveClassSkeleton, "suite_commands": ArchiveClassSkeleton,
	// Not keyed by one task family. The doctor journal has delete triggers and
	// holds; a family with doctor references is kept by the pending check.
	"repos": ArchiveClassGlobal, "identities": ArchiveClassGlobal, "poll_cursor": ArchiveClassGlobal, "repo_poll_cursors": ArchiveClassGlobal, "repo_webhooks": ArchiveClassGlobal,
	"chat_issue_previews": ArchiveClassGlobal,
	"chat_inbox":          ArchiveClassGlobal,
	// Chat delivery state follows its own short retention; source positions
	// must survive task archival so enabling notices never replays history.
	"chat_notices":           ArchiveClassGlobal,
	"chat_notice_positions":  ArchiveClassGlobal,
	"daemon_listener_status": ArchiveClassGlobal, "repo_discovery_status": ArchiveClassGlobal,
	"repository_team_bindings": ArchiveClassGlobal,
	"schema_migrations":        ArchiveClassGlobal, "sqlite_sequence": ArchiveClassGlobal, "mcp_runtime": ArchiveClassGlobal,
	"retirement_sweep": ArchiveClassGlobal, "archive_sweep": ArchiveClassGlobal,
	planArchiveGrantsTable: ArchiveClassGlobal,
	// Rights are keyed by repository, never archived or deleted.
	"rights_grants": ArchiveClassGlobal, "reviewer_assignments": ArchiveClassGlobal, "rights_history": ArchiveClassGlobal,
	"meter_price_revisions": ArchiveClassGlobal, "meter_price_rates": ArchiveClassGlobal,
	"graph_definitions": ArchiveClassGlobal,
	"doctor_attempts":   ArchiveClassGlobal, "doctor_conversations": ArchiveClassGlobal,
	"doctor_inbox": ArchiveClassGlobal, "doctor_local_presentations": ArchiveClassGlobal,
	"doctor_presentations": ArchiveClassGlobal, "doctor_proposals": ArchiveClassGlobal,
	"doctor_repair_holds": ArchiveClassGlobal, "doctor_repair_steps": ArchiveClassGlobal,
	"doctor_repair_write_permits": ArchiveClassGlobal, "doctor_repairs": ArchiveClassGlobal,
	"doctor_restarts": ArchiveClassGlobal, "doctor_scans": ArchiveClassGlobal,
}

// archiveGuardedReferences are retained tables that may reference archived
// tables. Families that reach them are kept by the doctor pending check, so a
// reference never turns into a failed delete.
var archiveGuardedReferences = map[string]bool{
	"doctor_conversations": true, "doctor_proposals": true, "doctor_attempts": true,
	"doctor_inbox": true, "doctor_presentations": true, "doctor_repairs": true,
	"doctor_local_presentations": true,
}

// ArchiveRegistry returns every classified table: archived tables first in
// DELETE order, then the rest by name. The result is a copy.
func ArchiveRegistry() []ArchiveTable {
	out := make([]ArchiveTable, 0, len(archiveRegistry)+len(archiveRetained))
	out = append(out, archiveRegistry...)
	names := make([]string, 0, len(archiveRetained))
	for n := range archiveRetained {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, ArchiveTable{Name: n, Class: archiveRetained[n]})
	}
	return out
}

// ArchiveRegistryProblems reads sqlite_master of the migrated database and
// reports every defect of the registry: a table that is unclassified or
// classified twice, a registry name that is not a table, a selector that does
// not execute, a foreign key whose child is deleted after its parent, and a
// retained table that references an archived one without being guarded. An
// empty result means the registry is complete and safe to delete in order.
func (db *DB) ArchiveRegistryProblems(ctx context.Context) ([]string, error) {
	var problems []string
	rows, err := db.sql.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("read tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("read table name: %w", err)
		}
		tables = append(tables, n)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	exists := make(map[string]bool, len(tables))
	for _, t := range tables {
		exists[t] = true
	}
	position := make(map[string]int, len(archiveRegistry))
	for i, t := range archiveRegistry {
		if _, dup := position[t.Name]; dup {
			problems = append(problems, fmt.Sprintf("%s is listed twice", t.Name))
		}
		position[t.Name] = i
		if _, both := archiveRetained[t.Name]; both {
			problems = append(problems, fmt.Sprintf("%s is both archived and retained", t.Name))
		}
		if !exists[t.Name] {
			problems = append(problems, fmt.Sprintf("%s is in the registry but is not a table", t.Name))
		}
	}
	for n := range archiveRetained {
		if !exists[n] {
			problems = append(problems, fmt.Sprintf("%s is classified but is not a table", n))
		}
	}
	for _, t := range tables {
		if _, ok := position[t]; ok {
			continue
		}
		if _, ok := archiveRetained[t]; !ok {
			problems = append(problems, fmt.Sprintf("%s is not classified", t))
		}
	}
	for _, t := range archiveRegistry {
		if !exists[t.Name] {
			continue
		}
		var n int64
		q := archiveCTE + `SELECT COUNT(*) FROM ` + t.Name + ` WHERE (` + t.Where + `)`
		if err := db.sql.QueryRowContext(ctx, q, archiveNamed(ArchiveFamily{})...).Scan(&n); err != nil {
			problems = append(problems, fmt.Sprintf("%s selector does not execute: %v", t.Name, err))
		}
	}
	for _, child := range tables {
		fk, err := db.sql.QueryContext(ctx, `SELECT "table" FROM pragma_foreign_key_list(?)`, child)
		if err != nil {
			return nil, fmt.Errorf("read foreign keys of %s: %w", child, err)
		}
		var parents []string
		for fk.Next() {
			var p string
			if err := fk.Scan(&p); err != nil {
				_ = fk.Close()
				return nil, fmt.Errorf("read foreign key of %s: %w", child, err)
			}
			parents = append(parents, p)
		}
		if err := fk.Err(); err != nil {
			_ = fk.Close()
			return nil, err
		}
		if err := fk.Close(); err != nil {
			return nil, err
		}
		for _, parent := range parents {
			pp, parentArchived := position[parent]
			cp, childArchived := position[child]
			switch {
			case child == parent:
			case parentArchived && childArchived && cp > pp:
				problems = append(problems, fmt.Sprintf("%s is deleted after its parent %s", child, parent))
			case parentArchived && !childArchived && !archiveGuardedReferences[child]:
				problems = append(problems, fmt.Sprintf("retained %s references archived %s", child, parent))
			}
		}
	}
	sort.Strings(problems)
	return problems, nil
}
