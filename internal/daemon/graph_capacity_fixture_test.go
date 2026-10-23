package daemon_test

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func graphCapacityFiles(t *testing.T, mode string, nodes ...string) []byte {
	t.Helper()
	var dot strings.Builder
	fmt.Fprintf(&dot, "digraph { team=test; mode=%s; profile_version=1; start=%s;", mode, nodes[0])
	for _, node := range nodes {
		action := "lf_agent"
		if mode == "implement" && !strings.Contains(node, "agent") {
			action = "lf_implement_work"
		}
		fmt.Fprintf(&dot, "%s [lf_action=%s,lf_outcomes=success];", node, action)
	}
	dot.WriteString("}")
	raw, err := json.Marshal(map[string][]byte{mode + ".dot": []byte(dot.String())})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// capturedGraphInstance gives scheduler fixtures a retained immutable workflow,
// just as production admission does before recording a pending dispatch.
func capturedGraphInstance(t *testing.T, db *store.DB, in store.GraphInstance, files ...[]byte) (store.GraphInstance, error) {
	t.Helper()
	ctx := t.Context()
	definition, err := db.GetGraphDefinition(ctx, in.DefinitionID)
	if err != nil {
		return store.GraphInstance{}, err
	}
	if definition.Mode == "plan" {
		_, err = db.AdmitPlanWorkflowPath(ctx, in.SessionID, "graph")
	} else {
		_, err = db.AdmitWorkflowPath(ctx, in.SessionID, "graph")
	}
	if err != nil {
		return store.GraphInstance{}, err
	}
	raw := graphCapacityFiles(t, definition.Mode, in.CurrentNode, "waiting")
	if len(files) > 0 {
		raw, err = json.Marshal(map[string][]byte{path.Base(definition.Path): files[0]})
		if err != nil {
			return store.GraphInstance{}, err
		}
	}
	admitted, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: definition.Mode, SessionID: in.SessionID, TriggerID: 22, DefinitionID: in.DefinitionID, Start: in.CurrentNode, ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: raw})
	return admitted.Instance, err
}
