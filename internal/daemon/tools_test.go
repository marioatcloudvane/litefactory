package daemon_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/turn"
)

// researchProfile and buildProfile are the two profiles most tool-permission
// tests in this file configure the daemon with.
func researchProfile() config.ToolProfile {
	return config.ToolProfile{Tools: []string{"Read", "Grep", "Glob"}, AllowedTools: []string{"Read"}}
}

func buildProfile() config.ToolProfile {
	return config.ToolProfile{Tools: []string{"Read", "Edit", "Bash"}}
}

// TestLabelSelectsProfileForTheNextRun is AC-2.2: a maintainer's lf:tools=
// label on the issue is what the next comment-triggered run carries onto its
// turn.Request, unchanged from the operator's own configuration.
func TestLabelSelectsProfileForTheNextRun(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		ToolProfiles: map[string]config.ToolProfile{"research": researchProfile()},
	})
	f.settle()
	f.issueWithLabels("Add a /health endpoint", "lf:tools=research")
	f.tick() // first sight

	f.forge.addComment(testIssue, 501, owner(), "/spike add a health endpoint")
	result := f.tick()

	if result.Runs != 1 {
		t.Fatalf("ran %d turns, want 1\nlogs:\n%s", result.Runs, f.logs)
	}
	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("the executor was called %d times, want 1", len(calls))
	}
	req := calls[0]
	if req.ToolProfile != "research" {
		t.Errorf("ToolProfile = %q, want %q", req.ToolProfile, "research")
	}
	if !slices.Equal(req.Tools, researchProfile().Tools) {
		t.Errorf("Tools = %v, want the profile's %v", req.Tools, researchProfile().Tools)
	}
	if !slices.Equal(req.AllowedTools, researchProfile().AllowedTools) {
		t.Errorf("AllowedTools = %v, want the profile's %v", req.AllowedTools, researchProfile().AllowedTools)
	}
}

// TestNoLabelRequestCarriesTodaysEmptySurface is the request-level half of
// AC-2.3: with no lf:tools= label anywhere on the issue, the turn.Request a
// run is built from carries the same empty tool fields it always did, which
// is what makes the harness fall back to its configured surface and keeps
// argv byte-identical to before this feature existed. The harness's own half
// of AC-2.3 — the literal argv — is pinned in internal/harness/runner_test.go.
func TestNoLabelRequestCarriesTodaysEmptySurface(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		ToolProfiles: map[string]config.ToolProfile{"research": researchProfile()},
	})
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick() // first sight

	f.forge.addComment(testIssue, 502, owner(), "/spike add a health endpoint")
	f.tick()

	calls := f.turns.calls()
	if len(calls) != 1 {
		t.Fatalf("the executor was called %d times, want 1", len(calls))
	}
	req := calls[0]
	if req.ToolProfile != "" || len(req.Tools) != 0 || len(req.AllowedTools) != 0 {
		t.Errorf("an unlabelled issue's request carries a tool surface: profile=%q tools=%v allowed=%v",
			req.ToolProfile, req.Tools, req.AllowedTools)
	}
}

// TestRelabelThenComment is AC-2.10, both halves: a label applied between two
// comments widens the run the next comment triggers, and applying the label
// with no following comment starts nothing at all — labels are read fresh
// per tick, but only a comment can trigger a run (classify.go:204-206).
func TestRelabelThenComment(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		ToolProfiles: map[string]config.ToolProfile{"research": researchProfile()},
	})
	f.settle()
	f.issue("Add a /health endpoint")
	f.tick() // first sight

	// First comment, before any label: the ordinary, unprofiled surface.
	f.forge.addComment(testIssue, 601, owner(), "/spike look at the endpoint")
	f.tick()
	if got := f.turns.calls()[0].ToolProfile; got != "" {
		t.Fatalf("the first run already carries a profile %q; the fixture is not testing what it claims", got)
	}

	// The label changes. No comment follows it, so nothing must run — only a
	// comment triggers a turn.
	f.relabel("lf:tools=research")
	afterRelabel := f.tick()
	if afterRelabel.Runs != 0 {
		t.Errorf("a relabel with no comment ran %d turns, want none", afterRelabel.Runs)
	}
	if len(f.turns.calls()) != 1 {
		t.Fatalf("the executor was called an extra time after a bare relabel")
	}

	// Now a second comment. The label that was already there widens this run.
	f.forge.addComment(testIssue, 602, owner(), "/spike widen the endpoint")
	result := f.tick()
	if result.Runs != 1 {
		t.Fatalf("ran %d turns after the comment following a relabel, want 1", result.Runs)
	}
	calls := f.turns.calls()
	if len(calls) != 2 {
		t.Fatalf("the executor was called %d times in total, want 2", len(calls))
	}
	if got := calls[1].ToolProfile; got != "research" {
		t.Errorf("the run after the relabel carries profile %q, want %q", got, "research")
	}
}

// TestToolsRefusal_SpawnsNothingAndAdvancesTheWatermark is AC-3.1, AC-3.4,
// AC-3.5, AC-3.6, AC-3.7: every one of the four ways an lf:tools= label can
// fail to resolve refuses the run, replies naming what it can safely name,
// spawns nothing, and still advances the watermark so the comment is not
// retried forever.
//
// "Spawns nothing" is asserted against the executor and the runs table, not
// assumed: a permission refusal is decided before [turn.Execute] is ever
// called (tick.go:676-685), so no worktree and no claude process can exist
// either — there is nothing downstream of the executor call that could have
// produced one.
func TestToolsRefusal_SpawnsNothingAndAdvancesTheWatermark(t *testing.T) {
	tests := []struct {
		name     string
		profiles map[string]config.ToolProfile
		labels   []string
		wantIn   string
	}{
		{
			name:     "unknown profile names it in the reply (AC-3.1)",
			profiles: map[string]config.ToolProfile{"research": researchProfile()},
			labels:   []string{"lf:tools=nosuch"},
			wantIn:   "nosuch",
		},
		{
			name:     "malformed value (AC-3.5)",
			profiles: map[string]config.ToolProfile{"research": researchProfile()},
			labels:   []string{"lf:tools=bad name"},
			wantIn:   "does not name a usable tool profile",
		},
		{
			name:     "mis-cased prefix is refused, not silently ignored (AC-3.5, D-6)",
			profiles: map[string]config.ToolProfile{"research": researchProfile()},
			labels:   []string{"LF:Tools=research"},
			wantIn:   "does not name a usable tool profile",
		},
		{
			name: "ambiguous (AC-3.3)",
			profiles: map[string]config.ToolProfile{
				"research": researchProfile(), "build": buildProfile(),
			},
			labels: []string{"lf:tools=research", "lf:tools=build"},
			wantIn: "more than one",
		},
		{
			name:     "a label with no tool_profiles configured at all refuses (AC-3.4, D-5)",
			profiles: nil,
			labels:   []string{"lf:tools=research"},
			wantIn:   "research",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.daemon = f.newDaemon(daemon.Options{ToolProfiles: tc.profiles})
			f.settle()
			f.issueWithLabels("Add a /health endpoint", tc.labels...)
			f.tick() // first sight

			f.forge.addComment(testIssue, 900, owner(), "/spike add a health endpoint")
			result := f.tick()

			if calls := f.turns.calls(); len(calls) != 0 {
				t.Errorf("the executor ran %d turns, want none: a permission refusal must be "+
					"run-less (AC-3.7, C9)", len(calls))
			}
			if runs := f.runs(); len(runs) != 0 {
				t.Errorf("%d run rows exist, want none: nothing may be allocated for a refusal", len(runs))
			}
			if result.Replies != 1 {
				t.Errorf("posted %d replies, want 1", result.Replies)
			}
			posted, _, _ := f.forge.snapshot()
			if len(posted) != 1 {
				t.Fatalf("posted %d comments, want 1", len(posted))
			}
			if !strings.Contains(posted[0], tc.wantIn) {
				t.Errorf("reply does not say %q:\n%s", tc.wantIn, posted[0])
			}
			if !strings.HasPrefix(posted[0], turn.MarkerPrefix) {
				t.Errorf("reply does not carry the litefactory marker:\n%s", posted[0])
			}
			if got := f.watermark(); got != 900 {
				t.Errorf("watermark = %d, want 900: a refusal still consumes the comment that asked for it", got)
			}
		})
	}
}

// TestMalformedLabelReplyNeverEchoesInjectedText is AC-3.2, pushed as hard as
// a hostile label value can push it: the malformed reply is not supposed to
// carry the raw label text at all (S-9 gives it no field to carry it in), so
// this tries several shapes of text that would matter if it ever leaked —
// markdown, a stray HTML comment, a second lf: marker, an @mention, and an
// embedded newline.
func TestMalformedLabelReplyNeverEchoesInjectedText(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"markdown", "*bold*`code`"},
		{"an html comment", "<!--evil-->"},
		{"a second lf marker", "<!-- lf:status task=x run=1 -->"},
		{"a mention", "@everyone"},
		{"an embedded newline", "line1\nline2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.daemon = f.newDaemon(daemon.Options{
				ToolProfiles: map[string]config.ToolProfile{"research": researchProfile()},
			})
			f.settle()
			f.issueWithLabels("Add a /health endpoint", "lf:tools="+tc.value)
			f.tick()

			f.forge.addComment(testIssue, 950, owner(), "/spike add a health endpoint")
			f.tick()

			posted, _, _ := f.forge.snapshot()
			if len(posted) != 1 {
				t.Fatalf("posted %d comments, want 1", len(posted))
			}
			body := posted[0]
			if strings.Contains(body, tc.value) {
				t.Errorf("the reply echoed the raw label value %q:\n%s", tc.value, body)
			}
			if strings.Count(body, turn.MarkerPrefix) != 1 {
				t.Errorf("the reply carries %d lf: markers, want exactly its own one:\n%s",
					strings.Count(body, turn.MarkerPrefix), body)
			}
			if strings.Contains(body, "@everyone") {
				t.Errorf("the reply echoed a mention back into the comment:\n%s", body)
			}
		})
	}
}

// TestAmbiguousLabelsNameNeitherLabel is the naming half of AC-3.3: two
// well-formed labels, each of which names a real, distinct, configured
// profile, and the reply must pick neither name to quote.
func TestAmbiguousLabelsNameNeitherLabel(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		ToolProfiles: map[string]config.ToolProfile{"research": researchProfile(), "build": buildProfile()},
	})
	f.settle()
	f.issueWithLabels("Add a /health endpoint", "lf:tools=research", "lf:tools=build")
	f.tick()

	f.forge.addComment(testIssue, 960, owner(), "/spike add a health endpoint")
	f.tick()

	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 {
		t.Fatalf("posted %d comments, want 1", len(posted))
	}
	if strings.Contains(posted[0], "research") || strings.Contains(posted[0], "build") {
		t.Errorf("the ambiguous reply named one of the two competing labels:\n%s", posted[0])
	}
	if len(f.turns.calls()) != 0 {
		t.Error("a run started from an ambiguous label")
	}
}

// TestFailedReplyPostLeavesTheCommentUnconsumed is AC-3.9: a tool-permission
// refusal is reported the same way every other run-less reply is, and a post
// that never reached the forge must not be mistaken for one that was
// answered.
func TestFailedReplyPostLeavesTheCommentUnconsumed(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		ToolProfiles: map[string]config.ToolProfile{"research": researchProfile()},
	})
	f.settle()
	f.issueWithLabels("Add a /health endpoint", "lf:tools=nosuch")
	f.tick()

	f.forge.addComment(testIssue, 970, owner(), "/spike add a health endpoint")

	f.forge.onPostComment = func(w http.ResponseWriter, call int) bool {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "boom"})
		return true
	}
	bootstrapped := f.watermark()
	result := f.tick()

	if result.Deferred != 1 {
		t.Errorf("tick deferred %d tasks, want 1", result.Deferred)
	}
	if got := f.watermark(); got != bootstrapped {
		t.Errorf("watermark moved to %d after a failed reply post; it must stay at %d", got, bootstrapped)
	}
	if len(f.turns.calls()) != 0 {
		t.Error("a run started despite the refusal")
	}

	// A later tick retries the post.
	f.forge.onPostComment = nil
	retry := f.tick()
	if retry.Replies != 1 {
		t.Errorf("the retried tick posted %d replies, want 1", retry.Replies)
	}
	if got := f.watermark(); got != 970 {
		t.Errorf("watermark after the retry = %d, want 970", got)
	}
	if posts, _, _ := f.forge.snapshot(); len(posts) != 1 {
		t.Errorf("the forge accepted %d comments across both ticks, want exactly 1", len(posts))
	}
}

// TestIssueBodyToolsRefusalPostsNothing pins the issue-body half of AC-3.7 and
// AC-3.1: a run started from the issue body has no comment id to answer with
// (Trigger.CommentID is 0, and [turn.ReplyRequest] requires a positive one),
// so a label that cannot be resolved on that path refuses the run and posts
// no comment at all — the next comment on the issue is refused loudly on the
// thread instead. This is a decision documented as one that may be revisited
// (refuseTools's doc comment), not a hidden default; see this test's report.
func TestIssueBodyToolsRefusalPostsNothing(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{
		ToolProfiles: map[string]config.ToolProfile{"research": researchProfile()},
	})
	f.settle()
	f.forge.addIssue(fakeIssue{
		Number:    testIssue,
		Title:     "Add a /health endpoint",
		Body:      "/spike add a health endpoint that returns 200",
		User:      stranger(),
		Assignees: []fakeUser{owner()},
		Labels:    toFakeLabels("lf:tools=nosuch"),
	})

	result := f.tick()

	if result.Runs != 0 {
		t.Errorf("ran %d turns from a body whose label does not resolve, want none", result.Runs)
	}
	if len(f.turns.calls()) != 0 {
		t.Error("the executor was called for a body-triggered tools refusal")
	}
	if runs := f.runs(); len(runs) != 0 {
		t.Errorf("%d run rows exist, want none", len(runs))
	}
	if posted, reacted, _ := f.forge.snapshot(); len(posted) != 0 || len(reacted) != 0 {
		t.Errorf("posted %d comments and %d reactions for a body-triggered refusal, want none",
			len(posted), len(reacted))
	}
	// The watermark was already planted at bootstrap (0, no existing comments);
	// the refusal does not move it, because there is no comment id to advance
	// to. The next comment on the issue still reaches handleComment normally
	// and is refused there, on the thread.
	if got := f.watermark(); got != 0 {
		t.Errorf("watermark = %d, want 0", got)
	}

	f.forge.addComment(testIssue, 980, owner(), "/spike try again")
	second := f.tick()
	if second.Replies != 1 {
		t.Fatalf("the following comment produced %d replies, want 1: it must be refused on the thread", second.Replies)
	}
	posted, _, _ := f.forge.snapshot()
	if len(posted) != 1 || !strings.Contains(posted[0], "nosuch") {
		t.Errorf("the comment's refusal reply = %v, want one naming %q", posted, "nosuch")
	}
}
