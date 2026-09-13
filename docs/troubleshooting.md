# Troubleshooting

Use these checks from the repository root.
Keep the database and its exchange together.
Stop commands using a demo before cleanup.

## Find the failed boundary

| Symptom | Meaning | Recovery |
| --- | --- | --- |
| A build reports missing modules or packages. | Dependency setup is incomplete. | Complete [README setup](../README.md#build-and-read-the-command-help) with internet access, then retry offline. |
| Python rejects the archive extraction option. | The walkthrough requires Python 3.12 or later. | Run Make with `PYTHON=python3.12`, or use a newer installed Python. |
| The recorded engine revision is unavailable. | The source repository lacks the pinned commit. | Fetch the [recorded revision](../compatibility/yamata.json) during setup, then set `YAMATA_SOURCE` to that repository. |
| A demo reports that its directory exists. | A previous run still owns that output. | Finish review, stop its server, and run `make demo-clean` before retrying. |
| Cleanup reports a missing or changed ownership marker. | The path is not a recognized demo directory. | Inspect ownership manually. Do not add a marker to authorize deletion. |
| Cleanup rejects a symbolic link. | The output path could lead outside the intended directory. | Use a regular checkout path and inspect the linked data separately. |
| DuckDB is missing or has the wrong version. | Comparisons require the pinned local query executable. | Run `make install-duckdb` during setup, then retry the comparison. |
| Bundled example hashes do not match. | A reference file or catalog changed. | Restore reviewed bytes, or follow [reference regeneration](../examples/reference/README.md#regenerate-for-review). |
| The server cannot bind its port. | Another process already uses that address. | Use `make demo-serve PORT=8081` and open the printed address. |
| The page opens but cannot load requests. | A static preview has no review API, or the server stopped. | Start `make demo-serve` after `make demo`. Use its printed address. |
| Request submission exceeds the team budget. | Accepted reservations consumed the available ticks. | Choose fewer tests or raise the team's limit with the [budget procedure](decisions.md#reject-work-and-retry-safely). |
| A request remains incomplete. | Its selected results or supporting files are unavailable. | Inspect progress. Complete publication, workers, and import using the [lifecycle procedure](lifecycle.md#track-a-request-through-execution). |
| A comparison says incompatible. | Selected scoring configurations differ or pairing is ambiguous. | Select matching score versions and inspect the [pairing rules](comparisons.md#matching-and-row-types). |
| A request name produces an identity conflict. | That name already identifies different frozen inputs. | Use a new request name. Accepted requests cannot be replaced. |
| An import reports conflicting bytes. | A published identity already has different accepted content. | Restore the exact original files before retrying. Do not edit hashes or reset publication records. |
| A saved comparison becomes live and incomplete. | Its supporting files no longer validate. | Restore exact evidence bytes. A saved page cannot replace missing evidence. |
| New scoring jobs stay pending in the standalone demo. | Bundled results cover only the documented requests and templates. | Use the combined demo or the [reanalysis workflow](reanalysis.md) for additional calculations. |
| Browser verification cannot find Chromium. | The test browser is not installed. | Run `npm exec --prefix web -- playwright install chromium` during setup. |

`demo-pending` deliberately has no results.
It demonstrates incomplete counts after the other demo requests finish.
The standalone demo does not start workers or create an execution queue.
Its bundled second analysis is previously calculated data, not a new local calculation.

## Inspect a demo without changing it

Prerequisites: a completed `make demo` and the [README tools](../README.md#build-and-read-the-command-help).
From the repository root, run:

```sh
./bin/copernicus request status --db .copernicus/standalone/catalog.sqlite --id demo-pending
./bin/copernicus outbox show --db .copernicus/standalone/catalog.sqlite
./bin/copernicus analysis list --db .copernicus/standalone/catalog.sqlite --request demo-candidate
./bin/copernicus budget show --db .copernicus/standalone/catalog.sqlite
```

Expect three incomplete tests for `demo-pending`, pending run jobs, and the candidate's `original` and `edge-v2` selections.
These reads do not publish jobs, change budgets, or start workers.
Replace `standalone` with `pair` to inspect the combined demo.

For cleanup, stop the demo server with `Ctrl+C`.
From the repository root, run:

```sh
make demo-clean
make clean
```

Demo cleanup removes only recognized standalone and combined demo directories.
Build cleanup removes only the CLI and built web assets.
Both keep installed tools, bundled examples, source files, and unrelated data.

## Report a reproducible failure

Record the command, exit code, operating system, and tool versions.
Include the failed check and a small synthetic example.
Do not include private data, credentials, or unrelated files.
`make verify` prints its tested environment and rejects failures rather than substituting results.
