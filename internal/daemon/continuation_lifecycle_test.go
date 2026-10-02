package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A cancellable gate covers the one real clone in each isolated lifecycle.
// No test exercises two unrelated clones through this helper.
type lifecycleCloneGate chan struct{}

func (g lifecycleCloneGate) Acquire(ctx context.Context, _ string) (func(), error) {
	select {
	case g <- struct{}{}:
		return func() { <-g }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func lifecycleQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// Fixtures below are synthetic future-reset variants of the authentic diagnostic
// forms in harness/codex testdata. No paid provider or external forge is called.
func TestRecoveryLifecycleBothRealCLIRunners(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      agent.Kind
		exhausted bool
	}{
		{name: "claude", kind: agent.Kind(store.AgentClaude)}, {name: "codex", kind: agent.Kind(store.AgentCodex)}, {name: "three automatic limits", kind: agent.Kind(store.AgentClaude), exhausted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := tc.kind
			f := newFixture(t)
			realRepo := gittest.New(t)
			if err := os.Rename(realRepo.Dir, f.repo.Path); err != nil {
				t.Fatal(err)
			}
			f.repo.RemoteURL = realRepo.Origin
			var err error
			f.repo, err = f.db.UpsertRepo(t.Context(), f.repo)
			if err != nil {
				t.Fatal(err)
			}
			binary, err := git.Resolve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			fakeBinary := filepath.Join(root, string(kind))
			argvPath := filepath.Join(root, "argv")
			invokedPath := filepath.Join(root, "invoked")
			reset := time.Now().In(time.Local).Add(48 * time.Hour).Truncate(time.Minute)
			var first, second string
			if kind == agent.Kind(store.AgentClaude) {
				first = fmt.Sprintf("{\"type\":\"rate_limit_event\",\"rate_limit_info\":{\"status\":\"rejected\",\"resetsAt\":%d}}\n{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"session limit\"}\n", reset.Unix())
				second = "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"Recovered original work\"}\n"
			} else {
				message := fmt.Sprintf("You've hit your usage limit. Please try again at %s.", reset.Format("Jan 2, 2006 3:04 PM"))
				encoded, _ := json.Marshal(message)
				first = "{\"type\":\"thread.started\",\"thread_id\":\"lifecycle-thread\"}\n{\"type\":\"turn.failed\",\"error\":{\"message\":" + string(encoded) + "}}\n"
				second = "{\"type\":\"thread.started\",\"thread_id\":\"lifecycle-thread\"}\n{\"type\":\"turn.started\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"Recovered original work\"}}\n{\"type\":\"turn.completed\"}\n"
			}
			if tc.exhausted {
				second = first
			}
			script := "#!/bin/sh\nset -eu\ncat >/dev/null\nprintf '%s ' \"$@\" >> " + lifecycleQuote(argvPath) + "\nprintf '\\n' >> " + lifecycleQuote(argvPath) + "\nif [ ! -e " + lifecycleQuote(invokedPath) + " ]; then\n touch " + lifecycleQuote(invokedPath) + "\n printf 'unfinished work\\n' > partial.txt\n cat <<'FIRST'\n" + first + "FIRST\nelse\n test -f partial.txt\n cat <<'SECOND'\n" + second + "SECOND\nfi\n"
			if err := os.WriteFile(fakeBinary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			env := []string{"PATH=/usr/bin:/bin", "HOME=" + root, "USER=lf-test"}
			var runner agent.Runner
			if kind == agent.Kind(store.AgentClaude) {
				cfg := config.DefaultClaude()
				cfg.Binary = fakeBinary
				cfg.TimeoutSeconds = 15
				runner, err = harness.New(harness.Options{Paths: f.paths, Claude: cfg, Model: config.DefaultClaudeModel, Environ: env, Logger: f.logger})
			} else {
				runner, err = codex.New(codex.Options{Paths: f.paths, Codex: config.Codex{Binary: fakeBinary, TimeoutSeconds: 15}, Model: "gpt-test", Environ: env, Logger: f.logger})
			}
			if err != nil {
				t.Fatal(err)
			}
			runners := turn.RunnerSet{kind: runner}
			executor := daemon.NewExecutor(turn.Options{AutoResumeOnSessionLimit: true, DB: f.db, Git: binary, Runners: runners, Paths: f.paths, InstanceID: "lifecycle", CloneGate: make(lifecycleCloneGate, 1), Author: git.Author{Name: "Recovery Test", Email: "recovery@example.invalid"}, Logger: f.logger})
			buildDaemon := func() *daemon.Daemon {
				d, err := daemon.New(daemon.Options{AutoResumeOnSessionLimit: true, DefaultAgent: kind, Agents: runners, Classifiers: map[agent.Kind]agent.Classifier{kind: fakeModeClassifier{}}, ToolProfiles: config.DefaultToolProfiles().Permissions, DB: f.db, Forge: f.client, Filter: f.filter, Turn: executor, Reporter: f.reporter, InstanceID: "lifecycle", Interval: testInterval, Logger: f.logger, Now: f.clock.Now})
				if err != nil {
					t.Fatal(err)
				}
				return d
			}
			f.daemon = buildDaemon()
			f.clock.advance(time.Now().UTC().Sub(f.clock.Now()))
			f.plantCursor()
			f.issue("Recover provider lifecycle")
			f.settle()
			f.forge.addComment(testIssue, 42, owner(), "/implement complete the original work")
			// Lose the first report; the subsequent daemon instance must discover it
			// without repeating the source execution.
			f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "synthetic report loss"})
				return true
			}
			f.settle()
			session := f.session()
			runs, err := f.db.ListRuns(t.Context(), session.ID)
			if err != nil || len(runs) != 1 || runs[0].SemanticOutcome != "interrupted" || runs[0].Succeeded() {
				t.Fatalf("provider did not interrupt through turn: %+v %v logs=%s", runs, err, f.logs)
			}
			actions, err := f.db.ListPendingContinuations(t.Context(), session.ID)
			if err != nil || len(actions) != 1 {
				t.Fatalf("no durable recovery: %+v %v logs=%s", actions, err, f.logs)
			}
			action := actions[0]
			if action.Context.ResumeIdentity == "" || action.Context.Agent != string(kind) || action.Context.Request != "complete the original work" {
				t.Fatalf("saved context=%+v", action.Context)
			}
			if data, err := os.ReadFile(filepath.Join(action.Context.Worktree, "partial.txt")); err != nil || string(data) != "unfinished work\n" {
				t.Fatalf("unsaved partial work lost=%q %v", data, err)
			}
			if runs[0].CommitSHA != "" {
				t.Fatalf("interrupted work was committed=%+v", runs[0])
			}
			if _, err := f.db.AdvanceWatermark(t.Context(), session.ID, 42); err != nil {
				t.Fatal(err)
			}
			f.forge.onPostComment = nil
			restoreTrustedRecoveryComment(t, f, session.ID, 42, 900001)
			f.daemon = buildDaemon()
			if _, err := f.daemon.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.settle()
			source, err := f.db.GetRun(t.Context(), runs[0].ID)
			if err != nil || source.Status != store.RunReported || source.Succeeded() {
				t.Fatalf("restart report=%+v %v", source, err)
			}
			// Before the persisted UTC deadline there is still only the source run.
			before, err := f.db.ListRuns(t.Context(), session.ID)
			if err != nil || len(before) != 1 {
				t.Fatalf("early recovery=%+v %v", before, err)
			}
			automaticAttempts := 1
			if tc.exhausted {
				automaticAttempts = 3
			}
			lastAction := action
			for attempt := 1; attempt <= automaticAttempts; attempt++ {
				if wait := lastAction.DueAt.Sub(f.clock.Now()) + time.Minute; wait > 0 {
					f.clock.advance(wait)
				}
				f.settle()
				if attempt < automaticAttempts {
					pending, err := f.db.ListPendingContinuations(t.Context(), session.ID)
					if err != nil || len(pending) != 1 || pending[0].Attempt != attempt+1 {
						t.Fatalf("fresh quota continuation missing=%+v %v logs=%s", pending, err, f.logs)
					}
					lastAction = pending[0]
				}
			}
			final, err := f.db.ListRuns(t.Context(), session.ID)
			if err != nil || len(final) != automaticAttempts+1 {
				t.Fatalf("automatic allocations=%+v %v logs=%s", final, err, f.logs)
			}
			terminal := final[len(final)-1]
			saved, err := f.db.GetContinuation(t.Context(), lastAction.ID)
			if err != nil || saved.AllocatedRunID != terminal.ID {
				t.Fatalf("chain settlement=%+v %v", saved, err)
			}
			if tc.exhausted {
				if terminal.Succeeded() || terminal.SemanticOutcome != "interrupted" || saved.Status != "failed" || !strings.Contains(terminal.RecoveryReason, "manual") {
					t.Fatalf("exhaustion did not request manual recovery: %+v %+v", terminal, saved)
				}
				pending, err := f.db.ListPendingContinuations(t.Context(), session.ID)
				if err != nil || len(pending) != 0 {
					t.Fatalf("fourth automatic action scheduled=%+v %v", pending, err)
				}
				posts, _, _ := f.forge.snapshot()
				if len(posts) == 0 || !strings.Contains(posts[len(posts)-1], "three automatic invocation limit") {
					t.Fatalf("exhaustion not visible=%v", posts)
				}
			} else if !terminal.Succeeded() || saved.Status != "succeeded" || terminal.CommitSHA == "" {
				t.Fatalf("recovery did not save successfully=%+v %+v logs=%s", terminal, saved, f.logs)
			}
			argv, err := os.ReadFile(argvPath)
			if err != nil {
				t.Fatal(err)
			}
			calls := strings.Split(strings.TrimSpace(string(argv)), "\n")
			if len(calls) != automaticAttempts+1 {
				t.Fatalf("CLI invoked %d times: %s", len(calls), argv)
			}
			resumeFlag := "--resume " + action.Context.ResumeIdentity
			if kind == agent.Kind(store.AgentCodex) {
				resumeFlag = "resume " + action.Context.ResumeIdentity
			}
			if !strings.Contains(calls[1], resumeFlag) {
				t.Fatalf("original conversation not explicitly resumed: %s", calls[1])
			}
			f.settle()
			after, err := f.db.ListRuns(t.Context(), session.ID)
			if err != nil || len(after) != automaticAttempts+1 {
				t.Fatalf("finished chain ran again=%+v %v", after, err)
			}
		})
	}
}
