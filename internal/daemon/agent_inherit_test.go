package daemon

import (
	"context"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/turn"
)

// inheritForge serves the parent issue and records labels added to stories.
type inheritForge struct {
	Forge
	parent github.Issue
	added  map[int][]string
}

func (f *inheritForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return f.parent, nil
}
func (f *inheritForge) AddIssueLabel(_ context.Context, _ github.Repo, issue int, label string) error {
	f.added[issue] = append(f.added[issue], label)
	return nil
}
func (f *inheritForge) RemoveIssueLabel(context.Context, github.Repo, int, string) error { return nil }

func TestStoryIssueInheritsItsParentsAgent(t *testing.T) {
	cases := map[string]struct {
		parentLabels, storyLabels []string
		want                      string
	}{
		"parent codex, story unlabelled": {[]string{"Coding Agent: Codex"}, []string{"Mode: Implement"}, "Coding Agent: Codex"},
		"story keeps its own label":      {[]string{"Coding Agent: Codex"}, []string{"Coding Agent: Claude"}, ""},
		"parent unlabelled":              {nil, nil, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newImplementDaemonFixture(t)
			story, err := f.d.db.GetOrCreateSession(t.Context(), f.repo, 11)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.d.db.SetImplementWorkerSession(t.Context(), implementDaemonParent, 1, "story-001", story.ID, implementDaemonClock); err != nil {
				t.Fatal(err)
			}
			forge := &inheritForge{parent: github.Issue{Number: 1, Labels: tc.parentLabels}, added: map[int][]string{}}
			f.d.forge = forge
			f.d.agents = turn.RunnerSet{agent.Kind("claude"): nil, agent.Kind("codex"): nil}
			tk := &task{repo: f.repo, forgeRepo: github.Repo{Owner: "acme", Name: "api"}, issue: github.Issue{Number: 11, Labels: slices.Clone(tc.storyLabels)}, session: story}
			if err := f.d.inheritParentAgent(t.Context(), tk); err != nil {
				t.Fatal(err)
			}
			got := forge.added[11]
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("labels added %v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tc.want || !slices.Contains(tk.issue.Labels, tc.want) {
				t.Fatalf("added %v, task labels %v, want %q", got, tk.issue.Labels, tc.want)
			}
			if choice := f.d.resolveAgent(tk.issue.Labels); choice.Agent != "codex" {
				t.Fatalf("resolved %+v, want codex", choice)
			}
		})
	}
}
