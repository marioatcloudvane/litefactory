from pathlib import Path
import os,subprocess
p=Path(__file__).parent
env=dict(os.environ,GOLANGCI_LINT_CACHE="/private/tmp/lf489-golangci-cache")
with (p/"make-check.log").open("w") as log:
    result=subprocess.run(["make","check"],stdout=log,stderr=subprocess.STDOUT,env=env)
(p/"make-check.exit").write_text(str(result.returncode)+"\n")
