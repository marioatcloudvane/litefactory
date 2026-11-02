package daemon_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestUnlabelledBugFilingSourcesUseConfiguredAgent(t *testing.T) {
	for _, origin := range []string{"hand-filed", "babysitter", "log analysis"} {
		t.Run(origin, func(t *testing.T) {
			f := newFixture(t)
			runtime, err := turn.NewGraphBugRuntime(t.Context(), turn.Options{DB: f.db, Git: new(git.Binary), CloneGate: unusedRecoveryCloneGate{}, ResearchReporter: f.reporter, MCP: &turn.MCPRuntime{Redactor: redact.New(redact.Options{Logger: f.logger}), BotID: botID}}, turn.BugRuntimeConfig{Forge: f.client, OwnerID: ownerID, Permissions: func(agent.Kind, string) (turn.BugRolePermissions, error) { return turn.BugRolePermissions{}, nil }})
			if err != nil {
				t.Fatal(err)
			}
			f.daemon = f.newDaemon(daemon.Options{Bug: runtime, BugAgent: "codex", DefaultAgent: "claude", Agents: turn.RunnerSet{"claude": nil, "codex": nil}})
			var agentLabelWrites, bugLabelWrites atomic.Int32
			f.forge.onAddLabels = func(_ http.ResponseWriter, r *http.Request) bool {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return false
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				var labels []string
				if err := json.Unmarshal(body, &labels); err != nil {
					t.Error(err)
					return false
				}
				for _, label := range labels {
					if strings.HasPrefix(label, "Coding Agent:") {
						agentLabelWrites.Add(1)
					}
					if label == "bug" {
						bugLabelWrites.Add(1)
					}
				}
				return false
			}
			f.plantCursor()
			f.forge.addIssue(fakeIssue{Number: testIssue - 1, Title: "Claude source", Body: "Existing unrelated source", User: stranger(), Labels: toFakeLabels("Coding Agent: Claude")})
			source := fmt.Sprintf("Source: %s/%s/%s/issues/%d", f.forge.server.URL, testOwnerName, testRepoName, testIssue-1)
			body := "Investigate the observed failure.\n\n" + source
			if origin == "log analysis" {
				marker := loganalysis.Marker("filing-test", "configured-agent")
				r := redact.New(redact.Options{Environ: []string{}})
				title, err := r.Apply(t.Context(), "Log-analysis Bug", 256)
				if err != nil {
					t.Fatal(err)
				}
				content, err := r.Apply(t.Context(), body+"\n\n"+marker+"\n", 4096)
				if err != nil {
					t.Fatal(err)
				}
				tracker := loganalysis.GitHubTracker{Client: f.client, Repo: github.Repo{Owner: testOwnerName, Name: testRepoName}}
				filed, err := tracker.FileBug(t.Context(), title, content, marker, f.clock.Now())
				if err != nil || filed.Reference != fmt.Sprintf("%s/%s#%d", testOwnerName, testRepoName, testIssue) {
					t.Fatalf("actual log-analysis filing failed: filed=%+v err=%v", filed, err)
				}
				f.settle()
				if len(f.turns.calls()) != 0 {
					t.Fatal("bot-filed issue body bypassed owner authorization")
				}
				f.forge.addComment(testIssue, 42, owner(), "Please investigate and fix this Bug")
			} else {
				f.forge.addIssue(fakeIssue{Number: testIssue, Title: origin + " Bug", Body: body, User: owner(), Labels: toFakeLabels("bug")})
			}
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Agent != "codex" || calls[0].Workflow != agent.WorkflowBug {
				t.Fatalf("origin %s did not use configured Codex: calls=%+v logs=%s", origin, calls, f.logs)
			}
			if agentLabelWrites.Load() != 0 {
				t.Fatal("dispatch wrote a Coding Agent label")
			}
			if origin == "log analysis" && bugLabelWrites.Load() != 1 {
				t.Fatal("actual filer did not add the Bug label")
			}
			issue, err := f.client.GetIssue(t.Context(), github.Repo{Owner: testOwnerName, Name: testRepoName}, testIssue)
			if err != nil {
				t.Fatal(err)
			}
			for _, label := range issue.Labels {
				if strings.HasPrefix(label, "Coding Agent:") {
					t.Fatalf("unlabelled Bug acquired agent label %q", label)
				}
			}
		})
	}
}
