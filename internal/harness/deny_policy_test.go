package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/denypolicy"
)

func testDenyPolicy(t *testing.T, entries ...string) *denypolicy.Policy {
	t.Helper()
	policy, err := denypolicy.Resolve(denypolicy.Catalogue{"checks": {Deny: entries}}, "checks", "", "worker")
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestClaudeDenyCompiler(t *testing.T) {
	for _, tc := range []struct{ entry, want string }{
		{"Bash(make check:*)", "Bash(make check:*)"},
		{"Bash(go test *)", "Bash(go test *)"},
		{"Read(secrets/**)", "Read(//repo/**/secrets/**)"},
		{"Read(.env)", "Read(//repo/**/.env)"},
		{"Read(./.env)", "Read(//repo/**/.env)"},
		{"Read(src/components/**)", "Read(//repo/src/components/**)"},
		{"Read(**/private/**)", "Read(//repo/**/private/**)"},
		{"Edit(./generated/*.go)", "Edit(//repo/generated/*.go)"},
		{"Read(//etc/secret)", "Read(//etc/secret)"},
		{"Edit(~/.local/config)", "Edit(~/.local/config)"},
		{"Agent", "Agent"},
		{"Agent(Explore)", "Agent(Explore)"},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			got, err := claudeDenyRules(testDenyPolicy(t, tc.entry), "/repo")
			if err != nil || !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("got %v, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, entry := range []string{"Read(/source-relative)", "Read(../outside)", "Read(config/../secret)", "Read(file?.txt)", "Read(!sample.env)", "Read(offset:0)", "Bash(command:parameter)", "Bash(echo [abc])", "Agent(worker:task)", "Agent(*)"} {
		t.Run("refuse "+entry, func(t *testing.T) {
			_, err := claudeDenyRules(testDenyPolicy(t, entry), "/repo")
			if err == nil {
				t.Fatal("unsupported entry accepted")
			}
			for _, context := range []string{"graph lf_deny", "checks", "entry 1", entry, "Claude cannot enforce"} {
				if !strings.Contains(err.Error(), context) {
					t.Fatalf("error %q lacks %q", err, context)
				}
			}
		})
	}
}

func TestSelectedClaudeSettingsWithoutBash(t *testing.T) {
	script := `while [ "$#" -gt 0 ]; do
 if [ "$1" = "--settings" ]; then shift; /bin/cat "$1" > selected-settings.dump; printf '%s' "$1" > settings-path.dump; fi
 shift
done
` + emit(t, initLine) + emit(t, resultLine)
	f := newFixture(t, script)
	original := []byte(`{"permissions":{"allow":["Read"],"ask":["Edit"],"deny":["Bash(operator:*)"],"additionalDirectories":["/operator"]},"custom":{"retained":true}}`)
	if err := os.WriteFile(f.runner.settingsPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	req := f.request()
	req.DenyPolicy = testDenyPolicy(t, "Read(private/**)", "Agent(Explore)")
	result, err := f.runner.Run(t.Context(), req)
	if err != nil || result.Outcome != OutcomeSucceeded {
		t.Fatalf("Run: %v %v", result, err)
	}
	var doc struct {
		Permissions struct{ Allow, Ask, Deny, AdditionalDirectories []string }
		Sandbox     json.RawMessage
		Custom      map[string]bool
	}
	if err := json.Unmarshal([]byte(f.dump(t, "selected-settings.dump")), &doc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Permissions.Allow, []string{"Read"}) || !reflect.DeepEqual(doc.Permissions.Ask, []string{"Edit"}) || !reflect.DeepEqual(doc.Permissions.AdditionalDirectories, []string{"/operator"}) || !doc.Custom["retained"] {
		t.Fatalf("operator settings changed: %+v", doc)
	}
	for _, rule := range []string{"Bash(operator:*)", "Bash(git push:*)", "Read(/" + filepath.ToSlash(f.worktree) + "/**/private/**)", "Agent(Explore)"} {
		if !slices.Contains(doc.Permissions.Deny, rule) {
			t.Errorf("missing deny %q", rule)
		}
	}
	if len(doc.Sandbox) > 0 {
		t.Fatalf("shell-free run gained sandbox: %s", doc.Sandbox)
	}
	if actual := f.dump(t, "settings-path.dump"); actual == f.runner.settingsPath || strings.HasPrefix(actual, f.worktree) {
		t.Fatalf("unprotected settings %s", actual)
	} else if _, err := os.Stat(actual); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings not cleaned up: %v", err)
	}
	if data, err := os.ReadFile(f.runner.settingsPath); err != nil || !reflect.DeepEqual(data, original) {
		t.Fatalf("operator file changed: %s, %v", data, err)
	}
}

func TestSelectedClaudeRefusesBeforeAnyProcess(t *testing.T) {
	for _, sandboxed := range []bool{false, true} {
		for _, invalid := range []string{`null`, `[]`, `{"permissions":null}`, `{"permissions":[]}`, `{"permissions":{"deny":null}}`, `{"permissions":{"deny":"Agent"}}`, `{"permissions":{"deny":[{}]}}`, `{"permissions":{"allow":[1]}}`, `{"permissions":{"ask":false}}`, `{"sandbox":null}`, `{"sandbox":[]}`} {
			t.Run(invalid+map[bool]string{false: " no Bash", true: " Bash"}[sandboxed], func(t *testing.T) {
				f := newFixture(t, "touch started\n")
				f.runner.thinkingLogs = config.ThinkingLogsSummarized
				if sandboxed {
					f.runner.tools = config.AgentTools()
				}
				if err := os.WriteFile(f.runner.settingsPath, []byte(invalid), 0o600); err != nil {
					t.Fatal(err)
				}
				req := f.request()
				req.DenyPolicy = testDenyPolicy(t, "Agent")
				result, err := f.runner.Run(t.Context(), req)
				if err == nil || result != nil {
					t.Fatalf("got %v, %v", result, err)
				}
				if _, err := os.Stat(filepath.Join(f.worktree, "started")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("a process started: %v", err)
				}
			})
		}
	}
	t.Run("unsupported entry", func(t *testing.T) {
		f := newFixture(t, "touch started\n")
		f.runner.thinkingLogs = config.ThinkingLogsSummarized
		req := f.request()
		req.DenyPolicy = testDenyPolicy(t, "Agent(worker:task)")
		if result, err := f.runner.Run(t.Context(), req); err == nil || result != nil {
			t.Fatalf("got %v, %v", result, err)
		}
		if _, err := os.Stat(filepath.Join(f.worktree, "started")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a process started: %v", err)
		}
	})
}

func TestSelectedClaudeSandboxPreservesOperatorLimits(t *testing.T) {
	f := newFixture(t, successScript(t))
	req := f.request()
	req.DenyPolicy = testDenyPolicy(t, "Agent")
	original := `{"permissions":{"allow":["Read"],"ask":["Edit"],"deny":[]},"sandbox":{"network":{"allowedDomains":["github.com"],"deniedDomains":["blocked.example"],"allowLocalBinding":false},"filesystem":{"denyRead":["/private/operator"],"denyWrite":["/private/output"]},"autoAllowBashIfSandboxed":false,"customLimit":"keep"}}`
	if err := os.WriteFile(f.runner.settingsPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(f.home, "logs", "attempt.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared, err := f.runner.prepareDenySettings(t.Context(), req, logPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.cleanup(); err != nil {
			t.Error(err)
		}
	}()
	raw, err := os.ReadFile(prepared.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Sandbox struct {
			Network struct {
				AllowedDomains, DeniedDomains []string
				AllowLocalBinding             bool
			}
			Filesystem                                                                     struct{ DenyRead, DenyWrite []string }
			Enabled, FailIfUnavailable, AutoAllowBashIfSandboxed, AllowUnsandboxedCommands bool
			CustomLimit                                                                    string
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	sandbox := doc.Sandbox
	if !reflect.DeepEqual(sandbox.Network.AllowedDomains, []string{"github.com"}) || !slices.Contains(sandbox.Network.DeniedDomains, "blocked.example") || sandbox.Network.AllowLocalBinding {
		t.Fatalf("network limits changed: %+v", sandbox.Network)
	}
	if !slices.Contains(sandbox.Filesystem.DenyRead, "/private/operator") || !slices.Contains(sandbox.Filesystem.DenyWrite, "/private/output") || !slices.Contains(sandbox.Filesystem.DenyRead, f.runner.paths.DBPath()) {
		t.Fatalf("filesystem limits changed: %+v", sandbox.Filesystem)
	}
	if !sandbox.Enabled || !sandbox.FailIfUnavailable || sandbox.AutoAllowBashIfSandboxed || sandbox.AllowUnsandboxedCommands || sandbox.CustomLimit != "keep" {
		t.Fatalf("sandbox limits changed: %+v", sandbox)
	}
}

func TestSelectedClaudeSettingsConcurrentAttemptsAreIsolated(t *testing.T) {
	f := newFixture(t, successScript(t))
	logPath := filepath.Join(f.home, "logs", "same.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	policies := []*denypolicy.Policy{testDenyPolicy(t, "Agent(first)"), testDenyPolicy(t, "Agent(second)")}
	prepared := make([]runSandbox, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range prepared {
		wg.Go(func() {
			req := f.request()
			req.DenyPolicy = policies[i]
			prepared[i], errs[i] = f.runner.prepareDenySettings(t.Context(), req, logPath, true)
		})
	}
	wg.Wait()
	for i, p := range prepared {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		defer func() {
			if err := p.cleanup(); err != nil {
				t.Error(err)
			}
		}()
		raw, err := os.ReadFile(p.settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), policies[i].Rules[0].Original) || strings.Contains(string(raw), policies[1-i].Rules[0].Original) {
			t.Fatalf("crossed policy: %s", raw)
		}
	}
	if prepared[0].settingsPath == prepared[1].settingsPath {
		t.Fatal("attempts shared settings")
	}
	if err := prepared[0].cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared[1].settingsPath); err != nil {
		t.Fatalf("one cleanup affected another: %v", err)
	}
}

func TestSelectedClaudeCancellationAndDirectoryFailure(t *testing.T) {
	f := newFixture(t, "touch started\n")
	req := f.request()
	req.DenyPolicy = testDenyPolicy(t, "Agent")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := f.runner.Run(ctx, req); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("cancelled Run: %v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled process started: %v", err)
	}
	// A filesystem failure while preparing the destination returns no settings.
	blocked := filepath.Join(f.home, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := f.runner.prepareDenySettings(t.Context(), req, filepath.Join(blocked, "attempt.log"), false)
	if err == nil || prepared.settingsPath != "" {
		t.Fatalf("write failure returned %v, %v", prepared, err)
	}
}

func TestSelectedClaudeRejectsWritablePolicyArtifacts(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "symlink"}[symlink], func(t *testing.T) {
			f := newFixture(t, "touch started\n")
			req := f.request()
			req.DenyPolicy = testDenyPolicy(t, "Agent")
			protected := filepath.Join(f.worktree, "logs")
			if err := os.MkdirAll(protected, 0o700); err != nil {
				t.Fatal(err)
			}
			parent := protected
			if symlink {
				parent = filepath.Join(f.home, "alias")
				if err := os.Symlink(protected, parent); err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := f.runner.prepareDenySettings(t.Context(), req, filepath.Join(parent, "attempt.log"), true)
			if err == nil || !strings.Contains(err.Error(), "agent-writable root") || prepared.settingsPath != "" {
				t.Fatalf("unprotected settings accepted: %v %v", prepared, err)
			}
		})
	}
}

func TestSelectedClaudePreservesOriginalSettingsSourceAnchors(t *testing.T) {
	f := newFixture(t, successScript(t))
	req := f.request()
	req.DenyPolicy = testDenyPolicy(t, "Agent")
	original := `{"permissions":{"allow":["Read(/source/*.go)","Read(./cwd.txt)"],"ask":["Edit(/source/**)","Edit(~/notes/**)"],"deny":["Read(/private/**)","Read(//etc/private)","Read(!/source/safe/**)"]}}`
	if err := os.WriteFile(f.runner.settingsPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(f.home, "logs", "attempt.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, sandboxed := range []bool{false, true} {
		prepared, err := f.runner.prepareDenySettings(t.Context(), req, logPath, sandboxed)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(prepared.settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Permissions struct{ Allow, Ask, Deny []string }
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		source := "/" + filepath.ToSlash(filepath.Dir(f.runner.settingsPath))
		if !reflect.DeepEqual(doc.Permissions.Allow, []string{"Read(" + source + "/source/*.go)", "Read(./cwd.txt)"}) || !reflect.DeepEqual(doc.Permissions.Ask, []string{"Edit(" + source + "/source/**)", "Edit(~/notes/**)"}) || !slices.Contains(doc.Permissions.Deny, "Read("+source+"/private/**)") || !slices.Contains(doc.Permissions.Deny, "Read(//etc/private)") || !slices.Contains(doc.Permissions.Deny, "Read(!/source/safe/**)") {
			t.Fatalf("settings source changed: %+v", doc.Permissions)
		}
		if err := prepared.cleanup(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelectedClaudeRefusesAmbiguousGeneratedAnchors(t *testing.T) {
	f := newFixture(t, successScript(t))
	for _, path := range []string{"/native[other]", "/native(other)", "/native:other"} {
		if _, err := claudeDenyRules(testDenyPolicy(t, "Read(private/**)"), path); err == nil || !strings.Contains(err.Error(), "glob characters") {
			t.Fatalf("ambiguous worktree %q accepted: %v", path, err)
		}
		doc, err := decodeDenySettings([]byte(`{"permissions":{"deny":["Read(/private/**)"]}}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := anchorSettingsFileRules(doc, path); err == nil || !strings.Contains(err.Error(), "glob characters") {
			t.Fatalf("ambiguous settings source %q accepted: %v", path, err)
		}
		if err := f.runner.checkPolicyDirectory(f.request(), path); err == nil || !strings.Contains(err.Error(), "glob characters") {
			t.Fatalf("ambiguous settings artifact directory %q accepted: %v", path, err)
		}
	}
}

func TestSelectedClaudeDoesNotEnableSandboxEscapeSettings(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		f := newFixture(t, successScript(t))
		req := f.request()
		req.DenyPolicy = testDenyPolicy(t, "Agent")
		req.Permissions = Permissions{Payload: ToolSurface{ReadOnly: readOnly}}
		original := `{"sandbox":{"enableWeakerNestedSandbox":true,"enableWeakerNetworkIsolation":true,"allowAppleEvents":true,"allowNestedUserNamespaces":true,"filesystem":{"disabled":true,"allowRead":["/private/credentials"]},"network":{"allowMachLookup":["*"]}}}`
		if err := os.WriteFile(f.runner.settingsPath, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		document, _, cleanup, err := f.runner.sandboxDocument(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Sandbox struct {
				EnableWeakerNestedSandbox, EnableWeakerNetworkIsolation, AllowAppleEvents, AllowNestedUserNamespaces bool
				Filesystem                                                                                           struct {
					Disabled  bool
					AllowRead []string
				}
				Network struct{ AllowMachLookup []string }
			}
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		sandbox := doc.Sandbox
		if sandbox.EnableWeakerNestedSandbox || sandbox.AllowAppleEvents || sandbox.AllowNestedUserNamespaces || sandbox.Filesystem.Disabled || len(sandbox.Filesystem.AllowRead) > 0 || len(sandbox.Network.AllowMachLookup) > 0 || readOnly && sandbox.EnableWeakerNetworkIsolation {
			t.Fatalf("factory isolation weakened: %+v", sandbox)
		}
		if !readOnly && !sandbox.EnableWeakerNetworkIsolation {
			t.Fatal("writable factory network compatibility setting lost")
		}
		if err := cleanup(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSelectedClaudeRefusesSandboxEnforcementOverrides(t *testing.T) {
	for _, original := range []string{
		`{"sandbox":{"network":{"httpProxyPort":8080}}}`,
		`{"sandbox":{"network":{"socksProxyPort":8081}}}`,
		`{"sandbox":{"bwrapPath":"/operator/bwrap"}}`,
		`{"sandbox":{"socatPath":"/operator/socat"}}`,
		`{"sandbox":{"ripgrep":{"command":"/operator/rg"}}}`,
	} {
		t.Run(original, func(t *testing.T) {
			f := newFixture(t, "touch started\n")
			f.runner.tools = config.AgentTools()
			if err := os.WriteFile(f.runner.settingsPath, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			req := f.request()
			req.DenyPolicy = testDenyPolicy(t, "Agent")
			if result, err := f.runner.Run(t.Context(), req); err == nil || result != nil {
				t.Fatalf("sandbox override accepted: %v %v", result, err)
			}
			if _, err := os.Stat(filepath.Join(f.worktree, "started")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a process started: %v", err)
			}
		})
	}
}
