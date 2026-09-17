"""Capture exact files and executed check evidence for the supplied factory helper."""
import base64
import hashlib
import json
import pathlib
import sys

story, snapshot, paths_file, checks_file, output_file = sys.argv[1:]
paths = sorted(set(json.loads(pathlib.Path(paths_file).read_text())))
files = []
manifest = bytearray()
for name in paths:
    path = pathlib.Path(name)
    if path.is_absolute() or ".." in path.parts or path.is_symlink():
        raise ValueError("unsafe snapshot path")
    deleted = not path.exists()
    data = b"" if deleted else path.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    files.append({"path": name, "content": base64.b64encode(data).decode(),
                  "sha256": digest, "deleted": deleted})
    manifest.extend((name + "\0" + digest + "\0" + str(deleted).lower() + "\n").encode())
digest = hashlib.sha256(manifest).hexdigest()
checks = json.loads(pathlib.Path(checks_file).read_text())
for check in checks:
    if check.get("passed") is not True or not check.get("evidence"):
        raise ValueError("only actual passing checks may be captured")
    check["implementation_digest"] = digest
payload = {"stage":"engineering", "story":story, "outcome":"engineering_complete",
           "engineering":{"snapshot_id":snapshot,"check_ids":[check["id"] for check in checks]},
           "capture":{"implementation_digest":digest,"files":files,"checks":checks}}
pathlib.Path(output_file).write_text(json.dumps(payload, separators=(",", ":")))
print(json.dumps({"story":story,"snapshot_id":snapshot,"implementation_digest":digest,
                  "files":len(files),"checks":len(checks)}))
