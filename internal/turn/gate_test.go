package turn

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// gateTimeout bounds every rendezvous in this file. A gate bug is a deadlock,
// and a deadlock reported as "panic: test timed out after 10m" names no test
// and no line. Failing on our own clock keeps the message useful.
const gateTimeout = testutil.Wait

// testGate is a real per-clone gate with an assertion harness bolted on.
//
// It is not a stand-in that always succeeds: it is the same 1-buffered channel
// per path that internal/daemon uses, because a fixture whose gate granted
// everything immediately would let every test in this package pass over a
// serialisation that production does not have. internal/turn cannot import
// internal/daemon — the arrow points the other way, and that is the whole
// ownership decision this feature rests on — so the semantics are restated
// here, and internal/daemon's own gate_internal_test.go tests the real one.
//
// The instrumentation is what the acceptance criteria are read off:
//
//   - held/overlaps prove mutual exclusion per path (AC-PAR-3.1).
//   - deadlined proves no caller wrapped a critical section in a timeout
//     (AC-PAR-3.6).
//   - acquisitions counts how many times each clone was entered, which is what
//     distinguishes "one section" from "one section per git command".
type testGate struct {
	mu           sync.Mutex
	gates        map[string]chan struct{}
	held         map[string]int
	acquisitions map[string]int
	overlaps     int
	deadlined    int
}

func newTestGate() *testGate {
	return &testGate{
		gates:        make(map[string]chan struct{}),
		held:         make(map[string]int),
		acquisitions: make(map[string]int),
	}
}

func (g *testGate) Acquire(ctx context.Context, path string) (func(), error) {
	gate := g.channel(path)

	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	g.mu.Lock()
	if _, ok := ctx.Deadline(); ok {
		g.deadlined++
	}
	g.acquisitions[path]++
	g.held[path]++
	if g.held[path] > 1 {
		g.overlaps++
	}
	g.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.held[path]--
			g.mu.Unlock()
			<-gate
		})
	}, nil
}

func (g *testGate) channel(path string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()

	gate, ok := g.gates[path]
	if !ok {
		gate = make(chan struct{}, 1)
		g.gates[path] = gate
	}
	return gate
}

// report is the gate's state, copied so a caller reads it without racing the
// goroutines that wrote it.
func (g *testGate) report() (acquisitions map[string]int, overlaps, deadlined int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	acquisitions = make(map[string]int, len(g.acquisitions))
	for path, n := range g.acquisitions {
		acquisitions[path] = n
	}
	return acquisitions, g.overlaps, g.deadlined
}

// TestExecuteRequiresACloneGate pins the decision that the gate is a required
// dependency rather than a nil-tolerant no-op.
//
// A nil gate degrading to "do not serialise" would be a silent regression: the
// measured failure rates it prevents are 0.75% for "worktree add" and 75% for a
// fetch against moved refs, so a build that lost the wiring would look healthy
// for a long time and then post fatal git errors on the issue that read as
// flakes. There is one production wiring point, so refusing the zero value
// costs nothing and turns forgetting it into a startup error (AC-PAR-3.1).
func TestExecuteRequiresACloneGate(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("claude ran for a turn with no clone gate")
		return nil, errors.New("must not be reached")
	}))

	opts := f.opts
	opts.CloneGate = nil
	if _, err := Execute(t.Context(), opts, f.request()); err == nil {
		t.Fatal("Execute with no clone gate = nil error, want ErrInvalidOptions")
	} else if !strings.Contains(err.Error(), "clone gate") {
		t.Errorf("err = %v, want it to name the missing clone gate", err)
	}
}

// TestCriticalSectionsAreSingleAcquisitions is the structural half of
// AC-PAR-3.3, in the style of internal/config/purity_test.go.
//
// A behavioural test for "the gate was not released between these two calls" is
// awkward to write and easy to write wrongly. What is actually being defended
// is a shape: the git of a turn is a small, named, registered set of critical
// sections, each one acquisition with one deferred release, and every git call
// that must be serialised is called from inside one of them and from nowhere
// else in the package. That is a property of the syntax tree, so it is asserted
// against the syntax tree.
//
// # Registered sections
//
// prepareClone and commitAndPush own ordinary turn preparation and saving.
// Bug adds separate lifecycle operations: reopening or restoring a worktree,
// fetching a fresh target without
// merging it into approved source, and publishing an already saved immutable
// repair without committing or relaunching a fixer. Each owns one acquisition.
// Managed Plan publication continues to use the normal save path.
//
// # Where the teeth are
//
// Generalising the model must not soften it, so the assertions are per call
// rather than per section:
//
//   - Each gated call is allowed in a named set of sections and refused
//     everywhere else. A new, ungated git write section fails here on its
//     function name.
//   - Each gated call must appear in *every* section that claims it, so a
//     section that quietly stops covering one is a failure rather than a
//     silently narrowed guarantee.
//   - The package-wide acquisition count must equal the number of registered
//     sections, so an additional acquisition fails until someone adds it to
//     the sections map below on purpose and writes down why.
//
// If this fails, someone either added an ungated section or split an existing
// one — most likely by gating each git call separately, which reintroduces the
// resolveBranchStart-then-worktree-add window the section exists to close.
func TestCriticalSectionsAreSingleAcquisitions(t *testing.T) {
	t.Parallel()

	// sections is every function in this package that may take the clone gate.
	sections := map[string]struct{}{
		"prepareClone": {},
		// Worker gate setup fetches, allocates and initializes its retained
		// checkout under one clone acquisition before reusable synchronization.
		"implementRuntime.prepareWorkerSyncCheckout": {},
		"sessionSyncCheckout.withCheckout":           {},
		"BugRuntime.syncBaseBranch":                  {},
		// Observe an unpublished Bug branch before opening its base scratch,
		// releasing this section before the scratch takes its own gate.
		"sessionGraphSyncRepo.initialBugSyncBranch": {},
		"commitAndPush": {},
		// Standalone doctor sync releases its snapshot section before advisory work,
		// then revalidates and performs merge, publication and rollback in one section.
		"prepareDoctorSync": {},
		"applyDoctorSync":   {},
		// Bug delivery snapshots and applies validated advice in disposable scratch.
		"prepareBugDeliveryConflict": {},
		"applyBugDeliveryConflict":   {},
		// A repair attempt merges fetched main into the idle task worktree,
		// and a Bug check adds and later removes the detached target
		// worktree its failing tests rerun on; each is one acquisition.
		// Simplified Bug preserves dirty work locally and synchronizes after
		// normal fetched preparation, releasing this section before invocation.
		"PrepareGraphBugSource":        {},
		"BugRuntime.saveGraphDelivery": {},
		"BugRuntime.pushGraphDelivery": {},
		// The Implement merge builds, publishes and removes its scratch tree
		// on the shared clone; each is one acquisition.
		"beginImplementScratch": {},
		"gatedScratch.Publish":  {},
		"gatedScratch.Close":    {},
		// The Validate suite checkout fetches and adds its scratch worktree in
		// one acquisition, and removes it in another.
		"GitValidateSuiteRepo.Begin": {},
		"gitSuiteScratch.Close":      {},
		// Validate pins the integration head: one fetch and one read, in one
		// acquisition.
		"IntegrationHead": {},
		// Validate's host bindings: the task worktree is created or reused,
		// a kept repair is committed, the tested commit is pushed and a story
		// branch is fetched and fast-forwarded to it, each in one acquisition.
		"validateHost.checkoutTaskWorktree": {},
		"validateHost.keepChange":           {},
		"validateHost.publish":              {},
		"validateHost.advanceStory":         {},
	}

	// gated names each call that must not happen outside a critical section,
	// mapped to the sections it may appear in.
	gated := map[string][]string{
		"fetch":          {"prepareClone"},
		"freezeSlug":     {"prepareClone", "implementRuntime.prepareWorkerSyncCheckout"},
		"ensureWorktree": {"prepareClone"},
		// syncBase runs inside the existing prepareClone gate, not a new
		// section — TEST-10. It is the fourth step of that critical section,
		// after ensureWorktree, and takes no acquisition of its own — but
		// only for an ordinary turn. It does not run at all when a confirmed
		// proposal is being applied (CR-B-003): see WriteResolvedFiles below.
		"syncBase": {"prepareClone", "PrepareGraphBugSource"},
		"syncTask": {"prepareClone", "PrepareGraphBugSource"},
		"Commit":   {"commitAndPush", "BugRuntime.saveGraphDelivery", "PrepareGraphBugSource", "validateHost.keepChange"},
		// WriteResolvedFiles, StageResolvedFiles and CompleteMerge are US-12's
		// three new git.Worktree mutators. They run as prepareClone's fourth
		// step, in place of syncBase (which this turn shape never calls),
		// only when a confirmed proposal is being applied — the same TEST-10
		// shape syncBase already has, and for the same reason: there is no
		// claude run between ensureWorktree and this step for the agentless
		// apply turn, so nothing needs the gate released and retaken.
		"WriteResolvedFiles": {"prepareClone", "applyDoctorSync", "applyBugDeliveryConflict"},
		"StageResolvedFiles": {"prepareClone", "applyDoctorSync", "applyBugDeliveryConflict"},
		"CompleteMerge":      {"prepareClone", "applyDoctorSync", "applyBugDeliveryConflict"},
		// Confirmed proposal application also pushes inside prepareClone's
		// existing acquisition.
		"Push": {"commitAndPush", "prepareClone", "BugRuntime.pushGraphDelivery", "applyDoctorSync", "implementRuntime.prepareWorkerSyncCheckout"},
	}

	funcs := packageFuncs(t)

	// Map iteration order is not stable, and a failure message list that
	// reorders between runs is a failure message that is harder to diff.
	for _, name := range slices.Sorted(maps.Keys(sections)) {
		fn, ok := funcs[name]
		if !ok {
			t.Fatalf("%s is not declared in package turn; every critical section is a named function on purpose", name)
		}

		if got := countCalls(fn.decl, "Acquire"); got != 1 {
			t.Errorf("%s makes %d gate acquisitions, want exactly 1", name, got)
		}
		deferred, immediate := countReleases(fn.decl)
		if deferred != 1 {
			t.Errorf("%s defers release() %d times, want exactly 1", name, deferred)
		}
		if immediate != 0 {
			t.Errorf("%s calls release() %d times outside a defer, want 0: "+
				"a release that is not deferred is a release an early return skips", name, immediate)
		}
	}

	for _, call := range slices.Sorted(maps.Keys(gated)) {
		allowed := make(map[string]bool, len(gated[call]))
		for _, name := range gated[call] {
			if _, ok := sections[name]; !ok {
				t.Fatalf("%s is allowed to call %s but is not a registered section; "+
					"a call may only be permitted inside a section this test also checks", name, call)
			}
			allowed[name] = true
		}

		for _, other := range slices.Sorted(maps.Keys(funcs)) {
			calls := countCalls(funcs[other].decl, call)
			if allowed[other] {
				if calls == 0 {
					t.Errorf("%s does not call %s; the section no longer covers it", other, call)
				}
				continue
			}
			if calls > 0 {
				t.Errorf("%s calls %s, but %s may only be called from inside %s: "+
					"moving it out of a critical section — or adding a new, ungated one — "+
					"is what this test exists to catch",
					other, call, call, strings.Join(gated[call], " or "))
			}
		}
	}

	// One acquisition per registered section, and no further acquisition hiding
	// anywhere else in the package.
	total := 0
	for _, fn := range funcs {
		total += countCalls(fn.decl, "Acquire")
	}
	if total != len(sections) {
		t.Errorf("package turn acquires the clone gate %d times, want exactly %d "+
			"(one each for %s)", total, len(sections),
			strings.Join(slices.Sorted(maps.Keys(sections)), ", "))
	}
}

// TestTurnImposesNoAmbientDeadline is AC-PAR-3.6's structural half.
//
// Every git command starts its own deadline when it execs, in
// internal/git/exec.go. That is only worth anything if no caller wraps a
// critical section in a deadline of its own, because a turn that waited 50
// seconds for the gate would then hand a 60-second fetch ten seconds to run —
// and would do it worst on exactly the busy clone where the wait is longest.
// The behavioural half is asserted by [testGate], which counts acquisitions
// made under a context that already carries a deadline.
func TestTurnImposesNoAmbientDeadline(t *testing.T) {
	t.Parallel()

	for name, fn := range packageFuncs(t) {
		for _, forbidden := range []string{"WithTimeout", "WithDeadline"} {
			// The one deliberate exception: an agent node that declares a time
			// budget (lf_budget) is cancelled when the budget runs out. The deadline
			// bounds the agent process only, and never encloses a git command or the
			// clone gate: the agent runs outside the gate and every git command
			// starts its own deadline at exec time.
			if name == "AgentActivity.execute" && forbidden == "WithTimeout" {
				continue
			}
			// The same holds for the doctor sync conflict advisor: its budget
			// bounds the advisor run only, which happens between the two gate
			// acquisitions and before any git write, so the merge, recheck and
			// push fit inside the repair broker's tool deadline.
			if name == "SyncDoctorBranch" && forbidden == "WithTimeout" {
				continue
			}
			if countCalls(fn.decl, forbidden) > 0 {
				t.Errorf("%s calls context.%s; internal/turn imposes no deadline of its own, "+
					"because git commands start theirs at exec time (AC-PAR-3.6)", name, forbidden)
			}
		}
	}
}

// TestClaudeRunsOutsideTheCloneGate is AC-PAR-3.2, and it is the criterion that
// says this feature was worth building.
//
// Two turns on the *same* clone are made to arrive inside their claude run and
// wait for each other. If either critical section enclosed the run, the second
// turn could not reach claude until the first had finished entirely, and this
// test would sit at the barrier until gateTimeout — which is the failure being
// tested for, stated as a deadlock rather than as a slow test.
func TestClaudeRunsOutsideTheCloneGate(t *testing.T) {
	t.Parallel()

	const otherIssue = 124

	var home string
	arrived := make(chan struct{}, 2)
	proceed := make(chan struct{})
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		arrived <- struct{}{}
		<-proceed
		return succeeded(req, home), nil
	})

	f := newFixture(t, runner)
	home = f.paths.Home()
	gate, ok := f.opts.CloneGate.(*testGate)
	if !ok {
		t.Fatalf("fixture gate is %T, want *testGate", f.opts.CloneGate)
	}
	other := f.otherSession(otherIssue)

	results := make(chan Result, 2)
	var wg sync.WaitGroup
	for _, req := range []Request{f.request(), f.requestFor(other, otherIssue, "Add a /ready endpoint", 9002)} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// t.Errorf rather than t.Fatalf: a Fatalf from a goroutine that
			// is not the test's own stops that goroutine and leaves the
			// barrier half open.
			result, err := Execute(t.Context(), f.opts, req)
			if err != nil {
				t.Errorf("Execute for issue %d: %v", req.Issue.Number, err)
			}
			results <- result
		}()
	}

	// Both turns inside claude at one instant. Reaching this point at all is
	// the criterion; the timeout is the failure.
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(gateTimeout):
			close(proceed)
			wg.Wait()
			t.Fatalf("only %d of 2 turns reached claude within %v; "+
				"a critical section is holding the clone gate across the run", i, gateTimeout)
		}
	}
	close(proceed)
	wg.Wait()
	close(results)

	for result := range results {
		if result.Disposition != DispositionRan {
			t.Errorf("issue %d: disposition = %v (%s), want DispositionRan",
				result.Session.IssueNumber, result.Disposition, result.Reason)
		}
	}

	acquisitions, overlaps, deadlined := gate.report()
	if overlaps != 0 {
		t.Errorf("the gate was held by two turns at once %d times", overlaps)
	}
	if deadlined != 0 {
		t.Errorf("%d acquisitions carried an ambient deadline, want 0 (AC-PAR-3.6)", deadlined)
	}
	// Two turns, one clone, two sections each: prepareClone and commitAndPush.
	if got := acquisitions[f.storeRepo.Path]; got != 4 {
		t.Errorf("the clone at %s was entered %d times, want 4 (two turns × two sections)",
			f.storeRepo.Path, got)
	}
	if len(acquisitions) != 1 {
		t.Errorf("acquisitions span %d clones, want 1", len(acquisitions))
	}
}

// TestConcurrentTurnsAgainstTwoClones is AC-PAR-3.5: two repositories, three
// issues each, six turns at once, one process-wide gate.
//
// It runs under -race like everything else, and it also reads what git said.
// The three strings below are the exact failures the spike measured at
// parallelism 4 and are the reason the gate exists; each of them reaches the
// result as a reportable failure and the log as an error line, so the check is
// over both.
//
// This is the level the plan asks the criterion to be driven at. The daemon
// dispatching turns concurrently is a later task; a gate that only works once
// the daemon is concurrent could not be developed, and [Execute] is a function
// of its arguments precisely so that it can be called from six goroutines here.
func TestConcurrentTurnsAgainstTwoClones(t *testing.T) {
	t.Parallel()

	issues := []struct {
		number  int
		title   string
		comment int64
	}{
		{123, testIssueTitle, testComment},
		{124, "Add a /ready endpoint", 9002},
		{125, "Add a /metrics endpoint", 9003},
	}

	// One registry for the whole process is what production has, so the two
	// fixtures share it. Their clone paths differ, so the two repositories must
	// not wait for each other — asserted below.
	gate := newTestGate()

	// One runner per fixture, so that the run result names the log path under
	// that fixture's own $LF_HOME.
	newRunner := func(home string) *stubRunner {
		return newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
			if err := os.WriteFile(
				filepath.Join(req.WorktreePath, "NOTES.md"),
				[]byte("run "+req.Ref.String()+"\n"),
				0o600,
			); err != nil {
				return nil, err
			}
			return succeeded(req, home), nil
		})
	}

	type turnCase struct {
		fixture *fixture
		request Request
	}
	var cases []turnCase
	fixtures := []*fixture{newFixture(t, nil), newFixture(t, nil)}
	for _, f := range fixtures {
		f.opts.CloneGate = gate
		f.opts.Runners = runnerSet(newRunner(f.paths.Home()))
		for _, issue := range issues {
			session := f.session
			if issue.number != testIssue {
				session = f.otherSession(issue.number)
			}
			cases = append(cases, turnCase{
				fixture: f,
				request: f.requestFor(session, issue.number, issue.title, issue.comment),
			})
		}
	}

	results := make([]Result, len(cases))
	errs := make([]error, len(cases))
	var wg sync.WaitGroup
	for i, c := range cases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = Execute(context.Background(), c.fixture.opts, c.request)
		}()
	}
	wg.Wait()

	// The failures the gate exists to prevent, verbatim from the measurement.
	contention := []string{
		"cannot lock ref",
		"failed to read .git/worktrees/",
		"could not lock config file",
	}

	for i, c := range cases {
		issue := c.request.Issue.Number
		if errs[i] != nil {
			t.Fatalf("issue %d in %s: Execute: %v", issue, c.fixture.storeRepo.Path, errs[i])
		}
		result := results[i]
		if result.Disposition != DispositionRan {
			t.Errorf("issue %d in %s: disposition = %v, reason = %q",
				issue, c.fixture.storeRepo.Path, result.Disposition, result.Reason)
		}
		if !result.Pushed {
			t.Errorf("issue %d in %s: not pushed (err = %v, reason = %q)",
				issue, c.fixture.storeRepo.Path, result.Err, result.Reason)
		}
		for _, phrase := range contention {
			if strings.Contains(result.Reason, phrase) {
				t.Errorf("issue %d: result reports git contention %q: %s", issue, phrase, result.Reason)
			}
			if result.Err != nil && strings.Contains(result.Err.Error(), phrase) {
				t.Errorf("issue %d: result error reports git contention %q: %v", issue, phrase, result.Err)
			}
		}
	}

	for _, f := range fixtures {
		logs := f.logs.String()
		for _, phrase := range contention {
			if strings.Contains(logs, phrase) {
				t.Errorf("%s: the turn log reports git contention %q", f.storeRepo.Path, phrase)
			}
		}
	}

	acquisitions, overlaps, deadlined := gate.report()
	if overlaps != 0 {
		t.Errorf("the gate was held by two turns at once %d times", overlaps)
	}
	if deadlined != 0 {
		t.Errorf("%d acquisitions carried an ambient deadline, want 0 (AC-PAR-3.6)", deadlined)
	}
	if len(acquisitions) != len(fixtures) {
		t.Errorf("acquisitions span %d clones, want %d: the gate is keyed on the clone path",
			len(acquisitions), len(fixtures))
	}
	// Every turn enters prepareClone and commitAndPush once.
	for _, f := range fixtures {
		if got := acquisitions[f.storeRepo.Path]; got != 2*len(issues) {
			t.Errorf("the clone at %s was entered %d times, want %d (three turns × two sections)",
				f.storeRepo.Path, got, 2*len(issues))
		}
	}
}

// funcDecl is a top-level function of this package with the file it came from.
type funcDecl struct {
	file string
	decl *ast.FuncDecl
}

// packageFuncs parses every non-test file in the package and returns its
// top-level functions and methods by name. Method names are qualified with
// their receiver type so a method cannot silently answer for a function.
func packageFuncs(t *testing.T) map[string]funcDecl {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	funcs := make(map[string]funcDecl)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			key := fn.Name.Name
			if fn.Recv != nil {
				key = receiverName(fn.Recv) + "." + fn.Name.Name
			}
			funcs[key] = funcDecl{file: name, decl: fn}
		}
	}
	if len(funcs) == 0 {
		t.Fatal("no functions found in package turn; the parse is not doing what it looks like")
	}
	return funcs
}

// receiverName is the type name a method hangs off, without the pointer.
func receiverName(recv *ast.FieldList) string {
	if len(recv.List) == 0 {
		return ""
	}
	expr := recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// countCalls counts calls to a function or method of the given name inside fn,
// whether it is called bare or through a selector.
func countCalls(fn *ast.FuncDecl, name string) int {
	count := 0
	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if fun.Name == name {
				count++
			}
		case *ast.SelectorExpr:
			if fun.Sel.Name == name {
				count++
			}
		}
		return true
	})
	return count
}

// countReleases counts calls to release() inside fn, split by whether they are
// deferred. The distinction is the point: a release that is not deferred is one
// an early return can skip, and a skipped release wedges the clone for the life
// of the process.
func countReleases(fn *ast.FuncDecl) (deferred, immediate int) {
	deferredCalls := make(map[*ast.CallExpr]struct{})
	ast.Inspect(fn, func(node ast.Node) bool {
		if stmt, ok := node.(*ast.DeferStmt); ok {
			deferredCalls[stmt.Call] = struct{}{}
		}
		return true
	})

	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "release" {
			return true
		}
		if _, isDeferred := deferredCalls[call]; isDeferred {
			deferred++
		} else {
			immediate++
		}
		return true
	})
	return deferred, immediate
}

// otherSession registers a second task on this fixture's repository, which is
// what makes "two turns on the same clone" expressible.
func (f *fixture) otherSession(issue int) store.Session {
	f.t.Helper()

	session, err := f.db.GetOrCreateSession(f.t.Context(), f.storeRepo, issue)
	if err != nil {
		f.t.Fatalf("create session for issue %d: %v", issue, err)
	}
	return session
}

// requestFor is [fixture.request] for an arbitrary task on this fixture.
func (f *fixture) requestFor(session store.Session, issue int, title string, comment int64) Request {
	f.t.Helper()

	body := fmt.Sprintf("/implement work on issue %d", issue)
	return Request{
		Mode:      agent.ModeImplement,
		Agent:     agent.Kind(store.AgentClaude),
		SessionID: session.ID,
		Issue: Issue{
			Number: issue,
			Title:  title,
			Body:   "The service has no way to say whether it is up.",
			Author: "octo-reporter",
		},
		Trigger: Trigger{
			CommentID: comment,
			Body:      body,
			Author:    "marioatcloudvane",
		},
		Command:      ParseCommand(body),
		ToolProfile:  "implement",
		Tools:        []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task"},
		AllowedTools: config.DefaultClaude().AllowedTools,
	}
}
