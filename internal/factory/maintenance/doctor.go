package maintenance

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/store"
)

// DoctorAttempt is one step of a task's Doctor instance.
type DoctorAttempt struct {
	Node, Status, Outcome string
	Attempt               int
	RunID                 int64
}

// DoctorState is what the factory recorded about a task's Doctor.
type DoctorState struct {
	// Known is false when the task has no factory work at all.
	Known bool
	// Stopped reports whether the Doctor may start: nothing of the task runs.
	Stopped bool
	// Ran is false when the Doctor never ran on the task.
	Ran                     bool
	InstanceID              int64
	Status, Node, StartedAt string
	Attempts                []DoctorAttempt
}

// DoctorState reads the task's latest Doctor instance and whether it is stopped.
func (s *Service) DoctorState(ctx context.Context, taskRef string) (DoctorState, error) {
	var out DoctorState
	session, err := s.db.GetSessionByTaskRef(ctx, taskRef)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Known = true
	switch err := s.db.CheckTaskStopped(ctx, session.ID); {
	case err == nil:
		out.Stopped = true
	case !errors.Is(err, store.ErrTaskNotStopped):
		return out, err
	}
	instance, err := s.db.LatestWorkflowInstance(ctx, session.ID, "doctor")
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Ran, out.InstanceID, out.Status, out.Node, out.StartedAt = true, instance.ID, instance.Status, instance.CurrentNode, instance.StartedAt.Format("2006-01-02 15:04")
	attempts, err := s.db.ListGraphNodeAttempts(ctx, instance.ID)
	if err != nil {
		return out, err
	}
	for _, a := range attempts {
		out.Attempts = append(out.Attempts, DoctorAttempt{Node: a.Node, Status: a.Status, Outcome: a.Outcome, Attempt: a.Attempt, RunID: a.RunID})
	}
	return out, nil
}

// StopTask asks the daemon to stop the task's running invocation and reports
// which run it named; zero means nothing was running.
func (s *Service) StopTask(ctx context.Context, taskRef string) (int64, error) {
	stop, err := s.db.RequestOwnerStop(ctx, taskRef)
	return stop.RunID, err
}
