#!/usr/bin/env python3
"""Disposable operator wrapper for the existing contract-upgrade CLI."""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_record(path, record):
    # Each run gets a new private directory; no previous evidence is overwritten.
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def invoke(binding, action, directory, timeout):
    path = directory / (action + ".json")
    record = dict(binding, action=action, started_at=now(), status="running")
    write_record(path, record)
    command = [binding["executable"], "story-contract", "upgrade",
               binding["task"], "--" + action]
    try:
        result = subprocess.run(command, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=False)
        record.update(status="finished", exit_code=result.returncode,
                      stdout=result.stdout.decode("utf-8", errors="replace"),
                      stderr=result.stderr.decode("utf-8", errors="replace"))
    except (OSError, subprocess.TimeoutExpired) as error:
        record.update(status="uncertain" if action == "apply" else "failed",
                      exit_code=1, error=str(error))
        if isinstance(error, subprocess.TimeoutExpired):
            record["stdout"] = (error.stdout or b"").decode("utf-8", errors="replace")
            record["stderr"] = (error.stderr or b"").decode("utf-8", errors="replace")
    record["finished_at"] = now()
    write_record(path, record)
    print(record.get("stdout", ""), end="")
    print(record.get("stderr", ""), end="", file=sys.stderr)
    if "error" in record:
        print(record["error"], file=sys.stderr)
    print("Retained CLI observation: " + str(path), file=sys.stderr)
    return record


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("issue", choices=("230", "259"))
    parser.add_argument("--executable", default="/Users/mariomh/litefactory/dist/litefactory",
                        help="absolute path to the existing factory executable")
    parser.add_argument("--output-dir", required=True,
                        help="operator evidence directory outside LF_HOME")
    parser.add_argument("--apply", action="store_true",
                        help="explicitly authorize factory apply after matching a reviewed preview")
    parser.add_argument("--reviewed-preview", type=Path,
                        help="successful preview.json retained by this wrapper; required for apply")
    parser.add_argument("--timeout", type=int, default=300,
                        help="maximum seconds per factory invocation (default: 300)")
    args = parser.parse_args()
    if args.apply != (args.reviewed_preview is not None):
        parser.error("--apply and --reviewed-preview must be used together")
    if args.timeout <= 0:
        parser.error("--timeout must be positive")
    executable = Path(args.executable)
    if not executable.is_absolute() or not executable.is_file() or not os.access(executable, os.X_OK):
        parser.error("--executable must name an existing executable by absolute path")
    lf_home = Path(os.environ.get("LF_HOME") or Path.home() / ".litefactory").resolve()
    output = Path(args.output_dir).resolve()
    if output == lf_home or lf_home in output.parents:
        parser.error("--output-dir must be outside daemon runtime state (LF_HOME)")
    binding = {"task": "github:marioatcloudvane/litefactory#" + args.issue,
               "executable": str(executable.resolve()),
               "executable_sha256": digest(executable), "lf_home": str(lf_home)}
    reviewed = None
    if args.reviewed_preview:
        reviewed = json.loads(args.reviewed_preview.read_text(encoding="utf-8"))
        if (not isinstance(reviewed, dict)
                or any(reviewed.get(k) != v for k, v in binding.items())
                or reviewed.get("action") != "preview"
                or reviewed.get("status") != "finished"
                or reviewed.get("exit_code") != 0
                or not isinstance(reviewed.get("stdout"), str)
                or not reviewed["stdout"].strip()):
            parser.error("reviewed preview must be successful and match this task, binary and LF_HOME")
    output.mkdir(parents=True, exist_ok=True)
    directory = Path(tempfile.mkdtemp(prefix="upgrade-" + args.issue + "-", dir=output))
    preview = invoke(binding, "preview", directory, args.timeout)
    if preview["exit_code"] != 0:
        print("Factory preview refused or failed. Resolve its diagnostics through supported flows; no apply ran.", file=sys.stderr)
        return 1
    if not args.apply:
        print("Preview only. Review the saved preview.json before explicitly requesting --apply.")
        return 0
    if preview.get("stdout") != reviewed["stdout"]:
        print("Preview changed. Review the new preview.json and use that file on a new explicit apply invocation. No apply ran.", file=sys.stderr)
        return 1
    if digest(executable) != binding["executable_sha256"]:
        print("Factory executable changed; obtain a new preview. No apply ran.", file=sys.stderr)
        return 1
    applied = invoke(binding, "apply", directory, args.timeout)
    if applied["exit_code"] != 0:
        print("Apply failed or its outcome is uncertain. Retain these logs; preview again and resume the supported apply after review. Do not edit runtime state.", file=sys.stderr)
        return 1
    print("Factory apply returned success. Publish and approve the next Plan before engineering. This does not prove reader retirement or delivery drain.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as error:
        print("Upgrade wrapper failed: " + str(error), file=sys.stderr)
        sys.exit(1)
