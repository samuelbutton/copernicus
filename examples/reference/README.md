# Bundled review results

These files come from the pinned Yamata revision in [the source record](../../compatibility/yamata.json).
They use only the synthetic [catalog](../catalog.json) and [second scoring template](../reanalysis-catalog.json).
No private recordings or external services supply this data.

The [manifest](v1/manifest.json) records source metadata, catalog hashes, and every bundled file hash.
The walkthrough validates those hashes before standalone import.
Copernicus then validates jobs, result references, recording hashes, and score semantics through its normal importer.

| Stage | Contents |
| --- | --- |
| `original` | Baseline and candidate jobs, six recordings, and their original results. |
| `candidate-v2` | Published job context and the candidate's three second-analysis results. |
| `baseline-v2` | Published job context and the baseline's three second-analysis results. |

The bundle omits execution databases and event notifications.
Result files provide enough information for validated import and index rebuild.
Repeated job context lets each stage verify exact accepted job bytes.
Measured result durations are kept as recorded. They are not performance guarantees.

## Run without the execution engine

Prerequisites: the [README setup](../../README.md#build-and-read-the-command-help).
From the repository root, run:

```sh
make demo
```

Expect incomplete groups, the collision regression, incompatible score versions, and restored comparisons after both sides select the second analysis.
No Yamata source checkout or executable is needed.
The command requests saved analysis selections and imports their previously calculated results.
It does not calculate new scores or run a simulation.
Follow [browser review](../../README.md#review-the-demo) to inspect inputs and cited ticks.

After review, stop the server and run from the repository root:

```sh
make demo-clean
make clean
```

These commands remove demo state and generated builds while keeping this bundle and installed dependencies.

## Regenerate for review

Prerequisites: the [combined-demo setup](../../README.md#run-the-combined-demo), installed dependencies, and no existing combined demo directory.
From the repository root, run:

```sh
make build
reference_dir=$(mktemp -d)
python3 scripts/walkthrough.py pair --yamata-source ../yamata --export "$reference_dir/bundle"
```

Expect a newly generated bundle and manifest under `$reference_dir/bundle`.
The command builds only the recorded commit through a temporary Git archive.
It does not edit the engine checkout or read that engine's database.
It checks unchanged original files, frozen snapshots, compatible metrics, and six simulation dispatches.

Review generated jobs, recordings, results, catalog hashes, and source metadata before replacing bundled files.
`make verify` compares fresh output with the current bundle.
Fresh queues assign new attempt identifiers and measured durations.
Verification excludes only those two result fields. All other result fields, jobs, and recordings must match.
A new engine revision requires a reviewed contract and adapter change before new reference data.

For cleanup, stop any demo server and run from the same shell:

```sh
rm -r "$reference_dir"
unset reference_dir
make demo-clean
make clean
```

Cleanup removes only the temporary export, recognized demo output, and generated builds.
The checked-in reference remains unchanged.
