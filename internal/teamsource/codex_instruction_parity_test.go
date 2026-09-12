package teamsource

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file gives T-011.2 (US-011) a scoped, temporary comparison between the
// .codex/skills/** files this story owns and their corresponding
// teams/source/** shared-source content, per docs/story-contract-instruction-
// manifest.md's mapping table. It deliberately never reads or asserts on
// .claude/** content: cross-tree parity belongs to the aggregate US-012 gate,
// not to this independently verifiable story. It also never requires the
// sibling Claude story to be complete.
//
// Run: go test -race -cover -run TestCodexSkills ./internal/teamsource

// repoRoot is two levels above this package, matching the convention already
// used by workflow_render_test.go in this package.
const repoRoot = "../.."

func readRepoFile(t *testing.T, relPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, relPath))
	if err != nil {
		t.Fatalf("reading %s: %v", relPath, err)
	}
	return string(data)
}

// stripFrontmatter drops a leading YAML frontmatter block, if present,
// keeping only the instruction body that follows it.
func stripFrontmatter(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return text
	}
	parts := strings.SplitN(text, "\n---\n", 2)
	if len(parts) != 2 {
		return text
	}
	return parts[1]
}

// blockquotePrefix strips a leading Markdown blockquote marker from each
// line. Codex preambles are wrapped in "> " blockquotes purely for
// presentation; a sentence that wraps across quoted lines must still read as
// one contiguous clause once that marker is removed.
var blockquotePrefix = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)

func normalizeWhitespace(s string) string {
	unquoted := blockquotePrefix.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(unquoted), " ")
}

// paragraphs splits body text on blank lines and normalizes whitespace within
// each paragraph, dropping empties.
func paragraphs(body string) []string {
	raw := strings.Split(body, "\n\n")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = normalizeWhitespace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// assertParagraphsAppearInOrder verifies every paragraph of source appears, in
// order and non-overlapping, inside installed. The installed Codex copy is
// allowed to interleave its own extra paragraphs (preambles, footers, legacy
// notes) between source paragraphs -- that is the documented "wrapper" shape
// -- but it must not drop, reorder or reword a single shared-source paragraph.
func assertParagraphsAppearInOrder(t *testing.T, installedPath string, source, installed string) {
	t.Helper()
	installedNorm := normalizeWhitespace(installed)
	cursor := 0
	for i, para := range paragraphs(source) {
		idx := strings.Index(installedNorm[cursor:], para)
		if idx == -1 {
			preview := para
			if len(preview) > 160 {
				preview = preview[:160] + "…"
			}
			t.Errorf("%s: shared-source paragraph %d missing or out of order: %q", installedPath, i, preview)
			continue
		}
		cursor += idx + len(para)
	}
}

// TestCodexSkills_SharedSourceParity is the scoped comparison required by
// T-011.2: each listed .codex/skills/** file must carry its corresponding
// teams/source/** shared-source content forward intact. It needs no .claude
// content and no US-010 completion to pass or fail.
func TestCodexSkills_SharedSourceParity(t *testing.T) {
	tests := []struct {
		name          string
		sourcePath    string
		installedPath string
	}{
		{
			name:          "workflow contract",
			sourcePath:    "teams/source/skills/workflow/SKILL.md",
			installedPath: ".codex/skills/workflow/SKILL.md",
		},
		{
			name:          "review-evidence contract",
			sourcePath:    "teams/source/skills/review-evidence/SKILL.md",
			installedPath: ".codex/skills/review-evidence/SKILL.md",
		},
		{
			name:          "planning-contracts reaches plan-orchestrator",
			sourcePath:    "teams/source/skills/planning-contracts/SKILL.md",
			installedPath: ".codex/skills/plan-orchestrator/SKILL.md",
		},
		{
			name:          "planning-contracts reaches proxy-product-owner",
			sourcePath:    "teams/source/skills/planning-contracts/SKILL.md",
			installedPath: ".codex/skills/proxy-product-owner/SKILL.md",
		},
		{
			name:          "planning-contracts reaches requirements-engineer",
			sourcePath:    "teams/source/skills/planning-contracts/SKILL.md",
			installedPath: ".codex/skills/requirements-engineer/SKILL.md",
		},
		{
			name:          "planning-contracts reaches go-architect",
			sourcePath:    "teams/source/skills/planning-contracts/SKILL.md",
			installedPath: ".codex/skills/go-architect/SKILL.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := stripFrontmatter(readRepoFile(t, tt.sourcePath))
			installed := stripFrontmatter(readRepoFile(t, tt.installedPath))

			assertParagraphsAppearInOrder(t, tt.installedPath, source, installed)
		})
	}
}

// structuredProtocolHandoff is the exact block T-011.1 added to every
// specialist .codex skill that has no dedicated contract section of its own
// (workflow and review-evidence carry the contract directly instead).
const structuredProtocolHandoff = "## Structured protocol handoff\n\n" +
	"Read `.codex/skills/workflow/SKILL.md` for the persisted content/submission\n" +
	"contract, absolute preflight/submit/receipt helper, list-B evidence, editing and\n" +
	"explicit legacy upgrade. Follow `docs/story-contract-instruction-manifest.md`;\n" +
	"shared-source guidance does not widen this role's tools or scope. Specialists\n" +
	"return typed contributions to their orchestrator for the authorized helper\n" +
	"handoff when their own tool policy does not permit helper execution. Pending\n" +
	"receipts and synthetic fixtures never establish authentic runtime delivery."

// TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree checks AC-002's
// scoping requirement for the twelve specialist skills that received the
// handoff block: it must be present verbatim and must point only at the
// .codex tree, never at the Claude installed copy.
func TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree(t *testing.T) {
	files := []string{
		".codex/skills/implement-orchestrator/SKILL.md",
		".codex/skills/go-implementation-engineer/SKILL.md",
		".codex/skills/go-integration-test-engineer/SKILL.md",
		".codex/skills/go-unit-test-engineer/SKILL.md",
		".codex/skills/go-code-reviewer/SKILL.md",
		".codex/skills/plan-orchestrator/SKILL.md",
		".codex/skills/proxy-product-owner/SKILL.md",
		".codex/skills/requirements-engineer/SKILL.md",
		".codex/skills/go-architect/SKILL.md",
	}
	wantNorm := normalizeWhitespace(structuredProtocolHandoff)

	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			text := readRepoFile(t, path)

			if !strings.Contains(normalizeWhitespace(text), wantNorm) {
				t.Errorf("%s: missing the exact structured protocol handoff block pointing at .codex/skills/workflow/SKILL.md", path)
			}
			// The handoff block's own literal text (pinned above) names only
			// .codex/skills/workflow/SKILL.md. Unrelated preamble/legacy
			// mentions of .claude/** elsewhere in the file (e.g. "Codex copy
			// of `.claude/agents/x.md`") are expected documentation, not an
			// AC-002 scope violation, so this check stays narrow to the block.
		})
	}
}

// TestCodexSkills_AC003GuardClausesPreserved spot-checks AC-003: the changed
// .codex files must keep the guard sentences that rule out unsupported
// nested-agent proof, fabricated successful receipts, automatic mode
// chaining and treating a clean process exit as acceptance.
func TestCodexSkills_AC003GuardClausesPreserved(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "workflow forbids automatic mode chaining and exit-as-acceptance",
			path: ".codex/skills/workflow/SKILL.md",
			want: []string{
				"Never automatically chain or dispatch modes.",
				"Pending, failed, missing or disconnected is never success; exit zero and a claimed summary prove none of these decisions.",
				"Never widen permissions, launch paid nested agents or substitute a successful synthetic fixture for that observation.",
			},
		},
		{
			name: "go-code-reviewer forbids exit-as-acceptance",
			path: ".codex/skills/go-code-reviewer/SKILL.md",
			want: []string{
				"Successful process exit alone is not acceptance.",
			},
		},
		{
			name: "review-evidence keeps the read-only, no-new-checks boundary",
			path: ".codex/skills/review-evidence/SKILL.md",
			want: []string{
				"This does not authorize shell commands, new checks or a wider specialist tool policy.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text := normalizeWhitespace(readRepoFile(t, tt.path))
			for _, clause := range tt.want {
				if !strings.Contains(text, normalizeWhitespace(clause)) {
					t.Errorf("%s: missing guard clause %q", tt.path, clause)
				}
			}
		})
	}
}

// TestCodexSkills_ForbiddenClaimsAbsent asserts none of the fourteen changed
// .codex files affirmatively claim the behaviors AC-003 rules out. These are
// deliberately worded as the claims a regression would introduce, not as the
// negations already asserted above.
func TestCodexSkills_ForbiddenClaimsAbsent(t *testing.T) {
	files := []string{
		".codex/skills/workflow/SKILL.md",
		".codex/skills/review-evidence/SKILL.md",
		".codex/skills/plan-orchestrator/SKILL.md",
		".codex/skills/proxy-product-owner/SKILL.md",
		".codex/skills/requirements-engineer/SKILL.md",
		".codex/skills/go-architect/SKILL.md",
		".codex/skills/go-code-reviewer/SKILL.md",
		".codex/skills/research-orchestrator/SKILL.md",
		".codex/skills/implement-orchestrator/SKILL.md",
		".codex/skills/go-implementation-engineer/SKILL.md",
		".codex/skills/go-integration-test-engineer/SKILL.md",
		".codex/skills/go-unit-test-engineer/SKILL.md",
	}
	forbidden := []string{
		"clean exit is acceptance",
		"exit code proves acceptance",
		"process exit is sign-off",
		"automatically chain the next mode",
		"automatically dispatch the next mode",
		"fabricate a receipt",
		"fabricate a successful receipt",
		"assume the receipt succeeded",
		"nested agent proves",
		"proves nested-agent execution",
	}

	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			text := strings.ToLower(normalizeWhitespace(readRepoFile(t, path)))
			for _, phrase := range forbidden {
				if strings.Contains(text, phrase) {
					t.Errorf("%s: contains forbidden claim %q", path, phrase)
				}
			}
		})
	}
}

// modeHeaderPattern matches the root router's literal "- Mode: X" style
// header line contract (see CLAUDE.md's Classify section).
var modeHeaderPattern = regexp.MustCompile(`(?m)^\s*-\s*Mode:\s*\S`)

// TestCodexSkills_ModeHeaderContractNotDuplicatedOrAltered spot-checks the
// other half of AC-003: the changed .codex files must preserve the literal
// mode-header contract by leaving it alone. None of the twelve specialist
// skills nor workflow/review-evidence owned a copy of the "- Mode: X" header
// parsing rule before this story, and none may introduce a competing or
// reworded copy of it now -- that contract stays solely in the root router
// files, which are outside this story's .codex/** scope.
func TestCodexSkills_ModeHeaderContractNotDuplicatedOrAltered(t *testing.T) {
	files := []string{
		".codex/skills/workflow/SKILL.md",
		".codex/skills/review-evidence/SKILL.md",
		".codex/skills/plan-orchestrator/SKILL.md",
		".codex/skills/proxy-product-owner/SKILL.md",
		".codex/skills/requirements-engineer/SKILL.md",
		".codex/skills/go-architect/SKILL.md",
		".codex/skills/go-code-reviewer/SKILL.md",
		".codex/skills/research-orchestrator/SKILL.md",
		".codex/skills/implement-orchestrator/SKILL.md",
		".codex/skills/go-implementation-engineer/SKILL.md",
		".codex/skills/go-integration-test-engineer/SKILL.md",
		".codex/skills/go-unit-test-engineer/SKILL.md",
	}

	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			text := readRepoFile(t, path)
			if modeHeaderPattern.MatchString(text) {
				t.Errorf("%s: introduces a '- Mode: X' style header line; that literal contract belongs only to the root router files", path)
			}
		})
	}

	// The one place in this tree that already discussed mode scaffolding
	// keeps saying the same thing: mode layout lives under .claude/modes/,
	// shared between both harnesses, and is not something this story's
	// files should restate or contradict.
	t.Run("workflow mode-scaffolding pointer unchanged", func(t *testing.T) {
		t.Parallel()
		want := "**Mode scaffolding lives in `.claude/modes/`.** Those files are shared " +
			"between both harnesses and describe session layout and the Validate and " +
			"Spike modes; read them there. They are not a Claude-only tree."
		text := normalizeWhitespace(readRepoFile(t, ".codex/skills/workflow/SKILL.md"))
		if !strings.Contains(text, normalizeWhitespace(want)) {
			t.Errorf("workflow.md: missing the literal mode-scaffolding pointer sentence")
		}
	})
}
