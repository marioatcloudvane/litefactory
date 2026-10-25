package agent_test

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestTypedRepairCatalogIsClosedAndIndependent(t *testing.T) {
	want := []string{"cancel_schedule", "consume_comment", "graph_attempt_cancel", "graph_dispatch_cancel", "graph_fail", "graph_wait_cancel", "park_story", "requeue_schedule", "resume_story", "stop_run", "sync_branch"}
	if got := agent.TypedRepairToolKinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds %v", got)
	}
	tools := []string{"cancel_schedule", "communication_preflight", "communication_receipt", "communication_submit", "consume_comment", "graph_attempt_cancel", "graph_dispatch_cancel", "graph_fail", "graph_wait_cancel", "park_story", "requeue_schedule", "resume_story", "stop_run", "sync_branch"}
	if got := agent.RepairToolNames(); !reflect.DeepEqual(got, tools) {
		t.Fatalf("tools %v", got)
	}
	changed := agent.TypedRepairToolKinds()
	changed[0] = "shell"
	if !reflect.DeepEqual(agent.TypedRepairToolKinds(), want) {
		t.Fatal("caller changed shared catalog")
	}
}
