#!/usr/bin/env python3
"""Count pinned, complete offline agent-result exchanges; bytes are not tokens."""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORPUS = ROOT / "internal/agentresult/testdata/v1/exchanges/corpus.json"


def main():
    corpus = json.loads(CORPUS.read_text(encoding="utf-8"))
    report = {"version": corpus["version"], "unit": "UTF-8 bytes", "scenarios": [],
              "aggregate": {key: {"input": 0, "output": 0, "total": 0}
                            for key in ("full", "scoped")}}
    for scenario in corpus["scenarios"]:
        row = {"name": scenario["name"]}
        for approach in ("full", "scoped"):
            totals = {"input": 0, "output": 0, "total": 0}
            for step in scenario[approach]:
                path = (ROOT / step["path"]).resolve()
                if not path.is_relative_to(ROOT):
                    raise ValueError("corpus path escapes repository")
                raw = path.read_bytes()
                raw.decode("utf-8", errors="strict")
                if hashlib.sha256(raw).hexdigest() != step["sha256"]:
                    raise ValueError("pinned corpus drift: " + step["path"])
                totals[step["direction"]] += len(raw)
                totals["total"] += len(raw)
            row[approach] = totals
            for key, value in totals.items():
                report["aggregate"][approach][key] += value
        report["scenarios"].append(row)
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
