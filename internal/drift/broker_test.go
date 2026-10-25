package drift

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestBrokerCatalogAndRejectedEnvelopesShareAllowance(t *testing.T) {
	s, req := serviceFixture(t)
	s.Runner = &controlledRunner{run: func(ctx context.Context, i *Invocation) error {
		for _, request := range []submission.Request{
			{Method: "tools/list", Payload: json.RawMessage(`{}`)},
			{Method: "tools/call", Payload: json.RawMessage(`{"name":42}`)},
			{Method: "unsupported"},
		} {
			before, err := s.Store.GetDriftAnalysis(ctx, i.ID())
			if err != nil {
				return err
			}
			raw, callErr := i.handle(ctx, request)
			if request.Method == "tools/list" {
				if callErr != nil {
					return callErr
				}
				if len(raw) == 0 || len(raw) > 32<<10 {
					return fmt.Errorf("invalid catalog size %d", len(raw))
				}
			} else if callErr == nil {
				return fmt.Errorf("invalid %s request accepted", request.Method)
			}
			after, err := s.Store.GetDriftAnalysis(ctx, i.ID())
			if err != nil {
				return err
			}
			if after.Usage.Calls-before.Usage.Calls != 1 || after.Usage.OutboundBytes <= before.Usage.OutboundBytes {
				return fmt.Errorf("broker request escaped allowance: before=%+v after=%+v", before.Usage, after.Usage)
			}
		}
		return nil
	}}
	if _, err := s.Start(t.Context(), req); err != nil {
		t.Fatal(err)
	}
}

// transportRunner exercises the actual private adapter without a paid provider.
type transportRunner struct {
	run func(context.Context, agent.DriftRequest) (agent.DoctorRunResult, error)
}

func (r *transportRunner) DriftIsolation(context.Context) (string, error) {
	return "controlled transport fixture; no live certification", nil
}
func (r *transportRunner) RunDrift(ctx context.Context, request agent.DriftRequest) (agent.DoctorRunResult, error) {
	return r.run(ctx, request)
}

func TestBrokerReceiptUsesDurableTransportIdentity(t *testing.T) {
	s, req := serviceFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var directory string
	runner := &transportRunner{run: func(ctx context.Context, request agent.DriftRequest) (agent.DoctorRunResult, error) {
		directory = request.Invocation.WorkingDirectory
		if !strings.Contains(request.Invocation.Prompt, req.Base) || !strings.Contains(request.Invocation.Prompt, req.Head) || !strings.Contains(request.Invocation.Prompt, req.Repository) {
			return agent.DoctorRunResult{}, errors.New("analyst context omitted pinned scope")
		}
		analysisID := request.Invocation.Attempt
		client := submission.Client{DescriptorPath: request.Invocation.MCPLaunch.DescriptorPath()}
		catalog, err := client.List(ctx)
		if err != nil || catalog.Error != "" {
			return agent.DoctorRunResult{}, fmt.Errorf("catalog failed: %s: %w", catalog.Error, err)
		}
		refused, err := client.Call(ctx, "drift_evidence_list", json.RawMessage(`{}`))
		if err != nil {
			return agent.DoctorRunResult{}, err
		}
		if refused.Error == "" {
			return agent.DoctorRunResult{}, errors.New("broker work admitted before native catalog verification")
		}
		observed := agent.DoctorRunResult{ObservedModel: "fixture-observed-model", ProviderVersion: "fixture-native-version"}
		if err := request.OnInitialized(observed); err != nil {
			return observed, err
		}
		input := ResultInput{RequestID: "analyst-label", Report: Report{Disposition: "insufficient_evidence", Explanation: "Bounded fixture observation only.", Uncertainty: "No semantic review established."}}
		payload := bytesJSON(input)
		checked, err := client.Call(ctx, "communication_preflight", payload)
		if err != nil || checked.Error != "" {
			return observed, fmt.Errorf("preflight failed: %s: %w", checked.Error, err)
		}
		accepted, err := client.Call(ctx, "communication_submit", payload)
		if err != nil || accepted.Error != "" {
			return observed, fmt.Errorf("submit failed: %s: %w", accepted.Error, err)
		}
		if accepted.RequestID == "" || accepted.RequestID == input.RequestID {
			return observed, errors.New("transport did not allocate independent correlation")
		}
		retained, err := s.Store.GetDriftReceipt(ctx, analysisID, accepted.RequestID)
		if err != nil {
			return observed, err
		}
		if retained.Digest != digest(payload) {
			return observed, errors.New("transport changed exact accepted request bytes")
		}
		recovered, err := client.Call(ctx, "communication_receipt", bytesJSON(map[string]string{"request_id": accepted.RequestID}))
		if err != nil || recovered.Error != "" || !bytes.Equal(accepted.Result, recovered.Result) {
			return observed, fmt.Errorf("receipt lost original transport references: %s: %w", recovered.Error, err)
		}
		replay, err := client.Call(ctx, "communication_submit", payload)
		if err != nil || replay.Error != "" || replay.RequestID != accepted.RequestID || !bytes.Equal(accepted.Result, replay.Result) {
			return observed, fmt.Errorf("replay changed durable receipt: %s: %w", replay.Error, err)
		}
		input.RequestID = "different-label"
		changed, err := client.Call(ctx, "communication_submit", bytesJSON(input))
		if err != nil {
			return observed, err
		}
		if changed.Error == "" {
			return observed, errors.New("changed bytes created a second accepted report")
		}
		return observed, nil
	}}
	s.Runner = &Provider{Runner: runner, Model: "fixture-model", Directory: "/tmp", Executable: executable}
	a, err := s.Start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	var provenance map[string]string
	if err := json.Unmarshal(a.Provenance, &provenance); err != nil {
		t.Fatal(err)
	}
	if provenance["observed_model"] != "fixture-observed-model" || provenance["observed_provider_version"] != "fixture-native-version" {
		t.Fatalf("native observation not retained: %s", a.Provenance)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private broker directory survived close: %v", err)
	}
}
