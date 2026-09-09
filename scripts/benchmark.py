#!/usr/bin/env python3
"""Compare local CLI latency and directory coverage with installed sesh/zoxide.

Run inside Herdr after scripts/build.sh. Only list and preview commands run.
Zoxide queries may perform their usual database cleanup.
"""

import argparse
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import time


def measure(argv, runs):
    samples = []
    output = ""
    for i in range(runs + 1):
        start = time.perf_counter()
        result = subprocess.run(argv, capture_output=True, text=True, check=True)
        elapsed = (time.perf_counter() - start) * 1000
        if i:
            samples.append(elapsed)
        output = result.stdout
    return {
        "median_ms": round(statistics.median(samples), 2),
        "min_ms": round(min(samples), 2),
        "max_ms": round(max(samples), 2),
    }, output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="./bin/herdr-sesh")
    parser.add_argument("--before", help="optional saved baseline binary")
    parser.add_argument("--config", help="optional config used by both binaries")
    parser.add_argument("--runs", type=int, default=20)
    parser.add_argument("--preview", default=str(Path.cwd()))
    args = parser.parse_args()
    if args.runs < 1:
        parser.error("--runs must be positive")

    commands = {"zoxide": ["zoxide", "query", "--list"]}
    if shutil.which("sesh"):
        commands["sesh_zoxide"] = ["sesh", "list", "-z"]
    binaries = {"after": args.binary}
    if args.before:
        binaries["before"] = args.before
    for label, binary in binaries.items():
        prefix = [str(Path(binary).resolve())]
        if args.config:
            prefix += ["--config", str(Path(args.config).resolve())]
        commands[label + "_zoxide"] = prefix + ["list", "--source", "zoxide", "--json"]
        commands[label + "_preview"] = prefix + ["preview", args.preview]
        if os.environ.get("HERDR_SOCKET_PATH"):
            commands[label + "_all"] = prefix + ["list", "--json"]
            commands[label + "_picker_rows"] = prefix + ["list", "--picker-lines"]

    outputs = {}
    for name, command in commands.items():
        try:
            timing, outputs[name] = measure(command, args.runs)
            print(name, json.dumps(timing), flush=True)
        except subprocess.CalledProcessError as exc:
            print(name, "ERROR:", exc.stderr.strip(), flush=True)

    if "zoxide" not in outputs:
        raise SystemExit("Cannot compare coverage: zoxide query failed")
    paths = outputs["zoxide"].splitlines()
    for label in binaries:
        name = label + "_zoxide"
        if name not in outputs:
            continue
        actual = [c["path"] for c in json.loads(outputs[name])]
        print(label + "_coverage", json.dumps({
            "zoxide": len(paths),
            "listed": len(actual),
            "missing": len(set(paths) - set(actual)),
            "extra": len(set(actual) - set(paths)),
            "same_order": actual == paths,
        }), flush=True)
    if "sesh_zoxide" in outputs:
        sesh_paths = [os.path.expanduser(p) for p in outputs["sesh_zoxide"].splitlines()]
        print("sesh_coverage", json.dumps({
            "listed": len(sesh_paths),
            "same_paths_and_order": sesh_paths == paths,
        }))


if __name__ == "__main__":
    main()
