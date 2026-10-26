package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// OverviewWait is a published current owner wait, independent of chat access.
type OverviewWait struct {
	RepoID, SessionID                                                                          int64
	Repository, TaskRef, TaskURL, Workflow, Stage, WaitID, Kind, Summary, URL, SinceProvenance string
	Since                                                                                      *time.Time
	Ticket                                                                                     OverviewIdentity
	identitySession                                                                            int64
	commentID                                                                                  int64
}

// OverviewWaits exposes omissions instead of treating exhausted budgets as empty.
type OverviewWaits struct {
	Entries  []OverviewWait
	HasMore  bool
	Warnings []string
}

// planStoryWaitCurrent is shared with the chat inbox's current-revision policy.
func planStoryWaitCurrent(state *planbrief.PlanState, story planbrief.PlanStoryState) bool {
	w := story.Wait
	if w == nil || !story.Active() || w.Candidate != story.Candidate {
		return false
	}
	revision, err := state.ReviewRevision(story.Key, story.Stage)
	return err == nil && revision == w.Revision
}

func planGraphWaitUsesState(workflow, node string) bool {
	if workflow != "plan" {
		return false
	}
	switch node {
	case "story_ask", "story_review", "technical_ask", "technical_review", "acceptance_ask", "acceptance_review", "set_review":
		return true
	}
	return false
}

func overviewWaitText(s string, limit int) string {
	if len(s) > limit {
		s = s[:limit]
	}
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// ListOverviewWaits reads each source in a single local read transaction. Graph
// candidates and Plan decoding have separate finite budgets; neither is capped
// by recent sessions or the publication-history window.
func (db *DB) ListOverviewWaits(ctx context.Context) (OverviewWaits, error) {
	out := OverviewWaits{Entries: []OverviewWait{}}
	warnings := map[string]bool{}
	warn := func(code string) {
		if !warnings[code] {
			warnings[code] = true
			out.Warnings = append(out.Warnings, code)
		}
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		seen := map[string]bool{}
		add := func(w OverviewWait) {
			if seen[w.WaitID] {
				return
			}
			seen[w.WaitID] = true
			w.Summary = overviewWaitText(w.Summary, 512)
			out.Entries = append(out.Entries, w)
			sort.Slice(out.Entries, func(i, j int) bool { return overviewWaitLess(out.Entries[i], out.Entries[j]) })
			if len(out.Entries) > 51 {
				out.Entries = out.Entries[:51]
				out.HasMore = true
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT r.id,s.id,substr(r.owner||'/'||r.name,1,512),substr(s.task_ref,1,1024),substr(COALESCE(NULLIF(g.workflow,''),g.mode),1,128),substr(c.node,1,128),substr(w.delivery_key,1,512),c.kind,substr(d.body,1,512),substr(COALESCE(d.confirmed_url,''),1,2048),d.comment_id,COALESCE(d.confirmed_at,a.finished_at,c.created_at,''),CASE WHEN d.confirmed_at IS NOT NULL THEN 'publication_confirmation' WHEN a.finished_at IS NOT NULL THEN 'source_attempt_approximate' WHEN c.created_at IS NOT NULL THEN 'publication_intent_approximate' ELSE 'unknown' END FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions g ON g.id=i.definition_id JOIN sessions s ON s.id=i.session_id JOIN repos r ON r.id=s.repo_id JOIN graph_comment_intents c ON c.delivery_key=w.delivery_key JOIN report_deliveries d ON d.delivery_key=w.delivery_key LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id WHERE i.status='running' AND i.id=(SELECT MAX(x.id) FROM graph_instances x WHERE x.session_id=s.id) AND w.state='waiting' AND d.completed=1 AND d.comment_id>0 AND s.task_ref NOT IN (SELECT parent_task FROM managed_parent_closures) ORDER BY COALESCE(d.confirmed_at,a.finished_at,c.created_at) IS NULL,COALESCE(d.confirmed_at,a.finished_at,c.created_at),w.delivery_key LIMIT 1025`)
		if err != nil {
			return err
		}
		n := 0
		for rows.Next() {
			n++
			if n > 1024 {
				out.HasMore = true
				warn("graph_wait_scan_truncated")
				break
			}
			var w OverviewWait
			var since string
			if err = rows.Scan(&w.RepoID, &w.SessionID, &w.Repository, &w.TaskRef, &w.Workflow, &w.Stage, &w.WaitID, &w.Kind, &w.Summary, &w.URL, &w.commentID, &since, &w.SinceProvenance); err != nil {
				return errors.Join(err, rows.Close())
			}
			if planGraphWaitUsesState(w.Workflow, w.Stage) {
				continue
			}
			if at, e := ParseTime(since); e == nil {
				w.Since = &at
			}
			add(w)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		// Only sessions with published receipts can contribute owner waits. The
		// decode bound is enforced in SQL before copying JSON into Go.
		rows, err = tx.QueryContext(ctx, `SELECT r.id,s.id,substr(r.owner||'/'||r.name,1,512),substr(s.task_ref,1,1024),CASE WHEN length(CAST(p.state_json AS BLOB))<=1048576 THEN p.state_json ELSE '' END,length(CAST(p.state_json AS BLOB)) FROM plan_workflow_states p JOIN sessions s ON s.task_ref=p.parent JOIN repos r ON r.id=s.repo_id WHERE s.task_ref NOT IN (SELECT parent_task FROM managed_parent_closures) AND EXISTS(SELECT 1 FROM report_deliveries d WHERE d.session_id=s.id AND d.completed=1 AND d.comment_id>0) AND COALESCE((SELECT i.status FROM graph_instances i JOIN graph_definitions g ON g.id=i.definition_id WHERE i.session_id=s.id AND (g.workflow='plan' OR g.mode='plan') ORDER BY i.id DESC LIMIT 1),'running')='running' ORDER BY s.id LIMIT 129`)
		if err != nil {
			return err
		}
		type planCandidate struct {
			base OverviewWait
			raw  string
			size int
		}
		var candidates []planCandidate
		bytes := 0
		for rows.Next() {
			if len(candidates) == 128 {
				out.HasMore = true
				warn("plan_wait_scan_truncated")
				break
			}
			var p planCandidate
			if err = rows.Scan(&p.base.RepoID, &p.base.SessionID, &p.base.Repository, &p.base.TaskRef, &p.raw, &p.size); err != nil {
				return errors.Join(err, rows.Close())
			}
			bytes += p.size
			if p.size > 1048576 || bytes > 8*1048576 {
				out.HasMore = true
				warn("plan_wait_decode_truncated")
				continue
			}
			candidates = append(candidates, p)
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, p := range candidates {
			if err = ctx.Err(); err != nil {
				return err
			}
			var state planbrief.PlanState
			if json.Unmarshal([]byte(p.raw), &state) != nil {
				warn("plan_wait_state_unavailable")
				continue
			}
			// A state is at most 1 MiB, but explicit story bounds also cap the
			// revision-policy work performed for unusually dense JSON.
			if len(state.Stories) > 256 {
				out.HasMore = true
				warn("plan_wait_story_budget_exhausted")
				continue
			}
			publication := func(id, kind, stage, summary string, story *planbrief.PlanStoryState) error {
				w := p.base
				w.WaitID = id
				w.Workflow = "plan"
				w.Kind = kind
				w.Stage = stage
				var since, body string
				e := tx.QueryRowContext(ctx, `SELECT substr(COALESCE(d.confirmed_url,''),1,2048),d.comment_id,COALESCE(d.confirmed_at,a.finished_at,c.created_at,''),CASE WHEN d.confirmed_at IS NOT NULL THEN 'publication_confirmation' WHEN a.finished_at IS NOT NULL THEN 'source_attempt_approximate' WHEN c.created_at IS NOT NULL THEN 'publication_intent_approximate' ELSE 'unknown' END,substr(d.body,1,512) FROM report_deliveries d LEFT JOIN graph_comment_intents c ON c.delivery_key=d.delivery_key LEFT JOIN graph_node_attempts a ON a.id=c.source_attempt_id WHERE d.session_id=? AND (d.delivery_key=? OR d.purpose=?) AND d.completed=1 AND d.comment_id>0 AND NOT EXISTS(SELECT 1 FROM graph_owner_waits x WHERE x.delivery_key=d.delivery_key AND x.state<>'waiting') ORDER BY d.rowid DESC LIMIT 1`, w.SessionID, id, id).Scan(&w.URL, &w.commentID, &since, &w.SinceProvenance, &body)
				if errors.Is(e, sql.ErrNoRows) {
					return nil
				}
				if e != nil {
					return e
				}
				w.Summary = summary
				if strings.TrimSpace(body) != "" {
					w.Summary += "\n" + body
				}
				if at, e := ParseTime(since); e == nil {
					w.Since = &at
				}
				if story != nil {
					w.identitySession = -1
					var task string
					var childSession int64
					e = tx.QueryRowContext(ctx, `SELECT s.id,s.task_ref FROM plan_story_drafts p JOIN sessions s ON s.repo_id=? AND s.issue_number=p.issue_number WHERE p.parent=? AND p.story_key=? AND p.issue_id=? AND p.retired=0 LIMIT 1`, w.RepoID, p.base.TaskRef, story.Key, story.Wait.IssueID).Scan(&childSession, &task)
					switch {
					case e == nil:
						w.TaskRef = overviewWaitText(task, 1024)
						w.identitySession = childSession
					case !errors.Is(e, sql.ErrNoRows):
						return e
					default:
						warn("story_ticket_identity_unknown")
					}
				}
				add(w)
				return nil
			}
			if state.AssembledWait != "" && state.AssembledWait == state.PlanSetRevision() {
				if err = publication("plan-assembled:"+state.AssembledWait, "assembled-review", "assembled", "Waiting for assembled Plan review", nil); err != nil {
					return err
				}
			}
			if set := state.StorySet; set != nil && !set.Promoted && set.Revision != "" {
				var purpose string
				e := tx.QueryRowContext(ctx, `SELECT d.purpose FROM report_deliveries d JOIN graph_comment_intents c ON c.delivery_key=d.delivery_key WHERE d.session_id=? AND c.node='set_review' AND d.completed=1 AND d.comment_id>0 AND instr(d.body,?)>0 ORDER BY d.rowid DESC LIMIT 1`, p.base.SessionID, set.Revision).Scan(&purpose)
				if e == nil {
					if err = publication(purpose, "set-review", "set", "Waiting for story set review", nil); err != nil {
						return err
					}
				} else if !errors.Is(e, sql.ErrNoRows) {
					return e
				}
			}
			for _, story := range state.Stories {
				if !planStoryWaitCurrent(&state, story) {
					continue
				}
				if err = publication(story.Wait.ID, story.Wait.Kind, story.Stage, story.Key+": waiting for owner "+story.Wait.Kind, &story); err != nil {
					return err
				}
			}
		}
		for i := range out.Entries {
			w := &out.Entries[i]
			identitySession := w.identitySession
			if identitySession == 0 {
				identitySession = w.SessionID
			}
			identity, e := overviewIdentity(ctx, tx, identitySession)
			if e != nil {
				return e
			}
			w.Ticket = identity
			w.TaskURL = identity.TaskURL
			if w.URL == "" && identity.Provider == "github" && w.TaskURL != "" && w.commentID > 0 {
				w.URL = w.TaskURL + "#issuecomment-" + strconv.FormatInt(w.commentID, 10)
			}
			if w.Since == nil {
				warn("unknown_wait_age")
			} else if w.SinceProvenance != "publication_confirmation" {
				warn("approximate_wait_age")
			}
			if w.URL == "" {
				warn("wait_publication_url_unknown")
			}
		}
		return nil
	})
	if err != nil {
		return OverviewWaits{}, err
	}

	if len(out.Entries) > 50 {
		out.Entries = out.Entries[:50]
		out.HasMore = true
	}
	return out, nil
}

func overviewWaitLess(a, b OverviewWait) bool {
	if a.Since == nil && b.Since != nil {
		return false
	}
	if a.Since != nil && b.Since == nil {
		return true
	}
	if a.Since != nil && !a.Since.Equal(*b.Since) {
		return a.Since.Before(*b.Since)
	}
	return a.WaitID < b.WaitID
}
