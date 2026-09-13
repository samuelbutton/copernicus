# Admission limits and saved comparisons

Copernicus limits new simulation work before publishing jobs.
It also saves complete comparison pages so repeated reviews can reuse their calculations.
Both features use local SQLite records.
They require no account, remote scheduler, or shared analytical service.

## Budget decisions

A team budget measures simulation ticks, not money or elapsed time.
Admission reserves each ready execution's `max_ticks` from its frozen run template.
Shared suite members count once because request resolution removes duplicate tests.
The repeat number identifies one run; it does not multiply its cost.
A resolution failure has no simulation job and reserves zero ticks.

The default team is `local` when `team_id` is omitted.
Its initial cumulative allowance comes from [DefaultTickLimit](../internal/store/budgets.go).
Other teams require an explicit budget before request creation.
Team labels organize local work; they provide no authentication or access control.

A request, its budget reservation, its executions, and its outgoing jobs commit together.
An insufficient budget rejects the complete transaction before dispatch.
Concurrent submissions cannot spend the same remaining ticks.
An identical accepted retry returns the original request before checking or reserving budget again.
Changing the team or another submission field requires a new request identifier.

Reservations remain charged after completion, failure, or early stopping.
This conservative allowance limits accepted work rather than estimating actual runtime use.
An operator can raise the cumulative limit but cannot lower it below accepted reservations.
There is no automatic daily reset, refund, or time-based schedule.
Reanalysis reads saved recordings and reserves no additional simulation ticks.

## Reject work and retry safely

Prerequisites: the [README tools and installed dependencies](../README.md#build-and-read-the-command-help), Python 3, and an ordinary local account.
Use one shell for the following commands.
From the Copernicus repository root, run:

```sh
make build
limits_dir=$(mktemp -d)
mkdir "$limits_dir/exchange"
./bin/copernicus catalog import --db "$limits_dir/catalog.sqlite" --file examples/catalog.json
./bin/copernicus budget set --db "$limits_dir/catalog.sqlite" --team example --ticks 299
if ./bin/copernicus request create --db "$limits_dir/catalog.sqlite" --id budget-baseline --team example --collection all-tests --controller baseline --requester reviewer
then
  echo "Unexpected admission"
  exit 1
fi
./bin/copernicus outbox show --db "$limits_dir/catalog.sqlite"
./bin/copernicus budget show --db "$limits_dir/catalog.sqlite"
./bin/copernicus budget set --db "$limits_dir/catalog.sqlite" --team example --ticks 600
./bin/copernicus request create --db "$limits_dir/catalog.sqlite" --id budget-baseline --team example --collection all-tests --controller baseline --requester reviewer > "$limits_dir/accepted.json"
./bin/copernicus request create --db "$limits_dir/catalog.sqlite" --id budget-baseline --team example --collection all-tests --controller baseline --requester reviewer > "$limits_dir/retry.json"
cmp "$limits_dir/accepted.json" "$limits_dir/retry.json"
./bin/copernicus budget show --db "$limits_dir/catalog.sqlite" > "$limits_dir/budget-after-retry.json"
./bin/copernicus request create --db "$limits_dir/catalog.sqlite" --id budget-candidate --team example --collection all-tests --controller candidate --requester reviewer
```

The first submission needs `300` ticks and returns exit code `1` because only `299` remain.
The conditional treats that rejection as the expected outcome.
The outbox contains no jobs, and the team's reserved ticks remain zero.
After the limit increases, the accepted request reserves `300` ticks.
Its identical retry returns the same bytes and leaves that reservation unchanged.
The candidate reserves the remaining `300` ticks.

## Comparison identity and freshness

Each cache key identifies exact inputs and one view.
The key includes the following fields:

| Input | Purpose |
| --- | --- |
| Request snapshot hashes | Preserve test membership, team, controller, simulator, source, seed, and original templates. |
| Selected analyses and template hashes | Keep new scoring configurations separate from original results. |
| Result identities, paths, hashes, and states | Bind calculations to the exact validated evidence. |
| Query version, SQL hash, and DuckDB version | Prevent reuse across changed calculation rules. |
| Filter, sort, cursor, and page size | Preserve the displayed row selection and order. |

[CacheInputs](../internal/comparison/view.go) owns the complete identity format.
The key uses a sorted comparison plan, so map iteration cannot change its value.
Both requests must be complete before a page can be saved or reused.
Terminal failures and errors remain visible; completeness does not imply passing tests.
An incomplete request bypasses storage even when a filter hides its missing tests.

Every comparison first revalidates the selected results and supporting recordings.
A missing or changed file makes that selection incomplete and bypasses any previously saved page.
A cache hit skips DuckDB calculation, not evidence validation.
New analyses get separate keys and never replace the original saved page.
Index rebuild leaves request identities and valid comparison keys unchanged.

The CLI saves terminal pages only with `compare --save`.
Without that flag, it can reuse existing pages but does not write or migrate SQLite.
The review server saves eligible pages automatically.
The default read-only API can reuse saved pages but cannot create or evict them.
Browser text distinguishes reused, newly saved, and partial live comparisons.

`view.cache_state` reports `saved`, `hit`, `read_only`, or `bypass_partial`.
`view.cache_key` identifies eligible terminal inputs.
The cache retains immutable entries until bounded FIFO eviction removes the oldest entries.
Storage is limited to 128 pages and 32 MiB of combined input and result JSON.
Eviction does not change requests, budget reservations, analysis selections, or published files.

## Verify partial bypass and terminal reuse

Prerequisites: the requests above, installed DuckDB, and the same shell variables.
Place `yamata` on `PATH`, built from the revision in [the execution source record](../compatibility/yamata.json).
From the repository root, run:

```sh
./bin/copernicus compare --db "$limits_dir/catalog.sqlite" --baseline budget-baseline --candidate budget-candidate --save --filter regressions --limit 1 > "$limits_dir/partial.json"
./bin/copernicus outbox publish --db "$limits_dir/catalog.sqlite" --exchange-dir "$limits_dir/exchange"
for job in "$limits_dir"/exchange/jobs/*.json
do
  yamata enqueue --exchange-dir "$limits_dir/exchange" "jobs/${job##*/}"
done
yamata workers --exchange-dir "$limits_dir/exchange" --drain
./bin/copernicus results import --db "$limits_dir/catalog.sqlite" --exchange-dir "$limits_dir/exchange"
./bin/copernicus compare --db "$limits_dir/catalog.sqlite" --baseline budget-baseline --candidate budget-candidate --save --filter regressions --sort id-desc --limit 1 > "$limits_dir/saved.json"
./bin/copernicus compare --db "$limits_dir/catalog.sqlite" --baseline budget-baseline --candidate budget-candidate --filter regressions --sort id-desc --limit 1 --duckdb "$limits_dir/no-query-executable" > "$limits_dir/reused.json"
python3 - "$limits_dir" <<'PY'
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
def read(name):
    return json.loads((root / name).read_text())
team = next(b for b in read("budget-after-retry.json") if b["team_id"] == "example")
assert team["reserved_ticks"] == 300
assert team["remaining_ticks"] == 300
partial = read("partial.json")
assert partial["view"]["cache_state"] == "bypass_partial"
assert partial["counts"]["incomplete"] == 3
assert partial["rows"] == []
saved = read("saved.json")
reused = read("reused.json")
assert saved["view"]["cache_state"] == "saved"
assert reused["view"]["cache_state"] == "hit"
assert reused["view"]["cache_key"] == saved["view"]["cache_key"]
assert reused["rows"] == saved["rows"]
assert saved["counts"]["rows"] == 3
assert saved["view"]["matched_rows"] == 2
assert len(saved["rows"]) == 1
print("Budget reserved once; partial comparison bypassed; terminal page reused.")
PY
```

Expect successful commands and the printed verification message.
The initial filtered page is empty, but its full comparison still reports three incomplete groups.
The terminal comparison has two groups containing metric regressions and displays one per page.
The repeated read succeeds without a query executable because its validated inputs match the saved page.
Changing a filter, order, cursor, page size, or selected analysis requires a different cache entry.

## Filters, ordering, and browser review

Filters select all groups, metric regressions, incomplete groups, incompatible groups, or execution errors.
A regression filter selects any group containing a metric labeled `REGRESSION`.
All report counts retain the complete unfiltered denominator.
`view.matched_rows` counts filtered groups before pagination; `rows` contains only the requested page.

Sort `id` orders stable group identifiers ascending; `id-desc` reverses that order.
Use the returned `view.next_after` as `--after` for the next CLI page.
The HTTP API uses the same `filter`, `sort`, `after`, and `limit` parameters.
Unknown options are rejected; they cannot become SQL fragments.
The API limits pages to 100 groups; the CLI supports up to 2000.

Prerequisites: the completed results above, a modern browser, and an available port `8080`.
From the same shell and repository root, run:

```sh
./bin/copernicus serve --db "$limits_dir/catalog.sqlite" --web-dir web/dist --port 8080 > "$limits_dir/server.log" 2>&1 &
limits_pid=$!
```

Expect `Review interface: http://127.0.0.1:8080` in the log.
Open [the local interface](http://127.0.0.1:8080/).

1. Open **Create request** to inspect **Team budget** and its remaining ticks.
2. Return to **Compare** and select `budget-baseline` and `budget-candidate`.
3. Choose **Metric regressions** under **Show groups**.
4. Choose **Group ID, descending** under **Group order**.
5. Select **Compare requests**, then reload the page.
6. Open an execution and select **Back to comparison**.

The comparison identifies saved-page reuse after reload.
Its links retain the filter, order, page size, cursor, and both analysis selections.
Budget rejection preserves the creation form's entries for retry.
Changing budget limits remains an explicit CLI operation.

## Why keep this local

SQLite owns admission reservations, immutable request state, and the bounded comparison cache.
DuckDB queries private copies of validated result JSON on a cache miss.
This provides reproducible calculations without operating a shared analytical service.
It is a teaching substitute, not a distributed quota system or a multi-user warehouse.
The engine still owns simulation scheduling, execution, recordings, and scoring.

[Schema six](../internal/store/admission-v6.sql) upgrades existing databases in one transaction.
Migration charges accepted legacy requests using their frozen maximum tick costs without changing their snapshots or jobs.
If legacy reservations exceed the default allowance, migration sets the limit to that reserved total.
Such a team has no remaining allowance until its limit increases.
Read-only commands never perform this migration.

## Stop and clean up

Prerequisites: the same shell variables and completed review.
From the repository root, run:

```sh
kill "$limits_pid"
wait "$limits_pid"
rm -r "$limits_dir"
unset limits_pid limits_dir
make clean
```

Expect graceful shutdown and exit code `0`.
Cleanup removes this walkthrough's database, exchange, logs, saved pages, and generated builds.
Source files and installed dependencies remain available.
