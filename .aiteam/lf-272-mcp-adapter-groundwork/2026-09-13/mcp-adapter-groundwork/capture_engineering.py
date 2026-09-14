"""Capture current files and executed checks; run explicitly after stable scoped tests."""
import base64
import hashlib
import json
import pathlib
import subprocess
import sys

session = pathlib.Path(__file__).resolve().parent
spec = json.loads(pathlib.Path(sys.argv[1]).read_text())
files = []
manifest = bytearray()
for name in sorted(spec["files"]):
    path = pathlib.Path(name)
    if path.is_absolute() or ".." in path.parts or path.is_symlink():
        raise ValueError("unsafe capture path")
    deleted = not path.exists()
    content = b"" if deleted else path.read_bytes()
    digest = hashlib.sha256(content).hexdigest()
    files.append(dict(path=name, content=base64.b64encode(content).decode(), sha256=digest, deleted=deleted))
    manifest.extend((name + "\0" + digest + "\0" + str(deleted).lower() + "\n").encode())
digest = hashlib.sha256(manifest).hexdigest()
checks = [dict(check, implementation_digest=digest) for check in spec["checks"]]
if not checks or not all(check["passed"] is True and check["evidence"] for check in checks):
    raise ValueError("missing executed passing checks")
contribution = dict(stage="engineering", story=spec["story"], outcome="engineering_complete", engineering=dict(snapshot_id=spec["snapshot_id"], check_ids=[check["id"] for check in checks]), capture=dict(implementation_digest=digest, files=files, checks=checks))
data = json.dumps(contribution, separators=(",", ":")).encode()
base = ["/Users/mariomh/litefactory/dist/litefactory", "submission"]
descriptor = "/Users/mariomh/.litefactory/s/3b21867672079ed4/descriptor.json"
output = session / (spec["snapshot_id"] + "-handoff.json")
records = {"story": spec["story"], "snapshot_id": spec["snapshot_id"], "implementation_digest": digest}
for command in ("preflight", "submit"):
    result = subprocess.run(base + [command, "--descriptor", descriptor], input=data, capture_output=True)
    response = json.loads(result.stdout)
    response.get("result", {}).pop("Payload", None)
    records[command] = dict(returncode=result.returncode, stdout=json.dumps(response), stderr=result.stderr.decode())
    output.write_text(json.dumps(records, indent=2) + "\n")
    print(command, result.returncode, json.dumps(response), result.stderr.decode())
    if result.returncode:
        sys.exit(result.returncode)
    if command == "preflight":
        response = json.loads(result.stdout)["result"]
        # Caller must inspect the actual factory response before submission.
        if not (response.get("valid") is True and response.get("complete") is True):
            print("Preflight response needs inspection; submit not attempted")
            sys.exit(2)
