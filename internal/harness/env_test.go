package harness

import (
	"slices"
	"strings"
	"testing"
)

// The env allowlist is the security boundary (SD-4, AC-3.13). These tests are
// about what the child must never receive, so they are written as "assert the
// absence of", not "assert the shape of".

// daemonEnviron is a realistic daemon environment: the variables claude needs,
// the ones litefactory sets, and the credentials that are in the daemon's
// process precisely because it needs them for the forge API and for git.
func daemonEnviron() []string {
	return []string{
		"PATH=/opt/homebrew/bin:/usr/bin:/bin",
		"HOME=/Users/tester",
		"USER=tester",
		"SHELL=/bin/zsh",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"TERM=xterm-256color",
		"TMPDIR=/var/folders/xx/T/",
		"EDITOR=vi",

		// Must not reach the child.
		"GITHUB_TOKEN=ghp_0123456789abcdef",
		"GH_TOKEN=gho_0123456789abcdef",
		"LF_HOME=/Users/tester/.litefactory",
		"LF_BOT_TOKEN=ghs_0123456789abcdef",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI",
		"DATABASE_PASSWORD=hunter2",
		"MYSQL_PASSWD=hunter2",
		"SOME_CREDENTIAL=abc",
		"STRIPE_API_KEY=sk_live_abc",
		"SIGNING_KEY=abc",
		"SESSION_SECRET=abc",
		"SSH_AUTH_SOCK=/private/tmp/agent.sock",
		"CLAUDE_CONFIG_DIR=/Users/tester/.claude",
		"ANTHROPIC_API_KEY=sk-ant-stale",
		"ANTHROPIC_BASE_URL=https://example.invalid",
	}
}

func envNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok {
			names = append(names, name)
		}
	}
	return names
}

func TestBuildEnvPassesOnlyTheAllowlist(t *testing.T) {
	env := buildEnv(daemonEnviron(), false)

	want := []string{
		"PATH=/opt/homebrew/bin:/usr/bin:/bin",
		"HOME=/Users/tester",
		"USER=tester",
		"SHELL=/bin/zsh",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"TERM=xterm-256color",
		"TMPDIR=/var/folders/xx/T/",
	}
	if !slices.Equal(env, want) {
		t.Fatalf("child environment\n got %q\nwant %q", env, want)
	}
}

// USER is not decoration. Without it claude's macOS Keychain lookup silently
// fails and the run dies "Not logged in", exit 1 (SD-4). The spike measured it.
func TestBuildEnvAlwaysCarriesUSER(t *testing.T) {
	env := buildEnv(daemonEnviron(), false)
	if !slices.Contains(env, "USER=tester") {
		t.Fatalf("USER missing from child environment %q; Keychain auth will fail", env)
	}
}

func TestBuildEnvExcludesSecretsAndLitefactoryVariables(t *testing.T) {
	env := buildEnv(daemonEnviron(), false)

	for _, name := range envNames(env) {
		if IsSecretEnvName(name) {
			t.Errorf("secret-shaped variable %q reached the child", name)
		}
		if strings.HasPrefix(name, "LF_") {
			t.Errorf("litefactory variable %q reached the child", name)
		}
		switch name {
		case "GITHUB_TOKEN", "GH_TOKEN":
			t.Errorf("forge token variable %q reached the child", name)
		case "SSH_AUTH_SOCK":
			// Deliberately not forwarded to claude (SD-4). litefactory performs
			// the push itself; the agent has no business holding the key.
			t.Errorf("SSH_AUTH_SOCK reached the claude child")
		case "CLAUDE_CONFIG_DIR":
			// Setting it breaks Keychain auth even when the directory is seeded.
			t.Errorf("CLAUDE_CONFIG_DIR reached the child")
		}
	}

	// The values, not just the names: nothing in the child's environment should
	// contain a credential we know the daemon holds.
	joined := strings.Join(env, "\n")
	for _, secret := range []string{"ghp_0123456789abcdef", "gho_0123456789abcdef", "hunter2", "sk-ant-stale"} {
		if strings.Contains(joined, secret) {
			t.Errorf("credential %q appears in the child environment", secret)
		}
	}
}

// The plan's original allowlist was self-cancelling: it listed "required
// ANTHROPIC_*" and also stripped every name matching KEY, which ANTHROPIC_API_KEY
// does. The carve-out has to be explicit, and it has to be off by default.
func TestBuildEnvAnthropicForwardingIsOptIn(t *testing.T) {
	off := buildEnv(daemonEnviron(), false)
	for _, name := range envNames(off) {
		if strings.HasPrefix(name, AnthropicEnvPrefix) {
			t.Fatalf("%s forwarded without opting in", name)
		}
	}

	on := buildEnv(daemonEnviron(), true)
	names := envNames(on)
	if !slices.Contains(names, "ANTHROPIC_API_KEY") {
		t.Errorf("ANTHROPIC_API_KEY not forwarded when opted in; the KEY strip is cancelling the carve-out")
	}
	if !slices.Contains(names, "ANTHROPIC_BASE_URL") {
		t.Errorf("ANTHROPIC_BASE_URL not forwarded when opted in")
	}
	// Opting into ANTHROPIC_* must not open the door to anything else.
	for _, name := range names {
		if strings.HasPrefix(name, AnthropicEnvPrefix) {
			continue
		}
		if IsSecretEnvName(name) {
			t.Errorf("opting into ANTHROPIC_* also leaked %q", name)
		}
	}
}

func TestBuildEnvAnthropicForwardingIsOrdered(t *testing.T) {
	parent := []string{"PATH=/bin", "ANTHROPIC_MODEL=x", "ANTHROPIC_API_KEY=y", "ANTHROPIC_BASE_URL=z"}
	first := buildEnv(parent, true)
	for range 20 {
		if got := buildEnv(parent, true); !slices.Equal(got, first) {
			t.Fatalf("environment is not deterministic across map iterations:\n got %q\nfirst %q", got, first)
		}
	}
}

func TestBuildEnvDropsEmptyValues(t *testing.T) {
	env := buildEnv([]string{"PATH=/bin", "HOME=", "USER=tester", "TMPDIR="}, false)
	want := []string{"PATH=/bin", "USER=tester"}
	if !slices.Equal(env, want) {
		t.Fatalf("empty values not dropped\n got %q\nwant %q", env, want)
	}
}

// A launchd-started daemon may have no PATH worth the name. An empty PATH would
// leave the child unable to find node or git at all, so the fallback is
// launchd's own PATH rather than nothing.
func TestBuildEnvFallsBackToLaunchdPATH(t *testing.T) {
	env := buildEnv([]string{"HOME=/Users/tester"}, false)
	if !slices.Contains(env, "PATH="+fallbackPATH) {
		t.Fatalf("PATH fallback missing from %q", env)
	}
}

func TestBuildEnvIgnoresMalformedEntries(t *testing.T) {
	env := buildEnv([]string{"PATH=/bin", "not-an-assignment", "=novalue", "USER=tester"}, false)
	want := []string{"PATH=/bin", "USER=tester"}
	if !slices.Equal(env, want) {
		t.Fatalf("malformed entries not ignored\n got %q\nwant %q", env, want)
	}
}

func TestIsSecretEnvName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"GITHUB_TOKEN", true},
		{"github_token", true},
		{"AWS_SECRET_ACCESS_KEY", true},
		{"DB_PASSWORD", true},
		{"MYSQL_PASSWD", true},
		{"SIGNING_KEY", true},
		{"SOME_CREDENTIAL", true},
		{"STRIPE_API_KEY", true},
		{"ANTHROPIC_API_KEY", true},
		{"PATH", false},
		{"HOME", false},
		{"USER", false},
		{"TERM", false},
		{"MONKEY", true}, // KEY is a substring; over-matching is the safe direction.
	}
	for _, tc := range tests {
		if got := IsSecretEnvName(tc.name); got != tc.want {
			t.Errorf("IsSecretEnvName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAllowedEnvNamesIsACopy(t *testing.T) {
	names := AllowedEnvNames()
	if len(names) == 0 {
		t.Fatal("allowlist is empty")
	}
	names[0] = "TAMPERED"
	if AllowedEnvNames()[0] == "TAMPERED" {
		t.Fatal("AllowedEnvNames exposes the package's own slice; a caller can rewrite the boundary")
	}
}
