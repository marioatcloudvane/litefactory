package messaging_test

import (
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

func fixtureNoticeEvents(source string, r store.NoticeRecord) ([]messaging.NoticeEvent, error) {
	_, stopped := r.Run.OwnerStop()
	return messaging.NoticeEvents(source, contracts.NoticeRecord{ID: r.ID, TaskRef: r.TaskRef, Workflow: r.Workflow, From: r.From, To: r.To, Key: r.Key, Kind: r.Kind, StopOrigin: r.StopOrigin, StopCause: r.StopCause, StopReason: r.StopReason, PreviousMode: r.PreviousMode, State: r.State, Published: r.Published, RunID: r.Run.ID, RunError: r.Run.Error, RunOwnerStopped: stopped, RunSucceeded: r.Run.Succeeded(), At: r.At, Candidate: r.Candidate, CommentID: r.CommentID, Ready: r.Ready, Ignore: r.Ignore})
}
