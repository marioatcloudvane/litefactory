#!/usr/bin/env python3
"""Build bounded factory contributions from current files and this run's executed logs.
Input JSON: {"run_started_unix": epoch, "stories": {"US-002": {
  "files": ["relative/file"], "checks": [{"id":"us002-scoped", "name":"command",
  "log":"absolute/current-run.log", "exit_code":0}]}, "US-003": {...}}}
Only root supplies observed check exit codes. Never infer execution from old logs.
"""
import base64
import hashlib
import json
import pathlib
import sys
import time

root = pathlib.Path.cwd().resolve()
config = json.loads(pathlib.Path(sys.argv[1]).read_text())
outdir = pathlib.Path(sys.argv[2]).resolve()
outdir.mkdir(parents=True, exist_ok=True)
started = float(config["run_started_unix"])
assert 0 < started <= time.time(), "invalid run start"
assert config["stories"] and set(config["stories"]) <= {"US-002", "US-003"}
ids = set()
summaries = []
for story in sorted(config["stories"]):
    spec = config["stories"][story]
    prefix = story.lower().replace("-", "") + "-"
    files = []
    manifest = bytearray()
    assert spec["files"] and len(spec["files"]) <= 256
    assert len(set(spec["files"])) == len(spec["files"]), "duplicate path"
    for rel in sorted(spec["files"]):
        path = pathlib.PurePosixPath(rel)
        assert not path.is_absolute() and ".." not in path.parts
        full = root / rel
        assert full.resolve().is_relative_to(root) and not full.is_symlink()
        assert full.is_file(), f"missing snapshot file: {rel}"
        content = full.read_bytes()
        sha = hashlib.sha256(content).hexdigest()
        files.append({"path": rel, "content": base64.b64encode(content).decode(), "sha256": sha, "deleted": False})
        manifest.extend((rel + "\0" + sha + "\0false\n").encode())
    digest = hashlib.sha256(manifest).hexdigest()
    checks = []
    assert spec["checks"], "no executed checks"
    for check in spec["checks"]:
        assert 0 < len(check["name"].encode()) <= 256, "invalid check name length"
        cid = check["id"]
        assert cid.startswith(prefix), f"unprefixed check: {cid}"
        assert cid not in ids, f"duplicate check reference: {cid}"
        ids.add(cid)
        assert check["exit_code"] == 0, "failed/waived checks cannot be marked passed"
        log = pathlib.Path(check["log"])
        assert log.is_absolute() and log.is_file()
        assert log.stat().st_mtime >= started, "log predates this run"
        evidence = log.read_text()
        assert evidence.strip(), "empty evidence; supply actual command/exit record for silent build"
        assert len(evidence.encode()) <= 16384, "use a factual selected current-output excerpt log"
        checks.append({"id": cid, "name": check["name"], "passed": True, "evidence": evidence, "implementation_digest": digest})
    snapshot = prefix + "run0006-" + digest[:16]
    payload = {"stage": "engineering", "story": story, "outcome": "engineering_complete", "engineering": {"snapshot_id": snapshot, "check_ids": [c["id"] for c in checks]}, "capture": {"implementation_digest": digest, "files": files, "checks": checks}}
    raw = (json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n").encode()
    assert len(raw) <= 4*1024*1024, "payload exceeds bound"
    target = outdir / (story.lower() + "-engineering.json")
    assert not target.exists(), "do not rewrite submitted payloads"
    target.write_bytes(raw)
    summaries.append({"story": story, "payload": str(target), "snapshot_id": snapshot, "digest": digest, "file_count": len(files), "files": [f["path"] for f in files], "check_ids": [c["id"] for c in checks], "bytes": len(raw)})
print(json.dumps(summaries, indent=2))
