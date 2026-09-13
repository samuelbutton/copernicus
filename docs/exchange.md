# Deliver jobs through the outbox

Saving a request also saves complete instructions for each compatible test.
The outbox holds these instructions until a separate command publishes them as files.
An interruption leaves unfinished deliveries available for the next invocation.
The same execution identifiers and file bytes survive that restart.

## Save a request before publication

Prerequisites: the [README tools](../README.md#build-and-read-the-command-help), installed Go dependencies, and a writable local filesystem.
Use macOS or Linux and one shell for this walkthrough.
No execution service or second checkout is required for publication.
From the repository root, run:

```sh
make build-cli
exchange_dir=$(mktemp -d)
mkdir "$exchange_dir/exchange"
./bin/copernicus catalog import --db "$exchange_dir/catalog.sqlite" --file examples/catalog.json
./bin/copernicus request create --db "$exchange_dir/catalog.sqlite" --id delivery-one --collection all-tests --controller baseline --requester reviewer > "$exchange_dir/request.json"
./bin/copernicus outbox show --db "$exchange_dir/catalog.sqlite"
test ! -e "$exchange_dir/exchange/jobs"
```

Expect exit code `0` from each command.
The outbox reports `pending: 3`, `published: 0`, and an empty `exchange_dir`.
The request process has exited after committing the snapshot, execution records, and job bytes together.
No job file exists yet because publication has not started.

## Resume delivery in another process

Prerequisites: the same temporary database and shell variable from the previous procedure.
Each invocation below starts a new process.
From the repository root, change the suite and repeat the accepted submission:

```sh
./bin/copernicus catalog set-suite --db "$exchange_dir/catalog.sqlite" --suite smoke --tests empty-lane
./bin/copernicus request create --db "$exchange_dir/catalog.sqlite" --id delivery-one --collection all-tests --controller baseline --requester reviewer > "$exchange_dir/retry.json"
cmp "$exchange_dir/request.json" "$exchange_dir/retry.json"
./bin/copernicus outbox publish --db "$exchange_dir/catalog.sqlite" --exchange-dir "$exchange_dir/exchange" --limit 1
cp -R "$exchange_dir/exchange/jobs" "$exchange_dir/first-batch"
```

The comparison succeeds silently because the accepted snapshot is unchanged.
Publication reports `acknowledged: 1`, `pending: 2`, and `published: 1`.
It creates one complete job file under `exchange/jobs/`.
The database now retains the resolved absolute exchange path as its delivery destination.

From the same shell and repository root, resume the remaining deliveries:

```sh
./bin/copernicus outbox publish --db "$exchange_dir/catalog.sqlite" --exchange-dir "$exchange_dir/exchange"
for job in "$exchange_dir"/first-batch/*.json
do
  cmp "$job" "$exchange_dir/exchange/jobs/${job##*/}"
done
cp -R "$exchange_dir/exchange/jobs" "$exchange_dir/expected-jobs"
./bin/copernicus outbox publish --db "$exchange_dir/catalog.sqlite" --exchange-dir "$exchange_dir/exchange"
diff -r "$exchange_dir/expected-jobs" "$exchange_dir/exchange/jobs"
./bin/copernicus outbox show --db "$exchange_dir/catalog.sqlite"
```

The first publication acknowledges the remaining two jobs.
The repeated publication acknowledges zero jobs; the comparisons succeed silently.
The final status reports `pending: 0` and `published: 3`.
Each job's `execution_id` matches its saved execution in `request.json`.
The suite edit does not change accepted job inputs or create replacement executions.

Publication means the files are durable and visible.
It does not mean that Yamata accepted them, ran simulations, or produced passing scores.
No background publisher or worker starts automatically.

## Request-to-job mapping

The [adapter](../internal/adapter/jobs.go) reads only the accepted snapshot.
It emits one `run` job for each `READY` execution, in snapshot order.
A `RESOLUTION_FAILED` execution emits no job; other compatible tests in that request remain eligible.
An invalid ready snapshot rejects the entire request transaction or migration.

| Saved value | Public job field |
| --- | --- |
| Execution identifier | `execution_id`, preserved exactly. |
| SHA-256 of `run`, a newline, and the execution identifier | `job_id`: `j` followed by the first 63 hexadecimal characters. |
| Request identifier | `correlation_id`. |
| Submission priority | `priority`. |
| Frozen scenario, controller, and simulator content | Matching fields under `inputs`, without catalog identifiers. |
| Frozen run template's tick duration | `inputs.run_template.tick_ms`. |
| Frozen run template's tick and time limits | `inputs.limits.max_ticks` and `inputs.limits.timeout_ms`. |
| Frozen analysis template | `inputs.analysis_template`. |
| Execution seed and repeat number | `inputs.seed` and `inputs.repeat`. |
| Complete translated inputs | `inputs_hash`, using the public canonical encoding. |

The requester's label, collection, suites, and catalog references remain in Copernicus.
Yamata receives resolved inputs and an optional correlation identifier, without request objects or database references.
The adapter checks the frozen source against [the pinned revision](../compatibility/yamata.json).
It rejects unsupported implementation versions before saving a job.

Public input hashes sort object keys recursively, retain array order, and use shortest decimal integers.
The encoding contains no insignificant whitespace or final newline.
File hashes cover the exact saved job bytes, including their final newline.
Publication retrieves those bytes from SQLite instead of encoding the snapshot again.
The [hash tests](../compatibility/contract_test.go) preserve the published example's known input hash.

## Pinned schemas and published examples

The complete [version-one contract](../compatibility/contract/v1/) is copied from the revision in the source record.
The copied [checksum manifest](../compatibility/contract/v1/SHA256SUMS) verifies every schema and example.
Tests also pin the manifest's hash and compare the embedded schema with its source file.
The binary loads no external schema URLs or files at runtime.
Contract upgrades require a reviewed source revision, copied fixtures, and adapter compatibility checks together.

The published [run job](../compatibility/contract/v1/examples/valid/jobs/run-1.json) supplies complete simulation and scoring inputs.
Its [result](../compatibility/contract/v1/examples/valid/results/run-1.json) references that job and its recording by exact file hash.
Its [completion event](../compatibility/contract/v1/examples/valid/events/completed-1.json) announces the result.
These are synthetic contract examples, not results produced by this walkthrough.
The [adapter test](../internal/adapter/jobs_test.go) reproduces the run example's input hash independently.

The [lifecycle guide](lifecycle.md) covers result import.
Analysis-only submission belongs to later work.

## Optional handoff to Yamata

Prerequisites: the completed publication walkthrough and a `yamata` executable on `PATH`.
Build that executable from the revision recorded in [the source record](../compatibility/yamata.json).
Use the same temporary exchange and shell; stop any other execution commands using that exchange.
From the Copernicus repository root, run:

```sh
for job in "$exchange_dir"/exchange/jobs/*.json
do
  yamata validate --exchange-dir "$exchange_dir/exchange" "jobs/${job##*/}"
  yamata enqueue --exchange-dir "$exchange_dir/exchange" "jobs/${job##*/}"
  yamata enqueue --exchange-dir "$exchange_dir/exchange" "jobs/${job##*/}"
done
yamata workers --exchange-dir "$exchange_dir/exchange" --drain
ls "$exchange_dir/exchange/results"
```

Validation prints `Contract valid.` for each file.
Each first receipt reports `duplicate=false`; its repeated receipt reports `duplicate=true` with the same identifiers.
Workers publish three result files and then exit.
Read each result's status to distinguish scores from operational errors.
Use the [result importer](lifecycle.md) to read these published outcomes.
Copernicus never reads Yamata's private queue database.

## Failure and restart behavior

The [outbox schema](../internal/store/outbox-v3.sql) preserves job identities, exact bytes, hashes, and publication acknowledgements.
Triggers prevent content changes, deletion, and resetting an acknowledged delivery.
Pending jobs remain durable after command errors or process exits.
The publisher acknowledges a file only after synchronizing its contents and directory.

| Interruption point | Next invocation |
| --- | --- |
| Before the request transaction commits | No request, execution, or job is accepted; repeat the submission. |
| After commit, before publication | Publish the pending saved bytes with their original identifiers. |
| During temporary-file writing | Ignore the incomplete `.tmp` file and publish a complete new temporary file. |
| After final rename, before acknowledgement | Compare existing bytes, synchronize them, and record delivery without replacing the file. |
| After acknowledgement or confirmation-output failure | Inspect `outbox show`; an acknowledged job is not sent again. |

Prerequisites: installed Go dependencies and the supported local filesystem described above.
From the repository root, run the interruption tests:

```sh
go test ./internal/store -run TestOutboxProcessExitRecovery -count=1
```

The tests use isolated child processes and temporary databases, and remove their temporary files automatically.
They exit during saving, immediately after commit, and after publication before acknowledgement.
Expect exit code `0` and preserved execution identifiers and job bytes after recovery.

Existing different bytes are a conflict, including formatting-only changes.
Publication stops with exit code `1`, leaving that job pending; earlier acknowledgements remain committed.
Resolve the conflicting file's ownership before retrying the same publication command.
Do not edit saved jobs, reset acknowledgements, or remove accepted exchange files to retry execution.

## Storage and operating limits

Use a dedicated local exchange on macOS or Linux with exclusive rename and directory synchronization support.
Linux uses `renameat2` with `RENAME_NOREPLACE`; macOS uses `renameatx_np` with `RENAME_EXCL`.
Unsupported filesystems fail publication rather than falling back to an overwriting rename.
Do not use network filesystems or move, replace, or delete the exchange while commands use it.

The exchange directory must already exist.
Copernicus creates `jobs/` with owner-only permissions and writes job files with mode `0600`.
The jobs directory and existing job files cannot be symbolic links.
Directory descriptors contain file operations within the opened exchange.
Different processes can publish identical bytes concurrently without replacing a winner.

The first publication binds the database to one resolved absolute exchange path, including an empty publication batch.
Aliases resolving to that path are accepted; a different path is rejected before job publication.
Preserve the database and exchange together until deliberate cleanup.
Acknowledged files are immutable retained records; this slice provides no relocation, deletion repair, or retention service.

Each publication handles at most `--limit` pending jobs; the default is `100` and the maximum is `1000`.
Repeat the command while `pending` is nonzero.
The 30-second command deadline and three-second SQLite lock wait also apply.
Each job is limited to one mebibyte; the outbox retains at most 10,000 jobs and 64 mebibytes.
Published jobs count toward those limits because their original bytes remain stored.
Exceeding a storage limit rolls back the request that would exceed it.

The outbox migration upgrades database versions `1` and `2` to version `3`.
Current write commands also apply [schema four](lifecycle.md#storage-and-recovery), which adds result imports.
Stop older commands and back up the database before upgrading.

The migration queues compatible executions from existing snapshots without consulting edited catalog definitions.
A corrupt snapshot or incompatible ready snapshot fails the upgrade and leaves the old schema intact.
Read-only catalog and request commands preserve supported older versions; `outbox show` requires version `3` or `4`.
Unknown database versions remain rejected.

## Clean up the walkthrough

Prerequisites: the same temporary-directory variable and completed commands above.
Stop all commands using the walkthrough database or exchange.
From the repository root, run:

```sh
rm -r "$exchange_dir"
unset exchange_dir
make clean
```

Remove only the temporary directory created by this walkthrough.
This removes its catalog, outbox, job copies, and any optional execution output.
`make clean` removes generated builds while preserving other databases and exchanges.
Source files and installed dependencies remain available.
