# Import results and check request progress

A published result says what happened during one identified execution.
Copernicus checks its supporting files before adding it to the local result index.
Request progress counts validated outcomes against every frozen test.
It defaults to original scores; an explicit analysis selection reads that saved set.
Missing files and unresolved tests cannot become passing results.

## Read the published contract examples

Prerequisites: the [README tools](../README.md#build-and-read-the-command-help), installed dependencies, and a writable local filesystem.
Use one shell for this procedure.
No Yamata executable or second checkout is required.
From the repository root, run:

```sh
make build-cli
lifecycle_dir=$(mktemp -d)
cp -R compatibility/contract/v1/examples/valid "$lifecycle_dir/exchange"
./bin/copernicus catalog import --db "$lifecycle_dir/catalog.sqlite" --file examples/catalog.json
./bin/copernicus results import --db "$lifecycle_dir/catalog.sqlite" --exchange-dir "$lifecycle_dir/exchange"
./bin/copernicus results show --db "$lifecycle_dir/catalog.sqlite"
./bin/copernicus results import --db "$lifecycle_dir/catalog.sqlite" --exchange-dir "$lifecycle_dir/exchange"
```

Expect exit code `0` from each command.
The index contains two processed events and two results from the synthetic contract examples.
The repeated import adds zero events and zero results.
Both results are unassigned because no local request owns their exact jobs.
A correlation label alone cannot assign an outcome to a request.

The [completed event](../compatibility/contract/v1/examples/valid/events/completed-1.json) references a result by relative path and exact file hash.
That [result](../compatibility/contract/v1/examples/valid/results/run-1.json) references its job and recording in the same way.
The importer checks those references, input hashes, analysis identity, metric units, score limits, and evidence ticks.
It validates reported scores without rerunning simulation or scoring algorithms.

## Rebuild the result index

Prerequisites: the completed example above and the same shell variable.
From the repository root, remove only the copied event files and rebuild:

```sh
./bin/copernicus results show --db "$lifecycle_dir/catalog.sqlite" > "$lifecycle_dir/before.json"
rm "$lifecycle_dir/exchange/events/"*.json
./bin/copernicus results rebuild --db "$lifecycle_dir/catalog.sqlite" --exchange-dir "$lifecycle_dir/exchange"
./bin/copernicus results show --db "$lifecycle_dir/catalog.sqlite" > "$lifecycle_dir/after.json"
cmp "$lifecycle_dir/before.json" "$lifecycle_dir/after.json"
```

The rebuild finds both results without reading event files.
The comparison succeeds silently because result counts and references remain unchanged.
Previously processed event identities remain recorded for duplicate detection.
Rebuild changes only the derived result index; request snapshots, outgoing jobs, and publication identity history remain intact.

For cleanup, stop commands using this temporary directory.
From the repository root, run:

```sh
rm -r "$lifecycle_dir"
unset lifecycle_dir
make clean
```

These commands remove only the copied examples, temporary database, and generated builds.
The published contract fixtures and source files remain unchanged.

## Track a request through execution

Prerequisites: installed Copernicus dependencies and a `yamata` executable on `PATH`.
Build Yamata from the revision in [the source record](../compatibility/yamata.json).
Use one shell and a dedicated local exchange.
From the Copernicus repository root, run:

```sh
make build-cli
run_dir=$(mktemp -d)
mkdir "$run_dir/exchange"
./bin/copernicus catalog import --db "$run_dir/catalog.sqlite" --file examples/catalog.json
./bin/copernicus request create --db "$run_dir/catalog.sqlite" --id review-one --collection all-tests --controller baseline --requester reviewer > "$run_dir/request.json"
./bin/copernicus outbox publish --db "$run_dir/catalog.sqlite" --exchange-dir "$run_dir/exchange"
for job in "$run_dir"/exchange/jobs/*.json
do
  yamata enqueue --exchange-dir "$run_dir/exchange" "jobs/${job##*/}"
done
./bin/copernicus results import --db "$run_dir/catalog.sqlite" --exchange-dir "$run_dir/exchange"
./bin/copernicus request status --db "$run_dir/catalog.sqlite" --id review-one
```

The request contains three tests and reports zero completed outcomes.
Accepted events can show `PENDING`, but only validated terminal results count as completed.
Each import invocation exits after its scan; it starts no background worker.

From the same shell and repository root, run simulation while the importer is stopped:

```sh
yamata workers --exchange-dir "$run_dir/exchange" --simulation-workers 2 --analysis-workers 0 --drain
./bin/copernicus results import --db "$run_dir/catalog.sqlite" --exchange-dir "$run_dir/exchange"
./bin/copernicus request status --db "$run_dir/catalog.sqlite" --id review-one
```

Recordings now exist, but completed results remain zero because analysis has not run.
The next import resumes by scanning published files and checking saved event identities.
It does not rely on file timestamps or alphabetical event order.

From the same shell and repository root, finish analysis and replay the files:

```sh
yamata workers --exchange-dir "$run_dir/exchange" --simulation-workers 0 --analysis-workers 2 --drain
./bin/copernicus results import --db "$run_dir/catalog.sqlite" --exchange-dir "$run_dir/exchange"
./bin/copernicus request status --db "$run_dir/catalog.sqlite" --id review-one > "$run_dir/completed.json"
./bin/copernicus results import --db "$run_dir/catalog.sqlite" --exchange-dir "$run_dir/exchange"
./bin/copernicus results rebuild --db "$run_dir/catalog.sqlite" --exchange-dir "$run_dir/exchange"
./bin/copernicus request status --db "$run_dir/catalog.sqlite" --id review-one > "$run_dir/rebuilt.json"
cmp "$run_dir/completed.json" "$run_dir/rebuilt.json"
cat "$run_dir/completed.json"
```

Expect `total: 3`, `completed: 3`, `incomplete: 0`, and `complete: true`.
The comparison succeeds silently; duplicate delivery and rebuilding preserve the selected outcomes.
Completion does not mean that every test passed.
Read the separate pass, failure, warning, and error counts.
Keep this temporary directory for the API procedure and cleanup below.

## Read progress through HTTP

Prerequisites: the completed request above, `curl`, and an available loopback port `8080`.
From the same shell and repository root, start the API:

```sh
./bin/copernicus serve --db "$run_dir/catalog.sqlite" --port 8080 > "$run_dir/api.log" 2>&1 &
api_pid=$!
```

The log prints `Read-only API: http://127.0.0.1:8080` when the listener is ready.
An occupied port causes startup failure; the server does not select a different port automatically.
From the same shell, read the selected request:

```sh
curl --fail --retry 5 --retry-connrefused --retry-delay 1 http://127.0.0.1:8080/readyz
curl --fail http://127.0.0.1:8080/api/requests/review-one/status
curl --fail 'http://127.0.0.1:8080/api/results?limit=2'
```

The readiness response reports `ready`.
The request response reports the same completion counts as the CLI.
The result page contains at most two entries and a `next_after` cursor when another page exists.
Pass that cursor as the URL-encoded `after` parameter to read the next page.

| GET path | Response |
| --- | --- |
| `/healthz` | Process liveness without a database query. |
| `/readyz` | Readiness after checking local index access. |
| `/api/requests` | Request identifiers, with `after` and `limit` pagination. |
| `/api/requests/{id}` | The original frozen request record. |
| `/api/requests/{id}/status` | Completion counts and per-execution states. |
| `/api/results` | Paged result references, assignment, and current availability. |
| `/api/catalog` | Current catalog definitions for suite selection. |
| `/api/requests/{id}/executions/{execution}` | Frozen inputs and freshly validated result metrics. |
| `/api/requests/{id}/executions/{execution}/ticks/{tick}` | A recording tick cited by that result. |
| `/api/index` | Stored event, result, and unassigned-result counts. |
| `/api/comparisons` | [Request comparisons](comparisons.md#read-a-comparison-through-http), with explicit baseline and candidate identifiers. |

Without `--web-dir`, the API accepts GET requests only and opens SQLite in read-only mode.
It binds only `127.0.0.1`, rejects foreign Host or Origin values, and grants no cross-origin access.
Use the printed address instead of a hostname alias.
Add `--web-dir web/dist` to enable the [browser review interface](review-guide.md) and request creation.
The review guide describes its origin checks and worker handoff.

## Completion counts

`total` is the number of unique tests in the saved request.
`completed` counts validated `PASS`, `FAIL`, `WARN`, and `ERROR` outcomes for exact accepted jobs.
`incomplete` is `total` minus `completed`.
`complete` is true only when every selected test has a validated terminal result.
Pass, failure, warning, and error counts partition the completed outcomes.

Resolution failures remain visible in `resolution_failed` and the per-execution list.
They are included in `incomplete` because they never produced execution results.
A warning preserves unavailable metrics; an error preserves its explicit failure class.
Neither is counted as a pass.
Unknown executions remain unassigned until a matching local request supplies the exact job identity and hash.

Before a result arrives, progress reports the furthest observed stage for the accepted job.
Stages include `QUEUED`, `PUBLISHED`, `PENDING`, `RUNNING`, and `ANALYZING`.
This is observed progress, not a claim about the worker's current lease or most recent attempt.
Sequences belong to their job and attempt; timestamps do not order attempts.
A late event cannot replace a validated terminal result with an earlier stage.

Progress and result listings revalidate referenced files when read.
A missing file, changed hash, invalid recording, or inconsistent score produces `INCOMPLETE` instead of a cached pass.
Index totals describe stored references and can exceed currently readable outcomes.
Preserve the exchange files for as long as the index is in use.

## Storage and recovery

Schema four adds imported events, immutable publication identities, and the derived result index.
Write commands upgrade supported older databases transactionally; read commands never migrate them.
Stop older commands and back up the database before upgrading.
The existing exchange binding applies to import, rebuild, and publication.
A database cannot silently switch to a different exchange.

Each successful import transaction accepts one event and its referenced result together.
An exit before commit accepts neither; replay processes the same file again.
Invalid files do not advance event progress, and other valid files can still commit.
The import report includes rejected paths and bounded explanations; any rejection returns exit code `1`.
Repair availability problems and repeat the command using the same database and exchange.

Different bytes under an accepted identity or path are a conflict, including whitespace-only changes.
The importer preserves the original accepted outcome and rejects the conflicting file.
Rebuild validates result files before replacing the index in one transaction.
Validation or identity conflicts leave the previous index intact.
Removed result files disappear from a successful rebuild; request snapshots and identity history remain available.

The reader selects direct public files from `events/`, `results/`, `jobs/`, and `bags/` only.
It rejects links, traversal paths, non-regular files, oversized records, ambiguous JSON, and unsupported versions.
Temporary files are ignored during scans.
The [reader](../internal/exchange/read.go), [contract checks](../compatibility/outcomes.go), and [schema](../internal/store/results-v4.sql) own these validation and storage rules.

Each scan accepts at most 10,000 selected files; each public folder has the same entry limit.
The database retains at most 10,000 events, 10,000 results, 100,000 publication identities, and 64 mebibytes of result JSON.
JSON records are limited to one mebibyte and complete recordings to 16 mebibytes.
Rebuild stages at most 64 mebibytes of result JSON.
No automatic retention or cleanup service runs.

CLI scans have a 30-second deadline and a three-second database lock wait.
The HTTP API allows eight concurrent reads, with a 15-second operation deadline and bounded pages and responses.
It reports `429` when busy, `503` for unavailable local data, and `504` for timed-out reads.
The server also bounds header, body, write, idle, and shutdown durations.
The command owns these settings in [serve.go](../cmd/copernicus/serve.go).

## Verify interruption and cleanup

Prerequisites: installed Go dependencies and the supported local filesystem.
From the repository root, run:

```sh
go test ./internal/store -run 'TestImporterProcessExitIsAtomic|TestResultProgressReplayRebuildAndMissingFiles' -count=1
```

The tests exercise abrupt importer exit, duplicate and late events, missing files, and index recovery.
Expect exit code `0`; temporary test files are removed automatically.

Prerequisites for walkthrough cleanup: the same shell variables and completed API procedure above.
From the repository root, stop the API and remove only this walkthrough's data:

```sh
kill "$api_pid"
wait "$api_pid"
rm -r "$run_dir"
unset api_pid run_dir
make clean
```

The server drains active requests before closing its database.
Cleanup removes the temporary request, outbox, exchange, logs, and generated builds.
Source files, installed dependencies, and unrelated databases remain available.

## Select another analysis

Use `analysis=ID` on request status, execution review, and cited-tick GET routes.
Omitting this query selects `original`; unknown IDs return `404`.
`GET /api/requests/{id}/analyses` lists the original selection and saved templates.
In review-server mode, `POST /api/requests/{id}/analyses` accepts exactly `{"template_id":"edge-v2"}`.
It requires the same origin, JSON content type, and custom request header as request creation.
The default read-only server rejects this write.

[Schema five](../internal/store/analysis-v5.sql) adds immutable analysis selections and permits separate analysis jobs for existing executions.
Original results retain their exact job mapping.
Index rebuild preserves all accepted analyses; selected progress never substitutes another result.
The [reanalysis guide](reanalysis.md) provides the complete procedure and cleanup.

`GET /api/budgets` lists configured team limits, reserved ticks, and remaining ticks.
[Schema six](decisions.md) adds admission accounting and saved comparison pages.
The review server may save derived terminal comparison pages during GET reads; the default read-only server cannot.
