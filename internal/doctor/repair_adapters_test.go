package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type failedRepairProvider struct {
	t       *testing.T
	calls   int
	attempt string
}

func (p *failedRepairProvider) DoctorRepairIsolation(context.Context) (string, error) {
	return "controlled fixture; not authentic provider isolation", nil
}
func (p *failedRepairProvider) RunDoctorRepair(ctx context.Context, request agent.RepairRequest) (agent.DoctorRunResult, error) {
	p.calls++
	p.attempt = request.Invocation.UsageAttemptID
	if err := request.Validate(); err != nil {
		p.t.Fatal(err)
	}
	descriptor := request.Invocation.MCPLaunch.DescriptorPath()
	response, err := submission.Call(ctx, descriptor, "tools/list", json.RawMessage(`{}`))
	if err != nil || response.Error != "" {
		p.t.Fatalf("catalog: %+v %v", response, err)
	}
	var list struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(response.Result, &list); err != nil {
		p.t.Fatal(err)
	}
	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, agent.RepairToolNames()) {
		p.t.Fatalf("unexpected native repair tools: %v", names)
	}
	for _, raw := range []string{`{"name":"shell","arguments":{"command":"echo unapproved"}}`, `{"name":"doctor_write","arguments":{"step":1,"path":"foreign"}}`, `{"name":"doctor_write","arguments":{"step":1}}`} {
		response, err := submission.Call(ctx, descriptor, "tools/call", json.RawMessage(raw))
		if err == nil && response.Error == "" {
			p.t.Fatalf("unapproved or pre-backup write accepted: %s", raw)
		}
	}
	coverage := agent.UsageCoverage{Status: agent.CollectionPartial, SourceFormat: "fixture-repair", Diagnostics: []string{"provider_interrupted"}}
	invocation := agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: p.attempt, ID: "root", SourceID: "fixture", SourceInvocationID: "root", Coverage: coverage}}
	if err := request.Invocation.Usage.Emit(ctx, invocation); err != nil {
		p.t.Fatal(err)
	}
	tokens := int64(2)
	observation := agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: p.attempt, SourceID: "fixture", EventKey: "partial-message", InvocationID: "root", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &tokens}, ModelProvenance: "unknown", Coverage: coverage}}
	if err := request.Invocation.Usage.Emit(ctx, observation); err != nil {
		p.t.Fatal(err)
	}
	return agent.DoctorRunResult{Usage: &coverage}, errors.New("controlled provider failure")
}

func TestDoctorRepairAdapterRetainsFailedUsageAndBrokerScope(t *testing.T) {
	service, target, presentation, path := fileRepairFixture(t, false)
	provider := &failedRepairProvider{t: t}
	// Unix socket limits are shorter than testing's descriptive temporary paths.
	transportRoot, err := os.MkdirTemp("/tmp", "lf-repair-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(transportRoot); err != nil {
			t.Error(err)
		}
	})
	transportRoot, err = filepath.EvalSymlinks(transportRoot)
	if err != nil {
		t.Fatal(err)
	}
	service.RepairRunner = &RepairAdapters{Providers: map[string]agent.DoctorRepairRunner{"claude": provider}, Paths: config.NewPaths(transportRoot), Executable: filepath.Join(t.TempDir(), "factory")}
	service.RepairEnvironment = repairFixtureEnvironment{}
	service.RestartRoute = &repairFixtureRestart{}
	operation, err := service.Apply(t.Context(), ApplyRequest{Target: target, SourceID: "native-repair-source", Principal: "local-owner", PresentationID: presentation})
	if err == nil || provider.calls != 1 {
		t.Fatalf("expected failed native repair attempt: %+v %v", operation, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "original issue notes\n" {
		t.Fatalf("forbidden effect: %q %v", content, err)
	}
	snapshot, err := service.DB.NewMeterSnapshot(t.Context(), store.MeterFilter{}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snapshot.Close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	rows, err := snapshot.Next(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.AttemptID == provider.attempt && row.Tokens.Output != nil {
			found = true
			if row.RunID != 0 || row.ExecutionPath != "doctor" || row.Outcome != "failure" || row.CollectionStatus != "partial" || *row.Tokens.Output != 2 || row.ActualModel != nil {
				t.Fatalf("partial failure accounting lost: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("failed repair usage missing from shared ledger")
	}
}
