#!/usr/bin/env python3
"""Run real pre-change/current/pre-change builds against latest synthetic state.

No live service is used. Every authorization phase is a separate process. The
SQLite database and synthetic credentials live only in a temporary directory.
Usage: python3 run.py [--prepare | --continue]
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=HERE, text=True).strip())
BASELINE = "5a08f9e4e99640ef56592c90094f1dae75babe87"
SESSION = HERE / "pending-local.json"
REPORT = HERE / "observed.log"

def command(args, cwd=ROOT, timeout=600):
    result = subprocess.run(args, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    with REPORT.open("a") as out:
        out.write("$ " + " ".join(str(a) for a in args if not str(a).startswith("/private/")) + "\n")
        out.write(result.stdout)
    if result.returncode:
        raise RuntimeError(result.stdout)
    return result.stdout

def build(root, target, identity):
    source = root / "cmd" / "lf1378rehearsal"
    source.mkdir(parents=True, exist_ok=False)
    inspector = root / "internal" / "store" / "lf1378_rehearsal.go"
    if inspector.exists():
        raise RuntimeError("temporary inspector path already exists")
    try:
        shutil.copy2(HERE / "main.go", source / "main.go")
        shutil.copy2(HERE / "store_schema.go", inspector)
        command(["go", "build", "-o", str(target), "-ldflags", "-X main.buildIdentity=" + identity, "./cmd/lf1378rehearsal"], cwd=root, timeout=900)
    finally:
        shutil.rmtree(source)
        inspector.unlink(missing_ok=True)
    with REPORT.open("a") as out:
        out.write(f"binary identity={identity} sha256={hashlib.sha256(target.read_bytes()).hexdigest()}\n")

def phase(binary, name, scratch):
    command([str(binary), name, str(scratch / "authorization.db"), str(scratch / "credentials.json")], timeout=120)

if "--continue" not in sys.argv:
    REPORT.write_text("Authorization compatibility rehearsal\nNo production URLs or real credentials. Controlled clock: 2026-11-03T12:00:00Z.\n")
    scratch = Path(tempfile.mkdtemp(prefix="lf1378-rehearsal-"))
    old = scratch / "baseline"
    old.mkdir()
    archive = subprocess.run(["git", "archive", BASELINE], cwd=ROOT, stdout=subprocess.PIPE, check=True, timeout=120).stdout
    subprocess.run(["tar", "-x", "-C", str(old)], input=archive, check=True, timeout=120)
    build(old, scratch / "old", BASELINE)
    phase(scratch / "old", "seed", scratch)
    phase(scratch / "old", "restart-old", scratch)
    SESSION.write_text(json.dumps({"scratch": str(scratch)}))
    if "--prepare" in sys.argv:
        print("Baseline build and restart passed; synthetic state prepared.")
        sys.exit(0)
else:
    scratch = Path(json.loads(SESSION.read_text())["scratch"])

# Copy a complete current source snapshot before injecting temporary helpers.
# Root packages may be under test: current builds never write into the worktree.
current = scratch / "current"
current.mkdir()
paths = subprocess.check_output(["git", "ls-files", "-c", "-o", "--exclude-standard", "-z"], cwd=ROOT).split(b"\0")
manifest = []
for raw in sorted(set(paths)):
    if not raw:
        continue
    name = os.fsdecode(raw)
    if name.startswith((".aiteam/", ".git/", ".litefactory/")):
        continue
    path = ROOT / name
    if not path.is_file():
        continue
    destination = current / name
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(path, destination)
    manifest.append((name, hashlib.sha256(destination.read_bytes()).hexdigest()))
identity = "source-sha256-" + hashlib.sha256(json.dumps(manifest, separators=(",", ":")).encode()).hexdigest()
(HERE / "changed-source-manifest.json").write_text(json.dumps({"identity": identity, "files": manifest}, indent=2) + "\n")
build(current, scratch / "changed", identity)
phase(scratch / "changed", "upgrade", scratch)
phase(scratch / "changed", "restart-current", scratch)
phase(scratch / "old", "rollback", scratch)
phase(scratch / "old", "restart-rollback", scratch)
phase(scratch / "changed", "generation-change", scratch)
phase(scratch / "old", "generation-restart", scratch)
shutil.rmtree(scratch)
SESSION.unlink()
print("PASS: old/restart/changed/restart/old/restart/changed-owner/old-restart; temporary database and credentials deleted.")
