package git

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

// timeoutPolicy is the whole deadline policy, by constant name. AC-3.22 fixes
// the two that reach the network: 60 seconds for a fetch, 120 for a push.
//
// divergenceTimeout and mergeTimeout cover "git rev-list --left-right --count"
// (US-1's Divergence) and "git merge" (US-2's MergeBase and its abort-and-verify
// rollback path) respectively; both are local, working-tree operations, so they
// share their budget with the other local commands in this table rather than
// the network ones.
var timeoutPolicy = map[string]time.Duration{
	"readTimeout":       15 * time.Second,
	"fetchTimeout":      60 * time.Second,
	"pushTimeout":       120 * time.Second,
	"worktreeTimeout":   120 * time.Second,
	"commitTimeout":     120 * time.Second,
	"divergenceTimeout": 30 * time.Second,
	"mergeTimeout":      120 * time.Second,
	"showBlobTimeout":   15 * time.Second,
}

// TestTimeoutPolicyIsWhatTheDeadlinesSay pins the durations themselves.
func TestTimeoutPolicyIsWhatTheDeadlinesSay(t *testing.T) {
	t.Parallel()

	got := map[string]time.Duration{
		"readTimeout":       readTimeout,
		"fetchTimeout":      fetchTimeout,
		"pushTimeout":       pushTimeout,
		"worktreeTimeout":   worktreeTimeout,
		"commitTimeout":     commitTimeout,
		"divergenceTimeout": divergenceTimeout,
		"mergeTimeout":      mergeTimeout,
		"showBlobTimeout":   showBlobTimeout,
	}
	for name, want := range timeoutPolicy {
		if got[name] != want {
			t.Errorf("%s = %s, want %s", name, got[name], want)
		}
	}
}

// TestEveryGitCommandCarriesADeadline is the structural half, and it matters
// more than the values do.
//
// An untimed git call is not a slow call, it is a hung tick — and a hung daemon
// looks alive, which is a worse failure than a loud one. ssh in BatchMode
// refuses to prompt, but a black-holed host or a half-open connection still
// blocks forever. The only durable way to keep that true is to refuse the
// literal at the call site: every runner.run must pass one of the named policy
// constants, so a future command cannot quietly invent its own deadline or omit
// one.
func TestEveryGitCommandCarriesADeadline(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	//nolint:staticcheck // SA1019: ParseDir is deprecated in favour of
	// golang.org/x/tools/go/packages, which is a third-party dependency. This
	// module ships three, deliberately, and adding a fourth to the graph so an
	// invariant test can parse its own directory is a worse trade than using a
	// deprecated stdlib function. The deprecation is about build tags, and no
	// file in this package is behind one.
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse the package source: %v", err)
	}

	var offenders []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "run" || len(call.Args) < 2 {
					return true
				}
				ident, ok := call.Args[1].(*ast.Ident)
				if ok {
					if _, known := timeoutPolicy[ident.Name]; known {
						return true
					}
				}
				offenders = append(offenders, fset.Position(call.Pos()).String())
				return true
			})
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("git commands are issued without a policy deadline at:\n  %s\n\n"+
			"Every runner.run must pass one of %v. An untimed git call is a hung "+
			"tick, and a hung daemon looks alive.",
			strings.Join(offenders, "\n  "), slices.Sorted(maps.Keys(timeoutPolicy)))
	}
}

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
