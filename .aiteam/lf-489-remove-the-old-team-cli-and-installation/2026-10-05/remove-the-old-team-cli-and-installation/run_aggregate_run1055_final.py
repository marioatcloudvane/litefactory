from pathlib import Path
import os
import subprocess
p = Path(__file__).parent
env = dict(os.environ, GOLANGCI_LINT_CACHE="/private/tmp/lf489-golangci-cache")
log_path = p / "make-check-run1055-final.log"
exit_path = p / "make-check-run1055-final.exit"
if log_path.exists() or exit_path.exists():
    raise SystemExit("Unique run1055 aggregate artifacts already exist; preserve them and choose a new execution identity.")
with log_path.open("x") as log:
    result = subprocess.run(["make", "check"], stdout=log, stderr=subprocess.STDOUT, env=env)
with exit_path.open("x") as exit_file:
    exit_file.write(str(result.returncode) + "\n")
raise SystemExit(result.returncode)
