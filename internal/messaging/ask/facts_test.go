package ask_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/store"
)

type catalog []store.Repo

func (c catalog) ListRepos(context.Context) ([]store.Repo, error) { return c, nil }

func TestProjectResolution(t *testing.T) {
	t.Parallel()
	repos := catalog{
		{ID: 1, Forge: "github", Owner: "one", Name: "app"},
		{ID: 2, Forge: "github", Owner: "two", Name: "app"},
		{ID: 3, Forge: "github", Owner: "one", Name: "unique"},
	}
	for _, tc := range []struct {
		name, option string
		history      []ask.Turn
		id           int64
		inherited    bool
		refusal      string
	}{
		{"qualified", "one/app", nil, 1, false, ""},
		{"unique", "unique", nil, 3, false, ""},
		{"ambiguous", "app", nil, 0, false, "Ambiguous"},
		{"missing", "", nil, 0, false, "Please name"},
		{"unknown", "missing", nil, 0, false, "Unknown"},
		{"newest history", "", []ask.Turn{{Project: "two/app"}, {Project: "one/app"}}, 2, true, ""},
		{"removed newest", "", []ask.Turn{{Project: "old/app"}, {Project: "one/app"}}, 0, false, "Unknown"},
		{"explicit wins", "unique", []ask.Turn{{Project: "two/app"}}, 3, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ask.ResolveProject(t.Context(), repos, tc.option, tc.history)
			if err != nil {
				t.Fatal(err)
			}
			if got.Repository.ID != tc.id || got.Inherited != tc.inherited || !strings.Contains(got.Refusal, tc.refusal) {
				t.Fatalf("resolution: %+v", got)
			}
			if tc.id == 0 && !strings.Contains(got.Refusal, "one/app, one/unique, two/app") {
				t.Fatalf("missing sorted catalog: %s", got.Refusal)
			}
		})
	}
	large := catalog{}
	for i := range 200 {
		large = append(large, store.Repo{ID: int64(i + 1), Forge: "github", Owner: "owner", Name: fmt.Sprintf("repository-%03d", i)})
	}
	got, err := ask.ResolveProject(t.Context(), large, "unknown", nil)
	if err != nil || len(got.Refusal) > 1000 || !strings.Contains(got.Refusal, "left out") {
		t.Fatalf("unbounded catalog: %s, %v", got.Refusal, err)
	}
}

type factory struct {
	tasks  []ask.Task
	paused bool
	err    error
}

func (f *factory) ReadProjectTasks(context.Context, int64) ([]ask.Task, bool, error) {
	return f.tasks, false, f.err
}
func (f *factory) ReadDiscoveryPause(context.Context) (bool, bool, error) {
	return f.paused, true, nil
}

type forge struct {
	issues []github.Issue
	err    error
	calls  int
}

func (f *forge) ReadOpenIssues(_ context.Context, repo github.Repo, limit int) ([]github.Issue, bool, error) {
	if repo.Owner != "owner" || repo.Name != "app" || limit != 50 {
		return nil, false, errors.New("wrong bounded project read")
	}
	f.calls++
	return f.issues, len(f.issues) > limit, f.err
}

func TestFreshFactsAndForgeFailure(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	repo := store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app", BaseBranch: "main"}
	f := &factory{tasks: []ask.Task{{IssueNumber: 7, Reference: "github:owner/app#7", State: "running", LatestRun: &store.Run{Ordinal: 3, Mode: "implement", Status: store.RunRunning, StartedAt: at}, PlanStages: []string{"story-001: acceptance"}}}}
	g := &forge{issues: []github.Issue{{Number: 7, State: "open", Title: "older", UpdatedAt: at}, {Number: 8, State: "open", Title: "newer", UpdatedAt: at.Add(time.Minute)}}}
	c := ask.Collector{Factory: f, Issues: g}
	b, err := c.Collect(t.Context(), repo, at)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"repository", "discovery", "issue:8", "issue:7", "task:7", "run:7/3", "plan:7/1"}
	if len(b.Facts) != len(want) || len(b.Gaps) != 0 {
		t.Fatalf("bundle: %+v", b)
	}
	for i, fact := range b.Facts {
		if fact.Handle != want[i] || !fact.ReadAt.Equal(at) || fact.Text == "" {
			t.Fatalf("fact %d: %+v", i, fact)
		}
	}
	if b.Facts[2].Link != "https://github.com/owner/app/issues/8" {
		t.Fatalf("issue link: %+v", b.Facts[2])
	}
	f.tasks[0].State = "idle"
	f.paused = true
	g.err = errors.New("remote unavailable")
	b, err = c.Collect(t.Context(), repo, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if g.calls != 2 || len(b.Facts) != 5 || !strings.Contains(b.Facts[2].Text, "idle") || !strings.Contains(b.Facts[1].Text, "true") || !strings.Contains(strings.Join(b.Gaps, " "), "factory data only") {
		t.Fatalf("stale or unsupported data: %+v, calls=%d", b, g.calls)
	}
	if strings.Contains(b.Facts[2].Text, "running") {
		t.Fatalf("old task state survived: %+v", b)
	}
}

func TestFactsLimitsCountEncodedMetadata(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	g := &forge{}
	for i := range 75 {
		g.issues = append(g.issues, github.Issue{Number: i + 1, State: "open", Title: "bounded", Body: strings.Repeat("界\"\n", 1000), UpdatedAt: at.Add(time.Duration(i) * time.Minute)})
	}
	b, err := (ask.Collector{Factory: &factory{}, Issues: g}).Collect(t.Context(), store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app"}, at)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > ask.MaxFactsSize || !utf8.Valid(raw) {
		t.Fatalf("invalid or excessive bundle (%d bytes)", len(raw))
	}
	gaps := strings.Join(b.Gaps, " ")
	for _, want := range []string{"50 issue limit", "2 KiB body limit", "64 KiB bundle limit"} {
		if !strings.Contains(gaps, want) {
			t.Errorf("missing omission %q in %s", want, gaps)
		}
	}
	if b.Facts[2].Handle != "issue:75" {
		t.Fatalf("did not keep freshest issue: %+v", b.Facts[2])
	}
}

// ownerWaitBundle has published owner waits on #629, #638 and #649, an open
// issue (#631) and a closed issue (#530) whose bodies have a research heading.
func ownerWaitBundle(t *testing.T, at time.Time) ask.Bundle {
	t.Helper()
	w := newWaitStore(at.Add(-50*time.Hour), 629, 638, 649)
	w.sessions = append(w.sessions, store.Session{ID: 631, IssueNumber: 631}, store.Session{ID: 530, IssueNumber: 530})
	g := &forge{}
	for _, n := range []int{629, 631, 638, 649} {
		g.issues = append(g.issues, github.Issue{Number: n, State: "open", Title: "feature", Body: "## Research questions\n- What?", UpdatedAt: at})
	}
	g.issues = append(g.issues, github.Issue{Number: 530, State: "closed", Title: "done", Body: "## Research questions\n- Old?", UpdatedAt: at})
	b, err := (ask.Collector{Factory: ask.StoreReader{Reader: w}, Issues: g}).Collect(t.Context(), store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app"}, at)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFactsListExactlyPublishedOwnerWaits(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 11, 21, 14, 0, 0, time.UTC)
	b := ownerWaitBundle(t, at)
	var waits []string
	for _, f := range b.Facts {
		if f.Kind != "owner_wait" {
			continue
		}
		waits = append(waits, f.Handle)
		var n int
		if _, err := fmt.Sscanf(f.Handle, "wait:%d", &n); err != nil {
			t.Fatalf("handle %q: %v", f.Handle, err)
		}
		if want := fmt.Sprintf("https://github.com/owner/app/issues/%d#issuecomment-%d", n, 9000+n); f.Link != want {
			t.Errorf("link %s, want %s", f.Link, want)
		}
		for _, want := range []string{fmt.Sprintf("#%d", n), "research", "question", "scope", fmt.Sprintf("Approve scope key-%d?", n), "Reply on the linked issue comment", "2026-10-09T19:14:00Z", "2 days before capture"} {
			if !strings.Contains(f.Text, want) {
				t.Errorf("fact %s missing %q: %s", f.Handle, want, f.Text)
			}
		}
	}
	if strings.Join(waits, ",") != "wait:629,wait:638,wait:649" {
		t.Fatalf("owner waits: %v in %+v", waits, b.Facts)
	}
}
