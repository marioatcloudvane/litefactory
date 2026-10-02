package bugchecks

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// Rerun bounds keep an expanded argv inside the recipe's own argv limits.
const (
	maxRerunTests    = 256
	maxRerunPackages = 64
)

// ErrUnattributable means the output cannot name every failing package's
// failing tests; the command then fails exactly as it would without a rerun.
var ErrUnattributable = errors.New("go test failures cannot be attributed to named tests")

var (
	goTestName    = regexp.MustCompile(`^(Test|Fuzz|Example)[A-Za-z0-9_]*$`)
	goImportPath  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.~+-]*(/[A-Za-z0-9_][A-Za-z0-9_.~+-]*)*$`)
	goTestFailure = regexp.MustCompile(`^--- FAIL: (\S+) \(`)
)

// GoTestReport is what one go test run says about each package it reported.
// Failed maps a failing package to its failing top-level tests, sorted.
type GoTestReport struct {
	Failed map[string][]string
	Passed []string
}

// FailedTests counts the failing tests across all packages.
func (r GoTestReport) FailedTests() int {
	n := 0
	for _, tests := range r.Failed {
		n += len(tests)
	}
	return n
}

// Packages lists the failing packages in sorted order.
func (r GoTestReport) Packages() []string {
	out := make([]string, 0, len(r.Failed))
	for pkg := range r.Failed {
		out = append(out, pkg)
	}
	slices.Sort(out)
	return out
}

func unattributable(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnattributable, fmt.Sprintf(format, args...))
}

// ParseGoTestReport reads non-JSON go test output. Package output is printed
// as one block ending in its "ok" or "FAIL" summary line, so each top-level
// "--- FAIL:" line belongs to the next package summary. Subtest failures are
// indented and are covered by their top-level test. The output is untrusted:
// a name or import path outside a conservative identifier pattern, a build or
// setup failure, a timeout panic, a failing package without a named test or an
// incomplete capture makes attribution impossible.
func ParseGoTestReport(output string, complete bool) (GoTestReport, error) {
	r := GoTestReport{Failed: map[string][]string{}}
	if !complete {
		return r, unattributable("output capture incomplete")
	}
	var pending []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "panic: test timed out after") {
			return r, unattributable("a test binary timed out")
		}
		if m := goTestFailure.FindStringSubmatch(line); m != nil {
			name, _, _ := strings.Cut(m[1], "/")
			if !goTestName.MatchString(name) {
				return r, unattributable("unrecognised test name")
			}
			if !slices.Contains(pending, name) {
				pending = append(pending, name)
			}
			continue
		}
		status, rest, ok := strings.Cut(line, "\t")
		status = strings.TrimSpace(status)
		if !ok || (status != "ok" && status != "FAIL" && status != "?") {
			continue
		}
		pkg, detail, _ := strings.Cut(rest, "\t")
		if pkg, suffix, found := strings.Cut(pkg, " "); found {
			if status == "FAIL" && (suffix == "[build failed]" || suffix == "[setup failed]") {
				return r, unattributable("package %s %s", pkg, strings.Trim(suffix, "[]"))
			}
			return r, unattributable("unrecognised package summary")
		}
		if !importPath(pkg) {
			return r, unattributable("unrecognised package path")
		}
		switch status {
		case "FAIL":
			if len(pending) == 0 {
				return r, unattributable("package %s failed without a named test", pkg)
			}
			if _, dup := r.Failed[pkg]; dup {
				return r, unattributable("package %s reported twice", pkg)
			}
			slices.Sort(pending)
			r.Failed[pkg] = pending
		case "ok":
			if len(pending) != 0 {
				return r, unattributable("named failures before passing package %s", pkg)
			}
			// A rerun that matched nothing proves nothing about the named tests.
			if strings.Contains(detail, "[no tests to run]") {
				return r, unattributable("package %s ran no tests", pkg)
			}
			r.Passed = append(r.Passed, pkg)
		}
		pending = nil
	}
	if len(pending) != 0 {
		return r, unattributable("named failures without a package summary")
	}
	return r, nil
}

// ParseGoTestFailures attributes a failed go test run: every failing package
// must name its failing tests, and at least one package must have failed.
func ParseGoTestFailures(output string, complete bool) (GoTestReport, error) {
	r, err := ParseGoTestReport(output, complete)
	if err != nil {
		return r, err
	}
	if len(r.Failed) == 0 {
		return r, unattributable("no failing package reported")
	}
	if len(r.Failed) > maxRerunPackages || r.FailedTests() > maxRerunTests {
		return r, unattributable("too many failing tests to rerun")
	}
	return r, nil
}

func importPath(pkg string) bool {
	return len(pkg) <= 512 && goImportPath.MatchString(pkg) && !strings.Contains(pkg, "..")
}

// ExpandGoTestRerun replaces the whole-element placeholders of a rerun argv:
// {tests} becomes one anchored alternation of the failing test names and
// {packages} becomes one argument per failing package. Only names already
// accepted by ParseGoTestReport are expanded, so no output text reaches argv
// unchecked.
func ExpandGoTestRerun(argv []string, failed map[string][]string) ([]string, error) {
	var names, packages []string
	for pkg, tests := range failed {
		if !importPath(pkg) || len(tests) == 0 {
			return nil, unattributable("invalid rerun package")
		}
		packages = append(packages, pkg)
		for _, name := range tests {
			if !goTestName.MatchString(name) {
				return nil, unattributable("invalid rerun test name")
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if len(packages) == 0 || len(packages) > maxRerunPackages || len(names) > maxRerunTests {
		return nil, unattributable("rerun selection outside bounds")
	}
	slices.Sort(names)
	slices.Sort(packages)
	var out []string
	for _, v := range argv {
		switch v {
		case config.BugRerunTests:
			out = append(out, "^("+strings.Join(names, "|")+")$")
		case config.BugRerunPackages:
			out = append(out, packages...)
		default:
			out = append(out, v)
		}
	}
	return out, nil
}
