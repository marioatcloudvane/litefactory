package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// ResolveWorkflowJudgeSource selects only the chosen node's latest terminal
// attempt. A newer failed or empty result never falls back to older work.
func (db *DB) ResolveWorkflowJudgeSource(ctx context.Context, instanceID int64, node string) (JudgeSource, GraphNodeAttempt, error) {
	var source JudgeSource
	if instanceID < 1 || node == "" {
		return source, GraphNodeAttempt{}, ErrJudgeSourceUnavailable
	}
	attempt, err := scanGraphAttempt(db.sql.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE instance_id=? AND node=? AND status IN ('succeeded','failed','cancelled') ORDER BY attempt DESC LIMIT 1`, instanceID, node))
	if errors.Is(err, ErrNotFound) {
		return source, attempt, fmt.Errorf("%w: chosen source has no completed attempt", ErrJudgeSourceUnavailable)
	}
	if err != nil {
		return source, attempt, err
	}
	if attempt.Status != GraphSucceeded {
		return source, attempt, fmt.Errorf("%w: latest chosen source attempt did not succeed", ErrJudgeSourceUnavailable)
	}
	var mode string
	err = db.sql.QueryRowContext(ctx, `SELECT s.task_ref,d.mode,COALESCE((SELECT coding_agent FROM meter_attempts WHERE run_id=? ORDER BY ordinal DESC LIMIT 1),(SELECT agent FROM runs WHERE id=?),'') FROM graph_instances i JOIN sessions s ON s.id=i.session_id JOIN graph_definitions d ON d.id=i.definition_id WHERE i.id=?`, attempt.RunID, attempt.RunID, instanceID).Scan(&source.Task, &mode, &source.Provider)
	if err != nil {
		return source, attempt, err
	}
	if mode != "research" && mode != "plan" {
		return source, attempt, ErrJudgeSourceUnavailable
	}
	outputs, err := db.ListGraphResultOutputsBounded(ctx, attempt.ID, 1<<20, 16)
	if err != nil {
		return source, attempt, err
	}
	for _, output := range outputs {
		if output.Kind != GraphOutputJSON {
			continue
		}
		payload := output.Payload
		var ref struct {
			Version  int           `json:"version"`
			Kind     string        `json:"kind"`
			Binding  ResultBinding `json:"binding"`
			Revision int64         `json:"revision"`
		}
		if json.Unmarshal(payload, &ref) == nil && ref.Kind == "shared_result" {
			if ref.Version != 1 || ref.Revision < 1 || ref.Binding.RunID != attempt.RunID || ref.Binding.Task != source.Task || ref.Binding.Mode != mode {
				return JudgeSource{}, attempt, ErrResultAuthority
			}
			saved, e := db.GetResultRevision(ctx, ref.Binding, ref.Revision)
			if e != nil {
				return JudgeSource{}, attempt, e
			}
			payload = saved.Snapshot
			source.AcceptedResultID = saved.ResultID
			source.AcceptedRevision = saved.Revision
		}
		if mode == "research" && ref.Kind != "shared_result" {
			saved, e := db.workflowResearchAcceptedResult(ctx, attempt.RunID, source.Task, payload)
			if e != nil {
				return JudgeSource{}, attempt, e
			}
			payload = saved.Snapshot
			source.AcceptedResultID = saved.ResultID
			source.AcceptedRevision = saved.Revision
		}
		if mode == "plan" && ref.Kind != "shared_result" {
			return JudgeSource{}, attempt, fmt.Errorf("%w: Plan source has no accepted shared-result binding", ErrJudgeSourceUnavailable)
		}
		if !judgeSubstantive(payload, mode) {
			continue
		}
		if source.ID != 0 {
			return JudgeSource{}, attempt, fmt.Errorf("%w: chosen source has ambiguous saved results", ErrJudgeSourceUnavailable)
		}
		source.ID = output.ID
		source.Kind = "graph"
		source.Revision = 1
		source.Snapshot = payload
	}
	if source.ID == 0 {
		return source, attempt, fmt.Errorf("%w: latest chosen source is not substantive saved research or Plan work", ErrJudgeSourceUnavailable)
	}
	err = db.sql.QueryRowContext(ctx, `SELECT COALESCE((SELECT sequence FROM judge_source_commits WHERE kind='graph' AND active=1 AND source_id=? AND revision=1),0)`, fmt.Sprint(source.ID)).Scan(&source.Sequence)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return source, attempt, err
	}
	if mode == "plan" {
		if err = db.pinWorkflowPlanContext(ctx, &source); err != nil {
			return JudgeSource{}, attempt, err
		}
	}
	finishJudgeSource(&source)
	return source, attempt, nil
}

// pinWorkflowPlanContext retains constituent revisions without consulting later
// Plan state. Shared commit ordering is required to bind any parent snapshot.
func (db *DB) pinWorkflowPlanContext(ctx context.Context, source *JudgeSource) error {
	var envelope struct {
		Result struct {
			Payload struct {
				Plan struct {
					StoryRequest *struct {
						Draft          planbrief.PlanDraft `json:"draft"`
						SharedRevision string              `json:"shared_revision"`
					} `json:"storyrequest"`
					StorySet *struct {
						Entries []struct {
							Name   string `json:"name"`
							Intent string `json:"intent"`
						} `json:"entries"`
					} `json:"storyset"`
				} `json:"plan"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(source.Snapshot, &envelope); err != nil {
		return err
	}
	plan := envelope.Result.Payload.Plan
	if plan.StoryRequest != nil {
		source.PlanContext, _ = json.Marshal(struct {
			SharedRevision string              `json:"shared_revision"`
			Draft          planbrief.PlanDraft `json:"draft"`
			DraftRevision  string              `json:"draft_revision"`
		}{plan.StoryRequest.SharedRevision, plan.StoryRequest.Draft, plan.StoryRequest.Draft.Revision()})
		return nil
	}
	source.PlanContextGap = "No parent snapshot with matching constituent drafts could be bound before this saved source; only the accepted story-set request is available."
	if plan.StorySet == nil || source.Sequence < 1 {
		return nil
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT h.revision,h.state_json FROM plan_workflow_history h JOIN judge_source_commits c ON c.kind='plan' AND c.active=1 AND c.source_id=h.parent AND c.revision=h.revision WHERE h.parent=? AND c.sequence<? AND length(CAST(h.state_json AS BLOB))<=4194304 ORDER BY c.sequence DESC LIMIT 16`, source.Task, source.Sequence)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var revision int64
		var raw []byte
		if err = rows.Scan(&revision, &raw); err != nil {
			return err
		}
		var state planbrief.PlanState
		if json.Unmarshal(raw, &state) != nil {
			continue
		}
		var drafts []planbrief.PlanDraft
		revisions := map[string]string{}
		matched := true
		for _, entry := range plan.StorySet.Entries {
			matches := 0
			for _, story := range state.Stories {
				if !story.Retired && story.Draft.Name == entry.Name && story.Draft.UserStory.Want == entry.Intent {
					matches++
					drafts = append(drafts, story.Draft)
					revisions[story.Key] = story.Draft.Revision()
				}
			}
			if matches != 1 {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		source.PlanContext, err = json.Marshal(struct {
			Revision             int64                 `json:"revision"`
			SharedRevision       string                `json:"shared_revision"`
			Drafts               []planbrief.PlanDraft `json:"drafts"`
			ConstituentRevisions map[string]string     `json:"constituent_revisions"`
		}{revision, state.SharedRevision, drafts, revisions})
		source.PlanContextGap = ""
		return err
	}
	return rows.Err()
}

// workflowResearchAcceptedResult proves the graph projection against only the
// latest accepted revision saved by this source run. A mismatch never searches
// older accepted work, even when an earlier revision would match.
func (db *DB) workflowResearchAcceptedResult(ctx context.Context, runID int64, task string, projected []byte) (ResultRevision, error) {
	var saved ResultRevision
	if runID < 1 {
		return saved, fmt.Errorf("%w: Research source has no accepted-result run", ErrJudgeSourceUnavailable)
	}
	var size int64
	err := db.sql.QueryRowContext(ctx, `SELECT r.result_id,r.revision,r.run_id,r.attempt,r.request_row,r.snapshot_hash,length(r.snapshot),CASE WHEN length(r.snapshot)<=? THEN r.snapshot ELSE NULL END FROM application_result_revisions r JOIN application_results a ON a.id=r.result_id JOIN application_result_requests q ON q.id=r.request_row WHERE r.run_id=? AND a.task=? AND a.mode='research' AND a.workflow='' AND q.input_state='accepted' ORDER BY r.request_row DESC LIMIT 1`, MaxResultSnapshotBytes, runID, task).Scan(&saved.ResultID, &saved.Revision, &saved.RunID, &saved.Attempt, &saved.RequestRow, &saved.SnapshotHash, &size, &saved.Snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return saved, fmt.Errorf("%w: Research source has no accepted saved revision", ErrJudgeSourceUnavailable)
	}
	if err != nil {
		return saved, err
	}
	if size > MaxResultSnapshotBytes {
		return saved, fmt.Errorf("%w: Research accepted source exceeds input bounds", ErrJudgeSourceUnavailable)
	}
	canonicalResult := func(raw []byte) ([]byte, error) {
		var envelope struct {
			Result json.RawMessage `json:"result"`
		}
		if e := json.Unmarshal(raw, &envelope); e != nil {
			return nil, e
		}
		var result any
		if len(envelope.Result) == 0 {
			return nil, errors.New("missing result projection")
		}
		if e := json.Unmarshal(envelope.Result, &result); e != nil {
			return nil, e
		}
		return json.Marshal(result)
	}
	left, err := canonicalResult(projected)
	if err != nil {
		return saved, fmt.Errorf("%w: invalid graph result projection", ErrJudgeSourceUnavailable)
	}
	right, err := canonicalResult(saved.Snapshot)
	if err != nil {
		return saved, err
	}
	if !bytes.Equal(left, right) {
		return saved, fmt.Errorf("%w: Research graph projection differs from latest accepted revision", ErrJudgeSourceUnavailable)
	}
	return saved, nil
}
