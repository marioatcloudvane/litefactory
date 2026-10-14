package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type recoveryJudgeRunner struct {
	calls     []agent.JudgeRequest
	failure   error
	malformed bool
}

func (r *recoveryJudgeRunner) RunJudge(ctx context.Context, q agent.JudgeRequest) (agent.JudgeRunResult, error) {
	r.calls = append(r.calls, q)
	if r.failure != nil {
		return agent.JudgeRunResult{}, r.failure
	}
	if r.malformed {
		return agent.JudgeRunResult{}, nil
	}
	client := submission.Client{DescriptorPath: q.Invocation.MCPLaunch.DescriptorPath()}
	call := func(name string, input any) (submission.Response, error) {
		raw, err := json.Marshal(input)
		if err != nil {
			return submission.Response{}, err
		}
		response, err := client.Call(ctx, name, raw)
		if err == nil && response.Error != "" {
			err = errors.New(response.Error)
		}
		return response, err
	}
	input := agentresult.JudgeInput{Report: agentresult.JudgeReport{Version: 1, Verdict: "unable_to_assess", Summary: "Captured source inspected; repository unavailable", Coverage: []string{"source"}, Uncertainty: []string{"Repository snapshot is unavailable."}, Findings: []agentresult.JudgeFinding{}}}
	preflight, err := call("communication_preflight", input)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	var d agentresult.Diagnostics
	if json.Unmarshal(preflight.Result, &d) != nil || !d.Valid {
		return agent.JudgeRunResult{}, fmt.Errorf("invalid preflight %s", preflight.Result)
	}
	response, err := call("communication_submit", input)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	if _, err = call("communication_receipt", map[string]string{"request_id": response.RequestID}); err != nil {
		return agent.JudgeRunResult{}, err
	}
	return agent.JudgeRunResult{ObservedModel: q.Invocation.Model + "-observed", ProviderVersion: "controlled-version"}, nil
}

func judgeRecoveryFixture(t *testing.T) (*JudgeService, JudgeRequest, store.ResultBinding) {
	t.Helper()
	db, session, options := agentResultFixture(t, "research")
	binding, err := db.BindResultAttempt(t.Context(), options.Attempt)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := []byte(`{"result":{"outcome":"completed","payload":{"research":{"request":"saved investigation","findings":[{"id":"f1","text":"Saved conclusion"}]}}}}`)
	if _, err = db.RecordResult(t.Context(), store.ResultRecord{Binding: binding, RequestID: "saved-source", ExpectedRevision: 0, Payload: []byte(`{}`), Diagnostics: []byte(`{}`), Snapshot: snapshot, Retired: []byte(`[]`), Accepted: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n  codex: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/tmp", "lf-jrec-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	service := &JudgeService{DB: db, Config: cfg, Paths: config.NewPaths(root), Executable: "/usr/bin/false", Providers: map[agent.Kind]agent.JudgeRunner{}, Redactor: redact.New(redact.Options{})}
	request := JudgeRequest{Ref: ref, SessionID: session.ID, CommentID: 9, TicketKey: "qualified-ticket", RemoteCommentID: "comment-9", TaskProvider: config.KindClaude, Options: JudgeCommandOptions{JudgeOptions: config.JudgeOptions{Agent: "other", Thinking: "high"}}}
	return service, request, binding
}

func TestJudgeAvailabilityFallbackAndDurableReplay(t *testing.T) {
	service, request, writer := judgeRecoveryFixture(t)
	primary := &recoveryJudgeRunner{failure: agent.ErrJudgeUnavailable}
	fallback := &recoveryJudgeRunner{}
	service.Providers[agent.Kind("codex")] = primary
	service.Providers[agent.Kind("claude")] = fallback
	first, err := service.Execute(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(primary.calls) != 1 || len(fallback.calls) != 1 {
		t.Fatalf("launches %d %d", len(primary.calls), len(fallback.calls))
	}
	if primary.calls[0].Invocation.Model == fallback.calls[0].Invocation.Model || primary.calls[0].Thinking != "high" || fallback.calls[0].Thinking != "high" {
		t.Fatal("fallback failed recompute or override preservation")
	}
	q, err := service.DB.FindJudgeRequest(t.Context(), request.TicketKey, request.RemoteCommentID)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := service.DB.ListJudgeAttempts(t.Context(), q.ID)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts %+v %v", attempts, err)
	}
	for _, call := range []agent.JudgeRequest{primary.calls[0], fallback.calls[0]} {
		meter, e := service.DB.GetMeterAttempt(t.Context(), call.Invocation.UsageAttemptID)
		if e != nil || meter.JudgeAttemptID != call.Invocation.Attempt || meter.RunID != 0 {
			t.Fatalf("meter %+v %v", meter, e)
		}
	}
	if err = service.DB.CheckResultWriter(t.Context(), writer); err != nil {
		t.Fatalf("source writer changed: %v", err)
	}
	request.Options.Target = "invalid-retarget"
	again, err := service.Execute(t.Context(), request)
	if err != nil || again != first || len(primary.calls) != 1 || len(fallback.calls) != 1 {
		t.Fatal("saved report replay relaunched or retargeted", err)
	}
}

func TestJudgeNonAvailabilityFailuresNeverFallback(t *testing.T) {
	for _, test := range []struct {
		name      string
		failure   error
		malformed bool
	}{{"invalid output", nil, true}, {"isolation refusal", agent.ErrJudgeIsolationUnverified, false}, {"authorization refusal", errors.New("authorization refused"), false}} {
		t.Run(test.name, func(t *testing.T) {
			service, request, _ := judgeRecoveryFixture(t)
			primary := &recoveryJudgeRunner{failure: test.failure, malformed: test.malformed}
			fallback := &recoveryJudgeRunner{}
			service.Providers[agent.Kind("codex")] = primary
			service.Providers[agent.Kind("claude")] = fallback
			body, err := service.Execute(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(primary.calls) != 1 || len(fallback.calls) != 0 || !strings.Contains(body, "unable to assess") {
				t.Fatalf("failure cleaned or retried: %s launches %d %d", body, len(primary.calls), len(fallback.calls))
			}
		})
	}
}

func TestJudgeInterruptedAndAcceptedLaunchRecovery(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(fmt.Sprintf("accepted_%v", accepted), func(t *testing.T) {
			service, request, _ := judgeRecoveryFixture(t)
			source, err := service.DB.ResolveJudgeSource(t.Context(), request.Ref.String(), "")
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := service.Config.ResolveJudge(request.Options.JudgeOptions, request.TaskProvider, config.Kind(source.Provider))
			if err != nil {
				t.Fatal(err)
			}
			cfg, _ := json.Marshal(judgeConfiguration{Requested: request.Options, Effective: invocation})
			evidence := JudgeEvidenceSnapshot{Evidence: []agentresult.JudgeEvidence{{ID: "source", Kind: "source", Available: true, Essential: true, Lines: 1}}, Contents: map[string]string{"source": "saved"}}
			rawEvidence, _ := json.Marshal(evidence)
			q, err := service.DB.AdmitJudgeRequest(t.Context(), store.JudgeRequest{Task: request.Ref.String(), TicketKey: request.TicketKey, CommentID: request.RemoteCommentID, SessionID: request.SessionID, TriggerCommentID: request.CommentID, Source: source, Configuration: cfg, Evidence: rawEvidence})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := service.DB.StartJudgeAttempt(t.Context(), q.ID, string(invocation.Provider), invocation.Model, string(invocation.Thinking))
			if err != nil {
				t.Fatal(err)
			}
			if accepted {
				results, err := bindJudgeResults(t.Context(), service.DB, attempt.ID, evidence)
				if err != nil {
					t.Fatal(err)
				}
				report := []byte(`{"report":{"version":1,"verdict":"no_material_concerns","summary":"Captured source assessed","coverage":["source"],"uncertainty":[],"findings":[]}}`)
				if _, err = results.Submit(t.Context(), "accepted-before-crash", report); err != nil {
					t.Fatal(err)
				}
				if err = service.DB.RecordJudgeInvocation(t.Context(), attempt.ID, "observed-recovery-model", "version-recovery"); err != nil {
					t.Fatal(err)
				}
				if err = service.DB.FinishJudgeAttempt(t.Context(), attempt.ID, "completed", ""); err != nil {
					t.Fatal(err)
				}
			}
			runner := &recoveryJudgeRunner{}
			service.Providers[agent.Kind("codex")] = runner
			service.Providers[agent.Kind("claude")] = runner
			body, err := service.Execute(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) != 0 {
				t.Fatal("recovery relaunched model")
			}
			if accepted && !strings.Contains(body, "observed-recovery-model") {
				t.Fatal("recovery lost invocation provenance", body)
			}
			if !accepted && !strings.Contains(body, "unable to assess") {
				t.Fatal("interruption became clean verdict", body)
			}
		})
	}
}

func TestJudgeInvalidTargetNeverSubstitutesLatest(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	runner := &recoveryJudgeRunner{}
	service.Providers[agent.Kind("codex")] = runner
	request.Options.Target = "result:missing"
	if _, err := service.Execute(t.Context(), request); !errors.Is(err, store.ErrJudgeSourceUnavailable) {
		t.Fatalf("invalid target did not explain source failure: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("invalid target launched latest instead")
	}
	if _, err := service.DB.FindJudgeRequest(t.Context(), request.TicketKey, request.RemoteCommentID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid target pinned a replacement: %v", err)
	}
}

func TestJudgeFailedFallbackRecoveryRetainsLastInvocation(t *testing.T) {
	for _, state := range []string{"failed", "interrupted"} {
		t.Run(state, func(t *testing.T) {
			service, request, _ := judgeRecoveryFixture(t)
			source, err := service.DB.ResolveJudgeSource(t.Context(), request.Ref.String(), "")
			if err != nil {
				t.Fatal(err)
			}
			invocation, err := service.Config.ResolveJudge(request.Options.JudgeOptions, request.TaskProvider, config.Kind(source.Provider))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := json.Marshal(judgeConfiguration{Requested: request.Options, Effective: invocation})
			if err != nil {
				t.Fatal(err)
			}
			evidence := JudgeEvidenceSnapshot{Evidence: []agentresult.JudgeEvidence{{ID: "source", Kind: "source", Available: true, Essential: true, Lines: 1}}, Contents: map[string]string{"source": "saved"}}
			rawEvidence, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			q, err := service.DB.AdmitJudgeRequest(t.Context(), store.JudgeRequest{Task: request.Ref.String(), TicketKey: request.TicketKey, CommentID: request.RemoteCommentID, SessionID: request.SessionID, TriggerCommentID: request.CommentID, Source: source, Configuration: cfg, Evidence: rawEvidence})
			if err != nil {
				t.Fatal(err)
			}
			primary, err := service.DB.StartJudgeAttempt(t.Context(), q.ID, string(invocation.Provider), invocation.Model, string(invocation.Thinking))
			if err != nil {
				t.Fatal(err)
			}
			if err = service.DB.FinishJudgeAttempt(t.Context(), primary.ID, "failed", "Provider unavailable"); err != nil {
				t.Fatal(err)
			}
			fallback, err := service.DB.StartJudgeAttempt(t.Context(), q.ID, "claude", "fallback-model", "medium")
			if err != nil {
				t.Fatal(err)
			}
			if err = service.DB.RecordJudgeInvocation(t.Context(), fallback.ID, "fallback-observed-model", "fallback-client-version"); err != nil {
				t.Fatal(err)
			}
			if err = service.DB.FinishJudgeAttempt(t.Context(), fallback.ID, state, "No usable assessment"); err != nil {
				t.Fatal(err)
			}
			runner := &recoveryJudgeRunner{}
			service.Providers[agent.Kind("codex")] = runner
			service.Providers[agent.Kind("claude")] = runner
			body, err := service.Execute(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) != 0 {
				t.Fatal("failed fallback recovery launched another model")
			}
			for _, required := range []string{"unable to assess", "Provider: claude; model: fallback-model; thinking: medium", "fallback-observed-model", "fallback-client-version", "Availability fallback:"} {
				if !strings.Contains(body, required) {
					t.Fatalf("recovery lost last invocation %q: %s", required, body)
				}
			}
			saved, err := service.DB.GetJudgeRequest(t.Context(), q.ID)
			if err != nil {
				t.Fatal(err)
			}
			var report judgeSavedReport
			if err = json.Unmarshal(saved.Report, &report); err != nil {
				t.Fatal(err)
			}
			if report.Presentation.Provider != "claude" || report.Presentation.Report.Verdict != "unable_to_assess" {
				t.Fatalf("retained recovery misattributed or clean: %+v", report)
			}
		})
	}
}
