package config

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAgentTiersMatchClaudeAgentsFrontmatter is T-6.6 / AC-6.3: it fails when
// a .claude/agents/*.md file's model: value disagrees with
// ModelByTier[claude][tier] for the tier .aiteam/AGENT_TIERS.md assigns that
// specialist, and it fails when a specialist has no assignment at all
// (AC-6.2 requires exactly one tier per file).
//
// This is the one test in the tree that keeps the two halves of US-6's
// bookkeeping honest: neither .claude/agents/*.md nor .aiteam/AGENT_TIERS.md
// is read by litefactory at runtime, so nothing else would ever notice the
// two drifting apart.
func TestAgentTiersMatchClaudeAgentsFrontmatter(t *testing.T) {
	root := moduleRoot(t)
	agentsDir := filepath.Join(root, ".claude", "agents")
	tiersDoc := filepath.Join(root, ".aiteam", "AGENT_TIERS.md")

	assignments := parseAgentTiers(t, tiersDoc)

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("read %s: %v", agentsDir, err)
	}

	byTier := Defaults().Tiers.ModelByTier[KindClaude]
	if len(byTier) == 0 {
		t.Fatal("Defaults().Tiers.ModelByTier[claude] is empty; nothing to check against")
	}

	seen := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		seen++

		tier, ok := assignments[name]
		if !ok {
			t.Errorf("%s: .aiteam/AGENT_TIERS.md assigns it no tier (AC-6.2)", name)
			continue
		}
		if !tier.Valid() {
			t.Errorf("%s: .aiteam/AGENT_TIERS.md assigns tier %q, which is not one of flagship, balanced, fast", name, tier)
			continue
		}

		wantModel, ok := byTier[tier]
		if !ok {
			t.Errorf("%s: assigned tier %q has no entry in ModelByTier[claude]", name, tier)
			continue
		}

		gotModel, err := frontmatterModel(filepath.Join(agentsDir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if gotModel != wantModel {
			t.Errorf("%s: frontmatter model: %q disagrees with ModelByTier[claude][%s] = %q",
				name, gotModel, tier, wantModel)
		}
	}

	if seen == 0 {
		t.Fatal("no .md files found under .claude/agents; the glob or the fixture is broken")
	}

	// AC-6.2 also runs the other direction: every row in AGENT_TIERS.md must
	// name a file that actually exists, so a specialist that was renamed or
	// removed cannot leave a stale, uncheckable assignment behind.
	for name := range assignments {
		if _, err := os.Stat(filepath.Join(agentsDir, name)); err != nil {
			t.Errorf(".aiteam/AGENT_TIERS.md assigns a tier to %q, which does not exist under .claude/agents", name)
		}
	}
}

// agentTierRowRE matches one row of AGENT_TIERS.md's assignment table:
// | `some-file.md` | tier | model |
var agentTierRowRE = regexp.MustCompile("^\\|\\s*`([^`]+\\.md)`\\s*\\|\\s*([a-z]+)\\s*\\|")

// parseAgentTiers reads the specialist→tier table out of .aiteam/AGENT_TIERS.md.
// It is a narrow, literal parser for that one table shape — not a markdown
// parser — because that table is the only thing this test needs from the
// file.
func parseAgentTiers(t *testing.T, path string) map[string]Tier {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	assignments := make(map[string]Tier)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		match := agentTierRowRE.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		assignments[match[1]] = Tier(match[2])
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if len(assignments) == 0 {
		t.Fatalf("%s: no assignment rows found; is the table shape still \"| `file.md` | tier | ... |\"?", path)
	}
	return assignments
}

// frontmatterModelRE matches the model: line inside a specialist's YAML
// frontmatter, e.g. "model: sonnet".
var frontmatterModelRE = regexp.MustCompile(`^model:\s*(\S+)\s*$`)

// frontmatterModel extracts the model: value from a .claude/agents/*.md
// file's frontmatter — the block between the first two "---" lines. It is a
// narrow literal reader rather than a YAML parser: frontmatter here also
// carries a multi-line description field that is not valid to decode as
// plain YAML without more care than this test needs to take.
func frontmatterModel(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Frontmatter descriptions run long; grow the buffer past bufio's 64KB
	// default token limit rather than let a long line abort the scan.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", errStr(path, "does not start with a --- frontmatter fence")
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			return "", errStr(path, "frontmatter has no model: key")
		}
		if match := frontmatterModelRE.FindStringSubmatch(line); match != nil {
			return match[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errStr(path, "frontmatter fence was never closed")
}

func errStr(path, msg string) error {
	return &frontmatterError{path: path, msg: msg}
}

type frontmatterError struct {
	path string
	msg  string
}

func (e *frontmatterError) Error() string { return e.path + ": " + e.msg }
