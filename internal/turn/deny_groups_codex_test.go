package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// This exercises graph resolution, real native delivery and the real adapter.
// The controlled CLI records what exists at process start; it does not prove
// Codex's own command matching or native descendant enforcement.
func TestGraphDenyGroupsCodexLaunch(t *testing.T) {
	for _, tc := range []struct {
		name, graph, node, extra, failure string
		resume, refused, fallback         bool
	}{
		{name: "supported", graph: "commands", node: "delegation"},
		{name: "delegation-allowed", graph: "commands"},
		{name: "resumed", graph: "commands", node: "delegation", resume: true},
		{name: "cold-fallback-frozen-snapshot", graph: "commands", node: "delegation", resume: true, fallback: true},
		{name: "complete-policy-refusal", graph: "commands", node: "unsupported", extra: "    unsupported:\n      deny: ['Edit(secret/**)']\n", refused: true},
		{name: "tamper-corrected-retry", graph: "commands", node: "delegation", failure: "tamper"},
		{name: "config-start-failure-corrected-retry", graph: "commands", node: "delegation", failure: "config"},
		{name: "no-selection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRetainedFixture(t, "implement")
			protected, err := os.MkdirTemp(".", ".codex-deny-test-")
			mustProgress(t, err)
			protected, err = filepath.Abs(protected)
			mustProgress(t, err)
			t.Cleanup(func() { mustProgress(t, os.RemoveAll(protected)) })
			paths := config.NewPaths(filepath.Join(protected, "factory"))
			mustProgress(t, os.MkdirAll(paths.Home(), 0700))
			caches := config.GoBuildCaches{Main: t.TempDir(), Reviewer: t.TempDir()}
			cfg := "agents:\n  codex: {}\nsettings:\n  denyrules:\n    commands:\n      deny: ['Bash(git push:*)']\n    delegation:\n      deny: ['Agent']\n" + tc.extra
			mustProgress(t, os.WriteFile(paths.ConfigPath(), []byte(cfg), 0600))
			home, worktree, capture := filepath.Join(protected, "user"), t.TempDir(), t.TempDir()
			mustProgress(t, os.MkdirAll(filepath.Join(home, ".codex"), 0700))
			mustProgress(t, os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("# unrelated-personal-config\n"), 0600))
			mustProgress(t, os.MkdirAll(filepath.Join(home, ".codex", "rules"), 0700))
			mustProgress(t, os.WriteFile(filepath.Join(home, ".codex", "rules", "personal.rules"), []byte(`prefix_rule(pattern=["git"], decision="allow")`), 0600))
			mustProgress(t, os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"OPENAI_API_KEY":"test-only"}`), 0600))
			script := codexDenyCaptureScript(t, capture)
			if tc.failure != "" {
				action := `printf '\n# changed after validation\n' >> "$CODEX_HOME/config.toml"`
				if tc.failure == "config" {
					action = "exit 8"
				}
				hook := fmt.Sprintf(`if [ "$arg" = app-server ]; then
 if [ ! -f %s ]; then
 touch %s
 %s
 fi
fi`, shellQuote(filepath.Join(capture, "preflight-failed")), shellQuote(filepath.Join(capture, "preflight-failed")), action)
				script = strings.Replace(script, "for arg in \"$@\"; do", "for arg in \"$@\"; do\n"+hook, 1)
			}

			if tc.fallback {
				hook := fmt.Sprintf(`if [ ! -f %s ]; then
 touch %s
 cp %s/rules %s/first-rules
 printf '%%s' %s > %s
 exit 7
fi
`, shellQuote(filepath.Join(capture, "first-start")), shellQuote(filepath.Join(capture, "first-start")), shellQuote(capture), shellQuote(capture), shellQuote(strings.ReplaceAll(cfg, "git push", "git fetch")), shellQuote(paths.ConfigPath()))
				script = strings.Replace(script, "cat >/dev/null", "cat >/dev/null\n"+hook, 1)
			}
			runner := realCodexRunner(t, paths.Home(), script, func(opts *codex.Options) {
				opts.GoBuildCaches = caches
				opts.Environ = append(opts.Environ, "TMPDIR="+t.TempDir())
				for i, entry := range opts.Environ {
					if strings.HasPrefix(entry, "HOME=") {
						opts.Environ[i] = "HOME=" + home
					}
				}
			})
			run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
			mustProgress(t, err)
			mustProgress(t, f.db.SetSegmentAgent(t.Context(), run.SegmentID, store.AgentCodex, "codex-existing-thread"))
			if tc.resume {
				mustProgress(t, f.db.SetSegmentResumeToken(t.Context(), run.SegmentID, "codex-existing-thread"))
			}
			run, err = f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: run.SegmentID, Mode: "implement", Agent: store.AgentCodex, TriggerCommentID: 2, LogPath: func(n int) string { return fmt.Sprintf("codex-%d.log", n) }})
			mustProgress(t, err)
			f.opts.Attempt.RunID = run.ID
			f.opts.Attempt.Attempt = "codex-attempt"
			f.service, err = BindAgentResultService(t.Context(), f.opts)
			mustProgress(t, err)

			segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
			mustProgress(t, err)
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			delivery := codexDenyDelivery(t, f.policy)
			submittedRevision := 0
			handler := AgentActivity{Policy: f.policy, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				if submittedRevision == 0 {
					f.submit(t, f.result)
				} else {
					submission, err := f.service.Submit(t.Context(), "continue", []byte(fmt.Sprintf(`{"expected_revision":%d,"operations":[{"op":"set","target":{"field":"comment"},"value":"Next controlled attempt"}]}`, submittedRevision)))
					mustProgress(t, err)
					if !submission.Validation.Valid || submission.Snapshot == nil {
						t.Fatalf("continuation submission: %+v", submission.Validation)
					}
				}
				submittedRevision++
				request := agent.Request{Delivery: delivery, Ref: ref, Mode: "implement", RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef}, WorktreePath: worktree, Prompt: "Inspect", Permissions: agent.Permissions{Profile: "implement", Payload: codex.Sandbox{Mode: "workspace-write"}}}
				if tc.resume {
					request.Path = agent.PathResume
					request.Session.Resume = "codex-existing-thread"
				}
				invocation := ResearchInvocation{Kind: agent.Kind(store.AgentCodex), SessionPath: ".aiteam/session", Results: f.service, Request: request}
				if tc.fallback {
					cold := request
					cold.Session.Resume = ""
					invocation.ColdRequest = &cold
				}
				return invocation, nil
			}}
			var specs graphexec.Registry
			var activities ActivityRegistry
			mustProgress(t, RegisterAgent(&specs, &activities, handler))
			dot := fmt.Sprintf(`digraph { team=test;mode=implement;profile_version=1;start=work;lf_deny=%q;work [lf_action=lf_agent,lf_outcomes="success,failed",lf_deny=%q];end [lf_terminal=true];work -> end [on=success];work -> end [on=failed];}`, tc.graph, tc.node)
			profile, err := graphexec.ParseProfile([]byte(dot))
			mustProgress(t, err)
			mustProgress(t, graphexec.Validate(profile, specs))
			at := time.Now().UTC()
			definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: profile.Team, Mode: "implement", ProfileVersion: 1, Path: "local/implement.dot", Digest: profile.Digest, CreatedAt: at})
			mustProgress(t, err)
			instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: profile.Start, Status: store.GraphRunning, StartedAt: at})
			mustProgress(t, err)
			out, err := ExecuteNodeAction(t.Context(), Options{DB: f.db, Paths: paths, Runners: RunnerSet{agent.Kind(store.AgentCodex): runner}, LoadDenyConfig: config.Load}, GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, RunID: run.ID, At: at})
			if tc.failure != "" {
				if err == nil {
					t.Fatalf("%s preflight failure was ignored", tc.failure)
				}
				if _, statErr := os.Stat(filepath.Join(capture, "argv")); !os.IsNotExist(statErr) {
					t.Fatalf("failed preflight started agent: %v", statErr)
				}
				instance, err = f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: profile.Start, Status: store.GraphRunning, StartedAt: time.Now().UTC()})
				mustProgress(t, err)
				out, err = ExecuteNodeAction(t.Context(), Options{DB: f.db, Paths: paths, Runners: RunnerSet{agent.Kind(store.AgentCodex): runner}, LoadDenyConfig: config.Load}, GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, RunID: run.ID, At: time.Now().UTC()})
			}
			if tc.refused {
				if _, statErr := os.Stat(filepath.Join(capture, "argv")); !os.IsNotExist(statErr) {
					t.Fatalf("unsupported entry launched agent: %v", statErr)
				}
				if err == nil {
					t.Fatalf("unsupported selection accepted: %+v", out)
				}
				for _, want := range []string{"Codex", "unsupported", "Edit(secret/**)", "work"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("refusal missing %q: %v", want, err)
					}
				}
				return
			}
			if err != nil || out.Activity.Outcome != "success" {
				t.Fatalf("graph launch: %+v: %v", out, err)
			}
			read := func(name string) string {
				t.Helper()
				b, e := os.ReadFile(filepath.Join(capture, name))
				mustProgress(t, e)
				return string(b)
			}
			argv := read("argv")
			if tc.graph == "" {
				if !strings.Contains(argv, "--ignore-rules") || read("codex-home") != "\n" {
					t.Fatalf("unselected launch changed: %s home=%q", argv, read("codex-home"))
				}
				return
			}
			if strings.Contains(argv, "--ignore-rules") || strings.Contains(argv, "--ignore-user-config") {
				t.Fatalf("selected rules/config disabled: %s", argv)
			}
			if tc.node == "delegation" && !strings.Contains(argv, "features.multi_agent=false") {
				t.Fatalf("bare Agent not delivered: %s", argv)
			}
			if !strings.Contains(argv, "default_permissions=") || !strings.Contains(read("config"), `extends = ":workspace"`) {
				t.Fatalf("existing workspace sandbox lost: argv=%s config=%s", argv, read("config"))
			}
			if tc.node == "" && strings.Contains(argv, "features.multi_agent=false") {
				t.Fatalf("command group disabled native delegation: %s", argv)
			}
			generated := read("config")
			if strings.Contains(generated, "unrelated-personal-config") || !strings.Contains(generated, "untrusted") {
				t.Fatalf("selected config did not isolate project/personal layers: %s", generated)
			}
			rules := read("rules")
			if strings.Contains(rules, `decision="allow"`) {
				t.Fatalf("personal allow imported: %s", rules)
			}
			if !strings.Contains(rules, `"git"`) || !strings.Contains(rules, `"push"`) || !strings.Contains(rules, `"forbidden"`) {
				t.Fatalf("literal forbidden prefix absent: %s", rules)
			}
			if strings.TrimSpace(read("home")) != home {
				t.Fatal("selected launch changed HOME")
			}
			selectedHome := strings.TrimSpace(read("codex-home"))
			if selectedHome == "" {
				t.Fatal("selected launch lacks protected home")
			}
			canonical, err := filepath.EvalSymlinks(filepath.Join(home, ".codex"))
			mustProgress(t, err)
			for _, name := range []string{"auth.json", "sessions", "archived_sessions"} {
				target, err := os.Readlink(filepath.Join(selectedHome, name))
				mustProgress(t, err)
				if target != filepath.Join(canonical, name) {
					t.Fatalf("%s bridge moved canonical state: %s", name, target)
				}
			}
			policyRoot, err := filepath.EvalSymlinks(paths.CodexPolicyDir())
			mustProgress(t, err)
			for _, protected := range []string{policyRoot, canonical} {
				if !strings.Contains(generated, fmt.Sprintf("%q = \"read\"", protected)) {
					t.Fatalf("protected runtime location lacks read ceiling: %s", generated)
				}
			}
			if !strings.Contains(generated, caches.Main) {
				t.Fatalf("existing cache grant lost: %s", generated)
			}
			if tc.fallback {
				if read("first-rules") != rules || strings.Contains(rules, "fetch") {
					t.Fatalf("cold fallback reloaded mutable settings: first=%s final=%s", read("first-rules"), rules)
				}
				if strings.Contains(argv, "resume") {
					t.Fatalf("fallback still resumes: %s", argv)
				}
			}
			if tc.resume && !tc.fallback && (!strings.Contains(argv, "resume") || !strings.Contains(argv, "codex-existing-thread")) {
				t.Fatalf("resume identity lost: %s", argv)
			}
			if tc.name == "supported" {
				advance := func(graph, node, newSession string) {
					t.Helper()
					if newSession != "" {
						mustProgress(t, f.db.SetSegmentAgent(t.Context(), run.SegmentID, store.AgentCodex, newSession))
						tc.resume = false
					} else {
						mustProgress(t, f.db.SetSegmentResumeToken(t.Context(), run.SegmentID, "codex-existing-thread"))
						tc.resume = true
					}
					segment, err = f.db.GetSegment(t.Context(), run.SegmentID)
					mustProgress(t, err)
					run, err = f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentCodex, TriggerCommentID: int64(run.Ordinal + 1), LogPath: func(n int) string { return fmt.Sprintf("codex-%d.log", n) }})
					mustProgress(t, err)
					f.opts.Attempt.RunID = run.ID
					f.opts.Attempt.Attempt = fmt.Sprintf("codex-attempt-%d", run.Ordinal)
					f.service, err = BindAgentResultService(t.Context(), f.opts)
					mustProgress(t, err)
					next, err := graphexec.ParseProfile([]byte(fmt.Sprintf(`digraph { team=test;mode=implement;profile_version=1;start=work;lf_deny=%q;work [lf_action=lf_agent,lf_outcomes="success,failed",lf_deny=%q];end [lf_terminal=true];work -> end [on=success];work -> end [on=failed];}`, graph, node)))
					mustProgress(t, err)
					mustProgress(t, graphexec.Validate(next, specs))
					at := time.Now().UTC()
					definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: next.Team, Mode: "implement", ProfileVersion: 1, Path: "local/implement.dot", Digest: next.Digest, CreatedAt: at})
					mustProgress(t, err)
					instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: next.Start, Status: store.GraphRunning, StartedAt: at})
					mustProgress(t, err)
					out, err := ExecuteNodeAction(t.Context(), Options{DB: f.db, Paths: paths, Runners: RunnerSet{agent.Kind(store.AgentCodex): runner}, LoadDenyConfig: config.Load}, GraphNodeRequest{Profile: next, Activities: activities, InstanceID: instance.ID, RunID: run.ID, At: at})
					if err != nil || out.Activity.Outcome != "success" {
						t.Fatalf("next graph attempt: %+v: %v", out, err)
					}
				}
				mustProgress(t, os.WriteFile(paths.ConfigPath(), []byte(strings.ReplaceAll(cfg, "git push", "git fetch")), 0600))
				advance("commands", "delegation", "")
				if strings.TrimSpace(read("codex-home")) != selectedHome || !strings.Contains(read("rules"), `"fetch"`) || strings.Contains(read("rules"), `"push"`) {
					t.Fatalf("new selected attempt did not replace policy in stable home: %s %s", read("codex-home"), read("rules"))
				}
				retainedRules := read("rules")
				advance("", "", "")
				if read("codex-home") != "\n" || !strings.Contains(read("argv"), "--ignore-rules") || !strings.Contains(read("argv"), "codex-existing-thread") {
					t.Fatalf("group removal changed canonical resumed launch: %s %s", read("codex-home"), read("argv"))
				}
				mustProgress(t, os.WriteFile(paths.ConfigPath(), []byte(strings.ReplaceAll(cfg, "git push", "git status")), 0600))
				advance("commands", "", "independent-codex-session")
				if strings.TrimSpace(read("codex-home")) == selectedHome {
					t.Fatal("independent factory session reused policy home")
				}
				oldRules, err := os.ReadFile(filepath.Join(selectedHome, "rules", "selected.rules"))
				mustProgress(t, err)
				if string(oldRules) != retainedRules {
					t.Fatal("independent session changed earlier session policy")
				}
			}

		})
	}
}

func codexDenyDelivery(t *testing.T, policy WorkflowPolicy) *agent.Delivery {
	t.Helper()
	opts, req, def := deliveryFixture()
	req.Agent, req.Mode, req.Workflow = "codex", policy.Mode, policy.Workflow
	opts.Tiers.ModelByTier["codex"] = map[config.Tier]string{"balanced": "gpt-test"}
	p, err := planWorkflowDelivery(opts, req, def, "first", policy)
	mustProgress(t, err)
	delivery, err := writeDelivery(t.Context(), t.TempDir(), "bundles/codex-deny", p)
	mustProgress(t, err)
	return delivery
}

func codexDenyCaptureScript(t *testing.T, capture string) string {
	t.Helper()
	executable, err := os.Executable()
	mustProgress(t, err)
	return fmt.Sprintf(`case "$1" in
 --version) printf 'codex-cli 0.160.0\n'; exit 0;;
 execpolicy) printf '{"decision":"forbidden"}\n'; exit 0;;
esac
for arg in "$@"; do
 if [ "$arg" = app-server ]; then exec %s -test.run=^TestCodexDenyAppServer$ -- codex-deny-app-server "$@"; fi
 if [ "$arg" = --help ]; then printf '%%s\n' '--ignore-rules --ignore-user-config --config --strict-config'; exit 0; fi
done
printf '%%s\n' "$@" > %s/argv
printf '%%s\n' "$CODEX_HOME" > %s/codex-home
printf '%%s\n' "$HOME" > %s/home
if [ -n "$CODEX_HOME" ]; then
 cat "$CODEX_HOME"/rules/* > %s/rules
 cp "$CODEX_HOME/config.toml" %s/config
fi
cat >/dev/null
printf '%%s\n' '{"type":"thread.started","thread_id":"codex-existing-thread"}' '{"type":"turn.started"}' '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}' '{"type":"turn.completed"}'
`, shellQuote(executable), shellQuote(capture), shellQuote(capture), shellQuote(capture), shellQuote(capture), shellQuote(capture))
}

// TestCodexDenyAppServer is the controlled CLI's protocol helper. It runs in the
// current Go test executable; no provider, network, or additional runtime is used.
func TestCodexDenyAppServer(t *testing.T) {
	if !slices.Contains(os.Args, "codex-deny-app-server") {
		return
	}
	if err := codexDenyConfigProtocol(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func codexDenyConfigProtocol(input io.Reader, output io.Writer) error {
	decode, encode := json.NewDecoder(input), json.NewEncoder(output)
	for {
		var message struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cwd string `json:"cwd"`
			} `json:"params"`
		}
		if err := decode.Decode(&message); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		var result any
		switch message.Method {
		case "initialize":
			result = map[string]any{}
		case "initialized":
			continue
		case "config/read":
			canonical, err := filepath.EvalSymlinks(filepath.Join(os.Getenv("HOME"), ".codex"))
			if err != nil {
				return err
			}
			profile := map[string]any{"extends": ":workspace"}
			projects := map[string]any{message.Params.Cwd: map[string]any{"trust_level": "untrusted"}}
			layer := map[string]any{"sqlite_home": canonical, "permissions": map[string]any{"factory_selected": profile}, "projects": projects}
			effective := map[string]any{"approval_policy": "never", "default_permissions": "factory_selected", "cli_auth_credentials_store": "file", "sqlite_home": canonical, "permissions": layer["permissions"], "projects": projects}
			if slices.Contains(os.Args, "features.multi_agent=false") {
				effective["features"] = map[string]any{"multi_agent": false}
			}
			result = map[string]any{"config": effective, "layers": []any{map[string]any{"name": map[string]any{"type": "user", "file": filepath.Join(os.Getenv("CODEX_HOME"), "config.toml")}, "config": layer}}}
		default:
			return fmt.Errorf("unexpected controlled configuration method %q", message.Method)
		}
		if err := encode.Encode(map[string]any{"id": message.ID, "result": result}); err != nil {
			return err
		}
	}
}
