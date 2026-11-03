package factory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/diskstatus"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// taskFactReads is the exact read-only factory capability needed by facts.
type taskFactReads interface {
	ListTaskSessionsByRepo(context.Context, int64, int) ([]store.Session, bool, error)
	ImplementStoryByWorkerSession(context.Context, int64) (store.ImplementStory, error)
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

// TaskFacts projects bounded task metadata without exposing storage operations.
type TaskFacts struct{ reader taskFactReads }

// NewTaskFacts composes factory-owned projection with private read-only persistence.
func NewTaskFacts(reader taskFactReads) *TaskFacts { return &TaskFacts{reader: reader} }

// ReadProjectTasks reads up to 100 current factory tasks and their latest run.
func (s *TaskFacts) ReadProjectTasks(ctx context.Context, repoID int64) ([]contracts.Task, bool, error) {
	sessions, more, err := s.reader.ListTaskSessionsByRepo(ctx, repoID, 100)
	if err != nil {
		return nil, false, err
	}
	tasks, err := s.readTaskSessions(ctx, sessions)
	return tasks, more, err
}

// ReadIssueTasks resolves an exact registered target without treating omission
// from the bounded project list as absence. It does not fetch remote history.
func (s *TaskFacts) ReadIssueTasks(ctx context.Context, repo contracts.TaskProject, issue int) ([]contracts.Task, error) {
	finder, ok := s.reader.(interface {
		FindSessionByIssue(context.Context, int, string, string) (store.Session, error)
	})
	if !ok {
		tasks, _, err := s.ReadProjectTasks(ctx, repo.ID)
		return tasks, err
	}
	session, err := finder.FindSessionByIssue(ctx, issue, repo.Owner, repo.Name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if session.RepoID != repo.ID || session.IssueNumber != issue {
		return nil, errors.New("ask: named issue does not match registered repository")
	}
	return s.readTaskSessions(ctx, []store.Session{session})
}

// ReadTaskSessions projects explicitly selected sessions using the same current facts.
func (s *TaskFacts) readTaskSessions(ctx context.Context, sessions []store.Session) ([]contracts.Task, error) {
	var tasks []contracts.Task
	for _, session := range sessions {
		t := contracts.Task{IssueNumber: session.IssueNumber, Reference: session.TaskRef}
		run, err := s.reader.LatestTaskRun(ctx, session.ID)
		if err == nil {
			t.LatestRun = &contracts.TaskRun{Ordinal: run.Ordinal, Mode: run.Mode, Workflow: run.Workflow, Status: string(run.Status), SemanticOutcome: run.SemanticOutcome, StartedAt: run.StartedAt}
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		// The stored session state is a bookkeeping flag that can stay
		// running after work ends; running comes only from the latest run.
		switch {
		case t.LatestRun.Active():
			t.State = string(store.SessionRunning)
		case session.State == store.SessionFailed:
			t.State = string(store.SessionFailed)
		default:
			t.State = string(store.SessionIdle)
		}
		state, _, err := s.reader.GetPlanWorkflowState(ctx, session.TaskRef)
		if err != nil {
			return nil, err
		}
		for _, story := range state.Stories {
			if !story.Retired {
				t.PlanStages = append(t.PlanStages, fmt.Sprintf("%s: %s", story.Key, story.Stage))
			}
		}
		if t.OwnerWait, err = s.ownerWait(ctx, session.ID); err != nil {
			return nil, err
		}
		story, err := s.reader.ImplementStoryByWorkerSession(ctx, session.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		// Implement parked work can only resume after an owner decision. Ordinary
		// graph scheduling/backoff is not an owner wait.
		if err == nil && story.State == store.ImplementParked {
			t.OwnerBlocked = factExcerpt(story.Reason, 512)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// ownerWait returns the session's published, unanswered owner question or
// review, using the same publication rule as /question. Nil means none.
func (s *TaskFacts) ownerWait(ctx context.Context, sessionID int64) (*contracts.OwnerWait, error) {
	instance, err := s.reader.LatestSessionGraphInstance(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil || instance.Status != store.GraphRunning {
		return nil, err
	}
	wait, err := s.reader.GetGraphOwnerWait(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil || wait.State != "waiting" {
		return nil, err
	}
	delivery, err := s.reader.GetReportDelivery(ctx, wait.DeliveryKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil || !delivery.Completed || delivery.CommentID < 1 {
		return nil, err
	}
	pin, err := s.reader.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return nil, err
	}
	intent, err := s.reader.GetGraphComment(ctx, wait.DeliveryKey)
	if err != nil {
		return nil, err
	}
	out := &contracts.OwnerWait{Workflow: pin.Workflow, Kind: intent.Kind, Node: intent.Node, CommentID: delivery.CommentID}
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
		attempt, err := s.reader.GetGraphNodeAttempt(ctx, wait.SourceAttemptID)
		if err == nil {
			out.Since = attempt.FinishedAt
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	return out, nil
}

// ReadDiscoveryPause uses the daemon's published health snapshot.
func (s *TaskFacts) ReadDiscoveryPause(ctx context.Context) (bool, bool, error) {
	snapshot, err := s.reader.ReadHealthSnapshot(ctx)
	return snapshot.Listener.DiscoveryPaused, snapshot.Found, err
}

// ReadRunsHeld uses the daemon's published health snapshot.
func (s *TaskFacts) ReadRunsHeld(ctx context.Context) (string, error) {
	snapshot, err := s.reader.ReadHealthSnapshot(ctx)
	return snapshot.Listener.RunsHeld, err
}

func factExcerpt(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text
}

// ReadDiskStatus returns the daemon's observations without refreshing their timestamps.
func (s *TaskFacts) ReadDiskStatus(ctx context.Context) (*diskstatus.Observation, error) {
	snapshot, err := s.reader.ReadHealthSnapshot(ctx)
	return snapshot.Listener.Disk, err
}
