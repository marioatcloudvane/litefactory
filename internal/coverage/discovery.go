package coverage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// MaxInventoryUnits bounds dependency graph breadth.
const MaxInventoryUnits = 10000

// MaxInventoryFiles bounds flattened source evidence.
const MaxInventoryFiles = 100000

// DiscoveryRunner uses the same sandbox, durable process claims and cleanup as
// test execution. It must return complete output before display truncation.
type DiscoveryRunner func(context.Context, config.BugCheckCommand) (DiscoveryResult, error)

// DiscoveryResult retains complete sandbox output before display truncation.
type DiscoveryResult struct {
	Output   []byte
	Complete bool
}

// Discover never executes host processes itself. Metadata evaluation executes
// repository code and consequently has the identical boundary as test commands.
func Discover(ctx context.Context, command config.BugCheckCommand, index int, commit, root string, run DiscoveryRunner) (Inventory, Facts, error) {
	inventory := Inventory{Commit: commit}
	facts := Facts{}
	if len(command.Argv) == 0 || run == nil {
		inventory.Reason = "sandbox discovery unavailable"
		return inventory, facts, nil
	}
	argv := command.Argv
	binary := filepath.Base(argv[0])
	switch {
	case binary == "go" && len(argv) > 1 && argv[1] == "test":
		facts.Runner = RunnerGo
	case binary == "pytest" || strings.HasPrefix(binary, "python") && len(argv) > 2 && argv[1] == "-m" && argv[2] == "pytest":
		facts.Runner = RunnerPytest
	case binary == "dotnet" && len(argv) > 1 && argv[1] == "test":
		facts.Runner = RunnerVSTest
		if slices.Contains(argv, "--project") {
			facts.Runner = RunnerMTP
		}
	case binary == "xcodebuild" && slices.Contains(argv, "test"):
		facts.Runner = RunnerXcode
	case binary == "make" && len(argv) == 2 && argv[1] == "test":
		return discoverMake(ctx, command, index, commit, root, run)
	default:
		if runner, known := KnownWrapper(command); known {
			facts.Runner = runner
			facts.WrapperDigest = Digest(argv)
			inventory.Reason = "recognized shipped wrapper requires unchanged full execution"
		} else {
			inventory.Reason = "opaque command requires unchanged full execution"
		}
		return inventory, facts, nil
	}
	probe := command
	probe.Env = slices.Clone(command.Env)
	switch facts.Runner {
	case RunnerGo:
		probe.Argv = []string{argv[0], "version"}
	case RunnerPytest:
		probe.Argv = slices.Clone(argv[:1])
		if binary != "pytest" {
			probe.Argv = append(probe.Argv, "-m", "pytest")
		}
		probe.Argv = append(probe.Argv, "--version")
	case RunnerVSTest, RunnerMTP:
		probe.Argv = []string{argv[0], "--version"}
	case RunnerXcode:
		probe.Argv = []string{argv[0], "-version"}
	}
	version, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		inventory.Reason = "runner capability probe failed"
		return inventory, facts, err
	}
	facts.Version = strings.TrimSpace(string(version))
	if facts.Version == "" {
		inventory.Reason = "runner version unavailable"
		return inventory, facts, nil
	}
	switch facts.Runner {
	case RunnerGo:
		environment, environmentErr := probeGoEnvironment(ctx, command, argv[0], root, run)
		if environmentErr != nil {
			inventory.Reason = environmentErr.Error()
			return inventory, facts, environmentErr
		}
		facts.Version += ";environment=" + Digest(environment)
		facts.GoFlags = strings.Fields(environment["GOFLAGS"])
		list, err := goListCommand(command)
		if err != nil {
			inventory.Reason = err.Error()
			return inventory, facts, err
		}
		data, err := boundedDiscovery(ctx, list, run)
		if err != nil {
			inventory.Reason = "Go dependency discovery incomplete"
			return inventory, facts, err
		}
		inventory, err = GoInventory(data, index, commit, root, command.CWD)
		facts.Dependencies = inventory.Complete
		facts.Structured = true
		facts.Fresh = true
		return inventory, facts, err
	case RunnerPytest:
		if binary == "pytest" {
			inventory.Reason = "pytest interpreter identity is unavailable"
			return inventory, facts, nil
		}
		probe.Argv = []string{argv[0], "-c", PythonEnvironmentProbe}
		environment, probeErr := boundedDiscovery(ctx, probe, run)
		if probeErr != nil {
			inventory.Reason = "Python execution environment is unproven"
			return inventory, facts, probeErr
		}
		var environmentFacts struct {
			Complete bool `json:"complete"`
		}
		environmentErr := json.Unmarshal(environment, &environmentFacts)
		if environmentErr != nil || !environmentFacts.Complete {
			inventory.Reason = "Python external editable dependencies prevent stable inheritance"
			return inventory, facts, environmentErr
		}
		facts.Version += ";environment=" + Digest(string(environment))
		probe.Argv = []string{argv[0], "-c", PythonDiscovery}
		probe.Argv = append(probe.Argv, argv[3:]...)
		data, err := boundedDiscovery(ctx, probe, run)
		if err != nil {
			inventory.Reason = "Python dependency discovery incomplete"
			return inventory, facts, err
		}
		inventory, err = ReadInventory(data, index, commit, facts.Runner)
		if inventory.Complete && command.CWD != "" && command.CWD != "." {
			for i := range inventory.Units {
				for j, file := range inventory.Units[i].Files {
					inventory.Units[i].Files[j] = path.Join(command.CWD, file)
				}
			}
		}
		facts.Dependencies = inventory.Complete
		facts.Structured = true
		facts.Fresh = true
		return inventory, facts, err
	case RunnerVSTest, RunnerMTP:
		inventory, facts, err = discoverDotnet(ctx, command, index, commit, root, run, facts)
		return inventory, facts, err
	case RunnerXcode:
		inventory, facts, err = discoverXcode(ctx, command, index, commit, root, run, facts)
		return inventory, facts, err
	}
	return inventory, facts, nil
}

func boundedDiscovery(ctx context.Context, command config.BugCheckCommand, run DiscoveryRunner) ([]byte, error) {
	result, err := run(ctx, command)
	if err != nil {
		return nil, err
	}
	if !result.Complete || len(result.Output) == 0 || len(result.Output) > MaxResultBytes {
		return nil, errors.New("discovery capture is incomplete or exceeds bounds")
	}
	return result.Output, nil
}

func goListCommand(command config.BugCheckCommand) (config.BugCheckCommand, error) {
	out := command
	out.Argv = []string{command.Argv[0], "list", "-deps", "-test", "-json"}
	out.Env = slices.Clone(command.Env)
	buildValue := map[string]bool{"-tags": true, "-mod": true, "-modfile": true, "-gcflags": true, "-ldflags": true, "-asmflags": true, "-p": true, "-pkgdir": true, "-coverpkg": true, "-covermode": true}
	buildBool := map[string]bool{"-race": true, "-trimpath": true, "-a": true, "-asan": true, "-msan": true, "-buildvcs": true, "-cover": true}
	testValue := map[string]bool{"-run": true, "-count": true, "-timeout": true, "-parallel": true, "-cpu": true, "-shuffle": true, "-vet": true, "-coverprofile": true}
	testBool := map[string]bool{"-json": true, "-v": true, "-short": true, "-failfast": true}
	packages := []string{}
	for i := 2; i < len(command.Argv); i++ {
		a := command.Argv[i]
		if !strings.HasPrefix(a, "-") {
			packages = append(packages, a)
			continue
		}
		flag, _, eq := strings.Cut(a, "=")
		switch {
		case buildValue[flag]:
			out.Argv = append(out.Argv, a)
			if !eq {
				i++
				if i >= len(command.Argv) {
					return out, errors.New("missing build option value")
				}
				out.Argv = append(out.Argv, command.Argv[i])
			}
		case buildBool[flag]:
			out.Argv = append(out.Argv, a)
		case testValue[flag]:
			if !eq {
				i++
				if i >= len(command.Argv) {
					return out, errors.New("missing test option value")
				}
			}
		case testBool[flag]:
		default:
			return out, fmt.Errorf("go discovery cannot preserve option %s", flag)
		}
	}
	if len(packages) == 0 {
		packages = []string{"."}
	}
	out.Argv = append(out.Argv, packages...)
	return out, nil
}

type goPackage struct {
	Module *struct {
		Replace *struct {
			Version string
			Dir     string
		}
	}
	Dir                                                                                                                                                                             string
	ImportPath                                                                                                                                                                      string
	ForTest                                                                                                                                                                         string
	Standard                                                                                                                                                                        bool
	DepOnly                                                                                                                                                                         bool
	Incomplete                                                                                                                                                                      bool
	Error                                                                                                                                                                           json.RawMessage
	DepsErrors                                                                                                                                                                      []json.RawMessage
	GoFiles, CgoFiles, CFiles, CXXFiles, MFiles, HFiles, FFiles, SFiles, SwigFiles, SwigCXXFiles, SysoFiles, EmbedFiles, TestGoFiles, XTestGoFiles, TestEmbedFiles, XTestEmbedFiles []string
	Imports, TestImports, XTestImports                                                                                                                                              []string
}

// GoInventory resolves the complete old/current graph from go list metadata.
// Files are flattened to each test unit so production-only dependencies do not
// become test obligations merely because they occur in the reverse graph.
func GoInventory(data []byte, index int, commit, root, cwd string) (Inventory, error) {
	out := Inventory{Commit: commit}
	if len(data) > MaxResultBytes {
		out.Reason = "Go metadata exceeds bounds"
		return out, nil
	}
	packages := map[string]goPackage{}
	order := []string{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var p goPackage
		err := decoder.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			out.Reason = "invalid Go metadata"
			return out, err
		}
		if len(packages) >= MaxInventoryUnits {
			out.Reason = "Go graph exceeds bounds"
			return out, nil
		}
		if p.Incomplete || len(p.Error) > 0 || len(p.DepsErrors) > 0 {
			out.Reason = "Go graph contains incomplete packages"
			return out, nil
		}
		if p.ImportPath == "" || p.Dir == "" {
			out.Reason = "Go package identity is missing"
			return out, nil
		}
		if p.Module != nil && p.Module.Replace != nil && p.Module.Replace.Version == "" {
			dir, relErr := filepath.Rel(root, p.Module.Replace.Dir)
			if relErr != nil || dir == ".." || strings.HasPrefix(dir, ".."+string(filepath.Separator)) {
				out.Reason = "mutable external Go module replacement prevents inheritance"
				return out, relErr
			}
		}
		if p.ForTest != "" || strings.Contains(p.ImportPath, " [") || strings.HasSuffix(p.ImportPath, ".test") {
			continue
		}
		if _, exists := packages[p.ImportPath]; exists {
			out.Reason = "Go package identity is ambiguous"
			return out, nil
		}
		packages[p.ImportPath] = p
		order = append(order, p.ImportPath)
	}
	if len(packages) == 0 {
		out.Reason = "Go inventory is empty"
		return out, nil
	}
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		out.Reason = "scratch root must be absolute"
		return out, nil
	}
	fileCount := 0
	var files func(string, map[string]bool) ([]string, error)
	files = func(key string, seen map[string]bool) ([]string, error) {
		if seen[key] {
			return nil, nil
		}
		seen[key] = true
		p, ok := packages[key]
		if !ok {
			if key == "C" {
				return nil, nil
			}
			return nil, errors.New("go dependency metadata is missing")
		}
		if p.Standard {
			return nil, nil
		}
		dir, err := filepath.Rel(root, p.Dir)
		if err != nil {
			return nil, err
		}
		// Dependencies outside the snapshot are immutable module/toolchain inputs;
		// lockfiles and runner versions are independently included in conditions.
		if dir == ".." || strings.HasPrefix(dir, ".."+string(filepath.Separator)) {
			return nil, nil
		}
		names := [][]string{p.GoFiles, p.CgoFiles, p.CFiles, p.CXXFiles, p.MFiles, p.HFiles, p.FFiles, p.SFiles, p.SwigFiles, p.SwigCXXFiles, p.SysoFiles, p.EmbedFiles, p.TestGoFiles, p.XTestGoFiles, p.TestEmbedFiles, p.XTestEmbedFiles}
		result := []string{}
		for _, group := range names {
			for _, name := range group {
				if !safeRelative(name) {
					return nil, errors.New("go file path escapes package")
				}
				fileCount++
				if fileCount > MaxInventoryFiles {
					return nil, errors.New("go file graph exceeds bounds")
				}
				result = append(result, path.Join(filepath.ToSlash(dir), filepath.ToSlash(name)))
			}
		}
		for _, dep := range append(append(slices.Clone(p.Imports), p.TestImports...), p.XTestImports...) {
			more, err := files(dep, seen)
			if err != nil {
				return nil, err
			}
			result = append(result, more...)
		}
		return result, nil
	}
	for _, key := range order {
		p := packages[key]
		if p.Standard || p.DepOnly {
			continue
		}
		dir, err := filepath.Rel(root, p.Dir)
		if err != nil || dir == ".." || strings.HasPrefix(dir, ".."+string(filepath.Separator)) {
			continue
		}
		dependencies, err := files(key, map[string]bool{})
		if err != nil {
			out.Reason = err.Error()
			return out, err
		}
		sort.Strings(dependencies)
		unitKey := fmt.Sprintf("%d:go:%s", index, key)
		if len(p.TestGoFiles)+len(p.XTestGoFiles) == 0 {
			unitKey = fmt.Sprintf("%d:compile:go:%s", index, key)
		}
		out.Units = append(out.Units, Unit{Key: unitKey, Command: index, Runner: RunnerGo, Selector: key, Files: slices.Compact(dependencies)})
	}
	if len(out.Units) == 0 {
		out.Reason = "no Go test units discovered"
		return out, nil
	}
	out.Complete = true
	return out, nil
}

// ReadInventory reads a probed runner observer's closed, versioned graph schema.
// Unknown dependencies, paths or identities leave the graph incomplete.
func ReadInventory(data []byte, index int, commit, runner string) (Inventory, error) {
	out := Inventory{Commit: commit}
	if len(data) > MaxResultBytes {
		out.Reason = "inventory exceeds bounds"
		return out, nil
	}
	var graph struct {
		Version  int    `json:"version"`
		Complete bool   `json:"complete"`
		Reason   string `json:"reason"`
		Units    []struct {
			Selector     string   `json:"selector"`
			Files        []string `json:"files"`
			Dependencies []string `json:"dependencies"`
		} `json:"units"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&graph); err != nil {
		out.Reason = "invalid inventory schema"
		return out, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		out.Reason = "inventory contains trailing data"
		return out, nil
	}
	if graph.Version != Version || !graph.Complete {
		out.Reason = graph.Reason
		if out.Reason == "" {
			out.Reason = "incompatible or incomplete inventory"
		}
		return out, nil
	}
	if len(graph.Units) == 0 || len(graph.Units) > MaxInventoryUnits {
		out.Reason = "inventory unit bound exceeded or empty"
		return out, nil
	}
	selectors := map[string]string{}
	count := 0
	for _, u := range graph.Units {
		if u.Selector == "" || strings.ContainsAny(u.Selector, "\x00\r\n") || selectors[u.Selector] != "" {
			out.Reason = "ambiguous unit identity"
			return out, nil
		}
		selectors[u.Selector] = fmt.Sprintf("%d:%s:%s", index, runner, u.Selector)
	}
	for _, u := range graph.Units {
		unit := Unit{Key: selectors[u.Selector], Command: index, Runner: runner, Selector: u.Selector, Files: slices.Clone(u.Files)}
		if len(u.Files) == 0 {
			out.Reason = "unit has no source coverage"
			return out, nil
		}
		for _, f := range u.Files {
			count++
			if count > MaxInventoryFiles || !safeRelative(f) {
				out.Reason = "inventory path is unsafe or exceeds bounds"
				return out, nil
			}
		}
		for _, d := range u.Dependencies {
			key, ok := selectors[d]
			if !ok {
				out.Reason = "dependency identity is unknown"
				return out, nil
			}
			unit.Dependencies = append(unit.Dependencies, key)
		}
		sort.Strings(unit.Files)
		unit.Files = slices.Compact(unit.Files)
		out.Units = append(out.Units, unit)
	}
	out.Complete = true
	return out, nil
}
func safeRelative(p string) bool {
	return p != "" && !filepath.IsAbs(p) && !strings.ContainsAny(p, "\x00\\") && p != ".." && !strings.HasPrefix(p, "../") && path.Clean(p) == p
}

// PythonDiscovery collects with the configured pytest arguments, then bounds a
// static import closure. Dynamic imports/hooks and shared fixtures force full
// project execution; no last-failed cache is used as the coverage ledger.
const PythonDiscovery = `import ast
import contextlib
import io
import json
import pathlib
import sys
import pytest

root = pathlib.Path.cwd().resolve()
nodes = []
plugins = []
class Collector:
    def pytest_plugin_registered(self, plugin, manager):
        name = getattr(plugin, "__name__", type(plugin).__module__)
        if not name.startswith("_pytest.") and name not in ("__main__", "builtins"):
            plugins.append(name)
    def pytest_collection_finish(self, session):
        nodes.extend(item.nodeid for item in session.items)
with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
    code = pytest.main(sys.argv[1:] + ["--collect-only", "-q"], plugins=[Collector()])
reason = ""
if code != 0 or not nodes:
    reason = "pytest collection was incomplete or empty"
if plugins:
    reason = "external pytest plugin dependency graph is unknown"
files = {}
total = 0
for p in root.rglob("*.py"):
    rel = p.relative_to(root)
    if any(part in (".git", ".venv", "venv", "node_modules", "__pycache__", ".tox") for part in rel.parts):
        continue
    if not p.resolve().is_relative_to(root) or p.stat().st_size > 1048576 or len(files) >= 10000:
        reason = "Python source graph exceeds safe bounds"
        break
    total += p.stat().st_size
    if total > 16777216:
        reason = "Python source graph exceeds safe bounds"
        break
    files[rel.as_posix()] = p.read_text(encoding="utf-8")
modules = {name[:-3].replace("/", "."): name for name in files}
modules.update({name[:-12].replace("/", "."): name for name in files if name.endswith("/__init__.py")})
graph = {}
for name, source in files.items():
    imports = set()
    try:
        tree = ast.parse(source)
    except SyntaxError:
        reason = "Python import graph is incomplete"
        continue
    if name.endswith("conftest.py"):
        reason = "shared pytest fixtures require complete project execution"
    for node in ast.walk(tree):
        if isinstance(node, ast.Name) and node.id == "pytest_plugins":
            reason = "pytest plugin consumers are unknown"
        if isinstance(node, ast.Call) and ((isinstance(node.func, ast.Name) and node.func.id in ("eval", "exec", "compile", "__import__", "getattr", "globals", "locals")) or (isinstance(node.func, ast.Attribute) and node.func.attr in ("import_module", "spec_from_file_location", "load_module", "run_module", "run_path", "find_spec", "exec_module"))):
            reason = "dynamic Python imports require complete project execution"
        if isinstance(node, ast.Import):
            imports.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom):
            prefix = ""
            if node.level:
                parent = name.split("/")[:-1]
                if node.level > len(parent):
                    reason = "relative Python import cannot be resolved"
                prefix = ".".join(parent[:len(parent) - node.level + 1])
            base = ".".join(s for s in (prefix, node.module or "") if s)
            imports.add(base)
            imports.update(base + "." + alias.name for alias in node.names)
    graph[name] = set()
    for module in imports:
        parts = module.split(".")
        for i in range(1, len(parts) + 1):
            selected = modules.get(".".join(parts[:i]))
            if selected:
                graph[name].add(selected)
def closure(name, seen):
    if name in seen:
        return
    seen.add(name)
    for dependency in graph.get(name, ()):
        closure(dependency, seen)
units = []
for name in sorted(set(node.split("::", 1)[0] for node in nodes)):
    if name not in files:
        reason = "collected Python file is outside the static graph"
        continue
    dependencies = set()
    closure(name, dependencies)
    units.append({"selector": name, "files": sorted(dependencies), "dependencies": []})
print(json.dumps({"version": 1, "complete": not bool(reason), "reason": reason, "units": units}))
`

type evaluatedProject struct {
	Properties map[string]string           `json:"Properties"`
	Items      map[string][]map[string]any `json:"Items"`
}

func discoverDotnet(ctx context.Context, command config.BugCheckCommand, index int, commit, root string, run DiscoveryRunner, facts Facts) (Inventory, Facts, error) {
	out := Inventory{Commit: commit}
	target := ""
	for i, a := range command.Argv {
		if a == "--project" && i+1 < len(command.Argv) {
			target = command.Argv[i+1]
		}
		if strings.HasSuffix(a, ".csproj") {
			if target != "" && target != a {
				out.Reason = "multiple .NET test projects require full execution"
				return out, facts, nil
			}
			target = a
		}
	}
	if target == "" || !safeRelative(target) {
		out.Reason = "explicit .NET project is required"
		return out, facts, nil
	}
	framework, configuration := "", ""
	for i, a := range command.Argv {
		if i+1 < len(command.Argv) {
			switch a {
			case "--framework", "-f":
				framework = command.Argv[i+1]
			case "--configuration", "-c":
				configuration = command.Argv[i+1]
			}
		}
	}
	if framework == "" || configuration == "" {
		out.Reason = "evaluated .NET framework and configuration must be explicit"
		return out, facts, nil
	}
	visited := map[string]bool{}
	allFiles := []string{}
	mtp, frameworkKind := "", ""
	var evaluate func(string) error
	evaluate = func(project string) error {
		if visited[project] {
			return nil
		}
		visited[project] = true
		if len(visited) > MaxInventoryUnits {
			return errors.New(".NET project graph exceeds bounds")
		}
		probe := command
		probe.Argv = []string{command.Argv[0], "msbuild", project, "-nologo", "-getProperty:TargetFramework,IsTestProject,IsTestingPlatformApplication,MSBuildAllProjects,UsingMicrosoftNETSdk,EnableDefaultCompileItems,TargetFrameworks", "-getItem:ProjectReference,Compile,EmbeddedResource,Content,None,PackageReference", "-property:TargetFramework=" + framework, "-property:Configuration=" + configuration}
		data, err := boundedDiscovery(ctx, probe, run)
		if err != nil {
			return err
		}
		var value evaluatedProject
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if value.Properties["UsingMicrosoftNETSdk"] != "true" {
			return errors.New("custom .NET project SDK requires full execution")
		}
		if value.Properties["TargetFramework"] != framework || value.Properties["TargetFrameworks"] != "" {
			return errors.New("multi-target or unmatched .NET framework requires full execution")
		}
		if project == target {
			if value.Properties["IsTestProject"] != "true" {
				return errors.New("evaluated .NET project is not a test project")
			}
			mtp = value.Properties["IsTestingPlatformApplication"]
		}
		// Repository-owned imported props/targets can add dynamic build inputs and
		// custom test tasks; a static reference graph cannot bound those effects.
		for _, imported := range strings.Split(value.Properties["MSBuildAllProjects"], ";") {
			if imported == "" {
				continue
			}
			rel, e := filepath.Rel(root, imported)
			if e == nil && safeRelative(filepath.ToSlash(rel)) && filepath.Clean(imported) != filepath.Join(root, command.CWD, project) {
				return errors.New("repository .NET imports require complete project execution")
			}
		}
		relProject := path.Join(command.CWD, project)
		allFiles = append(allFiles, relProject)
		for kind, items := range value.Items {
			for _, item := range items {
				identity, _ := item["Identity"].(string)
				full, _ := item["FullPath"].(string)
				if kind == "PackageReference" {
					if identity == "MSTest" || identity == "MSTest.TestFramework" {
						frameworkKind = "mstest"
					}
					continue
				}
				var relative string
				if full != "" {
					rel, e := filepath.Rel(root, full)
					if e != nil {
						return e
					}
					relative = filepath.ToSlash(rel)
				} else {
					relative = path.Clean(path.Join(path.Dir(relProject), identity))
				}
				if !safeRelative(relative) {
					return errors.New(".NET evaluated input escapes snapshot")
				}
				if kind == "ProjectReference" {
					p, e := filepath.Rel(filepath.Join(root, command.CWD), filepath.Join(root, relative))
					if e != nil || !safeRelative(filepath.ToSlash(p)) {
						return errors.New(".NET project reference escapes working directory")
					}
					if e := evaluate(filepath.ToSlash(p)); e != nil {
						return e
					}
				} else {
					allFiles = append(allFiles, relative)
				}
				if len(allFiles) > MaxInventoryFiles {
					return errors.New(".NET file graph exceeds bounds")
				}
			}
		}
		return nil
	}
	if err := evaluate(target); err != nil {
		out.Reason = err.Error()
		return out, facts, err
	}
	if mtp == "true" && facts.Runner != RunnerMTP || mtp != "true" && facts.Runner == RunnerMTP {
		out.Reason = ".NET invocation and evaluated test platform differ"
		return out, facts, nil
	}
	if facts.Runner == RunnerMTP {
		// Native MTP needs framework-defined filters and an installed TRX extension.
		// Only probed MSTest's VSTest-compatible filter grammar is currently bounded.
		if frameworkKind != "mstest" {
			out.Reason = "MTP framework filter capability is unknown"
			return out, facts, nil
		}
		probe := command
		probe.Argv = append(slices.Clone(command.Argv), "--help")
		help, probeErr := boundedDiscovery(ctx, probe, run)
		if probeErr != nil || !bytes.Contains(help, []byte("--report-trx")) || !bytes.Contains(help, []byte("--filter")) {
			out.Reason = "MTP filter and TRX extensions were not proved"
			return out, facts, probeErr
		}
		facts.FilterFlag = "--filter"
		facts.FilterProperty = "FullyQualifiedName"
		facts.Framework = frameworkKind
	}
	runtimeProbe := command
	runtimeProbe.Argv = []string{command.Argv[0], "--list-runtimes"}
	runtimes, probeErr := boundedDiscovery(ctx, runtimeProbe, run)
	if probeErr != nil || !bytes.Contains(runtimes, []byte("Microsoft.NETCore.App ")) {
		out.Reason = ".NET runtime identity is unavailable"
		return out, facts, probeErr
	}
	facts.Version += ";runtimes=" + Digest(string(runtimes))
	sort.Strings(allFiles)
	out.Units = []Unit{{Key: fmt.Sprintf("%d:%s:%s", index, facts.Runner, target), Command: index, Runner: facts.Runner, Selector: target, Files: slices.Compact(allFiles)}}
	out.Complete = true
	facts.Dependencies = true
	facts.Structured = true
	facts.Fresh = true
	return out, facts, nil
}

func discoverXcode(ctx context.Context, command config.BugCheckCommand, index int, commit, root string, run DiscoveryRunner, facts Facts) (Inventory, Facts, error) {
	out := Inventory{Commit: commit}
	project := ""
	scheme, destination := false, false
	for i, a := range command.Argv {
		if a == "-workspace" {
			out.Reason = "workspace graph requires complete project execution"
			return out, facts, nil
		}
		if i+1 < len(command.Argv) {
			switch a {
			case "-project":
				project = command.Argv[i+1]
			case "-scheme":
				scheme = true
			case "-destination":
				destination = true
			}
		}
	}
	if project == "" || !safeRelative(project) || !strings.HasSuffix(project, ".xcodeproj") || !scheme || !destination {
		out.Reason = "explicit Xcode project, scheme and destination are required"
		return out, facts, nil
	}
	probe := command
	probe.Argv = append(slices.Clone(command.Argv), "-showBuildSettings", "-json")
	settings, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		out.Reason = "Xcode build settings are incomplete"
		return out, facts, err
	}
	probe.Argv = []string{"/usr/bin/plutil", "-convert", "json", "-o", "-", path.Join(project, "project.pbxproj")}
	data, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		out.Reason = "Xcode project graph is incomplete"
		return out, facts, err
	}
	out, err = XcodeInventory(settings, data, index, commit, root, path.Join(command.CWD, project))
	if err != nil || !out.Complete {
		return out, facts, err
	}
	probe.Argv = []string{"xcrun", "xcresulttool", "get", "test-results", "tests", "--help"}
	schema, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		out.Complete = false
		out.Reason = "xcresult schema probe is unavailable"
		return out, facts, err
	}
	if !bytes.Contains(schema, []byte("--path")) {
		out.Complete = false
		out.Reason = "xcresult schema probe is incompatible"
		return out, facts, nil
	}
	platformFacts := []string{}
	for _, args := range [][]string{{"xcrun", "swift", "--version"}, {"xcrun", "--sdk", "iphonesimulator", "--show-sdk-version"}, {"xcrun", "simctl", "list", "runtimes", "--json"}} {
		probe.Argv = args
		value, probeErr := boundedDiscovery(ctx, probe, run)
		if probeErr != nil {
			out.Complete = false
			out.Reason = "Xcode SDK/compiler/runtime identity is unavailable"
			return out, facts, probeErr
		}
		platformFacts = append(platformFacts, string(value))
	}
	facts.Version += ";platform=" + Digest(platformFacts)
	facts.Dependencies = true
	facts.Structured = true
	facts.Fresh = true
	return out, facts, nil
}

// XcodeInventory accepts only explicit, acyclic native-target projects with
// ordinary source/resource file references. Script phases, synchronized groups,
// build rules and unresolved references expand to the unchanged full command.
func XcodeInventory(settings, project []byte, index int, commit, root, projectPath string) (Inventory, error) {
	out := Inventory{Commit: commit}
	if len(settings) > MaxResultBytes || len(project) > MaxResultBytes {
		out.Reason = "Xcode metadata exceeds bounds"
		return out, nil
	}
	var builds []struct {
		Target        string         `json:"target"`
		BuildSettings map[string]any `json:"buildSettings"`
	}
	if err := json.Unmarshal(settings, &builds); err != nil {
		return out, err
	}
	selected := map[string]bool{}
	for _, b := range builds {
		if b.Target == "" {
			out.Reason = "Xcode target identity unavailable"
			return out, nil
		}
		selected[b.Target] = true
		for _, key := range []string{"EXCLUDED_SOURCE_FILE_NAMES", "INCLUDED_SOURCE_FILE_NAMES"} {
			if s, ok := b.BuildSettings[key].(string); ok && strings.TrimSpace(s) != "" {
				out.Reason = "conditional Xcode source selection requires full execution"
				return out, nil
			}
		}
	}
	var doc struct {
		Objects map[string]map[string]json.RawMessage `json:"objects"`
		Root    string                                `json:"rootObject"`
	}
	if err := json.Unmarshal(project, &doc); err != nil {
		return out, err
	}
	if len(doc.Objects) == 0 || len(doc.Objects) > MaxInventoryFiles {
		out.Reason = "Xcode object graph empty or exceeds bounds"
		return out, nil
	}
	str := func(object map[string]json.RawMessage, key string) string {
		var s string
		_ = json.Unmarshal(object[key], &s)
		return s
	}
	array := func(object map[string]json.RawMessage, key string) []string {
		var values []string
		_ = json.Unmarshal(object[key], &values)
		return values
	}
	parents := map[string]string{}
	for id, obj := range doc.Objects {
		kind := str(obj, "isa")
		if kind == "PBXFileSystemSynchronizedRootGroup" {
			out.Reason = "synchronized Xcode groups require full execution"
			return out, nil
		}
		for _, child := range array(obj, "children") {
			if previous := parents[child]; previous != "" && previous != id {
				out.Reason = "ambiguous Xcode file ownership"
				return out, nil
			}
			parents[child] = id
		}
	}
	projectDir := path.Dir(projectPath)
	var resolve func(string, map[string]bool) (string, error)
	resolve = func(id string, seen map[string]bool) (string, error) {
		if seen[id] || len(seen) > 64 {
			return "", errors.New("cyclic Xcode group graph")
		}
		seen[id] = true
		obj, ok := doc.Objects[id]
		if !ok {
			return "", errors.New("missing Xcode file reference")
		}
		source := str(obj, "sourceTree")
		name := str(obj, "path")
		if strings.ContainsAny(name, "$\x00") || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return "", errors.New("dynamic or escaping Xcode file path")
		}
		switch source {
		case "SOURCE_ROOT":
			return path.Join(projectDir, name), nil
		case "<group>", "":
			parent, ok := parents[id]
			if !ok {
				return path.Join(projectDir, name), nil
			}
			base, err := resolve(parent, seen)
			if err != nil {
				return "", err
			}
			return path.Join(base, name), nil
		default:
			return "", errors.New("xcode input uses an external source tree")
		}
	}
	targetFiles := map[string][]string{}
	targetDeps := map[string][]string{}
	targetNames := map[string]string{}
	tests := []string{}
	for id, obj := range doc.Objects {
		if str(obj, "isa") != "PBXNativeTarget" {
			continue
		}
		name := str(obj, "name")
		if name == "" {
			out.Reason = "Xcode native target name missing"
			return out, nil
		}
		targetNames[id] = name
		if len(array(obj, "buildRules")) > 0 {
			out.Reason = "custom Xcode build rules require full execution"
			return out, nil
		}
		for _, phaseID := range array(obj, "buildPhases") {
			phase, ok := doc.Objects[phaseID]
			if !ok {
				out.Reason = "Xcode build phase missing"
				return out, nil
			}
			switch str(phase, "isa") {
			case "PBXSourcesBuildPhase", "PBXResourcesBuildPhase", "PBXFrameworksBuildPhase":
			case "PBXCopyFilesBuildPhase":
				out.Reason = "Xcode copy-file inputs require full execution"
				return out, nil
			default:
				out.Reason = "Xcode script or unknown build phase requires full execution"
				return out, nil
			}
			for _, buildID := range array(phase, "files") {
				file, ok := doc.Objects[buildID]
				if !ok {
					out.Reason = "Xcode build file missing"
					return out, nil
				}
				ref := str(file, "fileRef")
				if ref == "" {
					out.Reason = "Xcode package product dependency requires full execution"
					return out, nil
				}
				fileRef, ok := doc.Objects[ref]
				if !ok {
					out.Reason = "Xcode source reference missing"
					return out, nil
				}
				if str(phase, "isa") == "PBXFrameworksBuildPhase" && (str(fileRef, "sourceTree") == "SDKROOT" || str(fileRef, "sourceTree") == "BUILT_PRODUCTS_DIR") {
					continue
				}
				filename, err := resolve(ref, map[string]bool{})
				if err != nil {
					out.Reason = err.Error()
					return out, err
				}
				if !safeRelative(filename) {
					out.Reason = "Xcode source path escapes snapshot"
					return out, nil
				}
				targetFiles[id] = append(targetFiles[id], filename)
			}
		}
		for _, depID := range array(obj, "dependencies") {
			dep, ok := doc.Objects[depID]
			if !ok || str(dep, "target") == "" {
				out.Reason = "external Xcode target dependency requires full execution"
				return out, nil
			}
			targetDeps[id] = append(targetDeps[id], str(dep, "target"))
		}
		kind := str(obj, "productType")
		if (kind == "com.apple.product-type.bundle.unit-test" || kind == "com.apple.product-type.bundle.ui-testing") && selected[name] {
			tests = append(tests, id)
		}
	}
	// Test hosts can be recorded in project attributes rather than explicit
	// PBXTargetDependency edges. Include them before taking the reverse closure.
	if rootObject, ok := doc.Objects[doc.Root]; ok {
		var attributes struct {
			Targets map[string]struct {
				Host string `json:"TestTargetID"`
			} `json:"TargetAttributes"`
		}
		_ = json.Unmarshal(rootObject["attributes"], &attributes)
		for id, value := range attributes.Targets {
			if value.Host != "" {
				targetDeps[id] = append(targetDeps[id], value.Host)
			}
		}
	}
	sort.Slice(tests, func(i, j int) bool { return targetNames[tests[i]] < targetNames[tests[j]] })
	for _, id := range tests {
		files := []string{path.Join(projectPath, "project.pbxproj")}
		visited := map[string]bool{}
		var visit func(string) error
		visit = func(id string) error {
			if visited[id] {
				return nil
			}
			visited[id] = true
			if targetNames[id] == "" {
				return errors.New("xcode target dependency missing")
			}
			files = append(files, targetFiles[id]...)
			if len(files) > MaxInventoryFiles {
				return errors.New("xcode file graph exceeds bounds")
			}
			for _, dep := range targetDeps[id] {
				if err := visit(dep); err != nil {
					return err
				}
			}
			return nil
		}
		if err := visit(id); err != nil {
			out.Reason = err.Error()
			return out, err
		}
		if len(files) == 1 {
			out.Reason = "Xcode test target has no observed sources"
			return out, nil
		}
		sort.Strings(files)
		out.Units = append(out.Units, Unit{Key: fmt.Sprintf("%d:xcodebuild:%s", index, targetNames[id]), Command: index, Runner: RunnerXcode, Selector: targetNames[id], Files: slices.Compact(files)})
	}
	if len(out.Units) == 0 {
		out.Reason = "no bounded Xcode test targets in selected scheme"
		return out, nil
	}
	out.Complete = true
	_ = root // project-relative references were checked without host filesystem reads.
	return out, nil
}

// This fingerprint describes the supported test target's control flow, not the
// whole Makefile. Variable values are evaluated in the sandbox for each run.
const makeTestTargetDigest = "a8e8e5320d91f0a58c0528cf30a9361efcac6ee0e663c2c3fd1e982e7795e674"
const makeFactsTarget = "lf-coverage-facts"
const makeFactsRecipe = "lf-coverage-facts:\n\t@printf '%s\\0' '$(GO)' '$(PKGS)' '$(MIXED_PACKAGES)' '$(TEST_TIMEOUT)' '$(GOFLAGS)' '$(RACE_TESTS_turn)' '$(RACE_TESTS_store)' '$(RACE_TESTS_cli)'"

// makeFactsScript feeds the facts rule to make on standard input, read after
// the project Makefile. GNU Make 3.81 (the macOS system make) has no --eval,
// but every GNU Make reads "-f -" the same way, and nothing is written into
// the snapshot. The facts target is named explicitly, so the project's first
// target or .DEFAULT_GOAL is never built; discoverMake has already refused a
// Makefile that mentions the facts target, so the rule cannot collide.
const makeFactsScript = `printf '%s\n' "$1" | "$2" -f Makefile -f - --no-print-directory -s "$3"`

func makeFactsArgv(makeBinary string) []string {
	return []string{"/bin/sh", "-c", makeFactsScript, "lf-coverage-facts-probe", makeFactsRecipe, makeBinary, makeFactsTarget}
}

func discoverMake(ctx context.Context, command config.BugCheckCommand, index int, commit, root string, run DiscoveryRunner) (Inventory, Facts, error) {
	out := Inventory{Commit: commit}
	facts := Facts{Runner: RunnerGo, WrapperDigest: Digest(command.Argv)}
	probe := command
	probe.Argv = []string{"/bin/cat", "Makefile"}
	source, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		out.Reason = "make recipe source is unavailable"
		return out, facts, err
	}
	text := string(source)
	lines := strings.Split(text, "\n")
	target := ""
	count := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "include ") || strings.HasPrefix(line, "-include ") || strings.HasPrefix(line, "sinclude ") || strings.Contains(line, "$(eval") || strings.Contains(line, makeFactsTarget) {
			out.Reason = "dynamic make recipe requires full execution"
			return out, facts, nil
		}
		if strings.HasPrefix(line, "test:") {
			count++
			j := i + 1
			for j < len(lines) && strings.HasPrefix(lines[j], "\t") {
				j++
			}
			target = strings.Join(lines[i:j], "\n")
		}
	}
	hash := sha256.Sum256([]byte(target))
	if count != 1 || hex.EncodeToString(hash[:]) != makeTestTargetDigest {
		out.Reason = "unsupported make test control flow"
		return out, facts, nil
	}
	probe.Argv = makeFactsArgv(command.Argv[0])
	evaluated, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		out.Reason = "make variable evaluation incomplete"
		return out, facts, err
	}
	values := strings.Split(string(evaluated), "\x00")
	if len(values) != 9 || values[8] != "" {
		out.Reason = "make variable schema is incompatible"
		return out, facts, nil
	}
	goBinary := values[0]
	if goBinary != "go" {
		out.Reason = "custom make Go executable cannot be preserved"
		return out, facts, nil
	}
	for _, v := range values[1:8] {
		if strings.ContainsAny(v, "\x00\r\n$`'\"") {
			out.Reason = "dynamic make variable requires full execution"
			return out, facts, nil
		}
	}
	flags := strings.Fields(values[4])
	for _, flag := range flags {
		if !strings.HasPrefix(flag, "-") {
			out.Reason = "make GOFLAGS cannot be represented without shell parsing"
			return out, facts, nil
		}
	}
	probe.Argv = []string{goBinary, "version"}
	version, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		out.Reason = "make Go version unavailable"
		return out, facts, err
	}
	facts.Version = strings.TrimSpace(string(version))
	environment, environmentErr := probeGoEnvironment(ctx, command, goBinary, root, run)
	if environmentErr != nil {
		out.Reason = environmentErr.Error()
		return out, facts, environmentErr
	}
	for _, effectiveFlag := range strings.Fields(environment["GOFLAGS"]) {
		if !slices.Contains(flags, effectiveFlag) {
			out.Reason = "make GOFLAGS overrides a saved Go environment input"
			return out, facts, nil
		}
	}
	facts.Version += ";environment=" + Digest(environment) + ";make-goflags=" + Digest(values[4])
	facts.GoFlags = slices.Clone(flags)
	selected := strings.Fields(values[1])
	mixed := strings.Fields(values[2])
	if len(selected) == 0 || len(mixed) == 0 {
		out.Reason = "make package groups are empty"
		return out, facts, nil
	}
	selectedCommand := command
	selectedCommand.Argv = append(append([]string{goBinary, "test"}, flags...), selected...)
	list, err := goListCommand(selectedCommand)
	if err != nil {
		out.Reason = err.Error()
		return out, facts, err
	}
	metadata, err := boundedDiscovery(ctx, list, run)
	if err != nil {
		out.Reason = "make package discovery incomplete"
		return out, facts, err
	}
	inventory, err := GoInventory(metadata, index, commit, root, command.CWD)
	if err != nil || !inventory.Complete {
		return inventory, facts, err
	}
	probe.Argv = append([]string{goBinary, "list"}, mixed...)
	mixedOutput, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		out.Reason = "make mixed package discovery incomplete"
		return out, facts, err
	}
	mixedPackages := strings.Fields(string(mixedOutput))
	facts.MakeGroups = map[string]config.BugCheckCommand{}
	for _, unit := range inventory.Units {
		group := "full-race"
		if slices.Contains(mixedPackages, unit.Selector) {
			group = "plain"
		}
		concrete := command
		concrete.Argv = append([]string{goBinary, "test"}, flags...)
		if group == "full-race" {
			concrete.Argv = append(concrete.Argv, "-race")
		}
		concrete.Argv = append(concrete.Argv, "-timeout="+values[3], unit.Selector)
		key := fmt.Sprintf("%d:%s:go:%s", index, group, unit.Selector)
		if strings.Contains(unit.Key, ":compile:go:") {
			key = fmt.Sprintf("%d:%s:compile:go:%s", index, group, unit.Selector)
		}
		unit.Key = key
		out.Units = append(out.Units, unit)
		facts.MakeGroups[key] = concrete
		if group == "plain" {
			var tests string
			switch path.Base(unit.Selector) {
			case "turn":
				tests = values[5]
			case "store":
				tests = values[6]
			case "cli":
				tests = values[7]
			default:
				out.Reason = "unknown mixed make package"
				return out, facts, nil
			}
			names := strings.Fields(tests)
			if len(names) == 0 {
				out.Reason = "selected race obligation has no known identities"
				return out, facts, nil
			}
			for _, name := range names {
				if strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_") != "" {
					out.Reason = "unsupported selected race identity"
					return out, facts, nil
				}
			}
			race := command
			race.Argv = append([]string{goBinary, "test"}, flags...)
			race.Argv = append(race.Argv, "-race", "-timeout="+values[3], "-run", "^("+strings.Join(names, "|")+")$", unit.Selector)
			raceUnit := unit
			raceUnit.Key = fmt.Sprintf("%d:selected-race:go:%s", index, unit.Selector)
			out.Units = append(out.Units, raceUnit)
			facts.MakeGroups[raceUnit.Key] = race
		}
	}
	if len(out.Units) > MaxInventoryUnits {
		out.Units = nil
		out.Reason = "make inventory exceeds bounds"
		return out, facts, nil
	}
	out.Complete = true
	facts.Dependencies = true
	facts.Structured = true
	facts.Fresh = true
	return out, facts, nil
}

// PythonEnvironmentProbe retains installed version facts, never package index
// URLs or credentials. Editable sources outside the tested snapshot are mutable
// unversioned inputs and cannot support inherited or comparison evidence.
const PythonEnvironmentProbe = `import importlib.metadata
import json
import pathlib
import sys
import urllib.parse

root = pathlib.Path.cwd().resolve()
packages = []
complete = True
for distribution in importlib.metadata.distributions():
    if len(packages) >= 10000:
        complete = False
        break
    packages.append((distribution.metadata.get("Name", ""), distribution.version))
    direct = distribution.read_text("direct_url.json")
    if direct:
        try:
            metadata = json.loads(direct)
            if metadata.get("dir_info", {}).get("editable"):
                selected = pathlib.Path(urllib.parse.unquote(urllib.parse.urlparse(metadata.get("url", "")).path))
                if not selected.resolve().is_relative_to(root):
                    complete = False
        except (ValueError, OSError):
            complete = False
print(json.dumps({"complete": complete, "python": sys.version, "cache_tag": sys.implementation.cache_tag, "packages": sorted(packages)}))
`

// probeGoEnvironment captures only build-affecting Go facts, including saved
// HOME go/env settings that are absent from the declared command environment.
// Unknown flag inputs and external workspaces cannot establish inheritance.
func probeGoEnvironment(ctx context.Context, command config.BugCheckCommand, binary, root string, run DiscoveryRunner) (map[string]string, error) {
	keys := []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK", "GOMOD", "CC", "CXX"}
	probe := command
	probe.Argv = append([]string{binary, "env", "-json"}, keys...)
	raw, err := boundedDiscovery(ctx, probe, run)
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	if len(values) != len(keys) {
		return nil, errors.New("effective go environment schema is incomplete")
	}
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return nil, errors.New("effective go environment fact is missing")
		}
	}
	identity := regexp.MustCompile(`^[a-z0-9_]+$`)
	if !identity.MatchString(values["GOOS"]) || !identity.MatchString(values["GOARCH"]) || values["CGO_ENABLED"] != "0" && values["CGO_ENABLED"] != "1" {
		return nil, errors.New("effective go platform identity is unavailable")
	}
	if values["GOWORK"] != "" && values["GOWORK"] != "off" {
		return nil, errors.New("active go workspace inputs require full execution")
	}
	if err := boundedGoFlags(values["GOFLAGS"]); err != nil {
		return nil, err
	}
	cleanRoot := filepath.Clean(root)
	module := values["GOMOD"]
	if module == "" || !filepath.IsAbs(module) {
		return nil, errors.New("effective go module is unproven")
	}
	relative, err := filepath.Rel(cleanRoot, module)
	if err != nil {
		return nil, err
	}
	if !safeRelative(filepath.ToSlash(relative)) || filepath.Base(relative) != "go.mod" {
		return nil, errors.New("external go module inputs require full execution")
	}
	values["GOMOD"] = "<snapshot>/" + filepath.ToSlash(relative)
	for _, key := range []string{"CC", "CXX"} {
		compiler := values[key]
		if compiler == "" || strings.ContainsAny(compiler, " \t\r\n\x00$`'\"\\") {
			return nil, errors.New("custom go compiler invocation requires full execution")
		}
		// A compiler inside the snapshot is a mutable generated tool input. It is
		// not part of the ordinary source/import graph and cannot be inherited.
		if filepath.IsAbs(compiler) {
			rel, e := filepath.Rel(cleanRoot, compiler)
			if e != nil {
				return nil, e
			}
			if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, errors.New("snapshot-local go compiler requires full execution")
			}
		}
	}
	return values, nil
}

func boundedGoFlags(value string) error {
	if strings.ContainsAny(value, "\x00\r\n$`'\"\\") {
		return errors.New("effective go flags require unknown quoting or dynamic inputs")
	}
	for _, flag := range strings.Fields(value) {
		name, argument, equals := strings.Cut(flag, "=")
		switch name {
		case "-race", "-trimpath", "-asan", "-msan", "-json":
			if equals && argument != "true" && argument != "false" {
				return errors.New("effective go boolean build flag is unknown")
			}
		case "-tags":
			if !equals || !regexp.MustCompile(`^[A-Za-z0-9_,.\-]+$`).MatchString(argument) {
				return errors.New("effective go build tags are unproven")
			}
		case "-mod":
			if !equals || !slices.Contains([]string{"mod", "readonly", "vendor"}, argument) {
				return errors.New("effective go module policy is unproven")
			}
		case "-buildvcs":
			if equals && !slices.Contains([]string{"auto", "true", "false"}, argument) {
				return errors.New("effective go vcs policy is unproven")
			}
		case "-p":
			number, e := strconv.Atoi(argument)
			if !equals || e != nil || number < 1 {
				return errors.New("effective go parallelism is unproven")
			}
		case "-count":
			count, e := strconv.Atoi(argument)
			if !equals || e != nil || count <= 0 {
				return errors.New("effective go test repetition requires full execution")
			}
		default:
			return fmt.Errorf("effective go flag %s requires full execution", name)
		}
	}
	return nil
}
