package team

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenFlagFixture is the claude permission-skipping flag, assembled from
// fragments so this file does not itself trip internal/config's whole-tree
// TestForbiddenFlagIsAbsentFromTheTree scan (see internal/config/policy.go's
// forbiddenFlag and purity_test.go).
const forbiddenFlagFixture = "--dangerously-" + "skip-permissions"

// secretFixture is a literal credential shape config.ClassifySecret
// recognizes (a GitHub token), distinct from the one write_test.go's
// TestAddRejectsLiteralSecret already uses, so a leak check here proves
// nothing about that other test's fixture value.
const secretFixture = "ghp_zyxwvutsrqponmlkjihgfedcba0987654321"

// TestAddRejectsForbiddenFlag mirrors TestAddRejectsLiteralSecret
// (write_test.go) for the forbidden-flag check added in US-14: rendered
// bytes about to be written that smuggle in claude's permission-skipping
// flag abort the write, producing a *ForbiddenFlagError that names the
// agent key, harness and path but never the matched flag text itself
// (AC-2).
func TestAddRejectsForbiddenFlag(t *testing.T) {
	root := t.TempDir()
	ar := standaloneRender(t, "claude", "go-architect", "src/go-architect.md",
		"Run with: claude "+forbiddenFlagFixture+"\n")

	_, err := Add(root, []AgentRender{ar})

	if !errors.Is(err, ErrForbiddenFlag) {
		t.Fatalf("error = %v, want ErrForbiddenFlag", err)
	}
	var ffErr *ForbiddenFlagError
	if !errors.As(err, &ffErr) {
		t.Fatalf("error = %v, want *ForbiddenFlagError", err)
	}
	if ffErr.AgentKey != "go-architect" {
		t.Errorf("ForbiddenFlagError.AgentKey = %q, want %q", ffErr.AgentKey, "go-architect")
	}
	if ffErr.Harness != "claude" {
		t.Errorf("ForbiddenFlagError.Harness = %q, want %q", ffErr.Harness, "claude")
	}
	if ffErr.Path != ".claude/agents/go-architect.md" {
		t.Errorf("ForbiddenFlagError.Path = %q, want %q", ffErr.Path, ".claude/agents/go-architect.md")
	}
	if strings.Contains(err.Error(), forbiddenFlagFixture) {
		t.Errorf("error text %q must not contain the matched flag text", err.Error())
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-architect.md")); !os.IsNotExist(statErr) {
		t.Errorf("file was written despite a detected forbidden flag")
	}
	if _, statErr := os.Stat(filepath.Join(root, litefactoryDir, lockfileName)); !os.IsNotExist(statErr) {
		t.Errorf("lockfile was written despite a detected forbidden flag")
	}
}

// TestAddRejectsLiteralSecret_NamesAgentKeyHarnessAndDoesNotLeakValue extends
// write_test.go's TestAddRejectsLiteralSecret with the AC-2 error-naming and
// leakage assertions this story adds: the error names the agent key and
// harness (not just the path), and never contains the matched secret value
// itself.
func TestAddRejectsLiteralSecret_NamesAgentKeyHarnessAndDoesNotLeakValue(t *testing.T) {
	root := t.TempDir()
	ar := standaloneRender(t, "claude", "go-tester", "src/go-tester.md",
		"token: "+secretFixture+"\n")

	_, err := Add(root, []AgentRender{ar})

	var se *SecretDetectedError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v, want *SecretDetectedError", err)
	}
	if se.AgentKey != "go-tester" {
		t.Errorf("SecretDetectedError.AgentKey = %q, want %q", se.AgentKey, "go-tester")
	}
	if se.Harness != "claude" {
		t.Errorf("SecretDetectedError.Harness = %q, want %q", se.Harness, "claude")
	}
	if strings.Contains(err.Error(), secretFixture) {
		t.Errorf("error text %q must not contain the matched secret value", err.Error())
	}
}

// TestAddForbiddenFlagInBatch_AbortsEntireWrite covers AC-2's all-or-nothing
// guarantee specifically for the forbidden-flag check: one offending render
// among several clean ones still aborts the whole batch, and none of the
// clean renders' files land on disk either.
func TestAddForbiddenFlagInBatch_AbortsEntireWrite(t *testing.T) {
	root := t.TempDir()
	good := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Good\n")
	bad := standaloneRender(t, "claude", "go-tester", "src/go-tester.md",
		"flag: "+forbiddenFlagFixture+"\n")

	_, err := Add(root, []AgentRender{good, bad})

	if !errors.Is(err, ErrForbiddenFlag) {
		t.Fatalf("error = %v, want ErrForbiddenFlag", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-architect.md")); !os.IsNotExist(statErr) {
		t.Errorf("the clean render's file was written despite a forbidden flag elsewhere in the batch")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-tester.md")); !os.IsNotExist(statErr) {
		t.Errorf("the offending render's file was written")
	}
	if _, statErr := os.Stat(filepath.Join(root, litefactoryDir, lockfileName)); !os.IsNotExist(statErr) {
		t.Errorf("lockfile was written despite a forbidden flag aborting the whole batch")
	}
}

// TestAddSecretInBatch_AbortsEntireWrite is TestAddForbiddenFlagInBatch_AbortsEntireWrite's
// counterpart for the literal-secret check: one offending render among
// several clean ones still aborts the whole batch.
func TestAddSecretInBatch_AbortsEntireWrite(t *testing.T) {
	root := t.TempDir()
	good := standaloneRender(t, "claude", "go-architect", "src/go-architect.md", "# Good\n")
	bad := standaloneRender(t, "claude", "go-tester", "src/go-tester.md",
		"token: "+secretFixture+"\n")

	_, err := Add(root, []AgentRender{good, bad})

	if !errors.Is(err, ErrSecretDetected) {
		t.Fatalf("error = %v, want ErrSecretDetected", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-architect.md")); !os.IsNotExist(statErr) {
		t.Errorf("the clean render's file was written despite a secret elsewhere in the batch")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".claude/agents/go-tester.md")); !os.IsNotExist(statErr) {
		t.Errorf("the offending render's file was written")
	}
	if _, statErr := os.Stat(filepath.Join(root, litefactoryDir, lockfileName)); !os.IsNotExist(statErr) {
		t.Errorf("lockfile was written despite a secret aborting the whole batch")
	}
}
