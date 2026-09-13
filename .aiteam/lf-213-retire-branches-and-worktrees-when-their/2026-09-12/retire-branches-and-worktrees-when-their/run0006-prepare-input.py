#!/usr/bin/env python3
"""Run only after root confirms fresh scoped test completion; no prior-run inputs."""
from pathlib import Path
import json
import shlex
import sys

session = Path(__file__).resolve().parent
logs = session / "run0006-executed"
config = json.loads((session / "run0006-evidence-input-template.json").read_text())
selected = sys.argv[1:] or ["US-002", "US-003"]
assert set(selected) and set(selected) <= {"US-002", "US-003"}
config["stories"] = {story: config["stories"][story] for story in selected}
build_path = logs / "build-result.json"
build = json.loads(build_path.read_text())
assert build["exit_code"] == 0
config["run_started_unix"] = build["started_at_unix"]
# Metadata itself is factual output of the root's execution wrapper and records
# the successful exit of a silent command without inventing compiler stdout.
for number, story in (("002", "US-002"), ("003", "US-003")):
    if story not in selected:
        continue
    result = json.loads((logs / ("us" + number + "-tests-result.json")).read_text())
    assert result["exit_code"] == 0, "scoped test failure"
    assert result["started_at_unix"] >= config["run_started_unix"]
    name = result["command"]
    if isinstance(name, list):
        name = shlex.join(name)
    config["stories"][story]["checks"] = [
        {"id": "us" + number + "-build-r6", "name": "go build ./...", "log": str(build_path), "exit_code": build["exit_code"]},
        {"id": "us" + number + "-scoped-r6", "name": story + " scoped race tests (fresh -count=1; full command in execution metadata)", "log": str(logs / ("us" + number + "-tests.log")), "exit_code": result["exit_code"]},
    ]
suffix = "-".join(story.lower() for story in sorted(selected))
target = session / ("run0006-evidence-input-" + suffix + ".json")
assert not target.exists(), "preserve prior input"
target.write_text(json.dumps(config, indent=2) + "\n")
print(target)
