package turn_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/turn"
)

type feedbackUnitApplication struct {
	binding store.ResultBinding
	digest  string
	seen    []byte
	result  ar.UpdateDiagnostics
	err     error
	writes  int
}

func (f *feedbackUnitApplication) Binding() store.ResultBinding { return f.binding }
func (f *feedbackUnitApplication) MCPPermissionDigest() string  { return f.digest }
func (f *feedbackUnitApplication) Preflight(_ context.Context, data []byte) (ar.UpdateDiagnostics, error) {
	f.seen = bytes.Clone(data)
	return f.result, f.err
}
func (f *feedbackUnitApplication) Submit(context.Context, string, []byte) (turn.AgentResultSubmission, error) {
	f.writes++
	return turn.AgentResultSubmission{}, errors.New("unexpected mutation")
}
func (f *feedbackUnitApplication) Receipt(context.Context, int64, string) (turn.AgentResultSubmission, error) {
	f.writes++
	return turn.AgentResultSubmission{}, errors.New("unexpected receipt access")
}
func feedbackUnitApp() *feedbackUnitApplication {
	return &feedbackUnitApplication{binding: store.ResultBinding{Task: "github:owner/repo#273", RunID: 7, Attempt: "current", Mode: "research"}, digest: "application-grants"}
}

func TestFeedbackUnit_ApplicationParity(t *testing.T) {
	for _, valid := range []bool{false, true} {
		name := "diagnostics"
		if valid {
			name = "valid"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			app := feedbackUnitApp()
			app.result = ar.UpdateDiagnostics{Diagnostics: ar.Diagnostics{Valid: valid, Complete: true}, ExpectedRevision: 4, CurrentRevision: 5}
			svc, err := turn.NewFeedbackService(app, nil)
			if err != nil {
				t.Fatal(err)
			}
			input := "{\n  \"result\": {\"comment\":\"exact  bytes\"}\n}"
			got, err := svc.HandlePreflight(t.Context(), "request", []byte(`{"kind":"application","input":`+input+`}`))
			if err != nil {
				t.Fatal(err)
			}
			var diagnostics ar.UpdateDiagnostics
			if err := json.Unmarshal(got, &diagnostics); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(diagnostics, app.result) || string(app.seen) != input || app.writes != 0 {
				t.Fatalf("shared result or bytes changed: %s seen=%q writes=%d", got, app.seen, app.writes)
			}
		})
	}
	t.Run("shared error preserved", func(t *testing.T) {
		app := feedbackUnitApp()
		app.err = errors.New("cancelled shared validation")
		svc, err := turn.NewFeedbackService(app, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := svc.HandlePreflight(t.Context(), "request", []byte(`{"kind":"application","input":{}}`))
		if !errors.Is(err, app.err) || got != nil || app.writes != 0 {
			t.Fatalf("error not preserved: %s %v", got, err)
		}
	})
}

func TestFeedbackUnit_StrictRouting(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"unknown kind", `{"kind":"approve","input":{}}`},
		{"unknown outer field", `{"kind":"application","input":{},"grant":"approved"}`},
		{"missing input", `{"kind":"application"}`},
		{"null input", `{"kind":"application","input":null}`},
		{"duplicate kind", `{"kind":"creation","kind":"application","input":{}}`},
		{"duplicate nested field", `{"kind":"application","input":{"x":1,"x":2}}`},
		{"trailing document", `{"kind":"application","input":{}} {}`},
		{"checkpoint without stage authority", `{"kind":"checkpoint","input":{}}`},
		{"unknown creation field", `{"kind":"creation","input":{"token":"approved"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := feedbackUnitApp()
			svc, err := turn.NewFeedbackService(app, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.HandlePreflight(t.Context(), "request", []byte(tc.input))
			if err == nil || got != nil || app.seen != nil || app.writes != 0 {
				t.Fatalf("routing refusal failed: %s %v seen=%s writes=%d", got, err, app.seen, app.writes)
			}
		})
	}
}

func TestFeedbackUnit_CanonicalCreationParity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*submission.CreationEnvelope)
	}{
		{"valid", func(*submission.CreationEnvelope) {}},
		{"empty artifacts", func(e *submission.CreationEnvelope) { e.Artifacts = nil }},
		{"empty title", func(e *submission.CreationEnvelope) { e.Artifacts[0].Title = " " }},
		{"invalid relationship", func(e *submission.CreationEnvelope) { e.Relationship = "approved" }},
		{"invalid links", func(e *submission.CreationEnvelope) { e.Artifacts[0].LinkIssues = []int{0} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelope := submission.CreationEnvelope{Version: 1, Repository: "owner/repo", SourceTask: "github:owner/repo#273", Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Reviewed title", Body: "Reviewed body"}}}
			tc.change(&envelope)
			_, _, wantErr := submission.CanonicalCreation(envelope)
			input, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			app := feedbackUnitApp()
			svc, err := turn.NewFeedbackService(app, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.HandlePreflight(t.Context(), "request", append(append([]byte(`{"kind":"creation","input":`), input...), '}'))
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Valid       bool     `json:"valid"`
				Diagnostics []string `json:"diagnostics"`
			}
			if err := json.Unmarshal(got, &result); err != nil {
				t.Fatal(err)
			}
			if result.Valid != (wantErr == nil) || app.seen != nil || app.writes != 0 {
				t.Fatalf("canonical parity/effect mismatch: %s", got)
			}
			if wantErr != nil && (len(result.Diagnostics) != 1 || result.Diagnostics[0] != wantErr.Error()) {
				t.Fatalf("shared diagnostics changed: %s want %v", got, wantErr)
			}
		})
	}
}

func TestFeedbackUnit_PermissionDigest(t *testing.T) {
	makeOptions := func(history []store.FamilyScope) turn.FamilyDispatcherOptions {
		app := feedbackUnitApp()
		feedback, err := turn.NewFeedbackService(app, nil)
		if err != nil {
			t.Fatal(err)
		}
		receipts, err := turn.NewReceiptService(&store.DB{}, app.binding, history, nil)
		if err != nil {
			t.Fatal(err)
		}
		return turn.FamilyDispatcherOptions{Service: app, Feedback: feedback, Receipts: receipts, CommentTargets: []int{273, 281}}
	}
	base, err := turn.FamilyPermissionDigest(makeOptions(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *turn.FamilyDispatcherOptions)
		same   bool
	}{
		{"creation visibility", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.AllowCreation = true }, false},
		{"comment target", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.CommentTargets = []int{273} }, false},
		{"target ordering", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.CommentTargets = []int{281, 273} }, true},
		{"application grants", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			app, ok := o.Service.(*feedbackUnitApplication)
			if !ok {
				t.Fatalf("unexpected application type %T", o.Service)
			}
			app.digest = "different-grants"
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := makeOptions(nil)
			tc.change(t, &o)
			got, err := turn.FamilyPermissionDigest(o)
			if err != nil {
				t.Fatal(err)
			}
			if (got == base) != tc.same {
				t.Fatalf("permission digest equality=%v want=%v", got == base, tc.same)
			}
		})
	}
	o := makeOptions([]store.FamilyScope{{Repository: "owner/repo", Task: "github:owner/repo#273", RunID: 6, Attempt: "old"}})
	got, err := turn.FamilyPermissionDigest(o)
	if err != nil {
		t.Fatal(err)
	}
	if got == base {
		t.Fatal("historical read grant omitted from immutable digest")
	}
}
