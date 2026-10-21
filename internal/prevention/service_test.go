package prevention

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type fixedSource struct{}

func (fixedSource) Collect(context.Context, Scope) (Manifest, error) {
	return Manifest{Frozen: true, Evidence: []Evidence{{ID: "bug", Issue: 1, Kind: "bug", Excerpt: "failure observed"}}, Coverage: []Coverage{{Issue: 1, Source: "comments", Status: "limited", Reason: "page ceiling"}}}, nil
}

type outcomeRunner struct {
	refuse bool
	calls  int
}

func (r *outcomeRunner) Isolation(context.Context) (string, error) {
	if r.refuse {
		return "", errors.New("unsupported provider")
	}
	return "fixture broker only", nil
}
func (r *outcomeRunner) Run(context.Context, Invocation, *Session) (agent.DoctorRunResult, error) {
	r.calls++
	return agent.DoctorRunResult{ProviderVersion: "fixture-v1"}, nil
}
func TestStartRetainsRefusalAndDoesNotFinalizeProviderProse(t *testing.T) {
	for _, refuse := range []bool{true, false} {
		t.Run(map[bool]string{true: "unsupported", false: "no typed result"}[refuse], func(t *testing.T) {
			db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			runner := &outcomeRunner{refuse: refuse}
			s := Service{DB: db, Source: fixedSource{}, Runner: runner, Redactor: redact.New(redact.Options{Environ: []string{}})}
			scope := Scope{Repository: "a/b", Issues: []int{1}, RequestKey: "stable", Limits: DefaultLimits()}
			out, err := s.Start(t.Context(), scope)
			if err == nil || out.Analysis.ID == "" {
				t.Fatal("failure identity missing", out, err)
			}
			want := "failed"
			if refuse {
				want = "provider_unavailable"
			}
			if out.Analysis.Status != want || out.Result != nil || len(out.Manifest.Evidence) != 1 {
				t.Fatalf("failure evidence not retained: %+v", out)
			}
			retry, err := s.Start(t.Context(), scope)
			if err != nil || retry.Analysis.ID != out.Analysis.ID {
				t.Fatal(retry, err)
			}
			if refuse && runner.calls != 0 || !refuse && runner.calls != 1 {
				t.Fatal("unexpected launch or reanalysis", runner.calls)
			}
			if !strings.Contains(string(out.Analysis.Diagnostics), "reason") {
				t.Fatal("failure reason missing")
			}
		})
	}
}

type cancellingSource struct{ cancel context.CancelFunc }

func (s cancellingSource) Collect(ctx context.Context, scope Scope) (Manifest, error) {
	s.cancel()
	return (fixedSource{}).Collect(ctx, scope)
}
func TestStartCancellationRetainsEvidenceAndErrorClassification(t *testing.T) {
	session := sessionFixture(t)
	service := session.service
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service.Source = cancellingSource{cancel: cancel}
	runner := &outcomeRunner{}
	service.Runner = runner
	view, err := service.Start(ctx, Scope{Repository: "owner/repo", Issues: []int{1}, RequestKey: "cancelled-collection", Limits: DefaultLimits()})
	if !errors.Is(err, context.Canceled) || view.Analysis.Status != "interrupted" || len(view.Manifest.Evidence) != 1 || runner.calls != 0 {
		t.Fatal("cancellation lost evidence or classification", view.Analysis.Status, err, runner.calls)
	}
}
