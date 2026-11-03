#!/usr/bin/env python3
"""Generate or rehearse the offline pre-cutover pending-state fixture.

No running daemon, provider credentials, network provider calls or runtime state
are used. Go may fetch module dependencies. Each child has a finite timeout.
"""
import argparse
import gzip
import pathlib
import shutil
import subprocess
import tarfile
import tempfile

BASELINE = "eb72718bc6443c082b26150797f795b613559259"
ROOT = pathlib.Path(__file__).resolve().parents[1]
FIXTURE = ROOT / "internal/store/testdata/factory-pending-state"
SOURCE = ROOT / "scripts/factory-pending-state/main.go"


def run(args, cwd):
    subprocess.run(args, cwd=cwd, check=True, timeout=600)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["generate", "rehearse"])
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="lf-factory-compat-") as directory:
        scratch = pathlib.Path(directory)
        archive = scratch / "baseline.tar"
        with archive.open("wb") as output:
            subprocess.run(["git", "archive", BASELINE], cwd=ROOT, stdout=output,
                           check=True, timeout=60)
        baseline = scratch / "baseline"
        baseline.mkdir()
        with tarfile.open(archive) as contents:
            contents.extractall(baseline, filter="data")
        command = baseline / "scripts/factory-pending-state"
        command.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(SOURCE, command / "main.go")
        if args.operation == "generate":
            database = scratch / "pending.db"
            manifest = scratch / "manifest.json"
            run(["go", "run", "./scripts/factory-pending-state", "create",
                 str(database), str(manifest), BASELINE], baseline)
            FIXTURE.mkdir(parents=True, exist_ok=True)
            with database.open("rb") as source, gzip.GzipFile(
                    filename=str(FIXTURE / "pending.db.gz"), mode="wb", mtime=0) as output:
                shutil.copyfileobj(source, output)
            shutil.copyfile(manifest, FIXTURE / "manifest.json")
        else:
            database = scratch / "pending.db"
            with gzip.open(FIXTURE / "pending.db.gz", "rb") as source, database.open("wb") as output:
                shutil.copyfileobj(source, output)
            manifest = FIXTURE / "manifest.json"
            # Old -> current migration/read -> old migration/read. Compare every
            # recorded value after each open. The checked-in fixture stays intact.
            for implementation in [baseline, ROOT, baseline]:
                run(["go", "run", "./scripts/factory-pending-state", "verify",
                     str(database), str(manifest), BASELINE], implementation)
            print("offline baseline -> current -> baseline reopen passed")


if __name__ == "__main__":
    main()
