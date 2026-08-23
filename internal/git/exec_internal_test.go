package git

import (
	"slices"
	"strings"
	"testing"
)

// TestChildEnvIsTheAllowlist pins SD-5. The daemon's own environment can hold
// the bot token under token_env, so the git child environment is built from
// scratch rather than filtered: anything not named here is absent because it was
// never added, not because a deny-list happened to catch it.
func TestChildEnvIsTheAllowlist(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/Users/tester")
	t.Setenv("SSH_AUTH_SOCK", "/private/tmp/agent.sock")

	// Everything below is present in the parent and must not reach git.
	t.Setenv("GITHUB_TOKEN", "ghp_notreal")
	t.Setenv("GH_TOKEN", "ghp_notreal")
	t.Setenv("LF_HOME", "/Users/tester/.litefactory")
	t.Setenv("ANTHROPIC_API_KEY", "sk-notreal")
	t.Setenv("GIT_ASKPASS", "/usr/bin/true")
	t.Setenv("SSH_ASKPASS", "/usr/bin/true")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

	want := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/Users/tester",
		"SSH_AUTH_SOCK=/private/tmp/agent.sock",
		"GIT_TERMINAL_PROMPT=0",
		`GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new`,
	}
	got := childEnv()

	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("git child environment is not the SD-5 allowlist.\n got: %q\nwant: %q", got, want)
	}
}

// TestChildEnvOmitsAskpassRatherThanEmptyingIt matters more than it looks. An
// empty GIT_ASKPASS is not the same as an unset one: git still consults it, and
// the point of the allowlist is that no prompt helper exists to be consulted.
func TestChildEnvOmitsAskpassRatherThanEmptyingIt(t *testing.T) {
	t.Setenv("GIT_ASKPASS", "/usr/bin/true")
	t.Setenv("SSH_ASKPASS", "/usr/bin/true")

	for _, entry := range childEnv() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "GIT_ASKPASS" || name == "SSH_ASKPASS" {
			t.Errorf("child environment defines %s as %q; it must be absent", name, entry)
		}
	}
}

// TestChildEnvFallsBackToLaunchdPATH covers the daemon's starting condition. A
// launchd-started process gets a minimal environment, and a git child with no
// PATH at all cannot find ssh — which would fail every push with a message
// about the transport rather than about the environment.
func TestChildEnvFallsBackToLaunchdPATH(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", "")
	t.Setenv("SSH_AUTH_SOCK", "")

	got := childEnv()
	if !slices.Contains(got, "PATH="+fallbackPATH) {
		t.Errorf("child environment %q does not fall back to the launchd PATH", got)
	}
	for _, entry := range got {
		if entry == "HOME=" || entry == "SSH_AUTH_SOCK=" {
			t.Errorf("child environment defines %q; an empty value must be omitted", entry)
		}
	}
}
