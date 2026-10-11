package ask

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// StoreReads is the exact read-only factory capability needed by facts.
type StoreReads interface {
	ListTaskSessionsByRepo(context.Context, int64, int) ([]store.Session, bool, error)
	LatestTaskRun(context.Context, int64) (store.Run, error)
	GetPlanWorkflowState(context.Context, string) (planbrief.PlanState, int64, error)
	ReadHealthSnapshot(context.Context) (store.HealthSnapshot, error)
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
		t := Task{IssueNumber: session.IssueNumber, Reference: session.TaskRef, State: string(session.State)}
		run, err := s.Reader.LatestTaskRun(ctx, session.ID)
		if err == nil {
			t.LatestRun = &run
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, more, err
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
		tasks = append(tasks, t)
	}
	return tasks, more, nil
}

// ReadDiscoveryPause uses the daemon's published health snapshot.
func (s StoreReader) ReadDiscoveryPause(ctx context.Context) (bool, bool, error) {
	snapshot, err := s.Reader.ReadHealthSnapshot(ctx)
	return snapshot.Listener.DiscoveryPaused, snapshot.Found, err
}
