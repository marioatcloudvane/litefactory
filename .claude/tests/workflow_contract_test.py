"""Scoped US-008 instruction checks; no installed Codex tree or writes required.

Run: python3 -B .claude/tests/workflow_contract_test.py
These static checks establish instruction consistency, not live agent behavior.
"""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def normalized(text):
    return " ".join(text.split())


class WorkflowContract(unittest.TestCase):
    def assert_contract(self, text, clauses):
        text = normalized(text)
        for clause in clauses:
            with self.subTest(clause=clause):
                self.assertIn(normalized(clause), text)

    def test_AC_008_1_and_5_installed_contract_matches_shared_source(self):
        self.assertEqual(read(".claude/skills/shared/workflow.md"),
                         read("teams/source/skills/workflow/SKILL.md"))
        # Resolve only owned references; deliberately never load .codex content.
        for path in (ROOT / ".claude").rglob("*.md"):
            for target in re.findall(r"`(\.claude/[\w./-]+\.md)`", path.read_text()):
                with self.subTest(path=path.relative_to(ROOT), target=target):
                    self.assertTrue((ROOT / target).is_file(), target)

    def test_AC_008_2_read_only_reviewer_and_complete_results(self):
        reviewer = read(".claude/agents/go-code-reviewer.md")
        tools = re.search(r"^tools:\s*(.+)$", reviewer, re.MULTILINE).group(1)
        self.assertEqual({tool.strip() for tool in tools.split(",")},
                         {"Read", "Glob", "Grep"})
        self.assert_contract(read(".claude/modes/validate.md"), [
            "frozen identities/revisions", "retained implementation snapshots",
            "executed check outcomes", "exactly one outcome per criterion",
            "code/architecture/plan review", "blocking/advisory",
            "Do not edit source or session/context files", "commission fresh checks",
            "Successful exit is not sign-off", "Advisory improvements do not block",
        ])
        self.assert_contract(read(".claude/modes/session.md"), [
            "It does not scaffold a session", "artifacts are not prerequisites",
        ])

    def test_AC_008_2_role_metadata_and_contract_references(self):
        roles = ["go-architect", "go-code-reviewer", "go-implementation-engineer",
                 "go-integration-test-engineer", "go-unit-test-engineer",
                 "implement-orchestrator", "plan-orchestrator", "proxy-product-owner"]
        for role in roles:
            with self.subTest(role=role):
                text = read(f".claude/agents/{role}.md")
                self.assertTrue(text.startswith("---\n"))
                frontmatter, body = text[4:].split("\n---\n", 1)
                self.assertRegex(frontmatter, rf"(?m)^name: {role}$")
                for field in ("description", "tools", "model"):
                    self.assertRegex(frontmatter, rf"(?m)^{field}: \S.+$")
                self.assertRegex(body, r"\.claude/skills/shared/(workflow|review-evidence)\.md")
        self.assertRegex(read(".claude/skills/shared/review-evidence.md"),
                         r"(?m)^requires:\n\s+- workflow\.md$")

    def test_AC_008_2_separated_engineering_and_legacy_gate_are_distinct(self):
        text = read(".claude/agents/implement-orchestrator.md")
        separated, legacy = text.split("## Legacy workflow procedure only", 1)
        self.assert_contract(separated, [
            "test writing/execution", "Do not commission code, architecture, acceptance",
            "approved-plan review as an Implement completion prerequisite",
            "acceptance boxes/issues still open", "aggregate gates",
            "Do not send legacy `verified` acceptance evidence",
        ])
        self.assert_contract(legacy, ["apply only to the persisted legacy workflow"])

    def test_AC_008_3_legacy_history_and_owner_authority(self):
        self.assert_contract(read(".claude/skills/shared/workflow.md"), [
            "persisted workflow version", "retain legacy behavior through migration and restart",
            "Historical acceptance stays bound to its assessed snapshot permanently",
            "Suggest Validate only after complete engineering/test and successful save evidence",
            "Never automatically chain or dispatch modes",
            "override every substantive acceptance/readiness blocker with a reason and exact scope",
            "Preserve original failed/unverified evidence",
            "manual ticks, labels or manual closure alone grant no authority",
        ])

    def test_AC_008_4_delivery_has_separate_owner_authority(self):
        self.assert_contract(read(".claude/skills/shared/workflow.md"), [
            "every attached child", "including unmanaged children",
            "link only by default, attach only by explicit approval",
            "never implicitly waive the original obligation",
            "Only separate explicit owner approval of that PR/current head/main authorizes merge",
            "changed head needs fresh approval",
            "successful integration into intended main before closing the parent",
        ])

    def test_AC_008_5_and_6_independent_harness_handoff(self):
        self.assert_contract(read(".claude/skills/shared/workflow.md"), [
            "Codex updates `.claude/**` only", "Claude updates `.codex/**` only",
            "Neither story requires editing or completing the other tree",
            "never run a generator that writes both installed trees",
            "exact opposite-harness handoff",
            "Temporary installed-tree parity drift does not block an individual story",
            "reserve cross-tree parity for the aggregate feature gate",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
