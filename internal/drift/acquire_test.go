package drift

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type observedDiscussion struct{ timestampOnly bool }

func (observedDiscussion) ReadDriftDiscussion(context.Context, github.Repo, int, int) (github.DriftDiscussion, error) {
	return github.DriftDiscussion{Issue: github.Issue{ID: 701, Number: 7, Body: "API_KEY=disclosed_source_secret_123456789", HTMLURL: "https://example.test/issues/7"}, Bytes: 90}, nil
}
func (fixture observedDiscussion) ReadDriftComments(_ context.Context, _ github.Repo, _ int, page, _ int, _ int) (github.DriftComments, error) {
	text := "first retained observation"
	next := 2
	if page == 2 {
		if !fixture.timestampOnly {
			text = "edited observation"
		}
		next = 0
	}
	return github.DriftComments{Comments: []github.Comment{{ID: 99, Body: text, UpdatedAt: time.Unix(int64(page), 0), HTMLURL: "https://example.test/comments/99"}}, NextPage: next, Bytes: 80}, nil
}

func TestAcquisitionRetainsSanitizedFirstObservationAndExplicitGaps(t *testing.T) {
	ctx := t.Context()
	repo := gittest.New(t)
	base := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
	repo.WriteFile("architecture.md", "API_KEY=disclosed_source_secret_123456789\n")
	repo.WriteFile("binary.bin", "abc\x00def")
	if err := os.Symlink("architecture.md", filepath.Join(repo.Dir, "architecture-link")); err != nil {
		t.Fatal(err)
	}
	head := repo.Commit("selected changes")
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "analysis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	analysis, _, err := db.CreateDriftAnalysis(ctx, store.DriftAnalysis{ID: "acquire", Repository: "acme/repo", RequestDigest: "request", Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	ac := Acquirer{Store: db, Git: binary, ClonePath: repo.Dir, Repo: github.Repo{Owner: "acme", Name: "repo"}, Discussion: observedDiscussion{}, Redactor: redact.New(redact.Options{Environ: []string{"SOURCE_TOKEN=disclosed_source_secret_123456789"}})}
	pinned, coverage, err := ac.Acquire(ctx, analysis, Request{Base: base, Head: head, Issues: []int{7}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pinned), head) || !strings.Contains(string(pinned), base) {
		t.Fatal("pinned identities missing")
	}
	var c Coverage
	if err = json.Unmarshal(coverage, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Gaps) < 2 || !strings.Contains(strings.Join(c.Gaps, " "), "changed during collection") || c.DiscussionTermination[7] == "unfinished" {
		t.Fatalf("host coverage lost: %+v", c)
	}
	evidence, err := db.ListDriftEvidence(ctx, analysis.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if c.Retained != len(evidence) {
		t.Fatalf("retained coverage excludes metadata-only evidence: %d != %d", c.Retained, len(evidence))
	}
	var first bool
	for _, e := range evidence {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "disclosed_source_secret_123456789") {
			t.Fatal("known source credential retained")
		}
		if e.Kind == "issue" {
			var metadata struct {
				IssueID int64 `json:"issue_id"`
			}
			if err := json.Unmarshal(e.Metadata, &metadata); err != nil || metadata.IssueID != 701 {
				t.Fatalf("provider issue identity missing: %s", e.Metadata)
			}
		}
		if e.CommentID == 99 {
			first = true
			if e.Text != "first retained observation" {
				t.Fatal("later comment overwrote first observation")
			}
		}
		if e.Path == "binary.bin" && e.Omission == "" {
			t.Fatal("binary treated as analyzed text")
		}
	}
	if !first {
		t.Fatal("selected comment not retained")
	}
	current, err := db.GetDriftAnalysis(ctx, analysis.ID)
	if err != nil || current.Usage.Requests != 3 || current.Usage.SourceBytes < 250 {
		t.Fatalf("acquisition accounting missing: %+v %v", current.Usage, err)
	}
}

func TestAcquisitionGitGapStillRetainsDiscussionAndTimestampChanges(t *testing.T) {
	ctx := t.Context()
	repo := gittest.New(t)
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "analysis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a, _, err := db.CreateDriftAnalysis(ctx, store.DriftAnalysis{ID: "missing-git", Repository: "acme/repo", RequestDigest: "request", Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	ac := Acquirer{Store: db, Git: binary, ClonePath: repo.Dir, Repo: github.Repo{Owner: "acme", Name: "repo"}, Discussion: observedDiscussion{timestampOnly: true}, Redactor: redact.New(redact.Options{})}
	_, raw, err := ac.Acquire(ctx, a, Request{Base: "missing-endpoint", Head: "HEAD", Issues: []int{7}})
	if err != nil {
		t.Fatal(err)
	}
	var coverage Coverage
	if err = json.Unmarshal(raw, &coverage); err != nil {
		t.Fatal(err)
	}
	gaps := strings.Join(coverage.Gaps, " ")
	if !coverage.UnknownRemainder || !strings.Contains(gaps, "Git comparison unavailable") || !strings.Contains(gaps, "changed during collection") {
		t.Fatalf("source gaps missing: %s", raw)
	}
	evidence, err := db.ListDriftEvidence(ctx, a.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 2 || coverage.Retained != 2 {
		t.Fatalf("discussion lost after Git gap: %+v, %s", evidence, raw)
	}
	for _, item := range evidence {
		if item.CommentID == 99 && (!item.UpdatedAt.Equal(time.Unix(1, 0)) || item.Text != "first retained observation") {
			t.Fatalf("first observed version changed: %+v", item)
		}
	}
}

func TestAcquisitionDeduplicatesCommentAcrossSelectedDiscussions(t *testing.T) {
	ctx := t.Context()
	repo := gittest.New(t)
	head := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "analysis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a, _, err := db.CreateDriftAnalysis(ctx, store.DriftAnalysis{ID: "cross-selector", Repository: "acme/repo", RequestDigest: "request", Limits: DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	ac := Acquirer{Store: db, Git: binary, ClonePath: repo.Dir, Repo: github.Repo{Owner: "acme", Name: "repo"}, Discussion: observedDiscussion{}, Redactor: redact.New(redact.Options{})}
	_, raw, err := ac.Acquire(ctx, a, Request{Base: head, Head: head, Issues: []int{8, 7}})
	if err != nil {
		t.Fatal(err)
	}
	var coverage Coverage
	if err = json.Unmarshal(raw, &coverage); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(coverage.Gaps, " "), "observed under discussions #7 and #8") {
		t.Fatalf("cross-selector source identity gap missing: %s", raw)
	}
	evidence, err := db.ListDriftEvidence(ctx, a.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	comments := 0
	for _, item := range evidence {
		if item.CommentID != 99 {
			continue
		}
		comments++
		if item.Issue != 7 || item.Text != "first retained observation" || !item.UpdatedAt.Equal(time.Unix(1, 0)) {
			t.Fatalf("first comment observation replaced across selectors: %+v", item)
		}
	}
	if comments != 1 || len(evidence) != 3 || coverage.Retained != 3 {
		t.Fatalf("duplicate comment retained: %d comments, %d evidence rows, coverage %s", comments, len(evidence), raw)
	}
}
