package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// TaskEvidencePage contains allowlisted local facts only. Offset counts source
// records, including records too large to display, so unavailable items cannot
// prevent a caller reaching later records.
type TaskEvidencePage struct {
	Revision   string
	Items      []map[string]any
	Sources    []map[string]any
	Gaps       []string
	HasMore    bool
	NextOffset int
}

const taskEvidenceUnitBytes = 256 << 10

// ReadTaskEvidence reads without admitting work or normalizing recorded waits.
// A transaction binds the page membership and its revision to one local view.
func (db *DB) ReadTaskEvidence(ctx context.Context, sessionID int64, section string, offset, limit int) (TaskEvidencePage, error) {
	p := TaskEvidencePage{Items: []map[string]any{}, Sources: []map[string]any{}, Gaps: []string{"local_observations_not_remote_poll"}, NextOffset: offset}
	if sessionID < 1 || offset < 0 || limit < 1 || limit > 50 || !slices.Contains([]string{"status", "results", "questions", "research", "plan", "approvals"}, section) {
		return p, errors.New("invalid evidence request")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var task, state, updated string
		var repoID int64
		if err := tx.QueryRowContext(ctx, `SELECT task_ref,state,updated_at,repo_id FROM sessions WHERE id=?`, sessionID).Scan(&task, &state, &updated, &repoID); err != nil {
			return err
		}
		// Version only task evidence, excluding connector audit and credential writes.
		var watermark string
		if err := tx.QueryRowContext(ctx, `SELECT json_array((SELECT COALESCE(MAX(o.id),0) FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id JOIN graph_instances i ON i.id=a.instance_id WHERE i.session_id=?),(SELECT COALESCE(MAX(v.request_row),0) FROM application_result_revisions v JOIN application_results r ON r.id=v.result_id WHERE r.task=?),(SELECT COUNT(*) FROM application_result_observations o JOIN application_result_requests q ON q.id=o.request_row JOIN application_results r ON r.id=q.result_id WHERE r.task=?))`, sessionID, task, task).Scan(&watermark); err != nil {
			return err
		}
		var questionWatermark string
		if err := tx.QueryRowContext(ctx, `SELECT CASE WHEN COUNT(*)<=4096 THEN COALESCE(json_group_array(json_array(substr(w.delivery_key,1,1024),w.state,w.reply_id,w.source_attempt_id,w.continuation_attempt_id,w.claimed_at)),'[]') ELSE 'unavailable' END FROM (SELECT w.delivery_key,w.state,w.reply_id,w.source_attempt_id,w.continuation_attempt_id,w.claimed_at FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id WHERE i.session_id=? ORDER BY w.delivery_key LIMIT 4097) w`, sessionID).Scan(&questionWatermark); err != nil {
			return err
		}
		if questionWatermark == "unavailable" {
			return errors.New("question evidence exceeds read budget")
		}
		revisionSeed := fmt.Sprintf("%s:%s:%s:%s:%s", watermark, updated, task, section, questionWatermark)
		p.Sources = append(p.Sources, map[string]any{"source": "local_task", "task_ref": task, "observed_at": updated})
		switch section {
		case "status":
			if offset == 0 {
				item := map[string]any{"id": fmt.Sprintf("session-%d", sessionID), "state": state, "observed_at": updated}
				var run string
				err := tx.QueryRowContext(ctx, `SELECT json_object('id',id,'mode',mode,'status',status,'semantic_outcome',semantic_outcome,'started_at',started_at,'finished_at',finished_at,'publication',publication,'published',published) FROM runs WHERE session_id=? ORDER BY id DESC LIMIT 1`, sessionID).Scan(&run)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if run != "" {
					var observed map[string]any
					if err = json.Unmarshal([]byte(run), &observed); err != nil {
						return err
					}
					item["latest_run"] = observed
					revisionSeed += run
				}
				p.Items = append(p.Items, item)
				p.NextOffset = 1
			}
			return evidenceRevision(&p, revisionSeed)
		case "plan", "approvals", "questions":
			parent := task
			storyKey := ""
			// Only the recorded story mapping permits a parent-owned read.
			var mappingCount int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_story_drafts d JOIN sessions s ON s.task_ref=d.parent WHERE s.repo_id=? AND d.issue_number=(SELECT issue_number FROM sessions WHERE id=?) AND d.retired=0`, repoID, sessionID).Scan(&mappingCount); err != nil {
				return err
			}
			if mappingCount > 1 {
				return errors.New("ambiguous task evidence mapping")
			}
			err := tx.QueryRowContext(ctx, `SELECT d.parent,d.story_key FROM plan_story_drafts d JOIN sessions s ON s.task_ref=d.parent WHERE s.repo_id=? AND d.issue_number=(SELECT issue_number FROM sessions WHERE id=?) AND d.retired=0 ORDER BY d.parent LIMIT 1`, repoID, sessionID).Scan(&parent, &storyKey)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			var historyVersion string
			if err := tx.QueryRowContext(ctx, `SELECT CASE WHEN COUNT(*)<=4096 THEN COALESCE(json_group_array(json_array(substr(w.delivery_key,1,1024),w.state,w.reply_id,w.source_attempt_id,w.continuation_attempt_id,w.claimed_at)),'[]') ELSE 'unavailable' END FROM (SELECT w.delivery_key,w.state,w.reply_id,w.source_attempt_id,w.continuation_attempt_id,w.claimed_at FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN sessions s ON s.id=i.session_id WHERE s.task_ref=? ORDER BY w.delivery_key LIMIT 4097) w`, parent).Scan(&historyVersion); err != nil {
				return err
			}
			if historyVersion == "unavailable" {
				return errors.New("question evidence exceeds read budget")
			}
			revisionSeed += historyVersion
			var raw string
			var rev int64
			err = tx.QueryRowContext(ctx, `SELECT revision,CASE WHEN length(CAST(state_json AS BLOB))<=4194304 THEN state_json ELSE '' END FROM plan_workflow_states WHERE parent=?`, parent).Scan(&rev, &raw)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			revisionSeed += fmt.Sprintf(":plan:%d", rev)
			selectedCount := 0
			if raw != "" {
				var ps planbrief.PlanState
				if json.Unmarshal([]byte(raw), &ps) != nil {
					p.Gaps = append(p.Gaps, "plan_source_unavailable")
				} else {
					selected := []map[string]any{}
					for _, st := range ps.Stories {
						if err := ctx.Err(); err != nil {
							return err
						}
						if storyKey != "" && st.Key != storyKey {
							continue
						}
						if storyKey != "" {
							p.Gaps = append(p.Gaps, "sibling_and_dependency_details_withheld")
						}
						switch section {
						case "plan":
							p.Gaps = append(p.Gaps, "shared_content_not_recorded_in_plan_state")
							selected = append(selected, map[string]any{"id": st.Key, "story_key": st.Key, "stage": st.Stage, "candidate": st.Candidate, "draft": st.Draft, "shared_revision": ps.SharedRevision, "state_revision": rev})
						case "questions":
							if st.Wait != nil {
								w := st.Wait
								current, e := ps.ReviewRevision(st.Key, w.Stage)
								if ps.SharedRevision == "" {
									e = errors.New("missing comparable shared revision")
								}
								applicability := evidenceApplicability(w.Revision, current, e)
								if applicability == "current" {
									applicability = priorApprovalApplicability(&ps, st, w.Stage)
								}
								if w.Candidate == "" || st.Candidate == "" {
									applicability = "unknown"
								} else if w.Candidate != st.Candidate {
									applicability = "stale"
								}
								source, sourceErr := readPlanQuestionSource(ctx, tx, parent, st.Key, w.Candidate)
								if sourceErr != nil {
									return sourceErr
								}
								currentUpstream, upstreamErr := ps.UpstreamReferences(st.Key, w.Stage)
								switch {
								case upstreamErr != nil:
									currentUpstream = nil
									source.Gaps = append(source.Gaps, "current_upstream_binding_unavailable")
									if applicability == "current" {
										applicability = "unknown"
									}
								case source.Upstream == nil && len(st.Draft.Dependencies.StoryKeys) > 0 && applicability == "current":
									applicability = "unknown"
								case source.Upstream != nil && !slices.Equal(dependencyBindings(source.Upstream, st.Key), dependencyBindings(currentUpstream, st.Key)):
									applicability = "stale"
								}
								if w.Stage != st.Stage {
									applicability = "stale"
								}
								item := map[string]any{"id": w.ID, "story_key": st.Key, "stage": w.Stage, "recorded_stage": w.Stage, "current_stage": st.Stage, "questions": source.Questions, "recorded_waiting": true, "recorded_candidate": w.Candidate, "current_candidate": st.Candidate, "recorded_revision": w.Revision, "current_revision": current, "recorded_shared_revision": source.SharedRevision, "current_shared_revision": ps.SharedRevision, "recorded_upstream": source.Upstream, "current_upstream": currentUpstream, "applicability": applicability, "source_id": source.SourceID, "gaps": source.Gaps}
								answer, answerState, answerGap, answerErr := readPlanQuestionAnswer(ctx, tx, parent, w.Candidate)
								if answerErr != nil {
									return answerErr
								}
								item["recorded_answer"] = answer
								item["answer_state"] = answerState
								if answerGap != "" {
									item["gaps"] = append(source.Gaps, answerGap)
								}
								if len(source.Questions) == 0 {
									item["question_texts"] = st.Draft.Questions
									item["question_texts_basis"] = "current_draft_not_recorded_question_source"
								}
								selected = append(selected, item)
							}
						case "approvals":
							for _, stage := range []string{"story", "technical", "acceptance"} {
								recorded := st.Approvals[stage]
								current, e := ps.ReviewRevision(st.Key, stage)
								if ps.SharedRevision == "" {
									e = errors.New("missing shared binding")
								}
								selected = append(selected, map[string]any{"id": st.Key + ":" + stage, "story_key": st.Key, "stage": stage, "recorded_approval": recorded, "current_revision": current, "applicability": approvalApplicability(&ps, st, stage, recorded, current, e), "approved_by": evidenceApprovalActor(st.ApprovedBy[stage], recorded), "current_candidate": st.Candidate, "current_shared_revision": ps.SharedRevision})
							}
						}
					}
					selectedCount = len(selected)
					if offset < len(selected) {
						end := min(offset+limit, len(selected))
						p.Items = append(p.Items, selected[offset:end]...)
						p.NextOffset = end
						p.HasMore = end < len(selected)
					}
					p.Sources = append(p.Sources, map[string]any{"source": "persisted_plan_state", "task_ref": parent, "revision": rev, "observation_time_status": "not_recorded"})
				}
			} else {
				p.Gaps = append(p.Gaps, "plan_not_recorded_or_archive_only")
			}
			if section != "questions" {
				return evidenceRevision(&p, revisionSeed)
			}
			if p.HasMore {
				return evidenceRevision(&p, revisionSeed)
			}
			return readRecordedQuestionHistory(ctx, tx, &p, revisionSeed, parent, storyKey, offset, limit, selectedCount)
		case "research":
			var mapped bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plan_story_drafts d JOIN sessions parent ON parent.task_ref=d.parent WHERE parent.repo_id=? AND d.issue_number=(SELECT issue_number FROM sessions WHERE id=?) AND d.retired=0)`, repoID, sessionID).Scan(&mapped); err != nil {
				return err
			}
			if mapped {
				p.Gaps = append(p.Gaps, "parent_research_withheld_or_unlinked")
			}
			return readEvidenceRows(ctx, tx, &p, revisionSeed, `SELECT id,body,observed_at,size FROM (SELECT 'result-'||CAST(v.result_id AS TEXT)||':'||CAST(v.revision AS TEXT) AS id,CASE WHEN length(v.snapshot)<=262144 THEN CAST(v.snapshot AS TEXT) ELSE '' END AS body,COALESCE(run.finished_at,run.started_at,'') AS observed_at,CAST(length(v.snapshot) AS TEXT) AS size FROM application_result_revisions v JOIN application_results r ON r.id=v.result_id LEFT JOIN runs run ON run.id=v.run_id WHERE r.task=? AND r.mode='research' UNION ALL SELECT 'graph-'||CAST(o.id AS TEXT),CASE WHEN length(o.payload)<=262144 THEN CAST(o.payload AS TEXT) ELSE '' END,o.created_at,CAST(length(o.payload) AS TEXT) FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions g ON g.id=i.definition_id WHERE i.session_id=? AND g.mode='research' AND o.kind='json') ORDER BY id LIMIT ? OFFSET ?`, []any{task, sessionID, limit + 1, offset}, []string{"id", "research_source", "observed_at", "source_bytes"}, limit, offset)
		case "results":
			return readEvidenceRows(ctx, tx, &p, revisionSeed, `SELECT CAST(v.result_id AS TEXT)||':'||CAST(v.revision AS TEXT),a.mode,CASE WHEN length(v.snapshot)<=262144 THEN CAST(v.snapshot AS TEXT) ELSE '' END,CAST(v.revision AS TEXT),q.input_state,COALESCE((SELECT state FROM application_result_observations o WHERE o.request_row=q.id AND dimension='engineering' ORDER BY sequence DESC LIMIT 1),'pending'),COALESCE((SELECT state FROM application_result_observations o WHERE o.request_row=q.id AND dimension='acceptance' ORDER BY sequence DESC LIMIT 1),'pending'),COALESCE((SELECT state FROM application_result_observations o WHERE o.request_row=q.id AND dimension='publication' ORDER BY sequence DESC LIMIT 1),'pending'),COALESCE(run.finished_at,run.started_at,'') FROM application_result_revisions v JOIN application_results a ON a.id=v.result_id JOIN application_result_requests q ON q.id=v.request_row LEFT JOIN runs run ON run.id=v.run_id WHERE a.task=? ORDER BY v.result_id,v.revision LIMIT ? OFFSET ?`, []any{task, limit + 1, offset}, []string{"id", "mode", "result_source", "revision", "input_state", "engineering_state", "acceptance_state", "publication_state", "observed_at"}, limit, offset)
		}
		return nil
	})
	return p, err
}

func evidenceApplicability(recorded, current string, err error) string {
	if recorded == "" || current == "" || err != nil {
		return "unknown"
	}
	if recorded == current {
		return "current"
	}
	return "stale"
}
func evidenceRevision(p *TaskEvidencePage, seed string) error {
	p.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(seed)))
	p.Sources = append(p.Sources, map[string]any{"source": "task_section", "revision": p.Revision, "observation_time_status": "source_specific"})
	return nil
}
func readEvidenceRows(ctx context.Context, tx *sql.Tx, p *TaskEvidencePage, seed, query string, args []any, keys []string, limit, offset int) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if len(p.Items) == limit {
			p.HasMore = true
			break
		}
		values := make([]string, len(keys))
		dest := make([]any, len(keys))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			return err
		}
		item := map[string]any{}
		for i, k := range keys {
			item[k] = values[i]
		}
		// Result snapshots have no independent timestamp column. Disclose the
		// recorded run timing used here instead of implying a result receipt time.
		if at, ok := item["observed_at"].(string); ok {
			basis := "run_started_or_finished"
			if id, ok := item["id"].(string); ok && strings.HasPrefix(id, "graph-") {
				basis = "graph_output_created_at"
			}
			if at == "" {
				basis = "not_recorded"
			}
			item["observation_time_basis"] = basis
		}
		for _, field := range []string{"result_source", "research_source"} {
			if raw, ok := item[field].(string); ok {
				delete(item, field)
				if raw == "" {
					item["unavailable"] = true
					p.Gaps = append(p.Gaps, "source_unavailable_or_oversized")
					continue
				}
				var doc map[string]any
				if json.Unmarshal([]byte(raw), &doc) != nil {
					p.Gaps = append(p.Gaps, "unsupported_source_format")
					continue
				}
				item["recorded"] = allowEvidenceDocument(doc)
			}
		}
		if size, ok := item["source_bytes"].(string); ok {
			n, _ := strconv.Atoi(size)
			if n > taskEvidenceUnitBytes {
				item["unavailable"] = true
			}
		}
		if _, ok := item["question"]; ok {
			item["applicability"] = "unknown"
			item["recorded_waiting"] = item["recorded_state"] == "waiting"
		}
		p.Items = append(p.Items, item)
		p.NextOffset = offset + len(p.Items)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(p.Items) == 0 {
		p.Gaps = append(p.Gaps, "no_local_records_or_archive_only")
	}
	return evidenceRevision(p, seed)
}

// Only authored owner-facing result fields are selected. Internal prompts,
// logs, arbitrary artifact paths and workflow credentials are never projected.
func allowEvidenceDocument(doc map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"outcome", "comment", "reason", "summary"} {
		if v, ok := doc[key].(string); ok {
			out[key] = v
		}
	}
	if value, ok := doc["questions"]; ok {
		var questions []agentresult.Question
		if decodeEvidenceValue(value, &questions) {
			out["questions"] = questions
		}
	}
	if value, ok := doc["recommendation"]; ok {
		var recommendation agentresult.Recommendation
		if decodeEvidenceValue(value, &recommendation) {
			out["recommendation"] = recommendation
		}
	}
	if value, ok := doc["findings"]; ok {
		var findings []agentresult.Finding
		if decodeEvidenceValue(value, &findings) {
			out["findings"] = findings
		}
	}
	for _, key := range []string{"decisions", "assumptions"} {
		if value, ok := doc[key]; ok {
			var statements []agentresult.Statement
			if decodeEvidenceValue(value, &statements) {
				out[key] = statements
			}
		}
	}
	if payload, ok := doc["payload"].(map[string]any); ok {
		if value, ok := payload["research"]; ok {
			var research agentresult.Research
			if decodeEvidenceValue(value, &research) {
				out["research"] = research
			}
		}
	}
	if result, ok := doc["result"].(map[string]any); ok {
		out["result"] = allowEvidenceDocument(result)
	}
	return out
}
func decodeEvidenceValue(value, dest any) bool {
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, dest) == nil
}

type planQuestionSource struct {
	Questions                []agentresult.Question
	SourceID, SharedRevision string
	Upstream                 []string
	Gaps                     []string
}

func readPlanQuestionSource(ctx context.Context, tx *sql.Tx, parent, key, candidate string) (planQuestionSource, error) {
	source := planQuestionSource{Questions: []agentresult.Question{}, Gaps: []string{}}
	if candidate == "" {
		source.Gaps = append(source.Gaps, "recorded_question_binding_missing")
		return source, nil
	}
	var raw string
	var id, revision int64
	instanceID, attemptID, canonical := evidenceCandidate(candidate)
	err := sql.ErrNoRows
	if canonical {
		// An ask result has no story request. Resolve the exact immutable shared
		// result retained by the attempt named in this story's persisted Plan wait.
		const output = `CASE WHEN length(o.payload)<=8192 THEN CASE WHEN json_valid(CAST(o.payload AS TEXT)) THEN CAST(o.payload AS TEXT) ELSE '{}' END ELSE '{}' END`
		query := `SELECT v.result_id,v.revision,CASE WHEN length(v.snapshot)<=262144 THEN CAST(v.snapshot AS TEXT) ELSE '' END FROM graph_outputs o JOIN graph_node_attempts a ON a.id=o.node_attempt_id JOIN graph_instances i ON i.id=a.instance_id JOIN sessions s ON s.id=i.session_id JOIN application_result_revisions v ON v.result_id=json_extract(` + output + `,'$.binding.ResultID') AND v.revision=json_extract(` + output + `,'$.revision') AND v.run_id=a.run_id JOIN application_results r ON r.id=v.result_id WHERE i.id=? AND a.id=? AND s.task_ref=? AND r.task=? AND r.mode='plan' AND o.kind='json' AND json_extract(` + output + `,'$.kind')='shared_result' ORDER BY o.id DESC LIMIT 1`
		err = tx.QueryRowContext(ctx, query, instanceID, attemptID, parent, parent).Scan(&id, &revision, &raw)
	}
	if errors.Is(err, sql.ErrNoRows) {
		const snapshot = `CASE WHEN length(v.snapshot)<=262144 THEN CAST(v.snapshot AS TEXT) ELSE '{}' END`
		err = tx.QueryRowContext(ctx, `SELECT v.result_id,v.revision,`+snapshot+` FROM application_result_revisions v JOIN application_results r ON r.id=v.result_id WHERE r.task=? AND r.mode='plan' AND json_valid(`+snapshot+`) AND json_extract(`+snapshot+`,'$.result.payload.plan.storyrequest.candidate')=? AND json_extract(`+snapshot+`,'$.result.payload.plan.storyrequest.draft.key')=? ORDER BY v.result_id DESC,v.revision DESC LIMIT 1`, parent, candidate, key).Scan(&id, &revision, &raw)
	}
	if errors.Is(err, sql.ErrNoRows) {
		source.Gaps = append(source.Gaps, "recorded_question_source_unavailable")
		return source, nil
	}
	if err != nil {
		return planQuestionSource{}, err
	}
	if raw == "" {
		source.Gaps = append(source.Gaps, "recorded_question_source_oversized")
		return source, nil
	}
	var snapshot agentresult.Snapshot
	if err = json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return planQuestionSource{}, fmt.Errorf("decode question source: %w", err)
	}

	if snapshot.Result.Payload.Plan != nil && snapshot.Result.Payload.Plan.StoryRequest != nil {
		request := snapshot.Result.Payload.Plan.StoryRequest
		if request.Draft.Key != key || request.Candidate != candidate {
			source.Gaps = append(source.Gaps, "recorded_question_source_binding_mismatch")
			return source, nil
		}
		source.SharedRevision = request.SharedRevision
		source.Upstream = slices.Clone(request.Upstream)
	}
	if source.SharedRevision == "" {
		source.Gaps = append(source.Gaps, "recorded_shared_revision_not_separately_retained")
	}
	source.Questions = snapshot.Result.Questions
	source.SourceID = fmt.Sprintf("result-%d:%d", id, revision)
	if source.Upstream == nil {
		source.Gaps = append(source.Gaps, "recorded_upstream_not_separately_retained")
	}
	return source, nil
}

func evidenceCandidate(candidate string) (int64, int64, bool) {
	var instanceID, attemptID int64
	n, err := fmt.Sscanf(candidate, "plan-%d-%d", &instanceID, &attemptID)
	return instanceID, attemptID, err == nil && n == 2 && instanceID > 0 && attemptID > 0 && fmt.Sprintf("plan-%d-%d", instanceID, attemptID) == candidate
}

func readPlanQuestionAnswer(ctx context.Context, tx *sql.Tx, parent, candidate string) (string, string, string, error) {
	instanceID, attemptID, valid := evidenceCandidate(candidate)
	if !valid {
		return "", "unknown", "answer_source_binding_unavailable", nil
	}
	var answer, state string
	err := tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(w.reply_body AS BLOB))<=65536 THEN COALESCE(w.reply_body,'') ELSE '' END,w.state FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN sessions s ON s.id=i.session_id WHERE s.task_ref=? AND w.instance_id=? AND w.source_attempt_id=? ORDER BY w.rowid DESC LIMIT 1`, parent, instanceID, attemptID).Scan(&answer, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "unknown", "recorded_answer_source_unavailable", nil
	}
	return answer, state, "", err
}

func priorApprovalApplicability(state *planbrief.PlanState, story planbrief.PlanStoryState, stage string) string {
	for _, earlier := range []string{"story", "technical", "acceptance"} {
		if earlier == stage {
			break
		}
		current, err := state.ReviewRevision(story.Key, earlier)
		if err != nil || current == "" || story.Approvals[earlier] == "" {
			return "unknown"
		}
		if story.Approvals[earlier] != current {
			return "stale"
		}
	}
	return "current"
}
func approvalApplicability(state *planbrief.PlanState, story planbrief.PlanStoryState, stage, recorded, current string, err error) string {
	result := evidenceApplicability(recorded, current, err)
	if result == "current" {
		return priorApprovalApplicability(state, story, stage)
	}
	return result
}

// Historical graph waits are separate observations from a current Plan wait.
// A story grant only sees source results explicitly bound to that story.
func readRecordedQuestionHistory(ctx context.Context, tx *sql.Tx, p *TaskEvidencePage, seed, parent, storyKey string, offset, limit, selectedCount int) error {

	if storyKey != "" {
		p.Gaps = append(p.Gaps, "unlinked_parent_question_history_withheld")
	}
	remaining := limit - len(p.Items)
	query := `SELECT w.delivery_key,CASE WHEN length(CAST(w.reply_body AS BLOB))<=65536 THEN COALESCE(w.reply_body,'') ELSE '' END,w.state,COALESCE(w.claimed_at,''),CASE WHEN length(v.snapshot)<=262144 THEN CAST(v.snapshot AS TEXT) ELSE '' END,CASE WHEN length(CAST(d.body AS BLOB))<=262144 THEN COALESCE(d.body,'') ELSE '' END FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN sessions s ON s.id=i.session_id LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id LEFT JOIN application_result_revisions v ON v.run_id=a.run_id AND v.request_row=(SELECT MAX(x.request_row) FROM application_result_revisions x WHERE x.run_id=a.run_id) LEFT JOIN report_deliveries d ON d.delivery_key=w.delivery_key WHERE s.task_ref=? AND (?='' OR (json_valid(CASE WHEN length(v.snapshot)<=262144 THEN CAST(v.snapshot AS TEXT) ELSE '{}' END) AND json_extract(CASE WHEN length(v.snapshot)<=262144 THEN CAST(v.snapshot AS TEXT) ELSE '{}' END,'$.result.payload.plan.storyrequest.draft.key')=?)) ORDER BY w.delivery_key LIMIT ? OFFSET ?`
	rows, err := tx.QueryContext(ctx, query, parent, storyKey, storyKey, remaining+1, max(0, offset-selectedCount))
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		if count == remaining {
			p.HasMore = true
			break
		}
		var id, answer, recordedState, at, source, body string
		if err = rows.Scan(&id, &answer, &recordedState, &at, &source, &body); err != nil {
			return err
		}
		item := map[string]any{"id": "history:" + id, "recorded_answer": answer, "recorded_state": recordedState, "recorded_waiting": recordedState == "waiting", "observed_at": at, "applicability": "unknown", "gaps": []string{"historical_revision_applicability_not_established"}}
		if source != "" {
			var snapshot agentresult.Snapshot
			if err = json.Unmarshal([]byte(source), &snapshot); err != nil {
				return err
			}
			item["questions"] = snapshot.Result.Questions
			item["recommendation"] = snapshot.Result.Recommendation
			if snapshot.Result.Payload.Plan != nil && snapshot.Result.Payload.Plan.StoryRequest != nil {
				request := snapshot.Result.Payload.Plan.StoryRequest
				item["recorded_candidate"] = request.Candidate
				item["recorded_shared_revision"] = request.SharedRevision
				item["recorded_upstream"] = request.Upstream
				item["recorded_stage"] = request.Stage
				item["recorded_story_key"] = request.Draft.Key
			}
		}
		if storyKey == "" {
			item["published_question_text"] = body
		}
		p.Items = append(p.Items, item)
		count++
		p.NextOffset = offset + len(p.Items)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(p.Items) == 0 {
		p.Gaps = append(p.Gaps, "no_local_question_records_or_archive_only")
	}
	return evidenceRevision(p, seed)
}

func evidenceApprovalActor(actor, recorded string) string {
	if recorded == "" {
		return "unknown"
	}
	if actor == "" {
		return "owner"
	}
	return actor
}

func dependencyBindings(refs []string, key string) []string {
	out := []string{}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, key+":") {
			out = append(out, ref)
		}
	}
	slices.Sort(out)
	return out
}
