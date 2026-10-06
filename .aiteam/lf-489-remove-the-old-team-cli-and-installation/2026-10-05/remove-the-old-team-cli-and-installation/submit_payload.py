import json,subprocess,sys
from pathlib import Path
p=Path(__file__).parent
payload=p/sys.argv[1]
descriptor=sys.argv[2]
exe="/Users/mariomh/litefactory/dist/litefactory"
def invoke(command,data):
    r=subprocess.run([exe,"submission",command,"--descriptor",descriptor],input=data,text=True,capture_output=True)
    if r.returncode: print(r.stderr);print(r.stdout[:4000]);raise SystemExit(r.returncode)
    return json.loads(r.stdout)
def redact(value):
    if isinstance(value,dict): return {k:redact(v) for k,v in value.items() if k not in ("Payload","Diagnostics")}
    if isinstance(value,list): return [redact(v) for v in value]
    return value
def summarize(d):
    return redact(d)
preflight=invoke("preflight",payload.read_text())
print(json.dumps(preflight))
if not preflight.get("result",{}).get("valid") or not preflight.get("result",{}).get("complete"):raise SystemExit(2)
submitted=invoke("submit",payload.read_text())
summary=summarize(submitted)
(p/(payload.stem+"-submit.json")).write_text(json.dumps(summary,indent=2)+"\n")
print(json.dumps(summary))
request_id=submitted.get("request_id")
if not request_id: raise SystemExit("No request identity; preserve response and do not retry")
receipt=invoke("receipt",json.dumps({"request_id":request_id}))
summary=summarize(receipt)
(p/(payload.stem+"-receipt.json")).write_text(json.dumps(summary,indent=2)+"\n")
print(json.dumps(summary))
