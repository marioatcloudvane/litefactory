package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// LogIncidentQuery is launcher-owned authority. Empty repositories deny all access.
type LogIncidentQuery struct {
	RepositoryIDs []int64
	TaskRefs      []string
	RunIDs        []int64
	After         time.Time
	Before        time.Time
}

// LogIncident is one place where retained records show that work went wrong.
// Detail holds the finder's own words, unredacted; callers must redact it.
type LogIncident struct {
	ID         string
	Kind       string
	RepoID     int64
	TaskRef    string
	At         time.Time
	Role       string
	FoundBy    string
	RunIDs     []int64
	Detail     string
	Rounds     int
	Attempts   int
	FailedRuns int
}

// Incident kinds, in the words of the record that found them.
const (
	IncidentValidateReview  = "validate_review"
	IncidentValidateVerdict = "validate_verdict"
	IncidentValidateRepair  = "validate_repair"
	IncidentJudge           = "judge_concern"
	IncidentBug             = "bug"
	IncidentOwnerReply      = "owner_reply"
	IncidentFailedRun       = "failed_run"
	IncidentRepeatedNode    = "repeated_attempts"
)

const incidentRowsPerKind = 5000
const incidentDetailBytes = 8 << 10
const failedRunPredicate = `(r.status='failed' OR r.semantic_outcome IN ('failed','timed_out','abandoned')) AND r.status<>'running' AND r.recovery_reason NOT LIKE 'owner-stop:%'`

type incidentSession struct {
	repo    int64
	taskRef string
	failed  int
}

// ListLogIncidents builds incidents from validate, judge, Bug, owner, run and
// graph attempt records within the grant. Order is unspecified.
func (db *DB) ListLogIncidents(ctx context.Context, q LogIncidentQuery) ([]LogIncident, error) {
	if len(q.RepositoryIDs) == 0 {
		return nil, nil
	}
	sessions := map[int64]*incidentSession{}
	byTask := map[string]int64{}
	err := db.incidentRows(ctx, `SELECT id,repo_id,task_ref FROM sessions`, nil, func(rows *sql.Rows) error {
		var id int64
		s := &incidentSession{}
		if e := rows.Scan(&id, &s.repo, &s.taskRef); e != nil {
			return e
		}
		sessions[id], byTask[s.taskRef] = s, id
		return nil
	})
	if err == nil {
		err = db.incidentRows(ctx, `SELECT r.session_id,COUNT(*) FROM runs r WHERE `+failedRunPredicate+` GROUP BY r.session_id`, nil, func(rows *sql.Rows) error {
			var id int64
			var n int
			if e := rows.Scan(&id, &n); e != nil {
				return e
			}
			if s := sessions[id]; s != nil {
				s.failed = n
			}
			return nil
		})
	}
	if err != nil {
		return nil, fmt.Errorf("store: list log incidents: %w", err)
	}
	var out []LogIncident
	add := func(session int64, at string, in LogIncident) {
		s := sessions[session]
		t, e := ParseTime(at)
		if s == nil || e != nil {
			return
		}
		in.RepoID, in.TaskRef, in.At, in.FailedRuns = s.repo, s.taskRef, t, s.failed
		in.Detail = truncateIncident(in.Detail)
		if incidentInScope(q, in) {
			out = append(out, in)
		}
	}
	for _, kind := range []func(context.Context, func(int64, string, LogIncident), map[string]int64) error{db.validateReviewIncidents, db.validateVerdictIncidents, db.validateRepairIncidents, db.judgeIncidents, db.bugIncidents, db.ownerReplyIncidents, db.failedRunIncidents, db.repeatedNodeIncidents} {
		if err := kind(ctx, add, byTask); err != nil {
			return nil, fmt.Errorf("store: list log incidents: %w", err)
		}
	}
	// Validate findings judge the delivered implementation; add its last runs
	// when they ran under the same task (Plan-split stories run elsewhere).
	for i := range out {
		if out[i].Kind != IncidentValidateReview && out[i].Kind != IncidentValidateVerdict || out[i].Role != "implement" {
			continue
		}
		e := db.incidentRows(ctx, `SELECT r.id FROM runs r JOIN sessions s ON s.id=r.session_id WHERE s.task_ref=? AND r.mode='implement' AND r.started_at<=? ORDER BY r.id DESC LIMIT 3`, []any{out[i].TaskRef, FormatTime(out[i].At)}, func(rows *sql.Rows) error {
			var id int64
			e := rows.Scan(&id)
			out[i].RunIDs = append(out[i].RunIDs, id)
			return e
		})
		if e != nil {
			return nil, fmt.Errorf("store: list log incidents: %w", e)
		}
	}
	return out, nil
}

// incidentRows closes each result set before the caller issues another query:
// the store has one connection.
func (db *DB) incidentRows(ctx context.Context, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	for rows.Next() && err == nil {
		err = scan(rows)
	}
	if err == nil {
		err = rows.Err()
	}
	return errors.Join(err, rows.Close())
}

func incidentInScope(q LogIncidentQuery, in LogIncident) bool {
	if !slices.Contains(q.RepositoryIDs, in.RepoID) || len(q.TaskRefs) > 0 && !slices.Contains(q.TaskRefs, in.TaskRef) {
		return false
	}
	if !q.After.IsZero() && in.At.Before(q.After) || !q.Before.IsZero() && in.At.After(q.Before) {
		return false
	}
	if len(q.RunIDs) == 0 {
		return true
	}
	for _, id := range in.RunIDs {
		if slices.Contains(q.RunIDs, id) {
			return true
		}
	}
	return false
}

func truncateIncident(s string) string {
	if len(s) <= incidentDetailBytes {
		return s
	}
	n := incidentDetailBytes
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n]
}

// graphRole names a graph node by its workflow and node, e.g. implement/worker_agent.
func graphRole(mode, node string) string {
	mode = strings.TrimPrefix(mode, "workflow:")
	if node == "" {
		return mode
	}
	return mode + "/" + node
}

type reviewItem struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Classification string `json:"classification"`
	Evidence       []struct {
		Summary string `json:"summary"`
	} `json:"evidence"`
}

// unsatisfiedFindings keeps the review items the reviewer did not record as
// satisfied, with the status and classification it recorded.
func unsatisfiedFindings(findings string) string {
	var stories []struct {
		Story    string       `json:"story"`
		Criteria []reviewItem `json:"criteria"`
		Reviews  []reviewItem `json:"reviews"`
	}
	if json.Unmarshal([]byte(findings), &stories) != nil {
		return ""
	}
	var b strings.Builder
	for _, story := range stories {
		for _, item := range append(story.Criteria, story.Reviews...) {
			if item.Status == "satisfied" {
				continue
			}
			fmt.Fprintf(&b, "%s %s (%s, %s):", story.Story, item.Name, item.Status, item.Classification)
			for _, e := range item.Evidence {
				b.WriteString(" " + e.Summary)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func (db *DB) validateReviewIncidents(ctx context.Context, add func(int64, string, LogIncident), _ map[string]int64) error {
	return db.incidentRows(ctx, `SELECT v.id,i.session_id,v.role,v.state,v.reason,v.findings,COALESCE(NULLIF(v.finished_at,''),v.started_at),
 (SELECT COUNT(*) FROM validate_reviews x WHERE x.instance_id=v.instance_id AND x.role=v.role),
 (SELECT COUNT(*) FROM validate_repair_rounds rr WHERE rr.instance_id=v.instance_id),
 (SELECT r.id FROM runs r WHERE r.session_id=i.session_id AND r.started_at>=v.started_at AND r.started_at<=COALESCE(NULLIF(v.finished_at,''),v.started_at) ORDER BY r.id LIMIT 1)
 FROM validate_reviews v JOIN graph_instances i ON i.id=v.instance_id
 WHERE v.state IN ('failed','malformed') OR (v.state='accepted' AND v.findings<>'[]') ORDER BY v.id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var id, session int64
		var role, state, reason, findings, at string
		var reviewer sql.NullInt64
		in := LogIncident{Kind: IncidentValidateReview}
		if e := rows.Scan(&id, &session, &role, &state, &reason, &findings, &at, &in.Attempts, &in.Rounds, &reviewer); e != nil {
			return e
		}
		// The reviewer's own run shows how it reached the finding.
		if reviewer.Valid {
			in.RunIDs = []int64{reviewer.Int64}
		}
		in.ID = fmt.Sprintf("validate_review-%d", id)
		if state == "accepted" {
			// The reviewer found the delivered implementation lacking.
			in.Role, in.FoundBy, in.Detail = "implement", "validate-"+role, unsatisfiedFindings(findings)
		} else {
			in.Role, in.FoundBy, in.Detail = "validate-"+role, "host", state+": "+reason
		}
		if in.Detail != "" {
			add(session, at, in)
		}
		return nil
	})
}

func (db *DB) validateVerdictIncidents(ctx context.Context, add func(int64, string, LogIncident), _ map[string]int64) error {
	return db.incidentRows(ctx, `SELECT v.id,i.session_id,v.criterion,v.evidence,v.created_at FROM validate_verdicts v JOIN graph_instances i ON i.id=v.instance_id
 WHERE v.verdict='fail' AND v.current=1 ORDER BY v.id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var id, session int64
		var criterion, evidence, at string
		if e := rows.Scan(&id, &session, &criterion, &evidence, &at); e != nil {
			return e
		}
		add(session, at, LogIncident{ID: fmt.Sprintf("validate_verdict-%d", id), Kind: IncidentValidateVerdict, Role: "implement", FoundBy: "owner", Detail: criterion + " failed: " + evidence, Rounds: 1})
		return nil
	})
}

func (db *DB) validateRepairIncidents(ctx context.Context, add func(int64, string, LogIncident), _ map[string]int64) error {
	return db.incidentRows(ctx, `SELECT rr.id,i.session_id,rr.round,rr.state,rr.reason,rr.diagnosis,COALESCE(NULLIF(rr.finished_at,''),rr.started_at),a.run_id
 FROM validate_repair_rounds rr JOIN graph_instances i ON i.id=rr.instance_id LEFT JOIN graph_node_attempts a ON a.id=rr.node_attempt_id
 WHERE rr.state<>'running' ORDER BY rr.id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var id, session int64
		var state, reason, diagnosis, at string
		var run sql.NullInt64
		in := LogIncident{Kind: IncidentValidateRepair, Role: "implement", FoundBy: "validate-suite"}
		if e := rows.Scan(&id, &session, &in.Rounds, &state, &reason, &diagnosis, &at, &run); e != nil {
			return e
		}
		in.ID = fmt.Sprintf("validate_repair-%d", id)
		in.Detail = strings.TrimSpace("repair round " + strconv.Itoa(in.Rounds) + " " + state + ": " + reason + "\n" + diagnosis)
		if run.Valid {
			in.RunIDs = []int64{run.Int64}
		}
		add(session, at, in)
		return nil
	})
}

type judgeReport struct {
	Verdict string `json:"verdict"`
	Summary string `json:"summary"`
	Finding []struct {
		Title      string `json:"title"`
		Concern    string `json:"concern"`
		Correction string `json:"correction"`
	} `json:"findings"`
}

func (db *DB) judgeIncidents(ctx context.Context, add func(int64, string, LogIncident), byTask map[string]int64) error {
	return db.incidentRows(ctx, `SELECT j.id,j.task,j.created_at,j.source_node,CAST(j.report AS TEXT),
 (SELECT a.run_id FROM graph_node_attempts a WHERE a.id=j.source_node_attempt_id),
 (SELECT r.run_id FROM application_result_revisions r WHERE json_valid(CAST(j.source_json AS TEXT)) AND r.result_id=json_extract(CAST(j.source_json AS TEXT),'$.ID') AND r.revision=json_extract(CAST(j.source_json AS TEXT),'$.Revision')),
 (SELECT COALESCE(NULLIF(ar.workflow,''),ar.mode) FROM application_results ar WHERE json_valid(CAST(j.source_json AS TEXT)) AND ar.id=json_extract(CAST(j.source_json AS TEXT),'$.ID')),
 (SELECT COUNT(*) FROM judge_requests x WHERE x.task=j.task)
 FROM judge_requests j WHERE j.report IS NOT NULL ORDER BY j.id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var id int64
		var task, at, node, report string
		var nodeRun, resultRun sql.NullInt64
		var mode sql.NullString
		in := LogIncident{Kind: IncidentJudge, FoundBy: "judge"}
		if e := rows.Scan(&id, &task, &at, &node, &report, &nodeRun, &resultRun, &mode, &in.Rounds); e != nil {
			return e
		}
		var envelope struct {
			Presentation struct {
				Report judgeReport `json:"Report"`
			} `json:"presentation"`
		}
		// A judge that found nothing wrong recorded no incident.
		if json.Unmarshal([]byte(report), &envelope) != nil || envelope.Presentation.Report.Verdict == "" || envelope.Presentation.Report.Verdict == "no_material_concerns" {
			return nil
		}
		r := envelope.Presentation.Report
		var b strings.Builder
		b.WriteString(r.Verdict + ": " + r.Summary)
		for _, f := range r.Finding {
			b.WriteString("\n- " + f.Title + ": " + f.Concern + " Correction: " + f.Correction)
		}
		in.ID, in.Detail, in.Role = fmt.Sprintf("judge-%d", id), b.String(), graphRole(mode.String, node)
		for _, run := range []sql.NullInt64{nodeRun, resultRun} {
			if run.Valid {
				in.RunIDs = append(in.RunIDs, run.Int64)
			}
		}
		if session, ok := byTask[task]; ok {
			add(session, at, in)
		}
		return nil
	})
}

// bugAnalysis reads the cause from both the archived and the current Bug shape.
func bugAnalysis(snapshot string) string {
	var a struct {
		Expected string `json:"expected"`
		Actual   string `json:"actual"`
		Cause    string `json:"cause"`
		// Prevention is the analyst's note on whether a standard would have
		// prevented the bug; the log analyst aggregates these.
		Prevention *buganalysis.BugPrevention `json:"prevention"`
	}
	var current struct {
		Result struct {
			Payload struct {
				Bug struct {
					Analysis json.RawMessage `json:"analysis"`
				} `json:"bug"`
			} `json:"payload"`
		} `json:"result"`
	}
	raw := []byte(snapshot)
	if json.Unmarshal(raw, &current) == nil && len(current.Result.Payload.Bug.Analysis) > 0 {
		raw = current.Result.Payload.Bug.Analysis
	}
	if json.Unmarshal(raw, &a) != nil || a.Cause == "" && a.Actual == "" {
		return ""
	}
	text := "Expected: " + a.Expected + "\nActual: " + a.Actual + "\nCause: " + a.Cause
	if p := a.Prevention; p != nil && p.Kind != "" {
		text += "\nPrevention: " + p.Kind
		if p.StandardID != "" {
			text += " (" + p.StandardID + ")"
		}
		if p.Note != "" {
			text += ": " + p.Note
		}
	}
	return strings.TrimSpace(text)
}

// bugIncidents keeps the latest analysis per Bug task. No record links a Bug to
// the delivery that caused it, so the incident carries the Bug's own task.
func (db *DB) bugIncidents(ctx context.Context, add func(int64, string, LogIncident), byTask map[string]int64) error {
	type bug struct {
		in      LogIncident
		session int64
		at      string
		order   int64
	}
	latest := map[int64]bug{}
	keep := func(b bug) {
		if old, ok := latest[b.session]; b.in.Detail != "" && (!ok || old.order < b.order) {
			latest[b.session] = b
		}
	}
	err := db.incidentRows(ctx, `SELECT ar.id,ar.task,rv.run_id,CAST(rv.snapshot AS TEXT),rv.revision,COALESCE(r.finished_at,r.started_at)
 FROM application_results ar JOIN application_result_revisions rv ON rv.result_id=ar.id AND rv.revision=(SELECT MAX(x.revision) FROM application_result_revisions x WHERE x.result_id=ar.id)
 JOIN runs r ON r.id=rv.run_id WHERE ar.workflow='bug' ORDER BY ar.id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var id, run int64
		var task, snapshot, at string
		b := bug{in: LogIncident{Kind: IncidentBug, FoundBy: "bug-analyst"}}
		if e := rows.Scan(&id, &task, &run, &snapshot, &b.in.Rounds, &at); e != nil {
			return e
		}
		session, ok := byTask[task]
		if !ok {
			return nil
		}
		b.in.ID, b.in.RunIDs, b.in.Detail, b.session, b.at, b.order = fmt.Sprintf("bug-%d", id), []int64{run}, bugAnalysis(snapshot), session, at, id
		keep(b)
		return nil
	})
	if err != nil {
		return err
	}
	legacy := map[int64]bug{}
	err = db.incidentRows(ctx, `SELECT b.session_id,b.ordinal,b.run_id,CAST(b.content AS TEXT),COALESCE(r.finished_at,r.started_at)
 FROM bug_analyses b JOIN runs r ON r.id=b.run_id WHERE b.state='succeeded' AND b.content IS NOT NULL ORDER BY b.session_id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var session, ordinal, run int64
		var content, at string
		if e := rows.Scan(&session, &ordinal, &run, &content, &at); e != nil {
			return e
		}
		b := bug{in: LogIncident{ID: fmt.Sprintf("bug-legacy-%d-%d", session, ordinal), Kind: IncidentBug, FoundBy: "bug-analyst", RunIDs: []int64{run}, Detail: bugAnalysis(content), Rounds: int(ordinal)}, session: session, at: at, order: ordinal}
		if old, ok := legacy[session]; b.in.Detail != "" && (!ok || old.order < b.order) {
			legacy[session] = b
		}
		return nil
	})
	if err != nil {
		return err
	}
	for session, b := range legacy {
		if _, ok := latest[session]; !ok {
			latest[session] = b
		}
	}
	for _, b := range latest {
		add(b.session, b.at, b.in)
	}
	return nil
}

// ownerReplyIncidents lists every owner reply to a workflow wait as recorded.
// Whether a reply sent work back is for the reader to judge from its words.
func (db *DB) ownerReplyIncidents(ctx context.Context, add func(int64, string, LogIncident), _ map[string]int64) error {
	return db.incidentRows(ctx, `SELECT w.instance_id,COALESCE(w.reply_id,0),i.session_id,d.mode,COALESCE(a.node,w.origin_node,''),w.reply_body,w.claimed_at,a.run_id,COALESCE(ci.kind,''),
 (SELECT COUNT(*) FROM graph_owner_waits x WHERE x.instance_id=w.instance_id AND x.reply_body IS NOT NULL)
 FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions d ON d.id=i.definition_id
 LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id LEFT JOIN graph_comment_intents ci ON ci.delivery_key=w.delivery_key
 WHERE w.reply_body IS NOT NULL AND w.claimed_at IS NOT NULL AND w.state<>'cancelled' ORDER BY w.claimed_at DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var instance, reply, session int64
		var mode, node, body, at, kind string
		var run sql.NullInt64
		in := LogIncident{Kind: IncidentOwnerReply, FoundBy: "owner"}
		if e := rows.Scan(&instance, &reply, &session, &mode, &node, &body, &at, &run, &kind, &in.Rounds); e != nil {
			return e
		}
		in.ID, in.Role, in.Detail = fmt.Sprintf("owner_reply-%d-%d", instance, reply), graphRole(mode, node), "owner reply to "+kind+": "+body
		if run.Valid {
			in.RunIDs = []int64{run.Int64}
		}
		add(session, at, in)
		return nil
	})
}

func (db *DB) failedRunIncidents(ctx context.Context, add func(int64, string, LogIncident), _ map[string]int64) error {
	return db.incidentRows(ctx, `SELECT r.id,r.session_id,r.mode,r.status,r.semantic_outcome,COALESCE(r.error,''),r.recovery_reason,r.started_at,
 COALESCE((SELECT d.mode||'|'||a.node FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE a.run_id=r.id ORDER BY a.id LIMIT 1),'')
 FROM runs r WHERE `+failedRunPredicate+` ORDER BY r.id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var id, session int64
		var mode, status, outcome, reason, recovery, at, node string
		if e := rows.Scan(&id, &session, &mode, &status, &outcome, &reason, &recovery, &at, &node); e != nil {
			return e
		}
		role := mode
		if graphMode, graphNode, ok := strings.Cut(node, "|"); ok {
			role = graphRole(graphMode, graphNode)
		}
		detail := "run " + status
		if outcome != "" {
			detail += ", outcome " + outcome
		}
		detail = strings.TrimSpace(detail + ": " + reason + " " + recovery)
		add(session, at, LogIncident{ID: fmt.Sprintf("failed_run-%d", id), Kind: IncidentFailedRun, Role: role, FoundBy: "host", RunIDs: []int64{id}, Detail: detail, Attempts: 1})
		return nil
	})
}

// repeatedNodeIncidents counts agent runs per node. Several nodes can share one
// run; the run belongs to the first node that started it.
func (db *DB) repeatedNodeIncidents(ctx context.Context, add func(int64, string, LogIncident), _ map[string]int64) error {
	return db.incidentRows(ctx, `WITH o AS (SELECT run_id,MIN(id) id FROM graph_node_attempts WHERE run_id IS NOT NULL GROUP BY run_id)
 SELECT a.instance_id,i.session_id,d.mode,a.node,COUNT(*),SUM(a.status='failed'),GROUP_CONCAT(a.run_id),GROUP_CONCAT(COALESCE(a.outcome,a.status),', '),MAX(a.started_at)
 FROM graph_node_attempts a JOIN o ON o.id=a.id JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id
 GROUP BY a.instance_id,a.node HAVING COUNT(*)>1 ORDER BY a.instance_id DESC LIMIT ?`, []any{incidentRowsPerKind}, func(rows *sql.Rows) error {
		var instance, session int64
		var mode, node, runs, outcomes, at string
		var failed int
		in := LogIncident{Kind: IncidentRepeatedNode, FoundBy: "host"}
		if e := rows.Scan(&instance, &session, &mode, &node, &in.Attempts, &failed, &runs, &outcomes, &at); e != nil {
			return e
		}
		for _, v := range strings.Split(runs, ",") {
			if id, e := strconv.ParseInt(v, 10, 64); e == nil {
				in.RunIDs = append(in.RunIDs, id)
			}
		}
		slices.Sort(in.RunIDs)
		in.ID, in.Role = fmt.Sprintf("repeated_attempts-%d-%s", instance, node), graphRole(mode, node)
		in.Detail = fmt.Sprintf("node %s needed %d agent runs in one workflow (%d failed); outcomes: %s", node, in.Attempts, failed, outcomes)
		add(session, at, in)
		return nil
	})
}
