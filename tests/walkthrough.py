"""Verify standalone and combined public workflows, reference drift, and cleanup."""

import argparse
from contextlib import contextmanager
import json
import os
from pathlib import Path
import platform
import re
import selectors
import shutil
import subprocess
import sys
import tempfile
from urllib.request import urlopen

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from walkthrough import (ROOT, REFERENCE, MARKER, MARKER_BYTES, Walkthrough, build_engine,
                         call, check_reference, clean, create, files, require, transfer, verify_reference)


def rejected(action):
    try:
        action()
    except (OSError, ValueError):
        return
    raise AssertionError("Expected the operation to reject its input.")


def cleanup_checks(parent):
    other = parent / "unrelated"
    other.mkdir()
    (other / "keep").write_text("preserve")
    rejected(lambda: clean([other]))
    marked = create(parent / "owned")
    (marked / "keep").write_text("original")
    rejected(lambda: create(marked))
    # Validation of all targets precedes any deletion.
    rejected(lambda: clean([marked, other]))
    require((marked / "keep").read_text() == "original", "Rejected cleanup changed data.")
    link = parent / "linked"
    link.symlink_to(marked, target_is_directory=True)
    rejected(lambda: clean([link]))
    rejected(lambda: create(link / "child"))
    (marked / MARKER).write_bytes(b"changed")
    rejected(lambda: clean([marked]))
    (marked / MARKER).unlink()
    (other / "marker-copy").write_bytes(MARKER_BYTES)
    (marked / MARKER).symlink_to(other / "marker-copy")
    rejected(lambda: clean([marked]))
    (marked / MARKER).unlink()
    (marked / MARKER).write_bytes(MARKER_BYTES)
    (marked / "external-link").symlink_to(other, target_is_directory=True)
    clean([marked])
    require((other / "keep").read_text() == "preserve", "Cleanup followed an internal link.")
    clean([marked])  # Missing targets are harmless.
    print("PASS: exclusive creation and cleanup preserve unrelated files and reject linked paths.", flush=True)


def reference_checks(parent):
    check_reference()
    altered = parent / "altered-reference"
    shutil.copytree(REFERENCE, altered)
    result = next((altered / "original/results").glob("*.json"))
    result.write_bytes(result.read_bytes() + b" ")
    rejected(lambda: check_reference(altered))
    destination = parent / "conflicting-exchange"
    transfer(REFERENCE / "original", destination)
    before = files(destination)
    result = next((destination / "results").glob("*.json"))
    result.write_bytes(b"different")
    rejected(lambda: transfer(REFERENCE / "original", destination))
    require(result.read_bytes() == b"different", "Reference copy overwrote conflicting data.")
    for name, data in before.items():
        if name != result.relative_to(destination).as_posix():
            require((destination / name).read_bytes() == data, "Reference copy changed another file.")
    print("PASS: changed bundled hashes and conflicting publications are rejected.", flush=True)


@contextmanager
def serve(directory):
    process = subprocess.Popen([str(ROOT / "bin/copernicus"), "serve", "--db", str(directory / "catalog.sqlite"),
                                "--web-dir", str(ROOT / "web/dist"), "--duckdb", str(ROOT / "bin/duckdb"), "--port", "0"],
                               cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            require(selector.select(timeout=10), "Review server startup timed out.")
            output = process.stdout.readline()
        match = re.search(r"http://127\.0\.0\.1:\d+", output)
        require(match is not None, f"Review server failed: {output}")
        yield match[0]
    finally:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
            raise
        process.stdout.close()
    require(process.returncode == 0, "Review server did not shut down cleanly.")


def inspect_http(directory):
    with serve(directory) as url:
        def read(path):
            with urlopen(url + path, timeout=10) as response:
                return response.read()
        require(b'<div id="root">' in read("/"), "Built review assets are unavailable.")
        for path, category in (("&candidate=demo-candidate", "compared"),
                               ("&candidate=demo-pending", "incomplete"),
                               ("&candidate=demo-candidate&candidate_analysis=edge-v2", "incomparable")):
            report = json.loads(read("/api/comparisons?baseline=demo-baseline" + path))["comparison"]
            require(report["counts"][category] == 3, f"HTTP review lost {category} groups.")
    print("PASS: built review server exposes complete, incomplete, and incompatible demo selections.", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--yamata-source", required=True)
    parser.add_argument("--go", default="go")
    parser.add_argument("--npm", default="npm")
    args = parser.parse_args()
    print(f"Environment: {platform.platform()}; Python {platform.python_version()}", flush=True)
    for command in ((args.go, "version"), ("node", "--version"), (args.npm, "--version"),
                    (ROOT / "bin/duckdb", "-init", "/dev/null", "-version")):
        print(call(*command).strip(), flush=True)
    with tempfile.TemporaryDirectory(prefix="copernicus-verification-") as temporary:
        parent = Path(temporary).resolve()
        cleanup_checks(parent)
        reference_checks(parent)
        standalone = create(parent / "standalone with spaces")
        demo = Walkthrough(standalone)
        demo.run()
        verify_reference(demo)
        require(not (demo.exchange / ".queue").exists(), "Standalone review created engine state.")
        inspect_http(standalone)
        engine = build_engine(args.yamata_source, parent / "yamata", args.go)
        pair = create(parent / "pair with spaces")
        combined = Walkthrough(pair, engine)
        combined.run()
        verify_reference(combined)
        inspect_http(pair)
        for name in ("partial", "original", "incompatible", "matched", "pending"):
            saved = json.loads((standalone / (name + ".json")).read_text())
            fresh = json.loads((pair / (name + ".json")).read_text())
            require(saved["counts"] == fresh["counts"], f"Demo counts differ: {name}")
            require([r["metrics"] for r in saved["rows"]] == [r["metrics"] for r in fresh["rows"]],
                    f"Demo metric values differ: {name}")
        print("PASS: both demos agree; fresh engine output matches all reference fields except duration and queue attempt IDs.", flush=True)
        environment = dict(os.environ, YAMATA_BIN=str(engine))
        print(call(args.npm, "test", cwd=ROOT / "web", timeout=300, env=environment), flush=True)
        clean([standalone, pair])
        require(not standalone.exists() and not pair.exists(), "Demo cleanup left output behind.")
    print("PASS: walkthroughs, reference drift, browser acceptance, and scoped cleanup.", flush=True)


if __name__ == "__main__":
    main()
