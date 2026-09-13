# Compare two requests

A comparison shows which measured values changed between two saved requests.
It preserves missing tests, missing results, and incompatible scoring rules.
A numerical improvement does not erase a collision or an execution error.

## Install the query tool

Prerequisites: the [README tools](../README.md#build-and-read-the-command-help), internet access, `curl`, `gzip`, and `shasum` or `sha256sum`.
Use macOS or Linux on an arm64 or x86-64 computer.
From the repository root, run:

```sh
make install-duckdb
./bin/duckdb -init /dev/null -version
```

Expect DuckDB `v1.5.5` and exit code `0`.
The [installer](../scripts/install-duckdb.sh) downloads an official release and verifies its pinned archive checksum before installing it.
It writes only inside `bin/` and removes its temporary download directory.
The executable remains an installed dependency after `make clean`.
After installation, comparisons need no internet access or downloaded extensions.

The [DuckDB CLI](https://duckdb.org/docs/current/clients/cli/overview) keeps query execution separate from the Go build.
Copernicus requires the pinned version before calculating metrics.
Use `--duckdb PATH` to select another installation of that version.
Run `rm bin/duckdb` from the repository root to remove this installed copy when no command uses it.

## Run and compare the two controllers

Prerequisites: installed Copernicus dependencies, DuckDB above, and `yamata` on `PATH`.
Build Yamata from the revision in [the source record](../compatibility/yamata.json).
Use one shell and a dedicated local exchange.
From the Copernicus repository root, run:

```sh
make build-cli
repo_dir=$(pwd)
compare_dir=$(mktemp -d)
mkdir "$compare_dir/exchange"
./bin/copernicus catalog import --db "$compare_dir/catalog.sqlite" --file examples/catalog.json
./bin/copernicus request create --db "$compare_dir/catalog.sqlite" --id baseline-one --collection all-tests --controller baseline --requester reviewer > "$compare_dir/baseline.json"
./bin/copernicus request create --db "$compare_dir/catalog.sqlite" --id candidate-one --collection all-tests --controller candidate --requester reviewer > "$compare_dir/candidate.json"
./bin/copernicus compare --db "$compare_dir/catalog.sqlite" --baseline baseline-one --candidate candidate-one
./bin/copernicus outbox publish --db "$compare_dir/catalog.sqlite" --exchange-dir "$compare_dir/exchange"
for job in "$compare_dir"/exchange/jobs/*.json
do
  yamata enqueue --exchange-dir "$compare_dir/exchange" "jobs/${job##*/}"
done
yamata workers --exchange-dir "$compare_dir/exchange" --drain
./bin/copernicus results import --db "$compare_dir/catalog.sqlite" --exchange-dir "$compare_dir/exchange"
./bin/copernicus compare --db "$compare_dir/catalog.sqlite" --baseline baseline-one --candidate candidate-one > "$compare_dir/comparison.json"
cat "$compare_dir/comparison.json"
```

Expect exit code `0` from each command.
Before execution, both requests show zero completed tests and three incomplete comparison rows.
After import, both requests show three completed tests and three `COMPARED` rows.
Find the row containing test `stopped-obstacle`.
Its collision count changes from zero to one, with delta `1` and change `REGRESSION`.

The baseline can fail another score limit without colliding.
Compare individual metrics as well as overall statuses.
The output preserves each selected result path, file hash, and analysis identifier.
It also identifies both frozen snapshots and controller hashes.
Exit code `0` means comparison succeeded; it does not mean the candidate passed.

## Check equal outcomes and changed membership

Prerequisites: the completed execution above and the same shell variables.
From the same repository root, compare one request with itself:

```sh
./bin/copernicus compare --db "$compare_dir/catalog.sqlite" --baseline baseline-one --candidate baseline-one
./bin/copernicus catalog set-suite --db "$compare_dir/catalog.sqlite" --suite smoke --tests=
./bin/copernicus request create --db "$compare_dir/catalog.sqlite" --id smaller-selection --collection all-tests --controller candidate --requester reviewer > "$compare_dir/smaller.json"
./bin/copernicus compare --db "$compare_dir/catalog.sqlite" --baseline baseline-one --candidate smaller-selection
./bin/copernicus compare --db "$compare_dir/catalog.sqlite" --baseline smaller-selection --candidate baseline-one
./bin/copernicus request show --db "$compare_dir/catalog.sqlite" --id baseline-one > "$compare_dir/baseline-after.json"
cmp "$compare_dir/baseline.json" "$compare_dir/baseline-after.json"
```

The self-comparison has zero deltas for available values and zero status changes.
Unavailable values remain unavailable; null never becomes zero.
The smaller request selects two tests and has no execution results yet.
Comparison reports one `REMOVED` row and two `INCOMPLETE` rows.
Reversing the sides reports one `ADDED` row instead.
The final file comparison succeeds silently because the original snapshot remains unchanged.

## Matching and row types

Rows match by scenario content, run template content, simulator content, source revision, seed, repeat number, and analysis template name.
The analysis name is its frozen catalog identifier.
Controller content, test labels, scenario labels, suite order, and collection labels do not determine pairing.
Changing a matching field produces unmatched rows instead of a numerical delta.

Each matching group retains all its selected executions.
If either side contains several executions with the same key, the row is `INCOMPARABLE` with reason `AMBIGUOUS_PAIRING`.
Copernicus does not choose an arbitrary pair or multiply matches.
Otherwise, matching rows require identical analysis content before scores can be compared.
Metric versions and units must also agree.

| Row type | Example and meaning |
| --- | --- |
| `COMPARED` | Both selected analyses are readable and compatible; metric deltas are available where both values exist. |
| `INCOMPARABLE` | The same analysis name has different content, a metric version or unit differs, or pairing is ambiguous. |
| `INCOMPLETE` | A matched execution has no validated result, including resolution failures and changed or missing supporting files. |
| `ERROR` | Matched compatible selections include a validated execution error; no metric delta is calculated. |
| `ADDED` | Only the candidate selected this matching key. |
| `REMOVED` | Only the baseline selected this matching key. |

Classification checks membership, ambiguity, analysis content, result availability, and execution errors in that order.
An unmatched row can therefore contain an incomplete execution.
Per-side completion counts still expose that absence.
The add-only catalog currently prevents replacing an analysis definition under an existing name.
The content-conflict guard also protects selected reanalyses and independently constructed snapshots.

## Deltas and denominators

A delta is the candidate value minus the baseline value, in the stated unit.
More collisions are worse; less obstacle gap or goal progress is worse.
Metric changes are `REGRESSION`, `IMPROVEMENT`, `UNCHANGED`, or `UNAVAILABLE`.
Each metric preserves the two original pass flags.
An unavailable value has a null delta and never counts as an unchanged zero.

`status_changed` compares overall statuses for `COMPARED` rows only.
It is null for other row types.
A row can contain both an improved metric and a regressed metric.
The report does not collapse those changes into a safety judgement or causal explanation.

| Count | Denominator |
| --- | --- |
| Each side's `total` | Every unique test selected in that saved request. |
| Each side's `completed` | Its validated `PASS`, `FAIL`, `WARN`, and `ERROR` outcomes. |
| Each side's `incomplete` | Its total minus completed, including resolution failures. |
| `counts.rows` | Unique matching groups across both sides. |
| Row-type counts | Six mutually exclusive parts of `counts.rows`. |
| `metric_pairs` | Three metric slots for each `COMPARED` row. |
| `available_metrics` | Slots with values on both sides. |
| `unavailable_metrics` | Remaining slots with at least one unavailable value. |
| Regressions, improvements, unchanged | Three mutually exclusive parts of available metrics. |
| `status_changes` | Compared rows whose overall statuses differ. |

Group counts can differ from execution totals when duplicate content makes pairing ambiguous.
Missing and incompatible rows do not enter metric denominators.
An execution error counts as completed on its side, but never as passed or as an available metric pair.

## Inspect published metric data directly

Prerequisites: the completed controller execution and the same shell variables.
From the repository root, run this inspection query in the temporary directory:

```sh
(
  cd "$compare_dir"
  "$repo_dir/bin/duckdb" -init /dev/null -batch -json :memory: "SELECT execution_id, status, metrics.collision_count.value AS collisions FROM read_json_auto('exchange/results/*.json') ORDER BY execution_id;"
)
```

Expect six result rows with their execution identifiers, statuses, and collision counts.
This direct query inspects files; it does not perform Copernicus's validation or pairing checks.
The application uses the [delta query](../internal/comparison/deltas.sql) only after validating the exact selected outcomes.
The [query runner](../internal/comparison/duckdb.go) creates private temporary copies, then restricts DuckDB to those files.

## Read a comparison through HTTP

Prerequisites: the completed request data, `curl`, and an available loopback port `8080`.
From the same shell and repository root, run:

```sh
./bin/copernicus serve --db "$compare_dir/catalog.sqlite" --port 8080 > "$compare_dir/api.log" 2>&1 &
api_pid=$!
curl --fail --retry 5 --retry-connrefused --retry-delay 1 http://127.0.0.1:8080/readyz
curl --fail 'http://127.0.0.1:8080/api/comparisons?baseline=baseline-one&candidate=candidate-one&limit=2'
```

The response contains `comparison` and, when needed, `next_after`.
Pass the cursor as `after` with the same baseline and candidate identifiers.
The page contains at most `limit` rows; all counts describe the complete comparison.
The default limit is 50 and the maximum is 100.
Every page recalculates current availability; this is not a saved comparison.

## Limits, failures, and cleanup

Comparison defaults to read-only SQLite; `--save` enables terminal-page storage and schema upgrades.
It reads only indexed outcomes assigned to the exact selected jobs.
Missing, changed, or unreadable supporting files become incomplete before querying.
Saved results cannot supply a passing outcome when its supporting files are unavailable.

Selected result copies are limited to 64 mebibytes across both sides.
DuckDB uses one thread, a 128-megabyte memory setting, no disk spill, and a ten-second deadline.
The CLI keeps its 30-second overall deadline; HTTP keeps its 15-second deadline.
The HTTP server permits one active comparison and returns `429` for another concurrent comparison.
Other result reads retain their existing limits.

The query process ignores user startup configuration and receives no inherited credentials or extension configuration.
Automatic extension installation and loading are disabled.
File access is restricted to the generated input paths, and configuration is locked before the query.
Normal completion, cancellation, and query errors remove the temporary files.
Forced process termination can leave a `copernicus-comparison-*` directory under the operating system's temporary directory.

An absent or wrong DuckDB version returns a command error when metrics require querying.
Incomplete comparisons with no comparable pairs need no query process.
Query failures return no partial metric report.
HTTP returns a bounded generic error; the CLI retains the local cause.

Prerequisites for cleanup: the same shell variables and completed HTTP procedure.
From the repository root, run:

```sh
kill "$api_pid"
wait "$api_pid"
rm -r "$compare_dir"
unset api_pid compare_dir repo_dir
make clean
```

Expect exit code `0` and graceful server shutdown.
Cleanup removes this walkthrough's database, exchange, reports, and generated application builds.
It preserves source files, installed dependencies, and unrelated databases.

## Choose the scores on each side

`--baseline-analysis ID` and `--candidate-analysis ID` select saved analyses independently.
Both default to `original`.
The equivalent HTTP parameters are `baseline_analysis` and `candidate_analysis`.
The browser exposes both choices and preserves them in review and pagination links.

Pairing uses each test's original frozen analysis name.
Selected template content then determines scoring compatibility.
Changing one side to a different template makes matching rows incomparable; matching both sides restores compatible deltas.
An unknown selection fails explicitly, including when both sides use the same request.
Follow the [reanalysis procedure](reanalysis.md) for commands, expected outcomes, and cleanup.

[Saved comparison decisions](decisions.md#comparison-identity-and-freshness) define cache identity, filters, ordering, page limits, and partial-result bypass.
