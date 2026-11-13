package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/denypolicy"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

type denyJudgeRunner struct {
	run func(context.Context, agent.JudgeRequest) (agent.JudgeRunResult, error)
}

func (r denyJudgeRunner) RunJudge(ctx context.Context, req agent.JudgeRequest) (agent.JudgeRunResult, error) {
	return r.run(ctx, req)
}

func TestGraphJudgeDenySnapshotAndFallback(t *testing.T) {
	f, _, req, _, delegate := workflowJudgeFixture(t, agent.ModeResearch)
	req.Profile.Attrs[graphexec.DenyAttribute] = "selected"
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\nsettings:\n  denyrules:\n    selected:\n      deny: ['Agent(original)']\n"))
	mustProgress(t, err)
	loads, calls := 0, 0
	f.opts.LoadDenyConfig = func(config.Paths) (*config.Config, error) { loads++; return cfg, nil }
	check := func(request agent.JudgeRequest) {
		t.Helper()
		calls++
		if request.DenyPolicy == nil || len(request.DenyPolicy.Rules) != 1 || request.DenyPolicy.Rules[0].Original != "Agent(original)" {
			t.Fatalf("launch %d lost selected snapshot: %+v", calls, request.DenyPolicy)
		}
	}
	f.opts.Judge.Providers[agent.Kind(config.KindCodex)] = denyJudgeRunner{run: func(_ context.Context, request agent.JudgeRequest) (agent.JudgeRunResult, error) {
		check(request)
		request.DenyPolicy.Rules[0].Original = "Agent(mutated)"
		cfg.Settings.DenyRules["selected"] = denypolicy.Group{Deny: []string{"Agent(changed)"}}
		return agent.JudgeRunResult{}, agent.ErrJudgeUnavailable
	}}
	f.opts.Judge.Providers[agent.Kind(config.KindClaude)] = denyJudgeRunner{run: func(ctx context.Context, request agent.JudgeRequest) (agent.JudgeRunResult, error) {
		check(request)
		return delegate.RunJudge(ctx, request)
	}}
	out, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if err != nil || out.Activity.Outcome != "success" || calls != 2 || loads != 1 {
		t.Fatalf("judge snapshot: %+v err=%v calls=%d loads=%d", out, err, calls, loads)
	}
}

func TestGraphJudgeDenyReloadRefusalAndNoSelection(t *testing.T) {
	for _, selected := range []bool{true, false} {
		t.Run(map[bool]string{true: "selected", false: "no selection"}[selected], func(t *testing.T) {
			f, _, req, _, runner := workflowJudgeFixture(t, agent.ModeResearch)
			if selected {
				req.Profile.Attrs[graphexec.DenyAttribute] = "selected"
			}
			loads := 0
			f.opts.LoadDenyConfig = func(config.Paths) (*config.Config, error) { loads++; return nil, errors.New("settings unavailable") }
			out, err := ExecuteNodeAction(t.Context(), f.opts, req)
			if selected {
				if err == nil || !strings.Contains(err.Error(), "settings unavailable") || loads != 1 || len(runner.calls) != 0 {
					t.Fatalf("reload refusal: %+v err=%v loads=%d calls=%d", out, err, loads, len(runner.calls))
				}
			} else if err != nil || out.Activity.Outcome != "success" || loads != 0 || len(runner.calls) != 1 || runner.calls[0].DenyPolicy != nil {
				t.Fatalf("no-selection behavior changed: %+v err=%v loads=%d calls=%d", out, err, loads, len(runner.calls))
			}
		})
	}
}
