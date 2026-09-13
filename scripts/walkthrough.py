"""Prepare a local review through public commands and immutable exchange files."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
REFERENCE = ROOT / "examples/reference/v1"
MARKER = ".copernicus-demo"
MARKER_BYTES = b"Copernicus demo directory version 1\n"
PHASES = ("original", "candidate-v2", "baseline-v2")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def call(*args, cwd=ROOT, timeout=60, env=None):
    result = subprocess.run(list(map(str, args)), cwd=cwd, env=env,
                            capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f"Command failed: {args[0]}\n{result.stdout}{result.stderr}")
    return result.stdout


def safe_path(directory):
    directory = Path(os.path.abspath(directory))
    require(not any(p.is_symlink() for p in (directory, *directory.parents)),
            "Demo paths cannot contain symbolic links.")
    return directory


def owned(directory):
    directory = safe_path(directory)
    marker = directory / MARKER
    require(marker.is_file() and not marker.is_symlink() and
            marker.read_bytes() == MARKER_BYTES, "Demo ownership marker is missing or changed.")
    return directory


def create(directory):
    directory = safe_path(directory)
    directory.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    directory.mkdir(mode=0o700)  # Never reuse an existing experiment.
    (directory / MARKER).write_bytes(MARKER_BYTES)
    return directory


def clean(directories):
    # Check every target before removing any directory. Never traverse a link.
    targets = [safe_path(p) for p in directories]
    for path in targets:
        if path.exists():
            owned(path)
    for path in targets:
        if path.exists():
            shutil.rmtree(path)


def build_engine(source, destination, go="go"):
    """Build the exact recorded revision, independent of the source worktree."""
    revision = json.loads((ROOT / "compatibility/yamata.json").read_text())["revision"]
    source = Path(source).resolve()
    actual = call("git", "-C", source, "rev-parse", "--verify", revision + "^{commit}").strip()
    require(actual == revision, "The recorded engine revision is unavailable.")
    with tempfile.TemporaryDirectory(prefix="engine-source-", dir=destination.parent) as temporary:
        staging = Path(temporary)
        archive = staging / "source.tar"
        call("git", "-C", source, "archive", "--format=tar", "--output", archive, revision,
             "go.mod", "go.sum", "cmd", "internal")
        tree = staging / "source"
        tree.mkdir()
        with tarfile.open(archive) as tar:
            members = tar.getmembers()
            require(all(m.isfile() or m.isdir() for m in members), "Engine archive contains links or special files.")
            tar.extractall(tree, filter="data")
        environment = dict(os.environ, GOWORK="off", GOPROXY="off")
        call(go, "build", "-trimpath", "-o", destination, "./cmd/yamata",
             cwd=tree, timeout=180, env=environment)
    return destination


def files(directory):
    return {p.relative_to(directory).as_posix(): p.read_bytes()
            for folder in ("jobs", "bags", "results")
            for p in sorted((directory / folder).glob("*")) if p.is_file()}


def manifest(directory):
    return {p.relative_to(directory).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(directory.rglob("*")) if p.is_file() and p.name != "manifest.json"}


def check_reference(reference=REFERENCE):
    expected = json.loads((reference / "manifest.json").read_text())
    require(expected["files"] == manifest(reference), "Bundled example hashes do not match; restore the reference files.")
    require(expected["engine"] == json.loads((ROOT / "compatibility/yamata.json").read_text()),
            "Bundled results use a different engine revision.")
    for name, digest in expected["catalogs"].items():
        require(hashlib.sha256((ROOT / "examples" / name).read_bytes()).hexdigest() == digest,
                "The example catalog changed; regenerate and review the bundled results.")
    require(not any(p.is_symlink() for p in reference.rglob("*")), "Bundled examples cannot contain symbolic links.")


def transfer(source, destination):
    """Preserve existing bytes; the demo never overwrites published evidence."""
    for name, data in files(source).items():
        path = destination / name
        path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        if path.exists():
            require(path.read_bytes() == data, f"Published file conflict: {name}")
        else:
            with path.open("xb") as output:
                output.write(data)


def semantic(data, name):
    if name.startswith("results/"):
        result = json.loads(data)
        result.pop("timing")  # Runtime and queue attempt IDs differ across fresh runs.
        result.pop("attempt_id")
        return result
    return data


class Walkthrough:
    def __init__(self, directory, engine=None, reference=REFERENCE):
        self.directory = directory
        self.db = directory / "catalog.sqlite"
        self.exchange = directory / "exchange"
        self.engine = engine
        self.reference = reference

    def cli(self, *args):
        return call(ROOT / "bin/copernicus", *args)

    def command(self, *args):
        return self.cli(*args, "--db", self.db)

    def json(self, *args):
        return json.loads(self.command(*args))

    def save(self, name, value):
        (self.directory / (name + ".json")).write_text(json.dumps(value, indent=2) + "\n")
        return value

    def compare(self, name, *args):
        return self.save(name, self.json("compare", "--baseline", "demo-baseline", "--candidate", "demo-candidate",
                                        "--duckdb", ROOT / "bin/duckdb", "--save", *args))

    def process(self, phase):
        self.command("outbox", "publish", "--exchange-dir", self.exchange)
        before = files(self.exchange)
        if self.engine:
            for job in sorted((self.exchange / "jobs").glob("*.json")):
                call(self.engine, "enqueue", "--exchange-dir", self.exchange, "jobs/" + job.name)
            call(self.engine, "workers", "--exchange-dir", self.exchange, "--drain",
                 "--simulation-workers", "1" if phase == "original" else "0", "--analysis-workers", "1")
        else:
            transfer(self.reference / phase, self.exchange)
        for name, data in before.items():
            require((self.exchange / name).read_bytes() == data, f"Existing evidence changed: {name}")
        self.command("results", "import", "--exchange-dir", self.exchange)
        # Capture only this phase's additions for a small, staged standalone fixture.
        additions = {name: data for name, data in files(self.exchange).items() if name not in before}
        additions.update({name: data for name, data in before.items() if name.startswith("jobs/")})
        stage = self.directory / "stages" / phase
        for name, data in additions.items():
            target = stage / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)

    def run(self):
        self.exchange.mkdir(mode=0o700)
        for catalog in ("catalog.json", "reanalysis-catalog.json"):
            self.command("catalog", "import", "--file", ROOT / "examples" / catalog)
        snapshots = {}
        for controller in ("baseline", "candidate"):
            args = ("request", "create", "--id", "demo-" + controller, "--collection", "all-tests",
                    "--controller", controller, "--requester", "reviewer")
            snapshots[controller] = self.command(*args)
            require(self.command(*args) == snapshots[controller], "Request retry changed the snapshot.")
        partial = self.compare("partial")
        require(partial["counts"]["incomplete"] == 3 and partial["view"]["cache_state"] == "bypass_partial",
                "Expected three incomplete groups before results arrive.")
        print("Before results: 0 of 3 completed; partial comparisons bypass saved pages.", flush=True)
        self.process("original")
        original = self.compare("original")
        require(original["counts"]["compared"] == 3, "Expected three compared groups.")
        row = next(r for r in original["rows"] if r["candidate"][0]["test_id"] == "stopped-obstacle")
        collision = next(m for m in row["metrics"] if m["name"] == "collision_count")
        require((collision["baseline"], collision["candidate"], collision["delta"], collision["change"]) ==
                (0, 1, 1, "REGRESSION"), "Expected a collision regression from zero to one.")
        print("Stopped obstacle: collision count 0 → 1; delta +1; REGRESSION.", flush=True)
        preserved = files(self.exchange)
        for side in ("candidate", "baseline"):
            selection = self.command("analysis", "create", "--request", "demo-" + side, "--template", "edge-v2")
            require(self.command("analysis", "create", "--request", "demo-" + side, "--template", "edge-v2") == selection,
                    "Analysis retry changed the selection.")
            self.process(side + "-v2")
            if side == "candidate":
                mixed = self.compare("incompatible", "--candidate-analysis", "edge-v2")
                require(mixed["counts"]["incomparable"] == 3 and mixed["counts"]["metric_pairs"] == 0,
                        "Different scoring versions must not produce deltas.")
                print("Different scoring versions: 3 incompatible groups; no metric deltas.", flush=True)
        matched = self.compare("matched", "--baseline-analysis", "edge-v2", "--candidate-analysis", "edge-v2")
        require(matched["counts"]["compared"] == 3, "Matching scores did not restore comparison.")
        for name, data in preserved.items():
            require((self.exchange / name).read_bytes() == data, f"Original evidence changed: {name}")
        require(len(list((self.exchange / "bags").glob("*"))) == 6, "Reanalysis added recordings.")
        require(len(list((self.exchange / "results").glob("*"))) == 12, "Expected twelve results.")
        for side, snapshot in snapshots.items():
            require(self.command("request", "show", "--id", "demo-" + side) == snapshot, "Frozen inputs changed.")
        if self.engine:
            queue = json.loads(call(self.engine, "queue", "--exchange-dir", self.exchange))
            require(queue["dispatch_positions"]["simulation"] == 6, "Reanalysis ran a simulation.")
            self.save("queue", queue)
            public = [p.relative_to(self.exchange).as_posix() for folder in ("jobs", "bags", "results", "events")
                      for p in sorted((self.exchange / folder).glob("*"))]
            call(self.engine, "validate", "--exchange-dir", self.exchange, *public)
        self.command("results", "rebuild", "--exchange-dir", self.exchange)
        reused = self.compare("reused", "--duckdb", self.directory / "no-query-executable")
        require(reused["view"]["cache_state"] == "hit" and reused["rows"] == original["rows"], "Saved original comparison changed.")
        # Keep one explicit pending request available in the final browser review.
        self.command("request", "create", "--id", "demo-pending", "--collection", "all-tests",
                     "--controller", "candidate", "--requester", "reviewer")
        self.compare("pending", "--candidate", "demo-pending")
        print("Matched scores: 3 compared groups; 6 unchanged recordings; 12 preserved result sets.", flush=True)


def verify_reference(demo, reference=REFERENCE):
    check_reference(reference)
    for phase in PHASES:
        expected, actual = files(reference / phase), files(demo.directory / "stages" / phase)
        require(expected.keys() == actual.keys(), f"Reference file inventory changed: {phase}")
        for name in expected:
            require(semantic(expected[name], name) == semantic(actual[name], name), f"Reference values changed: {phase}/{name}")


def export_reference(directory, destination):
    require(not destination.exists(), "Reference export requires a new directory.")
    shutil.copytree(directory / "stages", destination)
    metadata = {"engine": json.loads((ROOT / "compatibility/yamata.json").read_text()),
                "catalogs": {name: hashlib.sha256((ROOT / "examples" / name).read_bytes()).hexdigest()
                             for name in ("catalog.json", "reanalysis-catalog.json")}, "files": manifest(destination)}
    (destination / "manifest.json").write_text(json.dumps(metadata, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("standalone", "pair", "clean", "serve"))
    parser.add_argument("--yamata-source", default="../yamata")
    parser.add_argument("--go", default="go")
    parser.add_argument("--review", choices=("standalone", "pair"), default="standalone")
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--export", type=Path, help="Export fresh pair files to a new reference directory.")
    args = parser.parse_args()
    directory = ROOT / ".copernicus" / (args.review if args.mode == "serve" else args.mode)
    try:
        if args.mode == "clean":
            clean([ROOT / ".copernicus" / mode for mode in ("standalone", "pair")])
            print("Demo directories removed; other data and installed tools remain.")
        elif args.mode == "serve":
            owned(directory)
            os.execv(ROOT / "bin/copernicus", [str(ROOT / "bin/copernicus"), "serve", "--db", str(directory / "catalog.sqlite"),
                     "--web-dir", str(ROOT / "web/dist"), "--duckdb", str(ROOT / "bin/duckdb"), "--port", str(args.port)])
        else:
            require(args.export is None or args.mode == "pair", "Only the combined demo can export reference files.")
            if args.mode == "standalone":
                check_reference()
            create(directory)
            engine = build_engine(args.yamata_source, directory / "yamata", args.go) if args.mode == "pair" else None
            demo = Walkthrough(directory, engine)
            demo.run()
            if args.export:
                export_reference(directory, args.export)
            else:
                verify_reference(demo)
            print(f"Read .copernicus/{args.mode}/*.json. Open the interface with make demo-serve MODE={args.mode}.")
            print("Stop the review server before make demo-clean. Existing demo directories are never overwritten.")
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired, tarfile.TarError) as error:
        parser.exit(1, f"Walkthrough failed: {error}\nOutput is preserved. See docs/troubleshooting.md.\n")


if __name__ == "__main__":
    main()
