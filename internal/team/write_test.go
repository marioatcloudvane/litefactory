package team

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// standaloneRender builds a minimal AgentRender for a standalone (non
// root-file) harness target, defaulting SourceHash to HashBytes of a
// caller-supplied source-marker string so tests can vary it independently
// of the rendered body.
func standaloneRender(t *testing.T, harness, agentKey, source, body string) AgentRender {
	t.Helper()
	path, err := standalonePath(harness, agentKey)
	if err != nil {
		t.Fatalf("standalonePath(%q, %q): %v", harness, agentKey, err)
	}
	return AgentRender{
		AgentKey:   agentKey,
		Harness:    harness,
		Teams:      []string{"go-team"},
		SourcePath: source,
		SourceHash: HashBytes([]byte(source)),
		Files: []RenderedFile{
			{Path: path, Bytes: []byte(body)},
		},
	}
}

// rootRender builds a minimal AgentRender targeting a root file
// (RootFileClaude / RootFileOpencode).
func rootRender(t *testing.T, harness, agentKey string, teams []string, source, body string) AgentRender {
	t.Helper()
	var rootFile string
	switch harness {
	case "claude":
		rootFile = RootFileClaude
	case "opencode":
		rootFile = RootFileOpencode
	default:
		t.Fatalf("rootRender: harness %q has no root file", harness)
	}
	return AgentRender{
		AgentKey:   agentKey,
		Harness:    harness,
		Teams:      teams,
		SourcePath: source,
		SourceHash: HashBytes([]byte(source)),
		Files: []RenderedFile{
			{Path: rootFile, Bytes: []byte(body)},
		},
	}
}

func TestAddWritesStandaloneFileAndLockRecord(t *testing.T) {
	root := t.TempDir()
	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Go Architect\n")

	res, err := Add(root, []AgentRender{ar})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(res.Written) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("Written/Skipped = %d/%d, want 1/0", len(res.Written), len(res.Skipped))
	}

	destAbs := filepath.Join(root, ".claude/agents/go-architect.md")
	got, err := os.ReadFile(destAbs)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != "# Go Architect\n" {
		t.Errorf("written content = %q, want %q", got, "# Go Architect\n")
	}

	lf, err := loadLockfile(root)
	if err != nil {
		t.Fatalf("loadLockfile: %v", err)
	}
	if len(lf.Records) != 1 {
		t.Fatalf("lockfile records = %d, want 1", len(lf.Records))
	}
	rec := lf.Records[0]
	if rec.AgentKey != "go-architect" || rec.Harness != "claude" || rec.DestPath != ".claude/agents/go-architect.md" {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.RenderedHash != HashBytes([]byte("# Go Architect\n")) {
		t.Errorf("RenderedHash = %q, want hash of body", rec.RenderedHash)
	}
}

func TestAddIsIdempotentNoOpForStandaloneFile(t *testing.T) {
	root := t.TempDir()
	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Go Architect\n")

	if _, err := Add(root, []AgentRender{ar}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	destAbs := filepath.Join(root, ".claude/agents/go-architect.md")
	info1, err := os.Stat(destAbs)
	if err != nil {
		t.Fatalf("stat after first add: %v", err)
	}

	res, err := Add(root, []AgentRender{ar})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(res.Written) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("Written/Skipped = %d/%d, want 0/1", len(res.Written), len(res.Skipped))
	}
	info2, err := os.Stat(destAbs)
	if err != nil {
		t.Fatalf("stat after second add: %v", err)
	}
	if info1.ModTime() != info2.ModTime() {
		t.Errorf("file was rewritten on a no-op Add: mtime changed from %v to %v", info1.ModTime(), info2.ModTime())
	}
}

func TestAddRewritesWhenBodyChanges(t *testing.T) {
	root := t.TempDir()
	ar1 := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "v1\n")
	if _, err := Add(root, []AgentRender{ar1}); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	ar2 := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "v2\n")
	res, err := Add(root, []AgentRender{ar2})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(res.Written) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("Written/Skipped = %d/%d, want 1/0", len(res.Written), len(res.Skipped))
	}
	got, err := os.ReadFile(filepath.Join(root, ".claude/agents/go-architect.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "v2\n" {
		t.Errorf("content = %q, want %q", got, "v2\n")
	}
}

func TestAddValidationFailureWritesNothing(t *testing.T) {
	root := t.TempDir()
	good := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Good\n")
	bad := AgentRender{
		AgentKey:   "go-architect",
		Harness:    "not-a-real-harness",
		Teams:      []string{"go-team"},
		SourcePath: "src/bad.md",
		SourceHash: HashBytes([]byte("src/bad.md")),
		Files: []RenderedFile{
			{Path: "whatever.md", Bytes: []byte("bad")},
		},
	}

	_, err := Add(root, []AgentRender{good, bad})
	if err == nil {
		t.Fatal("Add succeeded despite an unsupported harness in the batch")
	}
	var uh *UnsupportedHarnessError
	if !errors.As(err, &uh) {
		t.Fatalf("error = %v, want *UnsupportedHarnessError", err)
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-architect.md")); !os.IsNotExist(statErr) {
		t.Errorf("standalone file for the valid render was written despite a later validation failure")
	}
	if _, statErr := os.Stat(filepath.Join(root, litefactoryDir, lockfileName)); !os.IsNotExist(statErr) {
		t.Errorf("lockfile was written despite a validation failure aborting the whole call")
	}
}

func TestAddAggregatesAllValidationFailuresInOneBatch(t *testing.T) {
	root := t.TempDir()
	badHarness := AgentRender{
		AgentKey:   "go-architect",
		Harness:    "not-a-real-harness",
		Teams:      []string{"go-team"},
		SourcePath: "src/bad-harness.md",
		SourceHash: HashBytes([]byte("src/bad-harness.md")),
		Files: []RenderedFile{
			{Path: "whatever.md", Bytes: []byte("bad")},
		},
	}
	badPath := AgentRender{
		AgentKey:   "go-tester",
		Harness:    "claude",
		Teams:      []string{"go-team"},
		SourcePath: "src/go-tester.md",
		SourceHash: HashBytes([]byte("src/go-tester.md")),
		Files: []RenderedFile{
			{Path: ".claude/agents/someone-else.md", Bytes: []byte("nope")},
		},
	}

	_, err := Add(root, []AgentRender{badHarness, badPath})
	if err == nil {
		t.Fatal("Add succeeded despite two validation problems in the batch")
	}

	var uh *UnsupportedHarnessError
	if !errors.As(err, &uh) {
		t.Errorf("errors.As(err, *UnsupportedHarnessError) = false, want true; err = %v", err)
	}
	if !errors.Is(err, ErrPathNotOwned) {
		t.Errorf("errors.Is(err, ErrPathNotOwned) = false, want true; err = %v", err)
	}
	var pnoErr *PathNotOwnedError
	if !errors.As(err, &pnoErr) {
		t.Errorf("errors.As(err, *PathNotOwnedError) = false, want true; err = %v", err)
	}

	wantSubstrings := []string{badHarness.Harness, badPath.Files[0].Path}
	for _, want := range wantSubstrings {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("joined error %q does not mention %q — only the first problem was reported", err.Error(), want)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-tester.md")); !os.IsNotExist(statErr) {
		t.Errorf("standalone file was written despite multiple validation failures")
	}
	if _, statErr := os.Stat(filepath.Join(root, litefactoryDir, lockfileName)); !os.IsNotExist(statErr) {
		t.Errorf("lockfile was written despite multiple validation failures aborting the whole call")
	}
}

func TestAddRejectsUnownedPath(t *testing.T) {
	root := t.TempDir()
	ar := AgentRender{
		AgentKey:   "go-architect",
		Harness:    "claude",
		Teams:      []string{"go-team"},
		SourcePath: "src/go-architect.md",
		SourceHash: HashBytes([]byte("src/go-architect.md")),
		Files: []RenderedFile{
			{Path: ".claude/agents/someone-else.md", Bytes: []byte("nope")},
		},
	}

	_, err := Add(root, []AgentRender{ar})
	if !errors.Is(err, ErrPathNotOwned) {
		t.Fatalf("error = %v, want ErrPathNotOwned", err)
	}
}

func TestAddRejectsLiteralSecret(t *testing.T) {
	root := t.TempDir()
	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md",
		"token: ghp_0123456789abcdefghijklmnopqrstuvwx\n")

	_, err := Add(root, []AgentRender{ar})
	if !errors.Is(err, ErrSecretDetected) {
		t.Fatalf("error = %v, want ErrSecretDetected", err)
	}
	var se *SecretDetectedError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v, want *SecretDetectedError", err)
	}
	if se.Path != ".claude/agents/go-architect.md" {
		t.Errorf("SecretDetectedError.Path = %q, want the standalone owned path", se.Path)
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-architect.md")); !os.IsNotExist(statErr) {
		t.Errorf("file was written despite a detected secret")
	}
}

func TestAddRefusesSymlinkStandaloneTarget(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := filepath.Join(root, "elsewhere.md")
	if err := os.WriteFile(target, []byte("elsewhere"), 0o666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	symPath := filepath.Join(agentsDir, "go-architect.md")
	if err := os.Symlink(target, symPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "body\n")
	_, err := Add(root, []AgentRender{ar})
	if !errors.Is(err, ErrRefusedSymlink) {
		t.Fatalf("error = %v, want ErrRefusedSymlink", err)
	}
}

func TestAddRefusesSymlinkRootFileTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere.md")
	if err := os.WriteFile(target, []byte("elsewhere"), 0o666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, RootFileClaude)); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	ar := rootRender(t, "claude", "go-architect", []string{"go-team"}, "src/go-architect.md", "body\n")
	_, err := Add(root, []AgentRender{ar})
	if !errors.Is(err, ErrRefusedSymlink) {
		t.Fatalf("error = %v, want ErrRefusedSymlink", err)
	}
}

func TestAddUpsertsRootFileSectionAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	ar := rootRender(t, "claude", "go-architect", []string{"go-team"}, "src/go-architect.md", "hello\n")

	res, err := Add(root, []AgentRender{ar})
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if len(res.Written) != 1 {
		t.Fatalf("Written = %d, want 1", len(res.Written))
	}

	content, err := os.ReadFile(filepath.Join(root, RootFileClaude))
	if err != nil {
		t.Fatalf("read root file: %v", err)
	}
	wantBegin := "<!-- litefactory:team BEGIN team=go-team agent=go-architect -->\n"
	if !contains(string(content), wantBegin) {
		t.Errorf("root file content = %q, want it to contain BEGIN line %q", content, wantBegin)
	}
	if !contains(string(content), "hello\n") {
		t.Errorf("root file content = %q, want it to contain body", content)
	}

	// Second, identical Add is a no-op.
	res2, err := Add(root, []AgentRender{ar})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(res2.Written) != 0 || len(res2.Skipped) != 1 {
		t.Fatalf("Written/Skipped = %d/%d, want 0/1", len(res2.Written), len(res2.Skipped))
	}
	content2, err := os.ReadFile(filepath.Join(root, RootFileClaude))
	if err != nil {
		t.Fatalf("read root file after no-op: %v", err)
	}
	if string(content) != string(content2) {
		t.Errorf("root file content changed on a no-op Add:\nbefore: %q\nafter:  %q", content, content2)
	}

	// Third Add with a different body replaces in place.
	ar3 := rootRender(t, "claude", "go-architect", []string{"go-team"}, "src/go-architect.md", "goodbye\n")
	res3, err := Add(root, []AgentRender{ar3})
	if err != nil {
		t.Fatalf("third Add: %v", err)
	}
	if len(res3.Written) != 1 {
		t.Fatalf("Written = %d, want 1", len(res3.Written))
	}
	content3, err := os.ReadFile(filepath.Join(root, RootFileClaude))
	if err != nil {
		t.Fatalf("read root file after replace: %v", err)
	}
	if contains(string(content3), "hello\n") {
		t.Errorf("root file still contains old body after replace: %q", content3)
	}
	if !contains(string(content3), "goodbye\n") {
		t.Errorf("root file does not contain new body after replace: %q", content3)
	}
}

func TestAddDedupsSharedAgentAcrossTeams(t *testing.T) {
	root := t.TempDir()
	// Simulate the caller (internal/teamsource) having already collected
	// every contributing team into one AgentRender's Teams field, per
	// US-18's dedup rule.
	ar := rootRender(t, "claude", "shared-agent", []string{"other-team", "go-team"}, "src/shared-agent.md", "shared body\n")

	res, err := Add(root, []AgentRender{ar})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(res.Written) != 1 {
		t.Fatalf("Written = %d, want 1", len(res.Written))
	}

	content, err := os.ReadFile(filepath.Join(root, RootFileClaude))
	if err != nil {
		t.Fatalf("read root file: %v", err)
	}
	wantBegin := "<!-- litefactory:team BEGIN team=go-team,other-team agent=shared-agent -->\n"
	if !contains(string(content), wantBegin) {
		t.Errorf("root file content = %q, want sorted, comma-joined team list %q", content, wantBegin)
	}
	// Exactly one BEGIN line for this agent — no duplicate section.
	beginCount := countOccurrences(string(content), "agent=shared-agent -->")
	if beginCount != 1 {
		t.Errorf("found %d BEGIN lines for shared-agent, want exactly 1", beginCount)
	}
}

func TestAddMultipleAgentsInSameRootFileDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	ar1 := rootRender(t, "claude", "zeta-agent", []string{"go-team"}, "src/zeta.md", "zeta body\n")
	ar2 := rootRender(t, "claude", "alpha-agent", []string{"go-team"}, "src/alpha.md", "alpha body\n")

	if _, err := Add(root, []AgentRender{ar1, ar2}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, RootFileClaude))
	if err != nil {
		t.Fatalf("read root file: %v", err)
	}
	alphaIdx := indexOf(string(content), "agent=alpha-agent")
	zetaIdx := indexOf(string(content), "agent=zeta-agent")
	if alphaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("expected both sections present in %q", content)
	}
	if alphaIdx > zetaIdx {
		t.Errorf("expected alpha-agent section before zeta-agent section (deterministic sort by agent key), got content: %q", content)
	}
}

func TestAddSerializesConcurrentCalls(t *testing.T) {
	root := t.TempDir()
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ar := standaloneRender(t, "claude", "agent-x", "src/agent-x.md", "same body\n")
			_, err := Add(root, []AgentRender{ar})
			errs[i] = err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Add call %d: %v", i, err)
		}
	}

	lf, err := loadLockfile(root)
	if err != nil {
		t.Fatalf("loadLockfile: %v", err)
	}
	if len(lf.Records) != 1 {
		t.Fatalf("lockfile records = %d, want exactly 1 (no torn/duplicate records from concurrent Add calls)", len(lf.Records))
	}
}

func TestAddWritesAgentToBothStandaloneAndRootFile(t *testing.T) {
	root := t.TempDir()
	path, err := standalonePath("claude", "go-architect")
	if err != nil {
		t.Fatalf("standalonePath: %v", err)
	}
	ar := AgentRender{
		AgentKey:   "go-architect",
		Harness:    "claude",
		Teams:      []string{"go-team"},
		SourcePath: "src/go-architect.md",
		SourceHash: HashBytes([]byte("src/go-architect.md")),
		Files: []RenderedFile{
			{Path: path, Bytes: []byte("standalone body\n")},
			{Path: RootFileClaude, Bytes: []byte("root section body\n")},
		},
	}

	res, err := Add(root, []AgentRender{ar})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(res.Written) != 2 {
		t.Fatalf("Written = %d, want 2 (standalone file + root section)", len(res.Written))
	}

	if _, err := os.Stat(filepath.Join(root, path)); err != nil {
		t.Errorf("standalone file not written: %v", err)
	}
	rootContent, err := os.ReadFile(filepath.Join(root, RootFileClaude))
	if err != nil {
		t.Fatalf("read root file: %v", err)
	}
	if !contains(string(rootContent), "root section body\n") {
		t.Errorf("root file missing section body: %q", rootContent)
	}
}

func TestAddWritesFilesForEveryActiveHarnessInBatch(t *testing.T) {
	root := t.TempDir()
	arClaude := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "claude body\n")
	arCodex := standaloneRender(t, "codex", "go-tester", "src/go-tester.md", "codex body\n")
	arOpencode := standaloneRender(t, "opencode", "go-writer", "src/go-writer.md", "opencode body\n")

	res, err := Add(root, []AgentRender{arClaude, arCodex, arOpencode})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(res.Written) != 3 {
		t.Fatalf("Written = %d, want 3 (one per active harness)", len(res.Written))
	}

	wantFiles := map[string]string{
		".claude/agents/go-architect.md":   "claude body\n",
		".codex/skills/go-tester/SKILL.md": "codex body\n",
		".opencode/agents/go-writer.md":    "opencode body\n",
	}
	for relPath, wantBody := range wantFiles {
		got, err := os.ReadFile(filepath.Join(root, relPath))
		if err != nil {
			t.Fatalf("read %s: %v", relPath, err)
		}
		if string(got) != wantBody {
			t.Errorf("%s content = %q, want %q", relPath, got, wantBody)
		}
	}

	lf, err := loadLockfile(root)
	if err != nil {
		t.Fatalf("loadLockfile: %v", err)
	}
	if len(lf.Records) != 3 {
		t.Fatalf("lockfile records = %d, want 3 (one per harness written)", len(lf.Records))
	}
}

func TestAddLockRecordCarriesSourceHashAndRenderedHash(t *testing.T) {
	root := t.TempDir()
	standaloneAR := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "standalone body\n")
	rootAR := rootRender(t, "opencode", "go-writer", []string{"go-team"}, "src/go-writer.md", "root section body\n")

	if _, err := Add(root, []AgentRender{standaloneAR, rootAR}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	lf, err := loadLockfile(root)
	if err != nil {
		t.Fatalf("loadLockfile: %v", err)
	}

	standaloneRec, found := lf.findRecord(LockRecord{Harness: "claude", AgentKey: "go-architect", DestPath: ".claude/agents/go-architect.md"})
	if !found {
		t.Fatal("no lockfile record found for the standalone artifact")
	}
	if standaloneRec.SourceHash != standaloneAR.SourceHash {
		t.Errorf("standalone SourceHash = %q, want %q (ar.SourceHash)", standaloneRec.SourceHash, standaloneAR.SourceHash)
	}
	if standaloneRec.SourcePath != standaloneAR.SourcePath {
		t.Errorf("standalone SourcePath = %q, want %q", standaloneRec.SourcePath, standaloneAR.SourcePath)
	}
	if standaloneRec.RenderedHash != HashBytes([]byte("standalone body\n")) {
		t.Errorf("standalone RenderedHash = %q, want hash of written body", standaloneRec.RenderedHash)
	}

	rootRec, found := lf.findRecord(LockRecord{Harness: "opencode", AgentKey: "go-writer", DestPath: RootFileOpencode})
	if !found {
		t.Fatal("no lockfile record found for the root-file section artifact")
	}
	if rootRec.SourceHash != rootAR.SourceHash {
		t.Errorf("root-section SourceHash = %q, want %q (ar.SourceHash)", rootRec.SourceHash, rootAR.SourceHash)
	}
	if rootRec.SourcePath != rootAR.SourcePath {
		t.Errorf("root-section SourcePath = %q, want %q", rootRec.SourcePath, rootAR.SourcePath)
	}
	wantRenderedHash := markerSpanHash([]string{"go-team"}, "go-writer", []byte("root section body\n"))
	if rootRec.RenderedHash != wantRenderedHash {
		t.Errorf("root-section RenderedHash = %q, want markerSpanHash of the marker span, %q", rootRec.RenderedHash, wantRenderedHash)
	}
}

func TestAddConcurrentCallsForDifferentAgentsBothPersist(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		ar := standaloneRender(t, "claude", "agent-a", "src/agent-a.md", "body a\n")
		_, err := Add(root, []AgentRender{ar})
		errs[0] = err
	}()
	go func() {
		defer wg.Done()
		ar := standaloneRender(t, "claude", "agent-b", "src/agent-b.md", "body b\n")
		_, err := Add(root, []AgentRender{ar})
		errs[1] = err
	}()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Add call %d: %v", i, err)
		}
	}

	lf, err := loadLockfile(root)
	if err != nil {
		t.Fatalf("loadLockfile: %v", err)
	}
	if len(lf.Records) != 2 {
		t.Fatalf("lockfile records = %d, want exactly 2 (no lost update between two concurrent Add calls for different agents)", len(lf.Records))
	}
	if _, found := lf.findRecord(LockRecord{Harness: "claude", AgentKey: "agent-a", DestPath: ".claude/agents/agent-a.md"}); !found {
		t.Error("missing lockfile record for agent-a after concurrent Add calls")
	}
	if _, found := lf.findRecord(LockRecord{Harness: "claude", AgentKey: "agent-b", DestPath: ".claude/agents/agent-b.md"}); !found {
		t.Error("missing lockfile record for agent-b after concurrent Add calls")
	}
}

func TestAddNoOpRenderedHashUnaffectedByChangesOutsideMarkerSpan(t *testing.T) {
	root := t.TempDir()
	ar := rootRender(t, "claude", "go-architect", []string{"go-team"}, "src/go-architect.md", "hello\n")

	if _, err := Add(root, []AgentRender{ar}); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	// Simulate a human editing content outside the marker span: append
	// unrelated content after the section this package wrote.
	rootPath := filepath.Join(root, RootFileClaude)
	before, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("read root file: %v", err)
	}
	edited := append(append([]byte{}, before...), []byte("\nUnrelated human-written notes.\n")...)
	if err := os.WriteFile(rootPath, edited, 0o666); err != nil {
		t.Fatalf("simulate external edit: %v", err)
	}

	// Re-running Add with the exact same render must still be a no-op: the
	// recorded RenderedHash is computed only over the marker span, so a
	// change outside it must not be seen as a change to this record.
	res, err := Add(root, []AgentRender{ar})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(res.Written) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("Written/Skipped = %d/%d, want 0/1 (content outside the marker span must not affect the no-op decision)", len(res.Written), len(res.Skipped))
	}

	after, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("read root file after no-op Add: %v", err)
	}
	if !contains(string(after), "Unrelated human-written notes.\n") {
		t.Errorf("human-written content outside the marker span was lost by a no-op Add: %q", after)
	}
}

func TestAddSharedAgentSectionUpdatesTeamListAcrossSequentialAdds(t *testing.T) {
	root := t.TempDir()
	ar1 := rootRender(t, "claude", "shared-agent", []string{"go-team"}, "src/shared-agent.md", "shared body\n")

	if _, err := Add(root, []AgentRender{ar1}); err != nil {
		t.Fatalf("first Add: %v", err)
	}

	// A second team starts contributing the same shared agent; the caller
	// is expected to have already merged both teams into Teams (US-18's
	// dedup rule), simulated here across two sequential Add calls.
	ar2 := rootRender(t, "claude", "shared-agent", []string{"go-team", "other-team"}, "src/shared-agent.md", "shared body\n")
	res2, err := Add(root, []AgentRender{ar2})
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if len(res2.Written) != 1 {
		t.Fatalf("Written = %d, want 1 (team list change must be treated as a change, not a no-op)", len(res2.Written))
	}

	content, err := os.ReadFile(filepath.Join(root, RootFileClaude))
	if err != nil {
		t.Fatalf("read root file: %v", err)
	}
	beginCount := countOccurrences(string(content), "agent=shared-agent -->")
	if beginCount != 1 {
		t.Fatalf("found %d BEGIN lines for shared-agent after team joined, want exactly 1 (single section, updated in place)", beginCount)
	}
	wantBegin := "<!-- litefactory:team BEGIN team=go-team,other-team agent=shared-agent -->\n"
	if !contains(string(content), wantBegin) {
		t.Errorf("root file content = %q, want updated team list %q", content, wantBegin)
	}
}

// --- small local string helpers, to avoid pulling in strings just for
// substring/count/index checks used only by a handful of assertions above.

func contains(s, substr string) bool {
	return indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			count++
			i += len(substr) - 1
		}
	}
	return count
}
