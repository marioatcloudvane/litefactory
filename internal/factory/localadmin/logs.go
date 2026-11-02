package localadmin

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/logreader"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// LogTask contains the local task labels and paths needed by log rendering.
type LogTask struct{ TaskRef, State, Branch, WorktreePath string }

// LogRun is a detached presentation of a run. Database bookkeeping, lock and
// execution authority fields are deliberately absent.
type LogRun struct {
	Ordinal                           int
	Mode, Status, Error               string
	StartedAt, FinishedAt             time.Time
	ExitCode, DurationMS              *int64
	CostUSD                           *float64
	LogPath, CommitSHA                string
	TriggerCommentID, ResultCommentID int64
}

// Duration preserves unknown duration while a run has not finished.
func (r LogRun) Duration() (time.Duration, bool) {
	if r.DurationMS == nil {
		return 0, false
	}
	return time.Duration(*r.DurationMS) * time.Millisecond, true
}

// ResolveLogPath keeps recorded paths and the existing old-state fallback.
func ResolveLogPath(home, taskRef string, run LogRun) (string, error) {
	return logreader.ResolvePath(home, taskRef, store.Run{LogPath: run.LogPath, Ordinal: run.Ordinal})
}

// LogInspection retains one task and its ordered run history for local rendering.
type LogInspection struct {
	Session LogTask
	Runs    []LogRun
	Ref     naming.Ref
}

// InspectLogs loads a bounded task selection and closes storage before log streaming.
func InspectLogs(ctx context.Context, paths config.Paths, issue int, owner, name string) (_ LogInspection, err error) {
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return LogInspection{}, err
	}
	defer func() {
		if cerr := db.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	session, err := db.FindSessionByIssue(ctx, issue, owner, name)
	if err != nil {
		return LogInspection{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return LogInspection{}, fmt.Errorf("task %s: %w", session.TaskRef, err)
	}
	// ListRuns orders by ordinal, so the slice is oldest first and newest last
	// — the order the listing prints and the order the newest run is picked
	// from (AC-5.1).
	runs, err := db.ListRuns(ctx, session.ID)
	if err != nil {
		return LogInspection{}, err
	}
	view := LogInspection{Session: LogTask{TaskRef: session.TaskRef, State: string(session.State), Branch: session.Branch, WorktreePath: session.WorktreePath}, Ref: ref}
	for _, run := range runs {
		view.Runs = append(view.Runs, LogRun{Ordinal: run.Ordinal, Mode: run.Mode, Status: string(run.Status), Error: run.Error,
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, ExitCode: run.ExitCode, DurationMS: run.DurationMS, CostUSD: run.CostUSD,
			LogPath: run.LogPath, CommitSHA: run.CommitSHA, TriggerCommentID: run.TriggerCommentID, ResultCommentID: run.ResultCommentID})
	}
	return view, nil
}
