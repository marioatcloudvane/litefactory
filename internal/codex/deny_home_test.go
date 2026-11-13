package codex

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestDenyMkdirRejectsSymlinkBeforeCreatingChildren(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if err := denyMkdir(filepath.Join(alias, "child")); err == nil {
		t.Fatal("accepted symlinked policy ancestor")
	}
	if _, err := os.Stat(filepath.Join(target, "child")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created a directory through a symlink: %v", err)
	}
}

func TestDenyCredentialMode(t *testing.T) {
	for _, config := range []string{"", `cli_auth_credentials_store = "file"`, "# cli_auth_credentials_store = \"keyring\"\n", "[other]\nkey = \"value\"\n"} {
		if err := denyCredentialMode([]byte(config)); err != nil {
			t.Errorf("safe configuration %q: %v", config, err)
		}
	}
	for _, config := range []string{`cli_auth_credentials_store = "keyring"`, `cli_auth_credentials_store = "auto"`, `cli_auth_credentials_store = true`, `"cli_auth_credentials_store" = "keyring"`, `cli_auth_credentials_store = "file`, strings.Repeat(" ", (1<<20)+1)} {
		if err := denyCredentialMode([]byte(config)); err == nil {
			t.Errorf("accepted unsupported credential configuration %.80q", config)
		}
	}
}

func TestDenyCanonicalHome(t *testing.T) {
	for _, scenario := range []string{"file", "missing-auth", "symlink-auth", "custom-home", "keyring", "relative-home"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			canonical := filepath.Join(home, ".codex")
			if err := os.Mkdir(canonical, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(canonical, "auth.json"), []byte(`{"test":"not-a-credential"}`), 0600); err != nil {
				t.Fatal(err)
			}
			r := &Runner{environ: []string{"HOME=" + home}}
			switch scenario {
			case "missing-auth":
				if err := os.Remove(filepath.Join(canonical, "auth.json")); err != nil {
					t.Fatal(err)
				}
			case "symlink-auth":
				target := filepath.Join(home, "other-auth")
				if err := os.Rename(filepath.Join(canonical, "auth.json"), target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(canonical, "auth.json")); err != nil {
					t.Fatal(err)
				}
			case "custom-home":
				r.environ = append(r.environ, "CODEX_HOME="+t.TempDir())
			case "keyring":
				if err := os.WriteFile(filepath.Join(canonical, "config.toml"), []byte(`cli_auth_credentials_store = "keyring"`), 0600); err != nil {
					t.Fatal(err)
				}
			case "relative-home":
				r.environ = []string{"HOME=relative"}
			}
			got, err := r.denyCanonicalHome()
			if scenario == "file" {
				want, resolveErr := filepath.EvalSymlinks(canonical)
				if resolveErr != nil {
					t.Fatal(resolveErr)
				}
				if err != nil || got != want {
					t.Fatalf("home=%q, %v; want %q", got, err, want)
				}
			} else if err == nil {
				t.Fatalf("accepted unsafe home %q", got)
			}
		})
	}
}

func TestSelectedContainment(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(root, "protected")
	writable := filepath.Join(root, "worktree")
	for _, dir := range []string{protected, writable, filepath.Join(protected, "child")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(protected, alias); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		root string
		safe bool
	}{{writable, true}, {root, false}, {protected, false}, {filepath.Join(protected, "child"), false}, {alias, false}} {
		launch := &selectedLaunch{roots: []string{test.root}, protected: []string{protected}}
		if err := launch.containment(); (err == nil) != test.safe {
			t.Errorf("containment %q safe=%v: %v", test.root, test.safe, err)
		}
	}
}

func newSelectedArtifactFixture(t *testing.T) *selectedLaunch {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	canonical := filepath.Join(base, "canonical")
	for _, dir := range []string{filepath.Join(home, "rules"), canonical} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{filepath.Join(home, "rules", "selected.rules"): "prefix_rule(pattern=[\"safe\"], decision=\"forbidden\")\n", filepath.Join(canonical, "auth.json"): "{}"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	launch := &selectedLaunch{home: home, canonical: canonical, links: map[string]string{}}
	if err := launch.bridge(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{home, filepath.Join(home, "rules"), filepath.Join(home, "rules", "selected.rules")} {
		if err := launch.capture(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := launch.verify(); err != nil {
		t.Fatal(err)
	}
	return launch
}

func TestSelectedArtifactVerification(t *testing.T) {
	for _, scenario := range []string{"content", "inode", "extra-rule", "link-target", "state-directory", "auth-link"} {
		t.Run(scenario, func(t *testing.T) {
			launch := newSelectedArtifactFixture(t)
			rule := filepath.Join(launch.home, "rules", "selected.rules")
			switch scenario {
			case "content":
				if err := os.WriteFile(rule, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "inode":
				data, err := os.ReadFile(rule)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(rule+".new", data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(rule+".new", rule); err != nil {
					t.Fatal(err)
				}
			case "extra-rule":
				if err := os.WriteFile(filepath.Join(launch.home, "rules", "allow.rules"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "link-target":
				link := filepath.Join(launch.home, "sessions")
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), link); err != nil {
					t.Fatal(err)
				}
			case "state-directory":
				dir := filepath.Join(launch.canonical, "sessions")
				if err := os.Rename(dir, dir+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			case "auth-link":
				auth := filepath.Join(launch.canonical, "auth.json")
				if err := os.Rename(auth, auth+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(auth+".old", auth); err != nil {
					t.Fatal(err)
				}
			}
			if err := launch.verify(); err == nil {
				t.Fatal("accepted mutated protected launch")
			}
		})
	}
}

func TestSelectedConfigurationBaseline(t *testing.T) {
	launch := &selectedLaunch{canonical: "/canonical", protected: []string{"/policy", "/canonical"}}
	text, args, err := selectedConfiguration(agent.Request{WorktreePath: "/worktree/repo"}, &compiledDenyPolicy{DisableDelegation: true}, "workspace-write", []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "workspace-write", "--add-dir", "/cache", "-c", "sandbox_workspace_write.network_access=true", "--model", "test-model", "-"}, launch)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`approval_policy = "never"`, `cli_auth_credentials_store = "file"`, `sqlite_home = "/canonical"`, `trust_level = "untrusted"`, `"/policy"`, `"/canonical"`, `"/cache"`} {
		if !strings.Contains(text, want) {
			t.Errorf("configuration lacks %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"--ignore-rules", "--ignore-user-config", "--sandbox"} {
		if slices.Contains(args, forbidden) {
			t.Errorf("selected launch retained %s: %v", forbidden, args)
		}
	}
	for _, want := range []string{"--strict-config", `features.multi_agent=false`, `approval_policy="never"`, "test-model"} {
		if !slices.Contains(args, want) {
			t.Errorf("arguments lack %q: %v", want, args)
		}
	}
}
