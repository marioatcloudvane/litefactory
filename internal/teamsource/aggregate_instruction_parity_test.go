package teamsource

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

// This file is US-012's AC-002 aggregate check. It runs after US-010 (Codex
// writes .claude/**) and US-011 (Claude writes .codex/**) are both
// engineering-complete, and it compares BOTH installed instruction trees
// against the teams/source/** shared manifest side by side, purely by
// reading already-committed files. It never writes to either protected tree
// and starts no generator; it reuses the read-only helpers
// codex_instruction_parity_test.go already established in this package
// (readRepoFile, stripFrontmatter, paragraphs, normalizeWhitespace,
// assertParagraphsAppearInOrder, repoRoot) instead of duplicating them, and it
// does not require either single-tree test in this package to have run first.
//
// Run: go test -race -cover -run TestBothInstructionTrees ./internal/teamsource

// claudeStructuredProtocolHandoff mirrors structuredProtocolHandoff (the
// Codex block pinned in codex_instruction_parity_test.go) but names Claude's
// own shared workflow file, matching the wording every specialist
// .claude/agents/*.md file carries.
const claudeStructuredProtocolHandoff = "## Structured protocol handoff\n\n" +
	"Read `.claude/skills/shared/workflow.md` for the persisted content/submission\n" +
	"contract, absolute preflight/submit/receipt helper, list-B evidence, editing and\n" +
	"explicit legacy upgrade. Follow `docs/story-contract-instruction-manifest.md`;\n" +
	"shared-source guidance does not widen this role's tools or scope. Specialists\n" +
	"return typed contributions to their orchestrator for the authorized helper\n" +
	"handoff when their own tool policy does not permit helper execution. Pending\n" +
	"receipts and synthetic fixtures never establish authentic runtime delivery."

// bothTreeMapping reproduces docs/story-contract-instruction-manifest.md's
// role table as data, so this test reads the manifest the same way an
// operator would rather than deriving expectations from either tree itself.
func bothTreeMapping() []struct{ name, source, claude, codex string } {
	return []struct{ name, source, claude, codex string }{
		{"requirements-engineer", "teams/source/agents/shared/requirements-engineer.md", ".claude/agents/requirements-engineer.md", ".codex/skills/requirements-engineer/SKILL.md"},
		{"proxy-product-owner", "teams/source/agents/shared/proxy-product-owner.md", ".claude/agents/proxy-product-owner.md", ".codex/skills/proxy-product-owner/SKILL.md"},
		{"research-orchestrator", "teams/source/agents/shared/research-orchestrator.md", ".claude/agents/research-orchestrator.md", ".codex/skills/research-orchestrator/SKILL.md"},
		{"plan-orchestrator", "teams/source/agents/shared/plan-orchestrator.md", ".claude/agents/plan-orchestrator.md", ".codex/skills/plan-orchestrator/SKILL.md"},
		{"implement-orchestrator", "teams/source/agents/shared/implement-orchestrator.md", ".claude/agents/implement-orchestrator.md", ".codex/skills/implement-orchestrator/SKILL.md"},
		{"go-architect", "teams/source/agents/go-team/go-architect.md", ".claude/agents/go-architect.md", ".codex/skills/go-architect/SKILL.md"},
		{"go-implementation-engineer", "teams/source/agents/go-team/go-implementation-engineer.md", ".claude/agents/go-implementation-engineer.md", ".codex/skills/go-implementation-engineer/SKILL.md"},
		{"go-unit-test-engineer", "teams/source/agents/go-team/go-unit-test-engineer.md", ".claude/agents/go-unit-test-engineer.md", ".codex/skills/go-unit-test-engineer/SKILL.md"},
		{"go-integration-test-engineer", "teams/source/agents/go-team/go-integration-test-engineer.md", ".claude/agents/go-integration-test-engineer.md", ".codex/skills/go-integration-test-engineer/SKILL.md"},
		{"go-code-reviewer", "teams/source/agents/go-team/go-code-reviewer.md", ".claude/agents/go-code-reviewer.md", ".codex/skills/go-code-reviewer/SKILL.md"},
	}
}

// TestBothInstructionTrees_SharedSourceParity is AC-002's core aggregate
// check: for every role in docs/story-contract-instruction-manifest.md's
// table, both the US-010 Claude target and the US-011 Codex target exist and
// are nonempty, and both directly-mapped shared skills (workflow and
// review-evidence, which the manifest's own reproducible rendering section
// treats as literal shared text, not a wrapper) carry their shared
// teams/source/** paragraphs forward intact, compared side by side.
//
// The manifest is explicit that role sources under teams/source/agents/**
// feed native catalog rendering only: "Installed specialist copies have
// legacy tool-specific wrappers and expanded role instructions; native
// catalog rendering does not byte-generate these legacy copies." A
// paragraph-inclusion check against that short native template would fail
// against the deliberately different, expanded installed wrapper text for
// every mapped role; TestBothInstructionTrees_
// StructuredProtocolHandoffPointsAtOwnTreeOnly below is what actually proves
// each wrapper still carries the shared contract obligation forward.
func TestBothInstructionTrees_SharedSourceParity(t *testing.T) {
	for _, row := range bothTreeMapping() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(filepath.Join(repoRoot, row.source)); err != nil {
				t.Fatalf("manifest source missing: %v", err)
			}
			claudeText := readRepoFile(t, row.claude)
			codexText := readRepoFile(t, row.codex)
			if strings.TrimSpace(claudeText) == "" {
				t.Errorf("%s: mapped Claude target is empty", row.claude)
			}
			if strings.TrimSpace(codexText) == "" {
				t.Errorf("%s: mapped Codex target is empty", row.codex)
			}
		})
	}

	t.Run("shared workflow contract", func(t *testing.T) {
		t.Parallel()
		source := stripFrontmatter(readRepoFile(t, "teams/source/skills/workflow/SKILL.md"))
		assertParagraphsAppearInOrder(t, ".claude/skills/shared/workflow.md", source, stripFrontmatter(readRepoFile(t, ".claude/skills/shared/workflow.md")))
		assertParagraphsAppearInOrder(t, ".codex/skills/workflow/SKILL.md", source, stripFrontmatter(readRepoFile(t, ".codex/skills/workflow/SKILL.md")))
	})

	t.Run("shared review-evidence contract", func(t *testing.T) {
		t.Parallel()
		source := stripFrontmatter(readRepoFile(t, "teams/source/skills/review-evidence/SKILL.md"))
		assertParagraphsAppearInOrder(t, ".claude/skills/shared/review-evidence.md", source, stripFrontmatter(readRepoFile(t, ".claude/skills/shared/review-evidence.md")))
		assertParagraphsAppearInOrder(t, ".codex/skills/review-evidence/SKILL.md", source, stripFrontmatter(readRepoFile(t, ".codex/skills/review-evidence/SKILL.md")))
	})
}

// TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly
// checks AC-002's scoping requirement across both trees at once: every
// specialist role's installed copy in EACH tree carries the exact handoff
// block naming only that tree's own shared workflow file, and never the
// sibling tree's file.
func TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly(t *testing.T) {
	wantClaude := normalizeWhitespace(claudeStructuredProtocolHandoff)
	wantCodex := normalizeWhitespace(structuredProtocolHandoff)

	for _, row := range bothTreeMapping() {
		if row.name == "research-orchestrator" {
			continue // Research has no story submission contract; tested separately.
		}
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			claudeText := normalizeWhitespace(readRepoFile(t, row.claude))
			codexText := normalizeWhitespace(readRepoFile(t, row.codex))

			if !strings.Contains(claudeText, wantClaude) {
				t.Errorf("%s: missing the exact handoff pointing at .claude/skills/shared/workflow.md", row.claude)
			}
			if !strings.Contains(codexText, wantCodex) {
				t.Errorf("%s: missing the exact handoff pointing at .codex/skills/workflow/SKILL.md", row.codex)
			}
			if strings.Contains(readRepoFile(t, row.claude), ".codex/skills/workflow/SKILL.md") {
				t.Errorf("%s: Claude installed copy references the Codex tree's workflow file", row.claude)
			}
			if strings.Contains(readRepoFile(t, row.codex), ".claude/skills/shared/workflow.md") {
				t.Errorf("%s: Codex installed copy references the Claude tree's workflow file", row.codex)
			}
		})
	}
}

// TestBothInstructionTrees_ModeHeaderContractNotDuplicated is the two-tree
// version of the scoped single-tree check already retained for Codex
// (TestCodexSkills_ModeHeaderContractNotDuplicatedOrAltered): neither
// installed tree's specialist copies may introduce a competing "- Mode: X"
// header; that literal contract belongs solely to the root router files.
func TestBothInstructionTrees_ModeHeaderContractNotDuplicated(t *testing.T) {
	for _, row := range bothTreeMapping() {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if modeHeaderPattern.MatchString(readRepoFile(t, row.claude)) {
				t.Errorf("%s: introduces a '- Mode: X' style header line", row.claude)
			}
			if modeHeaderPattern.MatchString(readRepoFile(t, row.codex)) {
				t.Errorf("%s: introduces a '- Mode: X' style header line", row.codex)
			}
		})
	}
}

// hashRepoFiles is a stable, order-sensitive digest over an explicit file
// list (path and content), used only to prove a set of files did not change
// byte-for-byte across a comparison run -- never to derive expectations.
func hashRepoFiles(t *testing.T, paths []string) string {
	t.Helper()
	h := sha256.New()
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(repoRoot, p))
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestBothInstructionTrees_ComparisonIsReadOnly is the other half of AC-002:
// comparing both installed trees to the shared manifest, together with the
// same reproducible in-memory rendering docs/story-contract-instruction-
// manifest.md documents, never writes to either protected tree. It hashes
// every file this story's comparisons touch before and after exercising that
// rendering against a temporary copy of teams/source, and confirms no
// generator wrote an installed tree as a side effect anywhere in the run.
func TestBothInstructionTrees_ComparisonIsReadOnly(t *testing.T) {
	var claudePaths, codexPaths []string
	for _, row := range bothTreeMapping() {
		claudePaths = append(claudePaths, row.claude)
		codexPaths = append(codexPaths, row.codex)
	}
	claudePaths = append(claudePaths, ".claude/skills/shared/workflow.md", ".claude/skills/shared/review-evidence.md", ".claude/modes/session.md", ".claude/modes/validate.md")
	codexPaths = append(codexPaths, ".codex/skills/workflow/SKILL.md", ".codex/skills/review-evidence/SKILL.md")

	before := hashRepoFiles(t, claudePaths) + "|" + hashRepoFiles(t, codexPaths)

	// Re-read every mapped file (this story's own comparison work) and
	// separately exercise the documented reproducible in-memory rendering
	// procedure against an isolated temporary copy of teams/source, exactly
	// as TestSharedWorkflowRendering does. Neither step touches this
	// checkout's real .claude or .codex directories.
	for _, row := range bothTreeMapping() {
		_ = readRepoFile(t, row.claude)
		_ = readRepoFile(t, row.codex)
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join(repoRoot, "teams/source"))); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"claude", "codex"} {
		snapshot, err := CaptureRoot(t.Context(), root, "go-team", "implement")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range snapshot.Agents {
			if _, err := render.Native(harness, render.NativeRole{Source: entry.Source, Model: "test-model", Tools: entry.Source.Frontmatter.Tools, Skills: entry.Definition.Skills}, "workspace-write"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tree := range []string{".claude", ".codex", "plugin", "roles"} {
		if _, err := os.Lstat(filepath.Join(root, tree)); !os.IsNotExist(err) {
			t.Fatalf("rendering wrote an installed tree into the temporary root: %s", tree)
		}
	}

	after := hashRepoFiles(t, claudePaths) + "|" + hashRepoFiles(t, codexPaths)
	if before != after {
		t.Fatal("comparing both installed trees against the shared manifest mutated a protected tree")
	}
}

// rootRouting is the routing behaviour both root instruction files must state,
// with %s standing for the one thing that legitimately differs: where each tree
// keeps the shared lifecycle contract. Everything else — the authority of the
// prompt header, the mode-to-agent table, scaffolding, invoke-once, and the git
// rule — is tool-independent and must read the same in both.
const rootRouting = "`- Mode: X` in the prompt header is authoritative: never classify, never change it." +
	"\x00Research | `research-orchestrator`" +
	"\x00Plan | `plan-orchestrator`" +
	"\x00Implement | `implement-orchestrator`" +
	"\x00Validate | `go-code-reviewer`, read-only contract" +
	"\x00Spike | nobody here — the factory runs the graph in `docs/workflow/spike.md`; never execute it in this session" +
	"\x00Outside Validate, scaffold `$session` per `docs/workflow/session.md` first" +
	"\x00Validate reuses the supplied session and frozen scope read-only." +
	"\x00Do not scaffold a directory or MANIFEST, write artifacts, or require artifact existence to route or finish Validate." +
	"\x00Validate finishes with structured runner findings, not an artifact" +
	"\x00Invoke once, read the result once, check the artifact once. Never poll for status." +
	"\x00Do not read source, search the repo or form a view on the design" +
	"\x00Never `git add`, `commit`, `stash` or `push`" +
	"\x00modes, lifecycle, managed stories, Plan continuation and the story protocol are in\n%s."

// TestRootInstructionFilesShareRoutingBehaviour pins CLAUDE.md against
// AGENTS.md.
//
// Nothing did, before this. bothTreeMapping covers the twelve specialist roles
// and stops there, so the two root files were a hand-written pair that drifted
// until AGENTS.md gave up and delegated — "classify the request into a mode
// exactly as CLAUDE.md describes" — which made a Codex run shell out to read
// Claude's file mid-run, and let the two contradict each other the moment
// either changed alone.
//
// Claims are compared after whitespace normalization so reflowing a paragraph
// is not a failure, and each is required verbatim in both files: a routing rule
// that holds for one coding agent and not the other is a bug in whichever file
// is wrong, not a difference worth expressing.
func TestRootInstructionFilesShareRoutingBehaviour(t *testing.T) {
	files := map[string]struct{ path, workflow string }{
		"claude": {"CLAUDE.md", "`.claude/skills/shared/workflow.md`"},
		"codex":  {"AGENTS.md", "`.codex/skills/workflow/SKILL.md`"},
	}
	for name, f := range files {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := normalizeWhitespace(readRepoFile(t, f.path))
			for _, claim := range strings.Split(fmt.Sprintf(rootRouting, f.workflow), "\x00") {
				if want := normalizeWhitespace(claim); !strings.Contains(body, want) {
					t.Errorf("%s does not state: %s", f.path, want)
				}
			}
		})
	}

	// The delegation this test exists to prevent. AGENTS.md may name CLAUDE.md
	// to say which agent reads which file; it may not send a run there to
	// decide how to behave.
	codex := readRepoFile(t, "AGENTS.md")
	for _, forbidden := range []string{"as `CLAUDE.md` describes", "exactly as `CLAUDE.md`"} {
		if strings.Contains(codex, forbidden) {
			t.Errorf("AGENTS.md delegates routing to CLAUDE.md: %q", forbidden)
		}
	}
}
