package bugchecks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/redact"
)

// runObserved keeps proof artifacts in a private invocation directory instead
// of the checked-out source. Every executable, including result extraction,
// still uses the configured sandbox and durable process callback.
func (p Isolation) runObserved(ctx context.Context, worktree string, c config.BugCheckCommand, started func(CommandResult) error) (out CommandResult) {
	dir, err := os.MkdirTemp("", "lf-suite-evidence-")
	if err != nil {
		return CommandResult{Outcome: "blocked", Reason: err.Error()}
	}
	defer func() {
		if !out.CleanupConfirmed && out.ProcessID != 0 {
			return
		}
		if e := os.RemoveAll(dir); e != nil {
			out.CleanupConfirmed = false
			out.CaptureComplete = false
			out.Reason = "evidence cleanup failed"
		}
	}()
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return CommandResult{Outcome: "blocked", Reason: err.Error()}
	}
	p.WritableRoots = append(slices.Clone(p.WritableRoots), dir)
	c.Argv = slices.Clone(c.Argv)
	format := c.Observe
	c.Observe = ""
	file := filepath.Join(dir, "results.trx")
	switch format {
	case "make-go":
		executable, _, _, e := p.resolve(worktree, config.BugCheckCommand{Argv: []string{"go"}, CWD: c.CWD})
		if e != nil {
			return CommandResult{Outcome: "blocked", Reason: e.Error()}
		}
		shim := filepath.Join(dir, "go-observer")
		source := `#!/bin/sh
real_go='` + strings.ReplaceAll(executable, "'", "'\"'\"'") + `'
if [ "$1" != "test" ]; then exec "$real_go" "$@"; fi
shift
race=no
selected=no
for arg in "$@"; do
 case "$arg" in
  -race|-race=true) race=yes ;;
  -run|-run=*) selected=yes ;;
 esac
done
group=plain
if [ "$race" = yes ]; then
 group=full-race
 if [ "$selected" = yes ]; then group=selected-race; fi
fi
printf '{"lf_group":"%s"}\n' "$group"
count=no
case " $GOFLAGS " in *" -count="*|*" -count "*) count=yes ;; esac
for arg in "$@"; do
 case "$arg" in -args|--args|--) break ;; -count|-count=*) count=yes ;; esac
done
if [ "$count" = yes ]; then exec "$real_go" test -json "$@"; fi
exec "$real_go" test -json -count=1 "$@"
`
		// #nosec G306 -- private factory-owned executable observer, required by make GO override.
		if e = os.WriteFile(shim, []byte(source), 0700); e != nil {
			return CommandResult{Outcome: "blocked", Reason: e.Error()}
		}
		c.Argv = append(c.Argv, "GO="+shim)
		return p.Run(ctx, worktree, c, started)
	case coverage.RunnerGo:
		// Go JSON stays on the bounded stdout stream.
		return p.Run(ctx, worktree, c, started)
	case coverage.RunnerPytest:
		module := filepath.Join(dir, "lf_coverage_observer.py")
		if err = os.WriteFile(module, []byte(coverage.PytestObserver), 0600); err != nil {
			return CommandResult{Outcome: "blocked", Reason: err.Error()}
		}
		file = filepath.Join(dir, "pytest.jsonl")
		// The observer path is factory data and never an environment reference
		// resolved from a repository command.
		pythonPath := dir
		if slices.Contains(c.Env, "PYTHONPATH") && p.Environment["PYTHONPATH"] != "" {
			pythonPath += string(os.PathListSeparator) + p.Environment["PYTHONPATH"]
		}
		p.invocationEnv = append(slices.Clone(p.invocationEnv), "LF_PYTEST_OBSERVATIONS="+file, "PYTHONPATH="+pythonPath, "PYTHONDONTWRITEBYTECODE=1")
		c.Argv = append(c.Argv, "-p", "lf_coverage_observer")
	case coverage.RunnerVSTest:
		c.Argv = runnerArgs(c.Argv, []string{"--logger", "trx;LogFileName=results.trx", "--results-directory", dir})
	case coverage.RunnerMTP:
		c.Argv = append(c.Argv, "--report-trx", "--report-trx-filename", "results.trx", "--results-directory", dir)
	case coverage.RunnerXcode:
		file = filepath.Join(dir, "results.xcresult")
		if containsArgPrefix(c.Argv, "-resultBundlePath") {
			return CommandResult{Outcome: "blocked", Reason: "configured result bundle cannot be replaced"}
		}
		c.Argv = append(c.Argv, "-resultBundlePath", file)
	default:
		return CommandResult{Outcome: "blocked", Reason: "unknown factory result observer"}
	}
	out = p.Run(ctx, worktree, c, started)
	if !out.CleanupConfirmed || !out.CaptureComplete {
		return out
	}
	if format == coverage.RunnerXcode {
		extract := config.BugCheckCommand{Argv: []string{"xcrun", "xcresulttool", "get", "test-results", "tests", "--path", file}, CWD: c.CWD, Env: slices.Clone(c.Env), TimeoutSeconds: c.TimeoutSeconds}
		result := p.Run(ctx, worktree, extract, started)
		out.CleanupConfirmed = out.CleanupConfirmed && result.CleanupConfirmed
		if !result.CleanupConfirmed {
			out.ProcessID = result.ProcessID
			out.ProcessIdentity = result.ProcessIdentity
		}
		if result.Outcome != "passed" || !result.CaptureComplete || !result.CleanupConfirmed {
			out.CaptureComplete = false
			out.Reason = "xcresult extraction incomplete"
			return out
		}
		out.Structured = result.Output
		return out
	}
	raw, err := readObserved(dir, file)
	if err != nil {
		out.CaptureComplete = false
		out.Reason = fmt.Sprintf("structured test capture unavailable: %v", err)
		return out
	}
	// Declared environment values must receive the same hygiene as display logs.
	raw = redactDeclaredEnv(raw, c.Env, p.Environment)
	body, re := redact.New(redact.Options{Environ: []string{}}).Apply(context.WithoutCancel(ctx), raw, OutputLimit)
	if re != nil || body.Truncated() {
		out.CaptureComplete = false
		out.Reason = "structured report redaction incomplete"
		return out
	}
	out.Structured = body.String()
	return out
}

func containsArgPrefix(argv []string, prefix string) bool {
	for _, arg := range argv {
		if arg == prefix || strings.HasPrefix(arg, prefix+"=") {
			return true
		}
	}
	return false
}

func readObserved(root, file string) (string, error) {
	path, err := contained(root, file)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(file)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > OutputLimit {
		return "", errors.New("invalid or oversized structured report")
	}
	// #nosec G304 -- contained resolves the report within a private invocation directory.
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, OutputLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > OutputLimit {
		return "", errors.New("structured report capture exceeded bound")
	}
	return string(data), nil
}

// runnerArgs inserts factory reporter options before a VSTest settings boundary.
func runnerArgs(argv, args []string) []string {
	at := len(argv)
	if i := slices.Index(argv, "--"); i >= 0 {
		at = i
	}
	out := slices.Clone(argv[:at])
	out = append(out, args...)
	return append(out, argv[at:]...)
}
