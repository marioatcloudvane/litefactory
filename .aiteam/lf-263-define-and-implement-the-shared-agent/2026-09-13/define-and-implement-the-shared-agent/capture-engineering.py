"""Build bounded stdin contribution from exact files and executed-check output.
Usage: python3 capture-engineering.py STORY SNAPSHOT CHECK_ID CHECK_NAME OUTPUT PATH...
This captures facts only; it does not execute or certify checks.
"""
import base64
import hashlib
import json
import pathlib
import sys
story, snapshot, check_id, check_name, output, *paths = sys.argv[1:]
files = []
manifest = bytearray()
for name in sorted(set(paths)):
    path = pathlib.Path(name)
    if path.is_absolute() or ".." in path.parts or any(part.is_symlink() for part in [path, *path.parents]):
        raise SystemExit("unsafe capture path")
    data = path.read_bytes()
    sha = hashlib.sha256(data).hexdigest()
    files.append(dict(path=name, content=base64.b64encode(data).decode(), sha256=sha, deleted=False))
    manifest.extend((name + "\0" + sha + "\0false\n").encode())
digest = hashlib.sha256(manifest).hexdigest()
evidence = pathlib.Path(output).read_text()
if len(evidence.encode()) > 16384:
    raise SystemExit("check output exceeds evidence limit; provide an honest bounded summary")
contribution = dict(stage="engineering", story=story, outcome="engineering_complete", engineering=dict(snapshot_id=snapshot, check_ids=[check_id]), capture=dict(implementation_digest=digest, files=files, checks=[dict(id=check_id, name=check_name, passed=True, evidence=evidence, implementation_digest=digest)]))
print(json.dumps(contribution, separators=(",", ":")))
