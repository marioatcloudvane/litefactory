package daemon_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// moduleRoot walks up from the working directory to the directory holding
// go.mod. Go runs a test with its working directory set to the package
// directory, so the walk is short and stable.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory")
		}
		dir = parent
	}
}

// TestNoStaleConcurrencyStatementSurvives is the executable form of
// AC-PAR-8.4: "Every doc sentence and log line in the RD-11 table is either
// true after the change or rewritten. A grep for 'locked by another daemon
// instance' returns nothing false."
//
// Before this feature, a session's git critical section and its `claude`
// phase both ran under the store lock, and a second daemon *process* was the
// only thing that could ever find it held — hence the old line, which fired
// from turn.Execute and daemon/tick.go when a lock acquisition failed. Now
// that turns run concurrently within one process, the far more common
// contender for that lock is a sibling goroutine of the very same daemon,
// which "another daemon instance" describes wrongly on both axes: it is
// neither another daemon (it is this one) nor an instance in the sense that
// phrase meant (a separate `litefactory daemon` process). The corrected line
// says "another daemon process" instead, and RD-11 (feature-spec.md,
// IMPLEMENTATION_PLAN.md) is the inventory of every site that had to change to
// say it.
//
// A prose review checklist does not survive the next engineer who copies the
// old sentence into a new log line because it "sounds right" from a stale doc
// comment sitting nearby; this fails the build instead.
//
// Scope:
//
//   - .git, dist, vendor and .claude are skipped as build/tooling noise, the
//     same set the sibling structural tests in this module skip.
//   - .aiteam is skipped, and deliberately: the plan documents under
//     .aiteam/main/2026-08-30/parallelization-lf/ (feature-spec.md,
//     IMPLEMENTATION_PLAN.md) name the old phrase correctly, because they are
//     the RD-11 inventory *describing* the correction — quoting the very
//     sentence that had to be rewritten elsewhere. CLAUDE.md and
//     .aiteam/AC_AMENDMENTS.md's own header say archived planning documents
//     are historical record and are never rewritten to match the present; a
//     test that forced them to change would enforce the opposite of that
//     convention and would be wrong to exist.
//   - Only .go files are scanned. README.md and CLAUDE.md are the only
//     non-.aiteam Markdown that could carry the phrase, and neither does
//     (verified by hand before this test was written); scanning Markdown
//     brought no exemption logic worth the added surface, so it is left out.
//
// The needle is assembled from two fragments rather than written as one
// string literal, so that this test file is not itself a match for its own
// search — the same technique config.TestForbiddenFlagIsAbsentFromTheTree
// uses for the same reason.
func TestNoStaleConcurrencyStatementSurvives(t *testing.T) {
	t.Parallel()

	needle := strings.Join([]string{"locked by another daemon", "instance"}, " ")
	root := moduleRoot(t)

	// The path of this test file itself, so the scan can skip it: without
	// that exclusion the needle above — which this doc comment necessarily
	// discusses — would make the test fail against itself.
	selfPath, err := filepath.Abs("record_test.go")
	if err != nil {
		t.Fatalf("resolve own path: %v", err)
	}

	scanned := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "vendor", ".claude", ".aiteam":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			abs = path
		}
		if abs == selfPath {
			return nil
		}

		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		if strings.Contains(string(contents), needle) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			t.Errorf("%s contains %q; the daemon is a single process now, so the contender for a "+
				"held lock is a sibling goroutine, not another daemon instance — say \"another daemon "+
				"process\" instead (AC-PAR-8.4)", rel, needle)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	// A structural test that silently passes because it scanned nothing is
	// worse than no test at all: if the tree layout ever changes such that no
	// .go file is found here, that is this test failing to do its job, not
	// evidence the invariant holds.
	if scanned == 0 {
		t.Fatal("scanned zero .go files; the walk or its exclusions are broken")
	}
}

// acceptanceCriterionIDPattern matches a criterion identifier: "AC-" followed
// by any run of characters that are not whitespace, a pipe (the table column
// delimiter) or the closing "**" of a Markdown bold span (the checklist
// delimiter). It deliberately does not anchor on "PAR" — the whole point of
// this test is to catch an identifier that does *not* say PAR.
var acceptanceCriterionIDPattern = regexp.MustCompile(`AC-[^\s|*]+`)

// acCriteriaTable extracts every identifier from the first column of a
// Markdown table ("| ID | Criterion |") inside a section headed exactly
// "### Acceptance Criteria", running until the next heading of level 2 or 3.
// This is IMPLEMENTATION_PLAN.md's format: one such section per user story.
func acCriteriaTable(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var ids []string
	inSection := false
	sawHeaderSeparator := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "### Acceptance Criteria") {
			inSection = true
			sawHeaderSeparator = false
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
			// A different level-2/3 heading ends the section.
			inSection = false
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		// The table's own header row ("| ID | Criterion |") and its
		// separator ("|----|-----------|") are not data; skip past them.
		if !sawHeaderSeparator {
			if strings.Contains(trimmed, "---") {
				sawHeaderSeparator = true
			}
			continue
		}
		cols := strings.Split(trimmed, "|")
		if len(cols) < 2 {
			continue
		}
		id := strings.TrimSpace(cols[1])
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return ids
}

// acCriteriaChecklist extracts every identifier introduced by a checklist
// item ("- [ ] **AC-...** criterion text") inside the section headed
// "## Acceptance Criteria", running until the next level-2 heading. This is
// feature-spec.md's format: one flat section with category subheadings
// ("### Dispatch and concurrency", "### Fairness", ...) that are not
// themselves "Acceptance Criteria" headings and so do not end the section.
//
// It deliberately anchors on the bold span immediately after the checkbox,
// not on any "AC-..." text appearing later in the same line: a criterion's
// prose legitimately mentions other criteria by bold reference (for example
// AC-PAR-8.2's text references **AC-FC-2.6**, a criterion this work amends
// but does not introduce), and those references must not be flagged.
var checklistIDPattern = regexp.MustCompile(`^-\s*\[[ xX]\]\s*\*\*(AC-[^*]+)\*\*`)

func acCriteriaChecklist(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var ids []string
	inSection := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "## Acceptance Criteria" {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			inSection = false
			continue
		}
		if m := checklistIDPattern.FindStringSubmatch(trimmed); m != nil {
			ids = append(ids, m[1])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return ids
}

// TestIntroducedCriteriaCarryTheParPrefix is the executable form of
// AC-PAR-8.7: "All criteria introduced by this work carry the AC-PAR- prefix;
// none reuse a bare AC-N.M identifier."
//
// It parses this feature's own planning documents for the identifiers each
// one *introduces* — the ID column of an Acceptance Criteria table in
// IMPLEMENTATION_PLAN.md, and the bold id leading a checklist item in
// feature-spec.md — and asserts every one starts with "AC-PAR-". It
// deliberately does not grep either file for the bare pattern `AC-\d`: both
// documents legitimately reference pre-existing bare identifiers in prose
// ("this amends AC-2.8", "AC-2.9 is reviewed, unchanged"), and a naive grep
// would flag every one of those as a violation of a rule they do not violate.
func TestIntroducedCriteriaCarryTheParPrefix(t *testing.T) {
	t.Parallel()

	const sessionDir = "main/2026-08-30/parallelization-lf"
	root := moduleRoot(t)
	planPath := filepath.Join(root, ".aiteam", sessionDir, "IMPLEMENTATION_PLAN.md")
	specPath := filepath.Join(root, ".aiteam", sessionDir, "feature-spec.md")

	for _, p := range []string{planPath, specPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("scan target missing: %v", err)
		}
	}

	planIDs := acCriteriaTable(t, planPath)
	specIDs := acCriteriaChecklist(t, specPath)

	// A structural test that silently passes because its parser stopped
	// matching anything is worse than no test at all. This feature introduces
	// roughly 40 criteria across eight stories; a parser that found two would
	// be a broken parser, not a clean tree.
	const minExpected = 30
	if len(planIDs) < minExpected {
		t.Fatalf("parsed only %d criteria from %s; expected at least %d — the table parser is likely broken",
			len(planIDs), planPath, minExpected)
	}
	if len(specIDs) < minExpected {
		t.Fatalf("parsed only %d criteria from %s; expected at least %d — the checklist parser is likely broken",
			len(specIDs), specPath, minExpected)
	}

	check := func(path string, ids []string) {
		for _, id := range ids {
			if !acceptanceCriterionIDPattern.MatchString(id) {
				t.Errorf("%s: %q does not look like an acceptance-criterion id", path, id)
				continue
			}
			if !strings.HasPrefix(id, "AC-PAR-") {
				t.Errorf("%s: criterion id %q does not carry the AC-PAR- prefix (AC-PAR-8.7)", path, id)
			}
		}
	}
	check(planPath, planIDs)
	check(specPath, specIDs)

	// The two documents are expected to introduce the same set of criteria —
	// feature-spec.md states them, IMPLEMENTATION_PLAN.md distributes them
	// across stories — so a mismatch is worth surfacing, though it is not
	// itself a AC-PAR-8.7 violation and so is reported rather than failed.
	if len(planIDs) != len(specIDs) {
		t.Logf("plan lists %d criteria, spec lists %d; counts differ (not itself a prefix violation)",
			len(planIDs), len(specIDs))
	}

	// AC-PAR-8.4 and this test both rest on the amendment ledger's
	// convention of never rewriting archived plans; confirm the ledger itself
	// is present and non-empty, since a missing or empty ledger would mean
	// that convention has nothing behind it.
	ledgerPath := filepath.Join(root, ".aiteam", "AC_AMENDMENTS.md")
	info, err := os.Stat(ledgerPath)
	if err != nil {
		t.Fatalf("amendment ledger missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("amendment ledger %s is empty", ledgerPath)
	}
}
