#!/usr/bin/env python3
"""Conservative line accounting from the approved immutable Bug baseline.
Run from the repository root. Counts splitlines, including blank/comment lines.
Every added non-test Go line in internal/ and cmd/ is charged, regardless of its
filename or story. Only deleted lines in the saved baseline selection earn
credit. Relocation creates both deletion and addition, so earns no net credit.
Unrelated deletions earn no credit. No git index or worktree mutation occurs.
"""
import difflib
import hashlib
import json
from pathlib import Path
import subprocess

BASELINE = "f6c814ec4f8df34ec2e7c3c262f43ef5383f04ef"
SESSION = Path(".aiteam/lf-453-simplify-the-bug-workflow-one-stop-rule/2026-10-02/simplify-the-bug-workflow-one-stop-rule")

def selected(path):
    return path.startswith("internal/") and (Path(path).name.startswith("bug") or "buganalysis/" in path or "bugchecks/" in path)

def eligible(path):
    return path.endswith(".go") and not path.endswith("_test.go")

paths = subprocess.check_output(["git", "ls-tree", "-r", "--name-only", BASELINE], text=True).splitlines()
old = {p: subprocess.check_output(["git", "show", BASELINE + ":" + p]).splitlines() for p in paths if eligible(p)}
new_bytes = {str(p): p.read_bytes() for root in ("internal", "cmd") for p in Path(root).rglob("*.go") if eligible(str(p))}
new = {p: raw.splitlines() for p, raw in new_bytes.items()}
baseline = [{"path": p, "lines": len(old[p])} for p in sorted(old) if selected(p)]
if len(baseline) != 57 or sum(v["lines"] for v in baseline) != 15524:
    raise SystemExit("Saved baseline does not reproduce 57 files / 15,524 lines")
inventory = []
removed = added = 0
for p in sorted(old.keys() | new.keys()):
    deleted = inserted = 0
    for op, i, j, k, l in difflib.SequenceMatcher(None, old.get(p, []), new.get(p, []), autojunk=False).get_opcodes():
        if op in ("replace", "delete"):
            deleted += j - i
        if op in ("replace", "insert"):
            inserted += l - k
    credit = deleted if p in old and selected(p) else 0
    removed += credit
    added += inserted
    if deleted or inserted:
        inventory.append({"path": p, "baseline_lines": len(old.get(p, [])), "current_lines": len(new.get(p, [])), "deleted_lines": deleted, "credited_deletions": credit, "charged_additions": inserted})
manifest = b"".join(p.encode() + b"\0" + hashlib.sha256(raw).hexdigest().encode() + b"\n" for p, raw in sorted(new_bytes.items()))
result = {"baseline_commit": BASELINE, "baseline_file_count": len(baseline), "baseline_lines": 15524, "endpoint_non_test_go_digest": hashlib.sha256(manifest).hexdigest(), "credited_removal": removed, "charged_replacement_additions": added, "net_removal": removed - added, "target": 7762, "shortfall": max(0, 7762 - removed + added), "historical_sql_files_0066_0080": len([p for p in paths if p.startswith("internal/store/migrations/") and 66 <= int(Path(p).name[:4]) <= 80]), "current_sql_files": len(list(Path("internal/store/migrations").glob("*.sql"))), "active_legacy_bug_tables": 0, "retained_readonly_legacy_tables": ["bug_tasks", "bug_analyses", "bug_repairs", "bug_checks", "bug_delivery", "bug_events"], "baseline_inventory": baseline, "change_inventory": inventory}
SESSION.joinpath("BUG_RETIREMENT_MEASUREMENT.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps({k: v for k, v in result.items() if not k.endswith("inventory")}, indent=2))
