package daemon_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// Real Git preservation and HTTP closure are composed through the production
// evaluator; no observation callback is faked. All origins are temporary files.
func TestRetirementRealPreservation(t *testing.T) {
	for _, tc := range []struct{ name, wantWork, wantLocal, wantRemote, reason string }{
		{"merged", "would_remove", "would_remove", "would_remove", ""},
		{"young", "kept", "kept", "would_remove", ""},
		{"unmerged", "would_remove", "kept", "kept", ""},
		{"squashed", "would_remove", "kept", "kept", ""},
		{"unpushed", "kept", "kept", "kept", "local_commits_unpreserved"},
		{"missing_upstream", "kept", "kept", "kept", "local_commits_unpreserved"},
		{"missing_merged_upstream", "would_remove", "would_remove", "already_absent", ""},
		{"tracked", "kept", "kept", "kept", "local_files"},
		{"staged", "kept", "kept", "kept", "local_files"},
		{"untracked", "kept", "kept", "kept", "local_files"},
		{"ignored", "kept", "kept", "kept", "local_files"},
		{"ignored_dir_with_file", "kept", "kept", "kept", "local_files"},
		{"empty_ignored_dir", "would_remove", "would_remove", "would_remove", ""},
		{"generated_validation_logs", "would_remove", "would_remove", "would_remove", ""},
		{"generated_and_tracked", "kept", "kept", "kept", "local_files"},
		{"locked", "kept", "kept", "kept", "unsafe_worktree_identity"},
		{"detached", "kept", "kept", "kept", "unsafe_worktree_identity"},
		{"absent", "already_absent", "would_remove", "would_remove", ""},
		{"remote_only", "", "already_absent", "would_remove", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gittest.New(t)
			root := filepath.Join(filepath.Dir(r.Dir), "tasks")
			path := filepath.Join(root, "task-213")
			branch := "lf/213-retire"
			r.Git("worktree", "add", "-b", branch, path, "main")
			write := func(name string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(path, name), []byte("local content\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("feature.txt")
			r.GitIn(path, "add", "feature.txt")
			r.GitIn(path, "commit", "-m", "feature")
			r.GitIn(path, "push", "origin", branch)
			if tc.name != "unmerged" && tc.name != "missing_upstream" {
				if tc.name == "squashed" {
					r.Git("merge", "--squash", branch)
					r.Git("commit", "-m", "squashed feature")
				} else {
					r.Git("merge", "--ff-only", branch)
				}
				r.Git("push", "origin", "main")
			}
			switch tc.name {
			case "unpushed":
				write("unpublished.txt")
				r.GitIn(path, "add", ".")
				r.GitIn(path, "commit", "-m", "unpublished")
			case "missing_upstream", "missing_merged_upstream":
				r.Git("push", "origin", "--delete", branch)
			case "tracked":
				write("README.md")
			case "staged":
				write("staged.txt")
				r.GitIn(path, "add", "staged.txt")
			case "untracked":
				write("untracked.txt")
			case "ignored":
				if err := os.WriteFile(filepath.Join(r.Dir, ".git", "info", "exclude"), []byte("ignored.txt\n"), 0600); err != nil {
					t.Fatal(err)
				}
				write("ignored.txt")
			case "ignored_dir_with_file", "empty_ignored_dir", "generated_validation_logs", "generated_and_tracked":
				exclude := "teams/source/.litefactory/\n/.litefactory/*\ncache/\n"
				if err := os.WriteFile(filepath.Join(r.Dir, ".git", "info", "exclude"), []byte(exclude), 0600); err != nil {
					t.Fatal(err)
				}
				mkdir := func(name string) {
					t.Helper()
					if err := os.MkdirAll(filepath.Join(path, name), 0700); err != nil {
						t.Fatal(err)
					}
				}
				switch tc.name {
				case "ignored_dir_with_file":
					mkdir("cache")
					write("cache/object")
				case "empty_ignored_dir":
					// The factory leaves this ignored directory empty.
					mkdir("teams/source/.litefactory/nested")
				case "generated_validation_logs":
					mkdir(".litefactory/validation-logs")
					write(".litefactory/validation-logs/x.log")
				case "generated_and_tracked":
					mkdir("teams/source/.litefactory")
					mkdir(".litefactory/validation-logs")
					write(".litefactory/validation-logs/x.log")
					write("README.md")
				}
			case "locked":
				r.Git("worktree", "lock", path)
			case "detached":
				r.GitIn(path, "checkout", "--detach")
			case "absent":
				r.Git("worktree", "remove", path)
			case "remote_only":
				r.Git("worktree", "remove", path)
				r.Git("branch", "-D", branch)
				path = ""
			}
			r.Dirty()
			now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
			owner := store.RetirementOwner{Canonical: true, Repo: store.Repo{Forge: store.ForgeGitHub, Owner: "acme", Name: "api", Path: r.Dir, RemoteURL: r.Origin, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: root}, Session: store.Session{IssueNumber: 213, TaskRef: "github:acme/api#213", FeatureSlug: "retire", Branch: branch, WorktreePath: path, CreatedAt: now.Add(-30 * 24 * time.Hour)}}
			if tc.name == "young" {
				owner.Session.CreatedAt = now
			}
			client := retirementRealClient(t, 200, `{"id":2130,"number":213,"state":"closed","state_reason":"completed","url":"https://api.github.com/repos/acme/api/issues/213"}`)
			var got daemon.RetirementEvaluation
			gittest.AssertUnchanged(t, r.Dir, func() {
				got = daemon.EvaluateRetirement(t.Context(), daemon.RetirementOptions{Now: now, RetainDays: 14, Issues: client, ObserveGit: daemon.ObserveRetirementGit}, owner)
			})
			wants := map[string]string{"worktree": tc.wantWork, "local_branch": tc.wantLocal, "remote_branch": tc.wantRemote}
			for _, d := range got.Decisions {
				if d.Action != wants[d.Kind] || (tc.reason != "" && d.Reason != tc.reason) {
					t.Errorf("%s = %s/%s, want %s/%s; error=%v git=%+v", d.Kind, d.Action, d.Reason, wants[d.Kind], tc.reason, got.Error, got.Git)
				}
			}
			if got.Git.BaseSHA == "" {
				t.Fatalf("missing fresh base evidence: %+v", got)
			}
			if got.ObservedAt != now || got.IssueState != "closed" {
				t.Fatalf("missing structured observation: %+v", got)
			}
			if path != "" && tc.name != "absent" {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("preview removed worktree: %v", err)
				}
			}
			if tc.name != "missing_upstream" && tc.name != "missing_merged_upstream" && r.Git("ls-remote", "origin", "refs/heads/"+branch) == "" {
				t.Fatal("preview removed remote")
			}
		})
	}
}

func retirementRealClient(t *testing.T, status int, body string) *github.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/api/issues/213" || r.URL.RawQuery != "" {
			t.Errorf("unexpected individual request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("conditional closure observation")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture"), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRetirementRealIssueResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body, action string
		status             int
	}{
		{"open", `{"id":2130,"number":213,"state":"open"}`, "kept", 200},
		{"not_found", `{"message":"Not Found"}`, "unknown", 404},
		{"rate_limit", `{"message":"rate limited"}`, "unknown", 429},
		{"null", `null`, "unknown", 200},
		{"malformed", `{`, "unknown", 200},
		{"unknown_state", `{"id":2130,"number":213,"state":"other"}`, "unknown", 200},
		{"wrong_number", `{"id":2130,"number":214,"state":"closed","url":"https://api.github.com/repos/acme/api/issues/214"}`, "unknown", 200},
		{"wrong_repository", `{"id":2130,"number":213,"state":"closed","url":"https://api.github.com/repos/other/api/issues/213"}`, "unknown", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := store.RetirementOwner{Canonical: true, Repo: store.Repo{Forge: store.ForgeGitHub, Owner: "acme", Name: "api", FeaturePrefix: "lf"}, Session: store.Session{IssueNumber: 213, TaskRef: "github:acme/api#213", FeatureSlug: "retire", Branch: "lf/213-retire"}}
			got := daemon.EvaluateRetirement(t.Context(), daemon.RetirementOptions{Now: time.Now(), RetainDays: 14, Issues: retirementRealClient(t, tc.status, tc.body), ObserveGit: func(_ context.Context, _ store.RetirementOwner) (git.RetirementObservation, error) {
				t.Fatal("unverified closure reached Git")
				return git.RetirementObservation{}, nil
			}}, owner)
			for _, d := range got.Decisions {
				if d.Action != tc.action {
					t.Fatalf("decision=%+v, want %s", d, tc.action)
				}
			}
		})
	}
}
