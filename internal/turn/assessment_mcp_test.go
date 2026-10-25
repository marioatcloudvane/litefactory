package turn

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestManagedPreparationAssessmentMCPResult(t *testing.T) {
	db, session, options := agentResultFixture(t, "implement")
	authority, err := NewAttemptAuthority(t.Context(), db)
	mustProgress(t, err)
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	ref, err := naming.ParseRef(session.TaskRef)
	mustProgress(t, err)
	prep := &preparation{ref: ref, session: session, run: store.Run{ID: options.Attempt.RunID, PreparationCaveats: []string{assessmentPreparation}}, mcpRuntime: &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}}
	runtime, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "implement"}, nil)
	mustProgress(t, err)
	t.Cleanup(func() {
		if err := runtime.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	client := submission.Client{DescriptorPath: spec.DescriptorPath()}
	response, err := client.Call(t.Context(), "communication_preflight", resultCreate("implement"))
	mustProgress(t, err)
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	var diagnostics ar.Diagnostics
	mustProgress(t, json.Unmarshal(response.Result, &diagnostics))
	if !diagnostics.Valid || !diagnostics.Complete {
		t.Fatalf("reading-only result rejected: %+v", diagnostics)
	}
	c, err := runtime.service.resolve(t.Context())
	mustProgress(t, err)
	if c.RequireAggregate || c.WriteAccess || c.RepairRole || len(c.Stories) != 0 || runtime.service.prepareAggregate != nil || runtime.service.validateCompletion != nil {
		t.Fatalf("assessment gained engineering effects: %+v", c)
	}
	submitted, err := runtime.service.Submit(t.Context(), "assessment-result", resultCreate("implement"))
	mustProgress(t, err)
	if !submitted.Validation.Valid || submitted.Receipt == nil || submitted.Receipt.Latest.Input != "accepted" {
		t.Fatalf("assessment result not retained: %+v", submitted)
	}
	// A reading assessment has no selected story it could complete.
	unauthorized := []byte(`{"result":{"outcome":"incomplete","reason":"Reading only","comment":"assessment","payload":{"implement":{"stories":[{"story":"invented","outcome":"engineering_complete"}]}}}}`)
	rejected, err := runtime.service.Preflight(t.Context(), unauthorized)
	mustProgress(t, err)
	if rejected.Valid {
		t.Fatal("assessment accepted invented engineering scope")
	}
}
