package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// ErrJudgeSourceUnavailable identifies an owner-correctable saved-source selection failure.
var ErrJudgeSourceUnavailable = errors.New("saved judge source unavailable")

// JudgeSource is an immutable read projection, never a source writer grant.
type JudgeSource struct {
	Reference, Task, Kind, Provider, Digest string
	ID, Revision, Sequence                  int64
	Snapshot                                []byte
}

func judgeSubstantive(raw []byte, mode string) bool {
	var envelope struct {
		Result struct {
			Outcome string                     `json:"outcome"`
			Payload map[string]json.RawMessage `json:"payload"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Result.Outcome == "failed" {
		return false
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(envelope.Result.Payload[mode], &payload) != nil {
		return false
	}
	if mode == "research" {
		for _, key := range []string{"findings", "decisions", "assumptions"} {
			var items []json.RawMessage
			if json.Unmarshal(payload[key], &items) == nil && len(items) > 0 {
				return true
			}
		}
		return false
	}

	var stories []struct {
		Title     string `json:"title"`
		UserStory struct {
			Want string `json:"want"`
		} `json:"user_story"`
	}
	if json.Unmarshal(payload["stories"], &stories) == nil {
		for _, story := range stories {
			if strings.TrimSpace(story.Title) != "" && strings.TrimSpace(story.UserStory.Want) != "" {
				return true
			}
		}
	}
	var request struct {
		Draft planbrief.PlanDraft `json:"draft"`
	}
	if json.Unmarshal(payload["storyrequest"], &request) == nil && strings.TrimSpace(request.Draft.Name) != "" && strings.TrimSpace(request.Draft.UserStory.Want) != "" {
		return true
	}
	var set struct {
		Entries []struct {
			Name   string `json:"name"`
			Intent string `json:"intent"`
		} `json:"entries"`
	}
	if json.Unmarshal(payload["storyset"], &set) == nil {
		for _, entry := range set.Entries {
			if strings.TrimSpace(entry.Name) != "" && strings.TrimSpace(entry.Intent) != "" {
				return true
			}
		}
	}

	return false
}
func finishJudgeSource(s *JudgeSource) {
	s.Digest = submissionDigest(s.Snapshot)
	s.Reference = fmt.Sprintf("%s:%d:%d:%d:%s", s.Kind, s.ID, s.Revision, s.Sequence, s.Digest)
}

// ListJudgeSources reads retained local revisions only. Legacy saves with no
// cross-family ordering remain explicit choices when their order is unknowable.
func (db *DB) ListJudgeSources(ctx context.Context, task string) ([]JudgeSource, error) {
	var out []JudgeSource
	rows, err := db.sql.QueryContext(ctx, `SELECT a.id,r.revision,a.mode,r.snapshot,COALESCE((SELECT coding_agent FROM meter_attempts m WHERE m.run_id=r.run_id ORDER BY ordinal DESC LIMIT 1),(SELECT agent FROM runs WHERE id=r.run_id),''),COALESCE(c.sequence,0) FROM application_result_revisions r JOIN application_results a ON a.id=r.result_id LEFT JOIN judge_source_commits c ON c.kind='result' AND c.active=1 AND c.source_id=CAST(a.id AS TEXT) AND c.revision=r.revision WHERE a.task=? AND a.workflow='' AND a.mode IN ('research','plan') ORDER BY r.request_row`, task)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s JudgeSource
		s.Task = task
		if err = rows.Scan(&s.ID, &s.Revision, &s.Kind, &s.Snapshot, &s.Provider, &s.Sequence); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		if judgeSubstantive(s.Snapshot, s.Kind) {
			s.Kind = "result"
			finishJudgeSource(&s)
			out = append(out, s)
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}
	// Resolve a story only through its retained parent/story map, including retired history.
	parent, key := "", ""
	mappingRows, err := db.sql.QueryContext(ctx, `SELECT DISTINCT d.parent,d.story_key FROM plan_story_drafts d JOIN sessions p ON p.task_ref=d.parent JOIN sessions s ON s.repo_id=p.repo_id AND s.issue_number=d.issue_number WHERE s.task_ref=? LIMIT 2`, task)
	if err != nil {
		return nil, err
	}
	mappings := 0
	for mappingRows.Next() {
		if err = mappingRows.Scan(&parent, &key); err != nil {
			return nil, errors.Join(err, mappingRows.Close())
		}
		mappings++
	}
	err = errors.Join(mappingRows.Err(), mappingRows.Close())
	if err != nil {
		return nil, err
	}
	if mappings > 1 {
		return nil, fmt.Errorf("%w: this ticket has ambiguous retained Plan membership", ErrJudgeSourceUnavailable)
	}

	if parent == "" {
		parent = task
	}
	rows, err = db.sql.QueryContext(ctx, `SELECT h.rowid,h.revision,h.state_json,COALESCE(c.sequence,0) FROM plan_workflow_history h LEFT JOIN judge_source_commits c ON c.kind='plan' AND c.active=1 AND c.source_id=h.parent AND c.revision=h.revision WHERE h.parent=? ORDER BY h.revision`, parent)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s JudgeSource
		var raw []byte
		s.Task = task
		s.Kind = "plan"
		if err = rows.Scan(&s.ID, &s.Revision, &raw, &s.Sequence); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		var state planbrief.PlanState
		if json.Unmarshal(raw, &state) != nil {
			continue
		}
		if key != "" {
			for _, story := range state.Stories {
				if story.Key == key && story.Draft.Name != "" && story.Draft.UserStory.Want != "" {
					s.Snapshot, _ = json.Marshal(story.Draft)
					break
				}
			}
		} else {
			var stories []planbrief.PlanStoryState
			for _, story := range state.Stories {
				if !story.Retired && story.Draft.Name != "" && story.Draft.UserStory.Want != "" {
					stories = append(stories, story)
				}
			}
			if len(stories) > 0 {
				state.Stories = stories
				s.Snapshot, _ = json.Marshal(state)
			}
		}
		if len(s.Snapshot) > 0 {
			finishJudgeSource(&s)
			out = append(out, s)
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, err
	}

	for i := range out {
		if out[i].Kind != "plan" {
			continue
		}
		var drafts []planbrief.PlanDraft
		if key != "" {
			var draft planbrief.PlanDraft
			if json.Unmarshal(out[i].Snapshot, &draft) == nil {
				drafts = append(drafts, draft)
			}
		} else {
			var state planbrief.PlanState
			if json.Unmarshal(out[i].Snapshot, &state) == nil {
				for _, story := range state.Stories {
					drafts = append(drafts, story.Draft)
				}
			}
		}
		out[i].Provider, err = db.judgePlanProvider(ctx, parent, task, drafts)
		if err != nil {
			return nil, err
		}
	}
	rows, err = db.sql.QueryContext(ctx, `SELECT o.id,o.payload,COALESCE((SELECT coding_agent FROM meter_attempts m WHERE m.run_id=n.run_id ORDER BY ordinal DESC LIMIT 1),(SELECT agent FROM runs WHERE id=n.run_id),''),COALESCE(c.sequence,0),d.mode FROM graph_outputs o JOIN graph_node_attempts n ON n.id=o.node_attempt_id JOIN graph_instances i ON i.id=n.instance_id JOIN sessions s ON s.id=i.session_id JOIN graph_definitions d ON d.id=i.definition_id LEFT JOIN judge_source_commits c ON c.kind='graph' AND c.active=1 AND c.source_id=CAST(o.id AS TEXT) AND c.revision=1 WHERE s.task_ref=? AND d.mode IN ('research','plan') AND o.kind='json' AND n.status='succeeded' ORDER BY o.id`, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s JudgeSource
		var mode string
		s.Task = task
		s.Kind = "graph"
		s.Revision = 1
		if err = rows.Scan(&s.ID, &s.Snapshot, &s.Provider, &s.Sequence, &mode); err != nil {
			return nil, err
		}
		if judgeSubstantive(s.Snapshot, mode) {
			finishJudgeSource(&s)
			out = append(out, s)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}

// ResolveJudgeSource selects one eligible local revision without target substitution.
func (db *DB) ResolveJudgeSource(ctx context.Context, task, target string) (JudgeSource, error) {
	sources, err := db.ListJudgeSources(ctx, task)
	if err != nil {
		return JudgeSource{}, err
	}
	if target != "" {
		for _, s := range sources {
			if s.Reference == target {
				return s, nil
			}
		}
		return JudgeSource{}, fmt.Errorf("%w: target %q is unavailable in this ticket.%s", ErrJudgeSourceUnavailable, target, judgeSourceChoices(sources))
	}
	if len(sources) == 0 {
		return JudgeSource{}, fmt.Errorf("%w: no eligible saved research result or Plan exists in this ticket", ErrJudgeSourceUnavailable)
	}
	last := sources[len(sources)-1]
	if last.Sequence == 0 {
		families := map[string]bool{}
		for _, s := range sources {
			families[s.Kind] = true
		}
		if len(families) > 1 {
			return JudgeSource{}, fmt.Errorf("%w: legacy saved sources have no verifiable shared commit order; select an explicit saved reference.%s", ErrJudgeSourceUnavailable, judgeSourceChoices(sources))
		}
	}
	return last, nil
}

func judgeSourceChoices(sources []JudgeSource) string {
	if len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" Available saved references:")
	count := 0
	for i := len(sources) - 1; i >= 0 && count < 5; i-- {
		b.WriteString(" ")
		b.WriteString(sources[i].Reference)
		count++
	}
	if len(sources) > count {
		b.WriteString(" (showing latest five)")
	}
	return b.String()
}

// judgePlanProvider accepts provenance only from an exact retained draft match.
// A Plan assembled from different or unknown providers has no single provider.
func (db *DB) judgePlanProvider(ctx context.Context, parent, task string, drafts []planbrief.PlanDraft) (string, error) {
	if len(drafts) == 0 {
		return "", nil
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT r.snapshot,COALESCE((SELECT agent FROM runs WHERE id=r.run_id),'') FROM application_result_revisions r JOIN application_results a ON a.id=r.result_id WHERE a.task IN (?,?) AND a.mode='plan' AND a.workflow='' UNION ALL SELECT o.payload,COALESCE((SELECT agent FROM runs WHERE id=n.run_id),'') FROM graph_outputs o JOIN graph_node_attempts n ON n.id=o.node_attempt_id JOIN graph_instances i ON i.id=n.instance_id JOIN sessions s ON s.id=i.session_id JOIN graph_definitions d ON d.id=i.definition_id WHERE s.task_ref IN (?,?) AND d.mode='plan' AND o.kind='json' AND n.status='succeeded'`, parent, task, parent, task)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	providers := make(map[string]map[string]bool)
	for rows.Next() {
		var raw []byte
		var provider string
		if err = rows.Scan(&raw, &provider); err != nil {
			return "", err
		}
		var envelope struct {
			Result struct {
				Payload struct {
					Plan struct {
						StoryRequest *struct {
							Draft planbrief.PlanDraft `json:"draft"`
						} `json:"storyrequest"`
					} `json:"plan"`
				} `json:"payload"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Result.Payload.Plan.StoryRequest == nil {
			continue
		}
		draft := envelope.Result.Payload.Plan.StoryRequest.Draft
		revision := draft.Revision()
		if providers[revision] == nil {
			providers[revision] = map[string]bool{}
		}
		providers[revision][provider] = true
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	effective := ""
	for _, draft := range drafts {
		p := providers[draft.Revision()]
		if len(p) != 1 {
			return "", nil
		}
		for provider := range p {
			if provider != "claude" && provider != "codex" {
				return "", nil
			}
			if effective != "" && effective != provider {
				return "", nil
			}
			effective = provider
		}
	}
	return effective, nil
}
