package ask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// StoreReads is the exact read-only factory capability needed by facts.
type StoreReads interface {
	ListTaskSessionsByRepo(context.Context, int64, int) ([]store.Session, bool, error)
	LatestTaskRun(context.Context, int64) (store.Run, error)
	GetPlanWorkflowState(context.Context, string) (planbrief.PlanState, int64, error)
	ReadHealthSnapshot(context.Context) (store.HealthSnapshot, error)
	LatestSessionGraphInstance(context.Context, int64) (store.GraphInstance, error)
	GetGraphOwnerWait(context.Context, int64) (store.GraphOwnerWait, error)
	GetGraphDefinition(context.Context, int64) (store.GraphDefinition, error)
	GetGraphComment(context.Context, string) (store.GraphCommentIntent, error)
	GetReportDelivery(context.Context, string) (store.ReportDelivery, error)
	GetGraphNodeAttempt(context.Context, int64) (store.GraphNodeAttempt, error)
}

// StoreReader projects bounded task metadata without exposing a writing client.
type StoreReader struct{ Reader StoreReads }

// ReadProjectTasks reads up to 100 current factory tasks and their latest run.
func (s StoreReader) ReadProjectTasks(ctx context.Context, repoID int64) ([]Task, bool, error) {
	sessions, more, err := s.Reader.ListTaskSessionsByRepo(ctx, repoID, 100)
	if err != nil {
		return nil, false, err
	}
	var tasks []Task
	for _, session := range sessions {
		t := Task{IssueNumber: session.IssueNumber, Reference: session.TaskRef}
		run, err := s.Reader.LatestTaskRun(ctx, session.ID)
		if err == nil {
			t.LatestRun = &run
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, more, err
		}
		// The stored session state is a bookkeeping flag that can stay
		// running after work ends; running comes only from the latest run.
		switch {
		case runActive(t.LatestRun):
			t.State = string(store.SessionRunning)
		case session.State == store.SessionFailed:
			t.State = string(store.SessionFailed)
		default:
			t.State = string(store.SessionIdle)
		}
		state, _, err := s.Reader.GetPlanWorkflowState(ctx, session.TaskRef)
		if err != nil {
			return nil, more, err
		}
		for _, story := range state.Stories {
			if !story.Retired {
				t.PlanStages = append(t.PlanStages, fmt.Sprintf("%s: %s", story.Key, story.Stage))
			}
		}
		if t.OwnerWait, err = s.ownerWait(ctx, session.ID); err != nil {
			return nil, more, err
		}
		tasks = append(tasks, t)
	}
	return tasks, more, nil
}

// ownerWait returns the session's published, unanswered owner question or
// review, using the same publication rule as /question. Nil means none.
func (s StoreReader) ownerWait(ctx context.Context, sessionID int64) (*OwnerWait, error) {
	instance, err := s.Reader.LatestSessionGraphInstance(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil || instance.Status != store.GraphRunning {
		return nil, err
	}
	wait, err := s.Reader.GetGraphOwnerWait(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil || wait.State != "waiting" {
		return nil, err
	}
	delivery, err := s.Reader.GetReportDelivery(ctx, wait.DeliveryKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil || !delivery.Completed || delivery.CommentID < 1 {
		return nil, err
	}
	pin, err := s.Reader.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return nil, err
	}
	intent, err := s.Reader.GetGraphComment(ctx, wait.DeliveryKey)
	if err != nil {
		return nil, err
	}
	out := &OwnerWait{Workflow: pin.Workflow, Kind: intent.Kind, Node: intent.Node, CommentID: delivery.CommentID}
	if out.Workflow == "" {
		out.Workflow = pin.Mode
	}
	var candidate struct {
		Questions []struct {
			Text string `json:"text"`
		} `json:"questions"`
	}
	if json.Unmarshal(intent.Candidate, &candidate) == nil {
		var texts []string
		for _, q := range candidate.Questions {
			if strings.TrimSpace(q.Text) != "" {
				texts = append(texts, q.Text)
			}
		}
		out.Asked = strings.Join(texts, "\n")
	}
	if wait.SourceAttemptID > 0 {
		attempt, err := s.Reader.GetGraphNodeAttempt(ctx, wait.SourceAttemptID)
		if err == nil {
			out.Since = attempt.FinishedAt
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	return out, nil
}

// ReadDiscoveryPause uses the daemon's published health snapshot.
func (s StoreReader) ReadDiscoveryPause(ctx context.Context) (bool, bool, error) {
	snapshot, err := s.Reader.ReadHealthSnapshot(ctx)
	return snapshot.Listener.DiscoveryPaused, snapshot.Found, err
}
