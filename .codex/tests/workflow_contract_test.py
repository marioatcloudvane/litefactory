"""Scoped US-009 instruction checks; no installed Claude tree or writes required.

Run: python3 -B .codex/tests/workflow_contract_test.py
These static checks establish instruction consistency, not live agent behavior.

The Claude sibling (.claude/tests/workflow_contract_test.py) checks its own tree
the same way. Neither test loads the other tree's installed content, so either
story can pass while the other is still pending; cross-tree parity belongs to
the aggregate feature gate.
"""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]

# Every .codex skill is "the same instructions, different enforcement": a Codex
# preamble explaining what the harness does not hold for it, wrapped around the
# shared body. Equality would forbid the preamble, so the contract is that the
# approved shared text survives verbatim inside the installed copy.
CODEX_SKILLS = ROOT / ".codex" / "skills"
SHARED = ROOT / "teams" / "source" / "skills"


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def body(text):
    """Drop YAML frontmatter, keeping the instruction body."""
    return text.split("\n---\n", 1)[1] if text.startswith("---\n") else text


def normalized(text):
    # Codex preambles are blockquotes; the marker is presentation, so a clause
    # that wraps across quoted lines still has to match.
    unquoted = re.sub(r"(?m)^\s*>\s?", "", text)
    return " ".join(unquoted.split())


class WorkflowContract(unittest.TestCase):
    def assert_contract(self, text, clauses):
        text = normalized(text)
        for clause in clauses:
            with self.subTest(clause=clause):
                self.assertIn(normalized(clause), text)

    def test_AC_009_1_and_5_installed_contract_carries_shared_source(self):
        for skill in ("workflow", "review-evidence"):
            with self.subTest(skill=skill):
                installed = normalized(body(read(f".codex/skills/{skill}/SKILL.md")))
                approved = normalized(body(read(f"teams/source/skills/{skill}/SKILL.md")))
                self.assertIn(approved, installed)
        # Resolve only owned references; deliberately never require .claude content.
        for path in CODEX_SKILLS.rglob("SKILL.md"):
            for target in re.findall(r"`(\.codex/[\w./-]+\.md)`", path.read_text()):
                with self.subTest(path=path.relative_to(ROOT), target=target):
                    self.assertTrue((ROOT / target).is_file(), target)

    def test_AC_009_1_owned_tree_only(self):
        """Nothing here may point a reader at a Codex path that does not exist,
        and no Codex skill may claim ownership of the Claude tree's edits."""
        workflow = read(".codex/skills/workflow/SKILL.md")
        self.assert_contract(workflow, [
            "the tree you may edit is `.claude/**`",
            "This tree — `.codex/**` — is the one you must not write",
            "do not attempt to widen permissions",
        ])

    def test_AC_009_2_read_only_reviewer_and_complete_results(self):
        reviewer = read(".codex/skills/go-code-reviewer/SKILL.md")
        self.assert_contract(reviewer, [
            "Permitted here: `Read, Glob, Grep`",
            "never edits a file, never writes an artifact and never executes a build",
            "Read `.codex/skills/review-evidence/SKILL.md`",
            "return only that exact structured result",
            "Do not write CODE_REVIEW.md or other artifacts",
            "A clean review is not proof that unexecuted tests passed",
        ])
        # The legacy output template must be gone, not merely discouraged.
        self.assertNotIn("## Your output: CODE_REVIEW.md", reviewer)
        self.assert_contract(read(".codex/skills/review-evidence/SKILL.md"), [
            "Validate is read-only",
            "Do not invoke shell/build/test commands or widen the tool policy",
            "exactly one structured outcome per criterion",
            "advisory findings alone do not prevent acceptance",
        ])

    def test_AC_009_2_role_metadata_and_contract_references(self):
        roles = ["go-architect", "go-code-reviewer", "go-implementation-engineer",
                 "go-integration-test-engineer", "go-unit-test-engineer",
                 "implement-orchestrator", "plan-orchestrator", "proxy-product-owner"]
        for role in roles:
            with self.subTest(role=role):
                text = read(f".codex/skills/{role}/SKILL.md")
                self.assertTrue(text.startswith("---\n"))
                frontmatter, rest = text[4:].split("\n---\n", 1)
                self.assertRegex(frontmatter, rf"(?m)^name: {role}$")
                self.assertRegex(frontmatter, r"(?m)^description: \S.+$")
                self.assertRegex(rest, r"\.codex/skills/(workflow|review-evidence)/SKILL\.md")
                # Codex has no enforced tool list; the preamble must state one.
                self.assertIn("Codex copy of", rest)
        self.assertRegex(read(".codex/skills/review-evidence/SKILL.md"),
                         r"(?m)^requires:\n\s+- \.\./workflow/SKILL\.md$")

    def test_AC_009_2_separated_engineering_and_legacy_gate_are_distinct(self):
        text = read(".codex/skills/implement-orchestrator/SKILL.md")
        separated, legacy = text.split("## Legacy workflow procedure only", 1)
        self.assert_contract(separated, [
            "test writing/execution", "Do not commission code, architecture, acceptance",
            "approved-plan review as an Implement completion prerequisite",
            "acceptance boxes/issues still open", "aggregate gates",
            "Do not send legacy `verified` acceptance evidence",
        ])
        self.assert_contract(legacy, ["apply only to the persisted legacy workflow"])

    def test_AC_009_3_legacy_history_and_owner_authority(self):
        self.assert_contract(read(".codex/skills/workflow/SKILL.md"), [
            "persisted workflow version", "retain legacy behavior through migration and restart",
            "Historical acceptance stays bound to its assessed snapshot permanently",
            "Suggest Validate only after complete engineering/test and successful save evidence",
            "Never automatically chain or dispatch modes",
            "override every substantive acceptance/readiness blocker with a reason and exact scope",
            "Preserve original failed/unverified evidence",
            "manual ticks, labels or manual closure alone grant no authority",
        ])

    def test_AC_009_4_delivery_has_separate_owner_authority(self):
        self.assert_contract(read(".codex/skills/workflow/SKILL.md"), [
            "every attached child", "including unmanaged children",
            "link only by default, attach only by explicit approval",
            "never implicitly waive the original obligation",
            "Only separate explicit owner approval of that PR/current head/main authorizes merge",
            "changed head needs fresh approval",
            "successful integration into intended main before closing the parent",
        ])

    def test_AC_009_5_and_6_independent_harness_handoff(self):
        self.assert_contract(read(".codex/skills/workflow/SKILL.md"), [
            "Codex updates `.claude/**` only", "Claude updates `.codex/**` only",
            "Neither story requires editing or completing the other tree",
            "never run a generator that writes both installed trees",
            "exact opposite-harness handoff",
            "Temporary installed-tree parity drift does not block an individual story",
            "reserve cross-tree parity for the aggregate feature gate",
            "Report that the story requires the Claude harness",
            "name the exact paths it needs to change",
            "Other independently ready stories",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
