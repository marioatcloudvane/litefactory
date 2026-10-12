package ask_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// waitStore is a read-only fake keyed by session ID (instance ID equals session ID).
type waitStore struct {
	sessions   []store.Session
	instances  map[int64]store.GraphInstance
	waits      map[int64]store.GraphOwnerWait
	deliveries map[string]store.ReportDelivery
	finished   time.Time
	runs       map[int64]store.Run
}

func (w *waitStore) ListTaskSessionsByRepo(context.Context, int64, int) ([]store.Session, bool, error) {
	return w.sessions, false, nil
}
func (w *waitStore) ImplementStoryByWorkerSession(context.Context, int64) (store.ImplementStory, error) {
	return store.ImplementStory{}, store.ErrNotFound
}
func (w *waitStore) LatestTaskRun(_ context.Context, id int64) (store.Run, error) {
	if run, ok := w.runs[id]; ok {
		return run, nil
	}
	return store.Run{}, store.ErrNotFound
}
func (w *waitStore) GetPlanWorkflowState(context.Context, string) (planbrief.PlanState, int64, error) {
	return planbrief.PlanState{}, 0, nil
}
func (w *waitStore) ReadHealthSnapshot(context.Context) (store.HealthSnapshot, error) {
	return store.HealthSnapshot{}, nil
}
func (w *waitStore) LatestSessionGraphInstance(_ context.Context, id int64) (store.GraphInstance, error) {
	if in, ok := w.instances[id]; ok {
		return in, nil
	}
	return store.GraphInstance{}, store.ErrNotFound
}
func (w *waitStore) GetGraphOwnerWait(_ context.Context, id int64) (store.GraphOwnerWait, error) {
	if wait, ok := w.waits[id]; ok {
		return wait, nil
	}
	return store.GraphOwnerWait{}, store.ErrNotFound
}
func (w *waitStore) GetGraphDefinition(_ context.Context, id int64) (store.GraphDefinition, error) {
	return store.GraphDefinition{ID: id, Mode: "research", Workflow: "research"}, nil
}
func (w *waitStore) GetGraphComment(_ context.Context, key string) (store.GraphCommentIntent, error) {
	return store.GraphCommentIntent{Key: key, Node: "scope", Kind: "question", Candidate: []byte(`{"questions":[{"text":"Approve scope ` + key + `?"},{"text":" "}]}`)}, nil
}
func (w *waitStore) GetReportDelivery(_ context.Context, key string) (store.ReportDelivery, error) {
	if d, ok := w.deliveries[key]; ok {
		return d, nil
	}
	return store.ReportDelivery{}, store.ErrNotFound
}
func (w *waitStore) GetGraphNodeAttempt(_ context.Context, id int64) (store.GraphNodeAttempt, error) {
	return store.GraphNodeAttempt{ID: id, FinishedAt: w.finished}, nil
}

// newWaitStore seeds one session per issue, each with a published, waiting
// owner wait on a running graph instance.
func newWaitStore(finished time.Time, issues ...int) *waitStore {
	w := &waitStore{instances: map[int64]store.GraphInstance{}, waits: map[int64]store.GraphOwnerWait{}, deliveries: map[string]store.ReportDelivery{}, finished: finished}
	for _, n := range issues {
		id := int64(n)
		key := fmt.Sprintf("key-%d", n)
		w.sessions = append(w.sessions, store.Session{ID: id, IssueNumber: n, State: store.SessionState("running")})
		w.instances[id] = store.GraphInstance{ID: id, SessionID: id, DefinitionID: 1, Status: store.GraphRunning}
		w.waits[id] = store.GraphOwnerWait{DeliveryKey: key, InstanceID: id, SourceAttemptID: 100 + id, State: "waiting"}
		w.deliveries[key] = store.ReportDelivery{Key: key, Completed: true, CommentID: 9000 + id}
	}
	return w
}

func TestStoreReaderOwnerWaits(t *testing.T) {
	t.Parallel()
	finished := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	w := newWaitStore(finished, 1, 2)
	w.sessions = append(w.sessions, store.Session{ID: 3, IssueNumber: 3})
	tasks, _, err := ask.StoreReader{Reader: w}.ReadProjectTasks(t.Context(), 1)
	if err != nil || len(tasks) != 3 {
		t.Fatalf("tasks: %+v, %v", tasks, err)
	}
	got := tasks[0].OwnerWait
	if got == nil || got.Workflow != "research" || got.Kind != "question" || got.Node != "scope" || got.CommentID != 9001 || !got.Since.Equal(finished) || got.Asked != "Approve scope key-1?" {
		t.Fatalf("owner wait: %+v", got)
	}
	if tasks[1].OwnerWait == nil || tasks[2].OwnerWait != nil {
		t.Fatalf("waits: %+v", tasks)
	}
	for _, tc := range []struct {
		name  string
		alter func(*waitStore)
	}{
		{"claimed", func(w *waitStore) { w.waits[1] = store.GraphOwnerWait{DeliveryKey: "key-1", State: "claimed"} }},
		{"allocated", func(w *waitStore) { w.waits[1] = store.GraphOwnerWait{DeliveryKey: "key-1", State: "allocated"} }},
		{"cancelled", func(w *waitStore) { w.waits[1] = store.GraphOwnerWait{DeliveryKey: "key-1", State: "cancelled"} }},
		{"succeeded instance", func(w *waitStore) { w.instances[1] = store.GraphInstance{ID: 1, Status: store.GraphSucceeded} }},
		{"failed instance", func(w *waitStore) { w.instances[1] = store.GraphInstance{ID: 1, Status: store.GraphFailed} }},
		{"cancelled instance", func(w *waitStore) { w.instances[1] = store.GraphInstance{ID: 1, Status: store.GraphCancelled} }},
		{"undelivered", func(w *waitStore) { w.deliveries["key-1"] = store.ReportDelivery{Key: "key-1", CommentID: 9001} }},
		{"no comment", func(w *waitStore) { w.deliveries["key-1"] = store.ReportDelivery{Key: "key-1", Completed: true} }},
		{"no delivery", func(w *waitStore) { delete(w.deliveries, "key-1") }},
		{"no wait", func(w *waitStore) { delete(w.waits, 1) }},
		{"no instance", func(w *waitStore) { delete(w.instances, 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWaitStore(finished, 1)
			tc.alter(w)
			tasks, _, err := ask.StoreReader{Reader: w}.ReadProjectTasks(t.Context(), 1)
			if err != nil || len(tasks) != 1 || tasks[0].OwnerWait != nil {
				t.Fatalf("unexpected wait: %+v, %v", tasks, err)
			}
		})
	}
}

func TestStoreReaderTaskStateFromLatestRun(t *testing.T) {
	t.Parallel()
	w := &waitStore{runs: map[int64]store.Run{
		1: {Status: store.RunSucceeded},
		2: {Status: store.RunRunning},
		3: {Status: store.RunPrelaunch},
		4: {Status: store.RunFailed},
	}}
	w.sessions = []store.Session{
		{ID: 1, IssueNumber: 1, State: store.SessionRunning},
		{ID: 2, IssueNumber: 2, State: store.SessionIdle},
		{ID: 3, IssueNumber: 3, State: store.SessionIdle},
		{ID: 4, IssueNumber: 4, State: store.SessionFailed},
		{ID: 5, IssueNumber: 5, State: store.SessionRunning},
	}
	tasks, _, err := ask.StoreReader{Reader: w}.ReadProjectTasks(t.Context(), 1)
	if err != nil || len(tasks) != 5 {
		t.Fatalf("tasks: %+v, %v", tasks, err)
	}
	for i, want := range []string{"idle", "running", "running", "failed", "idle"} {
		if tasks[i].State != want {
			t.Errorf("task %d state %q, want %q", tasks[i].IssueNumber, tasks[i].State, want)
		}
	}
}
