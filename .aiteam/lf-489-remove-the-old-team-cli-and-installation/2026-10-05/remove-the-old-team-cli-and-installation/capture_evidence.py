import base64,hashlib,json,subprocess,sys
from pathlib import Path
session=Path(__file__).parent
paths=set(subprocess.check_output(["git","diff","--name-only","--diff-filter=ACDMRTUXB","-z"]).decode().split("\0"))
paths.update(subprocess.check_output(["git","ls-files","--others","--exclude-standard","-z"]).decode().split("\0"))
files=[]
manifest=b""
for name in sorted(paths):
    if not name or name.startswith(".aiteam/") or name.startswith(".litefactory-story-events/"): continue
    path=Path(name)
    deleted=not path.exists()
    data=b"" if deleted else path.read_bytes()
    sha=hashlib.sha256(data).hexdigest()
    files.append(dict(path=name,content=base64.b64encode(data).decode(),sha256=sha,deleted=deleted))
    manifest+=(name+"\0"+sha+"\0"+str(deleted).lower()+"\n").encode()
digest=hashlib.sha256(manifest).hexdigest()
checks=json.loads((session/sys.argv[2]).read_text())
for check in checks: check["implementation_digest"]=digest
payload={"engineering":{"snapshot_id":sys.argv[1],"check_ids":[c["id"] for c in checks]},"capture":{"implementation_digest":digest,"files":files,"checks":checks}}
if len(sys.argv)>3: payload.update(stage="engineering",story=sys.argv[3],outcome="engineering_complete")
output=session/(sys.argv[1]+".json")
output.write_text(json.dumps(payload,separators=(",",":")))
print(json.dumps(dict(path=str(output),files=len(files),bytes=output.stat().st_size,digest=digest)))
