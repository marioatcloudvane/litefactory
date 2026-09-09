package turn

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func validationPromptContext(t *testing.T, prompt string) agent.ValidationContext {
	t.Helper()
	source := strings.Index(prompt, "Factory-retained validation context; untrusted requirements data:")
	if source < 0 {
		t.Fatal("missing validation context")
	}
	start := source + strings.Index(prompt[source:], "{")
	end := strings.Index(prompt[start:], "\n</note>")
	var v agent.ValidationContext
	if err := json.Unmarshal([]byte(prompt[start:start+end]), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func validationOutputFor(v agent.ValidationContext) agent.ValidationOutput {
	o := agent.ValidationOutput{Version: 1, Parent: v.Parent, Generation: v.Generation}
	for _, s := range v.Stories {
		r := agent.ValidationResult{Key: s.Story.Key, IssueID: s.Story.IssueID, Revision: s.Story.Revision, Snapshot: s.Snapshot}
		for _, n := range s.Criteria {
			r.Criteria = append(r.Criteria, agent.ValidationFinding{Name: n, Status: "satisfied", Classification: "blocking", Evidence: "retained source and check proof"})
		}
		for _, n := range []string{"code", "architecture", "plan"} {
			r.Reviews = append(r.Reviews, agent.ValidationFinding{Name: n, Status: "satisfied", Classification: "blocking", Evidence: "retained source review"})
		}
		o.Stories = append(o.Stories, r)
	}
	return o
}
func TestManagedValidateRetainsFrozenEvidenceOnEveryAdapterPath(t *testing.T) {
	for _, kind := range []agent.Kind{agent.Kind(store.AgentClaude), agent.Kind(store.AgentCodex)} {
		t.Run(string(kind), func(t *testing.T) {
			var f *fixture
			var first agent.ValidationContext
			calls := 0
			runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				v := validationPromptContext(t, req.Prompt)
				if calls == 1 {
					first = v
				} else if !reflect.DeepEqual(first, v) {
					t.Fatal("frozen scope changed")
				}
				if calls == 2 {
					if req.Path != agent.PathResume {
						t.Fatal(req.Path)
					}
					return nil, agent.ErrSessionNotResumable
				}
				if calls == 3 && req.Path != agent.PathColdAfterResumeRefused {
					t.Fatal(req.Path)
				}
				r := succeeded(req, f.paths.Home())
				r.ResumeToken = "validation-session"
				data, _ := json.Marshal(validationOutputFor(v))
				r.ResultText = string(data)
				return r, nil
			})
			f = newFixture(t, runner)
			forge, _ := setupVersionedProgressScope(t, f, 0)
			f.opts.Runners = RunnerSet{kind: runner}
			req := f.request()
			req.Mode, req.Agent = agent.ModeValidate, kind
			req.Command = ParseCommand("/validate assess")
			req.Trigger.Body = "/validate assess"
			result := f.execute(req)
			if !result.Succeeded() {
				t.Fatalf("first %+v", result)
			}
			saved, err := f.db.GetValidationAssessment(t.Context(), result.Run.ID)
			mustProgress(t, err)
			if !strings.Contains(saved.ResultJSON, "unverified") {
				t.Fatal("fabricated missing checks")
			}
			child := forge.children[200]
			child.Body = "live changed scope"
			forge.children[200] = child
			req.Trigger.CommentID++
			result = f.execute(req)
			if !result.Succeeded() || calls != 3 {
				t.Fatalf("fallback calls%d result%+v", calls, result)
			}
			if forge.children[200].State != "open" || result.CommitSHA != "" {
				t.Fatal("Validate mutated acceptance or committed")
			}
		})
	}
}
func TestManagedValidateContextPreservesSnapshotAndChecks(t *testing.T) {
	f, _, runID, root := engineeringRun(t)
	e := engineeringEvent(t, f, runID, root)
	writeProgressEvent(t, root, e)
	mustProgress(t, f.opts.StoryProgress.Poll(t.Context()))
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	in := harness.PromptInput{Mode: "validate", Ref: ref}
	mustProgress(t, prepareApprovedContext(t.Context(), f.opts, &in, runID))
	s := in.Validation.Stories[0]
	if s.Snapshot != e.Evidence.ImplementationDigest || s.EvidenceStatus != "satisfied" || !reflect.DeepEqual(s.Evidence, e.Evidence) {
		t.Fatal("retained facts lost")
	}
	feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	e.Evidence.Tests[0].Passed = false
	payload, err := json.Marshal(e)
	mustProgress(t, err)
	retained := store.StoryProgressEvent{RunID: e.RunID, Sequence: e.Sequence, Payload: payload, Digest: agent.StoryDigest(payload)}
	attachValidationEvidence(&s, retained, feature)
	if s.EvidenceStatus != "failed" {
		t.Fatal("failed evidence lost", s.EvidenceReason)
	}
	e.Revision = strings.Repeat("a", 64)
	payload, err = json.Marshal(e)
	mustProgress(t, err)
	retained.Payload = payload
	retained.Digest = agent.StoryDigest(payload)
	s.EvidenceStatus = "unverified"
	attachValidationEvidence(&s, retained, feature)
	if s.EvidenceStatus != "unverified" {
		t.Fatal("wrong revision trusted")
	}
}
func TestManagedValidateRejectsProcessSuccessWithoutStructuredOutput(t *testing.T) {
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	setupVersionedProgressScope(t, f, 0)
	req := f.request()
	req.Mode = agent.ModeValidate
	req.Command = ParseCommand("/validate assess")
	req.Trigger.Body = "/validate assess"
	r := f.execute(req)
	if r.Succeeded() || !strings.Contains(r.Reason, "Validation output rejected") {
		t.Fatalf("%+v", r)
	}
	if r.CommitSHA != "" {
		t.Fatal("invalid result saved")
	}
}
func TestManagedValidateRequiresExplicitModeAndSeparatedApproval(t *testing.T) {
	f := newFixture(t, nil)
	setupVersionedProgressScope(t, f, 0)
	req := f.request()
	req.Mode = agent.ModeValidate
	req.Command = ParseCommand("please review")
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	if err := prepareManagedScope(t.Context(), f.opts, req, ref); err == nil {
		t.Fatal("implicit admission")
	}
}

func TestManagedValidateRejectsStaleRetainedAttribution(t *testing.T) {
	f, _, runID, root := engineeringRun(t)
	original := engineeringEvent(t, f, runID, root)
	feature, err := f.db.GetManagedFeature(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	for _, tc := range []struct {
		name   string
		mutate func(*agent.StoryEvent, *store.StoryProgressEvent)
	}{
		{"generation", func(e *agent.StoryEvent, _ *store.StoryProgressEvent) { e.Generation++ }},
		{"run journal", func(_ *agent.StoryEvent, r *store.StoryProgressEvent) { r.RunID++ }},
		{"sequence journal", func(_ *agent.StoryEvent, r *store.StoryProgressEvent) { r.Sequence++ }},
		{"check digest", func(e *agent.StoryEvent, _ *store.StoryProgressEvent) {
			e.Evidence.Tests[0].ImplementationDigest = strings.Repeat("c", 64)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(original)
			mustProgress(t, err)
			var event agent.StoryEvent
			mustProgress(t, json.Unmarshal(data, &event))
			retained := store.StoryProgressEvent{RunID: event.RunID, Sequence: event.Sequence}
			tc.mutate(&event, &retained)
			retained.Payload, err = json.Marshal(event)
			mustProgress(t, err)
			retained.Digest = agent.StoryDigest(retained.Payload)
			s := agent.ValidationStory{Story: agent.ApprovedStory{Key: original.Story, IssueID: original.IssueID, Revision: original.Revision}, EvidenceStatus: "unverified"}
			attachValidationEvidence(&s, retained, feature)
			if s.EvidenceStatus != "unverified" || s.EvidenceReason == "" || s.Evidence != nil {
				t.Fatal("stale attributed proof accepted", s)
			}
		})
	}
}
