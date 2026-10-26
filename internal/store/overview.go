package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// OverviewLimit bounds each independent overview list.
const OverviewLimit = 50

// OverviewIdentity retains provider-qualified ticket identity separately from
// historical task aliases. Missing mappings do not select a guessed host.
type OverviewIdentity struct {
	Provider  string `json:"provider"`
	Origin    string `json:"origin"`
	Container string `json:"container"`
	Key       string `json:"key"`
	RemoteID  string `json:"remote_id"`
	TaskURL   string `json:"task_url"`
}

func overviewIdentity(ctx context.Context, q inboxQuery, sessionID int64) (OverviewIdentity, error) {
	var s SessionTicket
	err := q.QueryRowContext(ctx, `SELECT CASE WHEN length(provider)<=128 THEN provider ELSE '' END,CASE WHEN length(api_origin)<=2048 THEN api_origin ELSE '' END,CASE WHEN length(container)<=512 THEN container ELSE '' END,CASE WHEN length(locator_key)<=256 THEN locator_key ELSE '' END,CASE WHEN length(remote_id)<=256 THEN remote_id ELSE '' END FROM session_ticket_identities WHERE session_id=?`, sessionID).Scan(&s.Locator.Provider, &s.Locator.Origin, &s.Locator.Container, &s.Locator.Key, &s.RemoteID)
	if errors.Is(err, sql.ErrNoRows) {
		return OverviewIdentity{}, nil
	}
	if err != nil {
		return OverviewIdentity{}, err
	}
	if err := s.Locator.Validate(); err != nil {
		return OverviewIdentity{}, fmt.Errorf("store: invalid overview identity: %w", err)
	}
	i := OverviewIdentity{Provider: string(s.Locator.Provider), Origin: string(s.Locator.Origin), Container: s.Locator.Container, Key: s.Locator.Key, RemoteID: s.RemoteID}
	// These mappings are defined by the GitHub adapter. Linear browser URLs
	// cannot be reconstructed from an opaque retained identity alone.
	if s.Locator.Provider == ticket.GitHub {
		u, e := url.Parse(string(s.Locator.Origin))
		if e != nil {
			return i, e
		}
		switch {
		case u.Host == "api.github.com" && u.Path == "":
			u.Host = "github.com"
		case strings.HasSuffix(u.Path, "/api/v3"):
			u.Path = strings.TrimSuffix(u.Path, "/api/v3")
		default:
			return i, nil
		}
		parts := strings.Split(s.Locator.Container, "/")
		number, e := strconv.ParseInt(s.Locator.Key, 10, 64)
		if e == nil && number > 0 && len(parts) == 2 && safeOverviewPathPart(parts[0]) && safeOverviewPathPart(parts[1]) {
			u.Path += "/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/issues/" + url.PathEscape(s.Locator.Key)
			i.TaskURL = u.String()
		}
	}
	return i, nil
}

// OverviewRepository contains registry coverage without clone paths or secrets.
type OverviewRepository struct {
	ID                     int64
	Name, ConnectionStatus string
	ObservedAt             time.Time
}

// OverviewRepositories reads every registration for membership, retaining only
// bounded detail and a sentinel. No connection authorization filters membership.
func (db *DB) OverviewRepositories(ctx context.Context) ([]OverviewRepository, bool, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT r.id,substr(r.owner||'/'||r.name,1,512),CASE WHEN v.id IS NULL THEN 'missing_connection' WHEN v.enabled=0 THEN 'disabled_connection' ELSE substr(COALESCE(o.status,'unknown'),1,128) END,COALESCE(o.checked_at,'') FROM repos r LEFT JOIN project_connections c ON c.repo_id=r.id AND c.role='ticket' LEFT JOIN project_connection_versions v ON v.id=c.version_id LEFT JOIN project_connection_observations o ON o.version_id=v.id ORDER BY r.id LIMIT 51`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []OverviewRepository{}
	for rows.Next() {
		var r OverviewRepository
		var at string
		if err = rows.Scan(&r.ID, &r.Name, &r.ConnectionStatus, &at); err != nil {
			return nil, false, err
		}
		if at != "" {
			r.ObservedAt, err = ParseTime(at)
			if err != nil {
				return nil, false, err
			}
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if err = rows.Close(); err != nil {
		return nil, false, err
	}
	more := len(out) > OverviewLimit
	if more {
		out = out[:OverviewLimit]
	}
	return out, more, nil
}

// OverviewHealth reads the single local observation without decoding an
// unbounded in-flight list or scanning repository discovery history.
func (db *DB) OverviewHealth(ctx context.Context) (HealthSnapshot, error) {
	var h HealthSnapshot
	var written string
	var beat, resume sql.NullString
	err := db.sql.QueryRowContext(ctx, `SELECT written_at,last_heartbeat_started_at,runs_in_flight,slots_used,slots_total,discovery_paused,CASE WHEN length(CAST(pause_reason AS BLOB))<=8192 THEN pause_reason ELSE 'pause_reason_omitted' END,expected_resume_at,CASE WHEN length(CAST(runs_held AS BLOB))<=8192 THEN runs_held ELSE 'dispatch_hold_detail_omitted' END FROM daemon_listener_status WHERE id=1`).Scan(&written, &beat, &h.Listener.RunsInFlight, &h.Listener.SlotsUsed, &h.Listener.SlotsTotal, &h.Listener.DiscoveryPaused, &h.Listener.PauseReason, &resume, &h.Listener.RunsHeld)
	if errors.Is(err, sql.ErrNoRows) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	h.Found = true
	h.Listener.WrittenAt, err = ParseTime(written)
	if err != nil {
		return h, err
	}
	h.Listener.LastHeartbeatStartedAt, err = scanTime(beat)
	if err != nil {
		return h, err
	}
	h.Listener.ExpectedResumeAt, err = scanTime(resume)
	return h, err
}

// OverviewActivity is current execution evidence, independent of session state.
type OverviewActivity struct {
	Ticket                                                         OverviewIdentity
	ID, Kind, Repository, TaskRef, TaskURL, Workflow, Stage, State string
	RepoID, SessionID                                              int64
	RunID                                                          *int64
	StartedAt                                                      time.Time
}

// ListOverviewActivity includes active agent runs, current graph work and
// worker-only occupancy. The UNION is sorted before applying its sentinel cap.
func (db *DB) ListOverviewActivity(ctx context.Context) ([]OverviewActivity, bool, error) {
	var out []OverviewActivity
	var more bool
	err := db.withTx(ctx, func(tx *sql.Tx) error { var err error; out, more, err = listOverviewActivity(ctx, tx); return err })
	return out, more, err
}
func listOverviewActivity(ctx context.Context, q inboxQuery) ([]OverviewActivity, bool, error) {
	rows, err := q.QueryContext(ctx, `WITH active AS (
 SELECT 'run:'||x.id id,'agent' kind,s.repo_id,s.id session_id,x.id run_id,s.task_ref,COALESCE(NULLIF(x.workflow,''),x.mode) workflow,COALESCE((SELECT a.node FROM graph_node_attempts a WHERE a.run_id=x.id ORDER BY a.id DESC LIMIT 1),'') stage,x.status state,x.started_at FROM runs x JOIN sessions s ON s.id=x.session_id WHERE x.status IN ('running','prelaunch')
 UNION ALL
 SELECT 'graph:'||i.id,'graph',s.repo_id,s.id,NULL,s.task_ref,COALESCE(NULLIF(g.workflow,''),g.mode),i.current_node,CASE WHEN EXISTS(SELECT 1 FROM graph_owner_waits w WHERE w.instance_id=i.id AND w.state='waiting') THEN 'waiting_for_owner' ELSE 'working' END,i.started_at FROM graph_instances i JOIN sessions s ON s.id=i.session_id JOIN graph_definitions g ON g.id=i.definition_id WHERE i.status='running' AND i.id=(SELECT MAX(z.id) FROM graph_instances z WHERE z.session_id=s.id) AND NOT EXISTS(SELECT 1 FROM runs x WHERE x.session_id=s.id AND x.status IN ('running','prelaunch'))
 UNION ALL
 SELECT 'worker:'||s.id,'worker',s.repo_id,s.id,NULL,s.task_ref,'','', 'working',s.lock_acquired_at FROM sessions s WHERE s.lock_owner<>'' AND s.lock_acquired_at IS NOT NULL AND NOT EXISTS(SELECT 1 FROM runs x WHERE x.session_id=s.id AND x.status IN ('running','prelaunch')) AND NOT EXISTS(SELECT 1 FROM graph_instances i WHERE i.session_id=s.id AND i.status='running')
 ) SELECT a.id,a.kind,a.repo_id,a.session_id,a.run_id,substr(r.owner||'/'||r.name,1,512),substr(a.task_ref,1,1024),substr(a.workflow,1,128),substr(a.stage,1,128),a.state,a.started_at FROM active a JOIN repos r ON r.id=a.repo_id ORDER BY a.started_at DESC,a.id LIMIT 51`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []OverviewActivity{}
	for rows.Next() {
		var a OverviewActivity
		var at string
		if err = rows.Scan(&a.ID, &a.Kind, &a.RepoID, &a.SessionID, &a.RunID, &a.Repository, &a.TaskRef, &a.Workflow, &a.Stage, &a.State, &at); err != nil {
			return nil, false, err
		}
		a.StartedAt, err = ParseTime(at)
		if err != nil {
			return nil, false, err
		}
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if err = rows.Close(); err != nil {
		return nil, false, err
	}
	for i := range out {
		out[i].Ticket, err = overviewIdentity(ctx, q, out[i].SessionID)
		if err != nil {
			return nil, false, err
		}
		out[i].TaskURL = out[i].Ticket.TaskURL
	}
	more := len(out) > OverviewLimit
	if more {
		out = out[:OverviewLimit]
	}
	return out, more, nil
}

// OverviewPublication is verified owner-facing delivery, not workflow approval.
type OverviewPublication struct {
	Ticket                                                                            OverviewIdentity
	Key, Kind, Repository, TaskRef, URL, Namespace, Purpose, Stage, Summary, Workflow string
	GraphInstanceID, SourceAttemptID                                                  *int64
	RepoID, SessionID                                                                 int64
	RunID                                                                             *int64
	ConfirmedAt                                                                       time.Time
}

// OverviewPublications keeps decode omissions separate from an empty history.
type OverviewPublications struct {
	Entries          []OverviewPublication
	HasMore, Undated bool
	Warnings         []string
}

// Only typed owner-facing journal purposes enter this projection. Parts and
// internal navigation are excluded before applying the global sentinel cap.
const overviewPublicationClass = `(c.kind IN ('question','review','final','failure','advisory') OR d.trigger_namespace IN ('comment','issue-body','automatic','schedule','graph','report-multipart','plan-observation','plan-reply','plan-notice','recovery-notice','research-reminder','doctor') OR (d.trigger_namespace='code-pr-proposal' AND d.purpose='exact-code-approval')) AND d.trigger_namespace<>'report-part' AND d.purpose NOT LIKE 'validate-guidance-part:%'`

// A completed logical parent has already passed the existing atomic multipart
// completion checks. Unsettled parents need bounded retained-part evidence.
const overviewPublicationEligible = overviewPublicationClass + ` AND (d.trigger_namespace<>'report-multipart' OR d.completed=1 OR CASE WHEN length(CAST(d.body AS BLOB))<=1048576 THEN CASE WHEN json_valid(d.body) THEN (json_array_length(d.body,'$.parts')>0 AND NOT EXISTS(SELECT 1 FROM json_each(d.body,'$.parts') part LEFT JOIN report_deliveries child ON child.delivery_key=json_extract(part.value,'$.Key') WHERE child.delivery_key IS NULL OR child.completed<>1 OR child.comment_id IS NULL OR child.trigger_namespace<>'report-part' OR child.session_id<>d.session_id OR child.task<>d.task OR child.trigger_id<>d.trigger_id OR child.run_id IS NOT d.run_id)) ELSE 0 END ELSE 0 END)`

// ListOverviewPublications uses the inclusive confirmation window, not run
// completion. Undated historical receipts remain visible as a coverage fact.
func (db *DB) ListOverviewPublications(ctx context.Context, from, to time.Time) (OverviewPublications, error) {
	var out []OverviewPublication
	var more, undated, decodeOmitted bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_deliveries d JOIN sessions s ON s.id=d.session_id JOIN repos r ON r.id=s.repo_id WHERE d.trigger_namespace='report-multipart' AND d.comment_id>0 AND d.completed=0 AND d.confirmed_at>=? AND d.confirmed_at<=? AND length(CAST(d.body AS BLOB))>1048576)`, FormatTime(from), FormatTime(to)).Scan(&decodeOmitted); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_deliveries d JOIN sessions s ON s.id=d.session_id OR (d.session_id IS NULL AND s.task_ref=d.task) JOIN repos r ON r.id=s.repo_id LEFT JOIN graph_comment_intents c ON c.delivery_key=d.delivery_key WHERE d.comment_id>0 AND d.confirmed_at IS NULL AND `+overviewPublicationClass+`)`).Scan(&undated); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT d.delivery_key,CASE WHEN c.kind='review' OR d.trigger_namespace='code-pr-proposal' OR d.purpose LIKE 'plan-assembled:%' OR d.purpose LIKE 'validate-repair-review:%' THEN 'review' ELSE 'report' END,r.id,s.id,d.run_id,substr(r.owner||'/'||r.name,1,512),substr(d.task,1,1024),substr(COALESCE(d.confirmed_url,''),1,2048),substr(d.trigger_namespace,1,128),substr(d.purpose,1,512),substr(COALESCE(c.node,''),1,128),substr(COALESCE((SELECT COALESCE(NULLIF(g.workflow,''),g.mode) FROM graph_instances i JOIN graph_definitions g ON g.id=i.definition_id WHERE i.id=c.instance_id),(SELECT COALESCE(NULLIF(x.workflow,''),x.mode) FROM runs x WHERE x.id=d.run_id),''),1,128),c.instance_id,c.source_attempt_id,substr(CASE WHEN d.trigger_namespace='report-multipart' THEN CASE WHEN length(CAST(d.body AS BLOB))<=1048576 THEN CASE WHEN json_valid(d.body) THEN COALESCE(NULLIF(json_extract(d.body,'$.summary'),''),'Multipart report') ELSE 'Multipart report; summary unavailable' END ELSE 'Multipart report; summary unavailable (decode budget)' END ELSE d.body END,1,2048),d.confirmed_at FROM report_deliveries d JOIN sessions s ON s.id=d.session_id OR (d.session_id IS NULL AND s.task_ref=d.task) JOIN repos r ON r.id=s.repo_id LEFT JOIN graph_comment_intents c ON c.delivery_key=d.delivery_key WHERE d.comment_id>0 AND d.confirmed_at>=? AND d.confirmed_at<=? AND `+overviewPublicationEligible+` ORDER BY d.confirmed_at DESC,d.delivery_key LIMIT 51`, FormatTime(from), FormatTime(to))
		if err != nil {
			return err
		}
		defer rows.Close()
		out = []OverviewPublication{}
		for rows.Next() {
			var p OverviewPublication
			var at string
			if err = rows.Scan(&p.Key, &p.Kind, &p.RepoID, &p.SessionID, &p.RunID, &p.Repository, &p.TaskRef, &p.URL, &p.Namespace, &p.Purpose, &p.Stage, &p.Workflow, &p.GraphInstanceID, &p.SourceAttemptID, &p.Summary, &at); err != nil {
				return err
			}
			p.ConfirmedAt, err = ParseTime(at)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		for i := range out {
			out[i].Ticket, err = overviewIdentity(ctx, tx, out[i].SessionID)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return OverviewPublications{}, fmt.Errorf("store: overview publications: %w", err)
	}
	more = len(out) > OverviewLimit
	if more {
		out = out[:OverviewLimit]
	}
	result := OverviewPublications{Entries: out, HasMore: more || decodeOmitted, Undated: undated, Warnings: []string{}}
	if decodeOmitted {
		result.Warnings = append(result.Warnings, "multipart_publication_decode_budget_exhausted")
	}
	return result, nil
}

func safeOverviewPathPart(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
