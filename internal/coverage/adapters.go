package coverage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// Runner identifiers and result limits are the closed adapter protocol.
const (
	RunnerGo        = "go"
	RunnerPytest    = "pytest"
	RunnerVSTest    = "vstest"
	RunnerMTP       = "mtp"
	RunnerXcode     = "xcodebuild"
	MaxResultBytes  = 4 << 20
	MaxObservations = 100000
)

// Facts are retained executable/schema probes supplied by the sandbox host.
// Neither runner names nor agent-authored metadata establish capabilities.
type Facts struct {
	Runner         string
	Version        string
	Structured     bool
	Dependencies   bool
	Fresh          bool
	Framework      string
	FilterFlag     string
	FilterProperty string
	// A wrapper is recognized only by the digest of its entire retained argv.
	// Its original invocation remains opaque: it is never rewritten as shell text.
	WrapperDigest string
	MakeGroups    map[string]config.BugCheckCommand
	GoFlags       []string
}

// Capability records what actual runner probes establish.
type Capability struct {
	Runner    string
	Selection bool
	Fresh     bool
	Reason    string
}

// Recognize checks the invocation shape and retained runner capabilities.
func Recognize(command config.BugCheckCommand, facts Facts) Capability {
	capability := Capability{Runner: facts.Runner, Fresh: facts.Fresh}
	if runner, ok := KnownWrapper(command); ok {
		capability.Runner = runner
		capability.Reason = "recognized shipped wrapper requires unchanged full execution"
		return capability
	}
	if len(command.Argv) == 0 || facts.Version == "" {
		capability.Reason = "runner version was not probed"
		return capability
	}
	if facts.Runner == RunnerGo {
		if err := boundedGoFlags(strings.Join(facts.GoFlags, " ")); err != nil {
			capability.Reason = err.Error()
			return capability
		}
	}
	argv := command.Argv
	var direct bool
	switch facts.Runner {
	case RunnerGo:
		direct = len(argv) > 1 && filepath.Base(argv[0]) == "go" && argv[1] == "test"
	case RunnerPytest:
		direct = filepath.Base(argv[0]) == "pytest" || len(argv) > 2 && strings.HasPrefix(filepath.Base(argv[0]), "python") && argv[1] == "-m" && argv[2] == "pytest"
	case RunnerVSTest, RunnerMTP:
		direct = len(argv) > 1 && filepath.Base(argv[0]) == "dotnet" && argv[1] == "test"
	case RunnerXcode:
		direct = filepath.Base(argv[0]) == "xcodebuild" && slices.Contains(argv, "test")
	default:
		capability.Reason = "unsupported runner"
		return capability
	}
	if !direct && len(facts.MakeGroups) > 0 && filepath.Base(argv[0]) == "make" && len(argv) == 2 && argv[1] == "test" {
		capability.Selection = facts.Structured && facts.Dependencies
		return capability
	}
	if !direct {
		if facts.WrapperDigest != "" && facts.WrapperDigest == Digest(argv) {
			capability.Reason = "recognized wrapper requires unchanged full execution"
		} else {
			capability.Reason = "unknown invocation or wrapper"
		}
		return capability
	}
	if !facts.Structured || !facts.Dependencies {
		capability.Reason = "complete result and dependency capabilities were not proved"
		return capability
	}
	if facts.Runner == RunnerMTP && (facts.FilterFlag == "" || facts.FilterProperty == "") {
		capability.Reason = "framework-specific MTP selection was not probed"
		return capability
	}
	capability.Selection = true
	return capability
}

// Select changes only the test selection and invocation-local result-cache policy.
// It returns an independent command; callers retain the original for full fallback.
func Select(command config.BugCheckCommand, unit Unit, cases []string, facts Facts) (config.BugCheckCommand, error) {
	if group, ok := facts.MakeGroups[unit.Key]; ok {
		direct := facts
		direct.MakeGroups = nil
		direct.WrapperDigest = ""
		return Select(group, unit, cases, direct)
	}
	command = inheritGoCount(command, facts)
	capability := Recognize(command, facts)
	if !capability.Selection || !capability.Fresh {
		return config.BugCheckCommand{}, fmt.Errorf("selection unavailable: %s", capability.Reason)
	}
	if unit.Runner != facts.Runner || unit.Selector == "" {
		return config.BugCheckCommand{}, errors.New("unit runner or selector is missing")
	}
	for _, s := range append(slices.Clone(cases), unit.Selector) {
		if s == "" || len(s) > 8192 || strings.ContainsAny(s, "\x00\n\r") || strings.HasPrefix(s, "-") {
			return config.BugCheckCommand{}, errors.New("invalid test identity")
		}
	}
	out := command
	out.Observe = facts.Runner
	out.Argv = slices.Clone(command.Argv)
	out.Env = slices.Clone(command.Env)
	if out.Rerun != nil {
		r := *out.Rerun
		r.Argv = slices.Clone(r.Argv)
		out.Rerun = &r
	}
	var err error
	switch facts.Runner {
	case RunnerGo:
		out.Argv, err = selectGo(out.Argv, unit.Selector, cases)
	case RunnerPytest:
		if containsOption(out.Argv, "--lf", "--last-failed", "--ff", "--failed-first", "--stepwise", "--sw") {
			return config.BugCheckCommand{}, errors.New("pytest cache-based selection requires full execution")
		}
		// Pytest intersects node arguments with original -k/-m expressions. Existing
		// positional roots must be removed to avoid also collecting their full tree.
		out.Argv, err = selectPytest(out.Argv, unit.Selector, cases)
	case RunnerVSTest, RunnerMTP:
		property, flag := "FullyQualifiedName", "--filter"
		if facts.Runner == RunnerMTP {
			property, flag = facts.FilterProperty, facts.FilterFlag
		}
		if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]*$`).MatchString(property) || !regexp.MustCompile(`^--[a-z][a-z-]*$`).MatchString(flag) {
			return config.BugCheckCommand{}, errors.New("unsupported filter capability")
		}
		// Unknown parameter encodings broaden to their containing method/project.
		// Results must still reconcile every requested parameter identity.
		out.Argv, err = selectDotnetProject(out.Argv, unit.Selector, facts.Runner)
		if err != nil {
			return config.BugCheckCommand{}, err
		}
		if len(cases) > 0 {
			filters := make([]string, 0, len(cases))
			for _, id := range cases {
				method := strings.SplitN(strings.SplitN(id, "::", 2)[0], "(", 2)[0]
				if strings.ContainsAny(method, "&|!=~\\\"'()") {
					return config.BugCheckCommand{}, errors.New("case requires containing-project retry")
				}
				filters = append(filters, property+"="+method)
			}
			filter := "(" + strings.Join(filters, "|") + ")"
			out.Argv, err = intersectFilter(out.Argv, flag, filter)
		}
	case RunnerXcode:
		if !containsOption(out.Argv, "-scheme") || !containsOption(out.Argv, "-destination") {
			return config.BugCheckCommand{}, errors.New("xcode scheme and destination must remain explicit")
		}
		if containsOption(out.Argv, "-only-testing", "-skip-testing") {
			return config.BugCheckCommand{}, errors.New("xcode existing test filters require full execution")
		}
		selected := cases
		if len(selected) == 0 {
			selected = []string{unit.Selector}
		}
		for _, id := range selected {
			if !strings.HasPrefix(id, unit.Selector+"/") && id != unit.Selector {
				return config.BugCheckCommand{}, errors.New("xcode identity is not target-qualified")
			}
			// Parameterized Swift cases are not uniformly addressable by installed
			// Xcode versions. Run their containing target and reconcile the exact case.
			if strings.Contains(id, "(") && !strings.HasSuffix(id, "()") {
				id = unit.Selector
			}
			out.Argv = append(out.Argv, "-only-testing:"+id)
		}
	}
	if err != nil {
		return config.BugCheckCommand{}, err
	}
	if len(out.Argv) > 128 {
		return config.BugCheckCommand{}, errors.New("selection exceeds command argument limit")
	}
	return out, nil
}

func containsOption(argv []string, options ...string) bool {
	for _, a := range argv {
		for _, o := range options {
			if a == o || strings.HasPrefix(a, o+"=") || strings.HasPrefix(a, o+":") {
				return true
			}
		}
	}
	return false
}

func selectGo(argv []string, selector string, cases []string) ([]string, error) {
	if len(cases) > 0 {
		for _, c := range cases {
			if strings.HasPrefix(c, "$package:") {
				cases = nil
				break
			}
		}
	}
	out := slices.Clone(argv[:2])
	originalRun := ""
	countSeen := false
	valued := map[string]bool{"-run": true, "-count": true, "-timeout": true, "-parallel": true, "-cpu": true, "-shuffle": true, "-tags": true, "-mod": true, "-modfile": true, "-covermode": true, "-coverpkg": true, "-coverprofile": true, "-vet": true, "-gcflags": true, "-ldflags": true, "-asmflags": true, "-p": true, "-pkgdir": true, "-exec": true, "-overlay": true, "-list": true, "-benchtime": true, "-bench": true, "-fuzz": true, "-fuzztime": true, "-fuzzminimizetime": true}
	boolean := map[string]bool{"-race": true, "-trimpath": true, "-v": true, "-json": true, "-short": true, "-failfast": true, "-cover": true, "-work": true, "-x": true, "-n": true, "-a": true, "-asan": true, "-msan": true, "-buildvcs": true}
	for i := 2; i < len(argv); i++ {
		a := argv[i]
		if !strings.HasPrefix(a, "-") {
			continue
		}
		flag, value, equals := strings.Cut(a, "=")
		if flag == "-args" || flag == "-exec" || flag == "-overlay" || flag == "-list" || flag == "-bench" || flag == "-fuzz" {
			return nil, errors.New("go invocation has unsupported execution semantics")
		}
		if !valued[flag] && !boolean[flag] {
			return nil, fmt.Errorf("unsupported Go option %s", flag)
		}
		if valued[flag] && !equals {
			i++
			if i >= len(argv) {
				return nil, errors.New("missing Go option value")
			}
			value = argv[i]
		}
		if flag == "-run" {
			originalRun = value
		}
		if flag == "-count" {
			n, e := strconv.Atoi(value)
			if e != nil || n <= 0 {
				return nil, errors.New("go repeat count cannot prove test execution")
			}
			countSeen = true
		}
		if flag == "-run" && len(cases) > 0 || flag == "-json" {
			continue
		}
		out = append(out, a)
		if valued[flag] && !equals {
			out = append(out, value)
		}
	}
	if len(cases) > 0 {
		for _, c := range cases {
			if !matchesGoRun(originalRun, c) {
				return nil, errors.New("retry case is outside original Go filter")
			}
		}
		out = append(out, "-run", goCasePattern(cases))
	}
	if !countSeen {
		out = append(out, "-count=1")
	}
	out = append(out, "-json", selector)
	return out, nil
}
func matchesGoRun(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	ps, ns := strings.Split(pattern, "/"), strings.Split(name, "/")
	for i, p := range ps {
		if i >= len(ns) {
			return false
		}
		re, e := regexp.Compile(p)
		if e != nil || !re.MatchString(ns[i]) {
			return false
		}
	}
	return true
}
func goCasePattern(cases []string) string {
	segments := [][]string{}
	for _, c := range cases {
		for i, s := range strings.Split(c, "/") {
			for len(segments) <= i {
				segments = append(segments, nil)
			}
			segments[i] = append(segments[i], regexp.QuoteMeta(s))
		}
	}
	result := make([]string, len(segments))
	for i, s := range segments {
		sort.Strings(s)
		result[i] = "^(?:" + strings.Join(slices.Compact(s), "|") + ")$"
	}
	return strings.Join(result, "/")
}
func selectPytest(argv []string, unit string, cases []string) ([]string, error) {
	start := 1
	if len(argv) > 2 && argv[1] == "-m" {
		start = 3
	}
	out := slices.Clone(argv[:start])
	valued := map[string]bool{"-k": true, "-m": true, "-c": true, "-o": true, "--override-ini": true, "--rootdir": true, "--confcutdir": true, "--basetemp": true, "--import-mode": true, "--tb": true, "--maxfail": true, "--junitxml": true, "--junit-xml": true, "-p": true, "--capture": true, "--deselect": true, "--ignore": true, "--ignore-glob": true, "--log-level": true, "--log-cli-level": true, "--color": true}
	boolean := map[string]bool{"-q": true, "-v": true, "-vv": true, "-s": true, "-x": true, "--strict-markers": true, "--strict-config": true, "--disable-warnings": true, "--no-header": true, "--no-summary": true, "--showlocals": true, "--cache-clear": true, "--continue-on-collection-errors": true}
	for i := start; i < len(argv); i++ {
		a := argv[i]
		if !strings.HasPrefix(a, "-") {
			continue
		}
		flag, _, eq := strings.Cut(a, "=")
		if !valued[flag] && !boolean[flag] {
			return nil, fmt.Errorf("unsupported pytest option %s", flag)
		}
		out = append(out, a)
		if valued[flag] && !eq {
			i++
			if i >= len(argv) {
				return nil, errors.New("missing pytest option value")
			}
			out = append(out, argv[i])
		}
	}
	if len(cases) == 0 {
		return append(out, unit), nil
	}
	for _, id := range cases {
		if !strings.HasPrefix(id, unit+"::") && id != unit {
			return nil, errors.New("pytest node is outside unit")
		}
		out = append(out, id)
	}
	return out, nil
}
func selectDotnetProject(argv []string, selector, runner string) ([]string, error) {
	out := slices.Clone(argv)
	replaced := false
	for i := 2; i < len(out); i++ {
		if out[i] == "--project" {
			if i+1 >= len(out) {
				return nil, errors.New("missing project path")
			}
			out[i+1] = selector
			replaced = true
			i++
			continue
		}
		if strings.HasSuffix(out[i], ".csproj") || strings.HasSuffix(out[i], ".sln") || strings.HasSuffix(out[i], ".slnx") {
			if replaced {
				return nil, errors.New("ambiguous project selection")
			}
			out[i] = selector
			replaced = true
		}
	}
	if !replaced {
		if runner == RunnerMTP {
			out = append(out, "--project", selector)
		} else {
			out = append(out, selector)
		}
	}
	return out, nil
}

func intersectFilter(argv []string, flag, filter string) ([]string, error) {
	out := []string{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == flag {
			i++
			if i >= len(argv) {
				return nil, errors.New("missing original filter")
			}
			filter = "(" + argv[i] + ")&" + filter
			continue
		}
		if strings.HasPrefix(a, flag+"=") {
			filter = "(" + strings.TrimPrefix(a, flag+"=") + ")&" + filter
			continue
		}
		if a == "--" {
			out = append(out, flag, filter)
			return append(out, argv[i:]...), nil
		}
		out = append(out, a)
	}
	return append(out, flag, filter), nil
}

// ParseResults never treats process success or a zero-match selection as proof.
// Complete is true only for a recognized, bounded, terminated result schema.
func ParseResults(runner, unit string, data []byte) ([]Observation, bool, error) {
	if len(data) == 0 || len(data) > MaxResultBytes {
		return nil, false, errors.New("result capture is empty or exceeds bounds")
	}
	switch runner {
	case RunnerGo:
		return parseGo(unit, data)
	case RunnerPytest:
		return parsePytest(unit, data)
	case RunnerVSTest, RunnerMTP:
		return parseTRX(unit, data)
	case RunnerXcode:
		return parseXcode(unit, data)
	default:
		return nil, false, errors.New("unsupported result parser")
	}
}

func parseGo(unit string, data []byte) ([]Observation, bool, error) {
	type event struct {
		Action, Package, Test, Output string
		Group                         string `json:"lf_group"`
	}
	expected := ""
	expectedGroup := ""
	for _, g := range []string{"plain", "full-race", "selected-race"} {
		if strings.Contains(unit, ":"+g+":") {
			expectedGroup = g
		}
	}
	currentGroup := ""
	if _, p, ok := strings.Cut(unit, ":go:"); ok {
		expected = p
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	observations := []Observation{}
	outputs := map[string]string{}
	completed := map[string]string{}
	started := map[string]bool{}
	terminal := map[string]bool{}
	running := map[string]bool{}
	packageOf := map[string]string{}
	for {
		var e event
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return observations, false, err
		}
		if e.Group != "" {
			if e.Group != "plain" && e.Group != "full-race" && e.Group != "selected-race" {
				return observations, false, errors.New("unknown make test group")
			}
			currentGroup = e.Group
			continue
		}
		if expectedGroup != "" && currentGroup != expectedGroup {
			continue
		}
		if e.Package == "" {
			return observations, false, errors.New("go event lacks package identity")
		}
		if expected != "" && e.Package != expected {
			continue
		}
		if len(outputs) > MaxObservations || len(observations) >= MaxObservations {
			return observations, false, errors.New("go observation bound exceeded")
		}
		key := e.Package + "\x00" + e.Test
		if e.Action == "start" {
			started[e.Package] = true
		}
		if e.Action == "run" && e.Test != "" {
			if terminal[key] {
				delete(terminal, key)
				outputs[key] = ""
			}
			running[key] = true
		}
		if e.Action == "output" {
			outputs[key] += e.Output
		}
		if e.Action == "pass" || e.Action == "fail" || e.Action == "skip" {
			if terminal[key] {
				return observations, false, errors.New("duplicate Go terminal identity")
			}
			terminal[key] = true
			delete(running, key)
			if e.Test == "" {
				completed[e.Package] = e.Action
				continue
			}
			packageOf[e.Test] = e.Package
			observations = append(observations, Observation{Unit: unit, Case: e.Test, Status: e.Action, Diagnostic: outputs[key], Complete: true})
		}
	}
	byCase := map[string]int{}
	aggregated := []Observation{}
	for _, o := range observations {
		if position, ok := byCase[o.Case]; ok {
			previous := &aggregated[position]
			if o.Status == "fail" {
				if previous.Status != "fail" {
					previous.Diagnostic = o.Diagnostic
				} else if previous.Diagnostic != o.Diagnostic {
					previous.Diagnostic += "\n" + o.Diagnostic
				}
				previous.Status = "fail"
			} else if o.Status == "skip" && previous.Status != "fail" {
				previous.Status = "skip"
			}
		} else {
			byCase[o.Case] = len(aggregated)
			aggregated = append(aggregated, o)
		}
	}
	observations = aggregated
	filtered := []Observation{}
	for _, o := range observations {
		aggregate := false
		if o.Status == "fail" && !goOwnDiagnostic(o.Diagnostic) {
			for _, child := range observations {
				if child.Status == "fail" && strings.HasPrefix(child.Case, o.Case+"/") {
					aggregate = true
					break
				}
			}
		}
		if !aggregate {
			filtered = append(filtered, o)
		}
	}
	for pkg, status := range completed {
		failed := false
		for _, o := range observations {
			if packageOf[o.Case] == pkg && o.Status == "fail" {
				failed = true
				break
			}
		}
		if (status == "fail" && !failed) || (status == "pass" && len(observations) == 0 && strings.Contains(unit, ":compile:go:")) {
			filtered = append(filtered, Observation{Unit: unit, Case: "$package:" + pkg, Status: status, Diagnostic: outputs[pkg+"\x00"], Complete: true})
		}
	}
	complete := len(completed) > 0 && len(filtered) > 0 && len(running) == 0
	for pkg := range started {
		if completed[pkg] == "" {
			complete = false
		}
	}
	return filtered, complete, nil
}
func goOwnDiagnostic(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "=== RUN") && !strings.HasPrefix(line, "=== PAUSE") && !strings.HasPrefix(line, "=== CONT") && !strings.HasPrefix(line, "=== NAME") && !strings.HasPrefix(line, "--- FAIL:") && !strings.HasPrefix(line, "--- PASS:") && !strings.HasPrefix(line, "--- SKIP:") {
			return true
		}
	}
	return false
}

// PytestObserver is loaded only by the sandbox invocation as lf_coverage_observer.
// The final session event is mandatory; collection, setup and teardown failures
// therefore cannot be mistaken for a passing selected call.
const PytestObserver = `import json
import os

_stream = None

def _emit(event):
    global _stream
    if _stream is None:
        _stream = open(os.environ["LF_PYTEST_OBSERVATIONS"], "x", encoding="utf-8")
    _stream.write(json.dumps(event, ensure_ascii=True) + "\n")
    _stream.flush()

def pytest_collection_finish(session):
    _emit({"event": "collection", "nodes": [item.nodeid for item in session.items]})

def pytest_runtest_logreport(report):
    _emit({"event": "case", "nodeid": report.nodeid, "phase": report.when, "status": report.outcome, "diagnostic": str(report.longrepr) if report.failed else ""})

def pytest_collectreport(report):
    if report.failed:
        _emit({"event": "collection-error", "nodeid": report.nodeid, "diagnostic": str(report.longrepr)})

def pytest_sessionfinish(session, exitstatus):
    _emit({"event": "finish", "exitstatus": int(exitstatus)})
    _stream.close()
`

func parsePytest(unit string, data []byte) ([]Observation, bool, error) {
	type event struct {
		Event      string   `json:"event"`
		Node       string   `json:"nodeid"`
		Phase      string   `json:"phase"`
		Status     string   `json:"status"`
		Diagnostic string   `json:"diagnostic"`
		Nodes      []string `json:"nodes"`
		Exit       int      `json:"exitstatus"`
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), MaxResultBytes)
	obs := []Observation{}
	collected := map[string]bool{}
	terminals := map[string]bool{}
	finish, collection, broken := false, false, false
	for scanner.Scan() {
		var e event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return obs, false, err
		}
		if finish {
			return obs, false, errors.New("pytest events follow session finish")
		}
		switch e.Event {
		case "collection":
			if collection {
				return obs, false, errors.New("duplicate pytest collection")
			}
			collection = true
			for _, n := range e.Nodes {
				if n == "" || collected[n] {
					return obs, false, errors.New("invalid collected node identity")
				}
				collected[n] = true
			}
		case "case":
			status := map[string]string{"passed": "pass", "failed": "fail", "skipped": "skip"}[e.Status]
			if e.Node == "" || status == "" || !slices.Contains([]string{"setup", "call", "teardown"}, e.Phase) {
				return obs, false, errors.New("invalid pytest phase report")
			}
			if e.Phase == "teardown" {
				terminals[e.Node] = true
			}
			obs = append(obs, Observation{Unit: unit, Case: e.Node, Phase: e.Phase, Status: status, Diagnostic: e.Diagnostic, Complete: true})
		case "collection-error":
			broken = true
			obs = append(obs, Observation{Unit: unit, Case: e.Node, Phase: "collection", Status: "fail", Diagnostic: e.Diagnostic, Complete: true})
		case "finish":
			finish = true
			if e.Exit != 0 && e.Exit != 1 {
				broken = true
			}
		default:
			return obs, false, errors.New("unknown pytest observer event")
		}
		if len(obs) > MaxObservations || len(collected) > MaxObservations {
			return obs, false, errors.New("pytest observation bound exceeded")
		}
	}
	if err := scanner.Err(); err != nil {
		return obs, false, err
	}
	complete := finish && collection && !broken && len(collected) > 0
	for n := range collected {
		if !terminals[n] {
			complete = false
		}
	}
	return summarizePytest(obs, collected), complete, nil
}

func summarizePytest(phases []Observation, collected map[string]bool) []Observation {
	ids := make([]string, 0, len(collected))
	for id := range collected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]Observation, 0, len(ids))
	for _, id := range ids {
		o := Observation{Case: id, Status: "skip"}
		call, teardown := false, false
		for _, p := range phases {
			if p.Case != id {
				continue
			}
			o.Unit = p.Unit
			if p.Phase == "call" && p.Status == "pass" {
				call = true
			}
			if p.Phase == "teardown" {
				teardown = true
			}
			if p.Status == "fail" {
				o.Status = "fail"
				o.Diagnostic += p.Phase + ": " + p.Diagnostic + "\n"
				o.Phase = p.Phase
			}
		}
		o.Complete = teardown
		if o.Status != "fail" && call && teardown {
			o.Status = "pass"
		}
		result = append(result, o)
	}
	return result
}

type trxRun struct {
	XMLName xml.Name `xml:"TestRun"`
	Results []struct {
		TestID      string `xml:"testId,attr"`
		ExecutionID string `xml:"executionId,attr"`
		Name        string `xml:"testName,attr"`
		Outcome     string `xml:"outcome,attr"`
		Output      struct {
			Error struct {
				Message string `xml:"Message"`
				Stack   string `xml:"StackTrace"`
			} `xml:"ErrorInfo"`
		} `xml:"Output"`
	} `xml:"Results>UnitTestResult"`
	Definitions []struct {
		ID     string `xml:"id,attr"`
		Method struct {
			Class string `xml:"className,attr"`
			Name  string `xml:"name,attr"`
		} `xml:"TestMethod"`
	} `xml:"TestDefinitions>UnitTest"`
	Summary struct {
		Outcome  string `xml:"outcome,attr"`
		Counters *struct {
			Total    int `xml:"total,attr"`
			Executed int `xml:"executed,attr"`
		} `xml:"Counters"`
	} `xml:"ResultSummary"`
}

func parseTRX(unit string, data []byte) ([]Observation, bool, error) {
	var run trxRun
	dec := xml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&run); err != nil {
		return nil, false, err
	}
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, err
		}
	}
	if len(run.Results) > MaxObservations {
		return nil, false, errors.New("TRX observation bound exceeded")
	}
	definitions := map[string]string{}
	for _, d := range run.Definitions {
		if d.ID == "" || definitions[d.ID] != "" {
			return nil, false, errors.New("ambiguous TRX test definition")
		}
		definitions[d.ID] = strings.TrimSuffix(d.Method.Class, ",") + "." + d.Method.Name
	}
	obs := []Observation{}
	seen := map[string]bool{}
	for _, r := range run.Results {
		status := map[string]string{"Passed": "pass", "Failed": "fail", "NotExecuted": "skip", "Inconclusive": "skip"}[r.Outcome]
		// The report's case display name preserves framework parameter identity;
		// the qualified method distinguishes equal names in different classes.
		method := definitions[r.TestID]
		if method == "" || method == "." || r.Name == "" || status == "" {
			return obs, false, errors.New("TRX case identity or outcome is unavailable")
		}
		id := method + "::" + r.Name
		if seen[id] {
			return obs, false, errors.New("ambiguous TRX parameter identity")
		}
		seen[id] = true
		obs = append(obs, Observation{Unit: unit, Case: id, Status: status, Diagnostic: r.Output.Error.Message + "\n" + r.Output.Error.Stack, Complete: true})
	}
	complete := len(obs) > 0 && run.Summary.Counters != nil && run.Summary.Counters.Total == len(obs) && run.Summary.Outcome != "Aborted" && run.Summary.Outcome != "Error"
	return obs, complete, nil
}

func parseXcode(unit string, data []byte) ([]Observation, bool, error) {
	// xcresulttool schemas vary. This is the probed get test-results tests
	// schema; older typed-object and unknown schemas require broader execution.
	var report struct {
		Devices   []json.RawMessage `json:"devices"`
		TestNodes []xcodeNode       `json:"testNodes"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, false, err
	}
	if report.TestNodes == nil {
		return nil, false, errors.New("unsupported xcresult test tree schema")
	}
	obs := []Observation{}
	seen := map[string]bool{}
	incomplete := false
	var visit func([]xcodeNode, int) error
	visit = func(nodes []xcodeNode, depth int) error {
		if depth > 64 {
			return errors.New("xcresult tree depth exceeded")
		}
		for _, n := range nodes {
			if len(obs) >= MaxObservations {
				return errors.New("xcresult observation bound exceeded")
			}
			if len(n.Children) > 0 {
				if err := visit(n.Children, depth+1); err != nil {
					return err
				}
				continue
			}
			if n.NodeType != "Test Case" && n.NodeType != "Test Case Run" {
				continue
			}
			status := map[string]string{"Passed": "pass", "Failed": "fail", "Skipped": "skip", "Expected Failure": "skip"}[n.Result]
			if n.Identifier == "" || status == "" || seen[n.Identifier] {
				incomplete = true
				continue
			}
			seen[n.Identifier] = true
			obs = append(obs, Observation{Unit: unit, Case: n.Identifier, Status: status, Diagnostic: n.Details, Complete: true})
		}
		return nil
	}
	if err := visit(report.TestNodes, 0); err != nil {
		return obs, false, err
	}
	return obs, len(obs) > 0 && !incomplete, nil
}

type xcodeNode struct {
	NodeType   string      `json:"nodeType"`
	Identifier string      `json:"nodeIdentifier"`
	Result     string      `json:"result"`
	Details    string      `json:"details"`
	Children   []xcodeNode `json:"children"`
}

// MissingCases includes skipped and absent identities. Deletion is not repair.
func MissingCases(requested []string, observations []Observation) []string {
	observed := map[string]bool{}
	for _, o := range observations {
		if o.Complete && (o.Status == "pass" || o.Status == "fail") {
			observed[o.Case] = true
		}
	}
	missing := []string{}
	for _, id := range requested {
		if !observed[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return slices.Compact(missing)
}

// FullCommand preserves the full configured selection. Cache and reporting
// controls apply to this invocation only; shared build caches remain intact.
func FullCommand(command config.BugCheckCommand, facts Facts) (config.BugCheckCommand, error) {
	command = inheritGoCount(command, facts)
	out := command
	out.Observe = facts.Runner
	out.Argv = slices.Clone(command.Argv)
	out.Env = slices.Clone(command.Env)
	if len(out.Argv) == 0 {
		return out, errors.New("empty full command")
	}
	if filepath.Base(out.Argv[0]) == "make" && len(out.Argv) == 2 && out.Argv[1] == "test" {
		out.Observe = "make-go"
	}
	if _, known := KnownWrapper(command); known {
		out.Observe = ""
		return out, nil
	}
	if facts.WrapperDigest != "" {
		if Digest(out.Argv) != facts.WrapperDigest {
			return out, errors.New("wrapper identity changed")
		}
		return out, nil
	}
	switch facts.Runner {
	case RunnerGo:
		if len(out.Argv) < 2 || out.Argv[1] != "test" {
			return out, errors.New("unsupported full Go command")
		}
		argv := slices.Clone(out.Argv[:2])
		countSeen := false
		for i := 2; i < len(out.Argv); i++ {
			a := out.Argv[i]
			flag, _, eq := strings.Cut(a, "=")
			if flag == "-count" {
				_, value, _ := strings.Cut(a, "=")
				if !eq {
					i++
					if i >= len(out.Argv) {
						return out, errors.New("missing Go count")
					}
					value = out.Argv[i]
				}
				n, e := strconv.Atoi(value)
				if e != nil || n <= 0 {
					return out, errors.New("go repeat count cannot prove test execution")
				}
				countSeen = true
				argv = append(argv, a)
				if !eq {
					argv = append(argv, value)
				}
				continue
			}
			if flag == "-json" {
				continue
			}
			if a == "-args" {
				return out, errors.New("go test binary arguments require full unchanged execution")
			}
			argv = append(argv, a)
		}
		// Go flags may follow packages, but flags before packages are supported by
		// both go test and invocation observers which inspect effective arguments.
		controls := []string{"-json"}
		if !countSeen {
			controls = append(controls, "-count=1")
		}
		fullArgv := slices.Clone(argv[:2])
		fullArgv = append(fullArgv, controls...)
		fullArgv = append(fullArgv, argv[2:]...)
		out.Argv = fullArgv
	case RunnerPytest:
		if containsOption(out.Argv, "--lf", "--last-failed", "--ff", "--failed-first", "--stepwise", "--sw") {
			return out, errors.New("pytest cached selection cannot prove fresh full execution")
		}
	case RunnerVSTest, RunnerMTP, RunnerXcode:
		// These runners execute test bodies on every invocation; sidecar reporting
		// is configured by the sandbox artifact collector, never inferred from logs.
	default:
		return out, errors.New("unsupported full runner")
	}
	return out, nil
}

// NormalizeDiagnostics version 1 removes only runner progress/summary banners
// and an exact factory scratch-root prefix. User failure messages, line numbers,
// exception kinds and stack locations remain part of comparison evidence.
// Original artifacts must be retained separately before this transformation.
func NormalizeDiagnostics(observations []Observation, scratchRoot string) []Observation {
	out := slices.Clone(observations)
	root := strings.TrimSuffix(filepath.ToSlash(scratchRoot), "/")
	banner := regexp.MustCompile(`^\s*(=== (RUN|PAUSE|CONT|NAME)\s+.*|--- (PASS|FAIL|SKIP): .*\([0-9]+(\.[0-9]+)?s\))\s*$`)
	for i := range out {
		text := out[i].Diagnostic
		if root != "" && root != "." {
			text = strings.ReplaceAll(text, root+"/", "<snapshot>/")
		}
		lines := []string{}
		for _, line := range strings.Split(text, "\n") {
			if !banner.MatchString(line) {
				lines = append(lines, line)
			}
		}
		out[i].Diagnostic = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return out
}

// KnownWrapper identifies the shipped prerequisite-bearing test scripts by
// complete script identity and explicit positional schema. Their scripts remain
// unchanged and are never rewritten by the generic argument adapter.
func KnownWrapper(command config.BugCheckCommand) (string, bool) {
	argv := command.Argv
	if len(argv) < 3 || argv[1] != "-c" {
		return "", false
	}
	hash := sha256.Sum256([]byte(argv[2]))
	digest := hex.EncodeToString(hash[:])
	switch digest {
	case "2448e1b89fb281f86f473c063459937e19b50715fa63b59ae4d91699c42da8c8":
		return RunnerPytest, len(argv) == 3 && strings.HasPrefix(filepath.Base(argv[0]), "python")
	case "112bfed34e7f78d1d4c02cf6737b62ffc6f764807f31183d2d6d77e0639224eb":
		if len(argv) != 9 || filepath.Base(argv[0]) != "sh" || argv[3] != "dotnet-web" || argv[4] != "test" {
			return "", false
		}
		if argv[7] == "VSTest" {
			return RunnerVSTest, true
		}
		if argv[7] == "Microsoft.Testing.Platform" {
			return RunnerMTP, true
		}
	case "dbb5d9bde7076b090a6d89ec8bd2edb5c7906f5928a5f049196e82ab9ac04268":
		return RunnerXcode, len(argv) == 8 && filepath.Base(argv[0]) == "sh" && argv[3] == "swift-ios"
	}
	return "", false
}

// inheritGoCount makes the effective saved count explicit before freshness
// controls are added. The remaining GOFLAGS continue to come from the original
// invocation environment, so build settings are not duplicated or reordered.
func inheritGoCount(command config.BugCheckCommand, facts Facts) config.BugCheckCommand {
	if facts.Runner != RunnerGo || len(command.Argv) < 2 || filepath.Base(command.Argv[0]) != "go" || command.Argv[1] != "test" || containsOption(command.Argv, "-count") {
		return command
	}
	effective := ""
	for _, flag := range facts.GoFlags {
		if name, value, ok := strings.Cut(flag, "="); ok && name == "-count" {
			effective = value
		}
	}
	if effective == "" {
		return command
	}
	argv := slices.Clone(command.Argv[:2])
	argv = append(argv, "-count="+effective)
	argv = append(argv, command.Argv[2:]...)
	command.Argv = argv
	return command
}
