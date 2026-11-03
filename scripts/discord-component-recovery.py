#!/usr/bin/env python3
"""Create or rehearse extraction-specific offline Discord state compatibility.

Uses a git archive, isolated temporary databases and bounded child processes.
No runtime configuration, provider credentials or live service is accessed.
"""
import argparse
import gzip
import os
import pathlib
import shutil
import subprocess
import tarfile
import tempfile

BASELINE = "d41b68afd2f722003d0d12efe396801be4195367"
ROOT = pathlib.Path(__file__).resolve().parents[1]
FIXTURE = ROOT / "internal/store/testdata/discord-component-recovery"
TEST = pathlib.Path("internal/store/discord_component_recovery_test.go")


def run(tree, directory, operation):
    env = os.environ.copy()
    env.update(LF_DISCORD_FIXTURE_OPERATION=operation,
               LF_DISCORD_FIXTURE_DIRECTORY=str(directory), GOFLAGS="-trimpath")
    subprocess.run(["go", "test", "-count=1", "-timeout=5m", "-run",
                    "^TestDiscordComponentBaselineRecovery$", "./internal/store"],
                   cwd=tree, env=env, check=True, timeout=360)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["generate", "rehearse"])
    parser.add_argument("--evidence-dir", type=pathlib.Path, help="retain fixture process manifests and logs, without the database")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="lf-discord-compat-") as name:
        scratch = pathlib.Path(name)
        archive = scratch / "baseline.tar"
        with archive.open("wb") as output:
            subprocess.run(["git", "archive", BASELINE], cwd=ROOT,
                           stdout=output, check=True, timeout=60)
        baseline = scratch / "baseline"
        baseline.mkdir()
        with tarfile.open(archive) as contents:
            contents.extractall(baseline, filter="data")
        shutil.copyfile(ROOT / TEST, baseline / TEST)
        state = scratch / "state"
        state.mkdir()
        if args.operation == "generate":
            run(baseline, state, "create")
            FIXTURE.mkdir(parents=True, exist_ok=True)
            with (state / "pending.db").open("rb") as source, gzip.GzipFile(
                    filename=str(FIXTURE / "pending.db.gz"), mode="wb", mtime=0) as output:
                shutil.copyfileobj(source, output)
            shutil.copyfile(state / "manifest.json", FIXTURE / "manifest.json")
            print("Fixture written by baseline " + BASELINE)
        else:
            with gzip.open(FIXTURE / "pending.db.gz", "rb") as source, (state / "pending.db").open("wb") as output:
                shutil.copyfileobj(source, output)
            shutil.copyfile(FIXTURE / "manifest.json", state / "manifest.json")
            for tree in [baseline, ROOT, baseline]:
                run(tree, state, "verify")
            run(ROOT, state, "recover")
            run(baseline, state, "verify")
            print("Phase: compile previous-code reader at " + BASELINE, flush=True)
            reader = scratch / "baseline-store-reader"
            subprocess.run(["go", "test", "-c", "-trimpath", "-o", str(reader), "./internal/store"],
                           cwd=baseline, check=True, timeout=360)
            process_state = scratch / "process-state"
            process_state.mkdir()
            env = os.environ.copy()
            env.update(LF_DISCORD_PROCESS_EXPORT=str(process_state),
                       LF_DISCORD_BASELINE_READER=str(reader), GOFLAGS="-trimpath")
            print("Phase: new start -> stop/drain -> previous-code pending-state check -> new resume -> stop/drain", flush=True)
            subprocess.run(["go", "test", "-race", "-v", "-count=1", "-timeout=2m", "-run",
                            "^TestDiscordComponentProcessStopDrainRestart$", "./internal/discordcomponent"],
                           cwd=ROOT, env=env, check=True, timeout=180)
            print("Phase: previous code verifies completed delivery fence and retained pending state", flush=True)
            subprocess.run(["go", "test", "-v", "-count=1", "-timeout=2m", "-run",
                            "^TestDiscordComponentProcessRollback$", "./internal/store"],
                           cwd=baseline, env=env, check=True, timeout=180)
            if args.evidence_dir:
                destination = args.evidence_dir.resolve()
                destination.mkdir(parents=True, exist_ok=True)
                for retained in ["pending-manifest.json", "process-manifest.json", "baseline-pending-read.log",
                                 "before.log", "after.log", "before.drained", "after.drained"]:
                    shutil.copyfile(process_state / retained, destination / retained)
                (destination / "baseline.txt").write_text(BASELINE + "\n")
                print("Fixture process evidence retained at " + str(destination), flush=True)
            print("Baseline/current/baseline reopen and current recovery/baseline reopen passed")
            print("New start/stop -> previous-code unchanged pending-state read -> new resume passed")


if __name__ == "__main__":
    main()
