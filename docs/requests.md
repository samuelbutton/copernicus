# Frozen requests

A catalog describes tests that a reviewer can select.
A request keeps one selection and its inputs at a specific point.
Changing a suite affects future requests. It cannot change a saved request.
This lets a reviewer explain which instructions produced a later result.

## Create a request and change a suite

Prerequisites: the [README tools](../README.md#build-and-read-the-command-help), installed dependencies, and an ordinary account.
Use one shell for this procedure.
From the repository root, run:

```sh
make build-cli
request_dir=$(mktemp -d)
./bin/copernicus catalog import --db "$request_dir/catalog.sqlite" --file examples/catalog.json
./bin/copernicus request create --db "$request_dir/catalog.sqlite" --id review-one --collection all-tests --controller baseline --requester reviewer > "$request_dir/before.json"
./bin/copernicus catalog set-suite --db "$request_dir/catalog.sqlite" --suite smoke --tests empty-lane
./bin/copernicus request show --db "$request_dir/catalog.sqlite" --id review-one > "$request_dir/after.json"
cmp "$request_dir/before.json" "$request_dir/after.json"
```

Expect exit code `0` from each command.
The request selects `stopped-obstacle`, `empty-lane`, and `moving-obstacle`, in that order.
Its execution records have resolution status `READY`.

The suite edit removes `stopped-obstacle` from `smoke`. The `obstacles` suite still contains that test.
The saved request keeps both original suite memberships.
`cmp` prints nothing because the two saved outputs have identical bytes.

From the same shell and repository root, repeat the submission:

```sh
./bin/copernicus request create --db "$request_dir/catalog.sqlite" --id review-one --collection all-tests --controller baseline --requester reviewer > "$request_dir/retry.json"
cmp "$request_dir/before.json" "$request_dir/retry.json"
./bin/copernicus request create --db "$request_dir/catalog.sqlite" --id review-two --collection all-tests --controller baseline --requester reviewer > "$request_dir/new.json"
```

The retry returns the original snapshot and execution identifiers.
It does not resolve the edited catalog again.
The new request uses the changed order: `empty-lane`, `moving-obstacle`, then `stopped-obstacle`.
Its snapshot hash differs from the first request's hash.

Now try to reuse the original identifier with different submission details:

```sh
./bin/copernicus request create --db "$request_dir/catalog.sqlite" --id review-one --collection all-tests --controller candidate --requester reviewer
```

Expect exit code `1` and a submission identity conflict.
The accepted request remains unchanged.
Use a new request identifier when changing the controller or other submission details.

## Record an incompatible test

Prerequisites: the temporary catalog from the previous procedure.
From the same shell and repository root, run:

```sh
./bin/copernicus catalog import --db "$request_dir/catalog.sqlite" --file examples/unsupported-catalog.json
./bin/copernicus request create --db "$request_dir/catalog.sqlite" --id review-unsupported --collection unsupported --controller baseline --requester reviewer > "$request_dir/unsupported.json"
```

The extra catalog contains one unsupported scenario type and one supported test.
Request creation saves both selections and returns exit code `0`.
The unsupported test has status `RESOLUTION_FAILED` and reason `UNSUPPORTED_SCENARIO_TYPE`.
The supported test keeps status `READY`.
The request's overall resolution status is `RESOLUTION_FAILED` because at least one test is incompatible.

Exit code `0` means the record was saved or read successfully.
It does not mean that resolution succeeded or that any driving test passed.
Read `resolution_status` and each execution's `reason` to determine the resolution outcome.

Compatible tests also receive jobs in the durable outbox.
Failed tests receive no job, even when other tests in the request are compatible.
The separate [publication command](exchange.md) delivers those files. Execution workers are started independently.

## Clean up the walkthrough

Prerequisites: the same shell variables and completed commands above.
Stop any commands using the temporary catalog.
From the repository root, run:

```sh
rm "$request_dir/before.json" "$request_dir/after.json" "$request_dir/retry.json" "$request_dir/new.json" "$request_dir/unsupported.json"
rm "$request_dir/catalog.sqlite"
rmdir "$request_dir"
unset request_dir
make clean
```

These commands remove only the walkthrough files and generated builds.
Source files and installed dependencies remain available.
`make clean` alone keeps all catalog and request databases.

## Submission identity

The required fields are the request identifier, collection identifier, controller identifier, and requester identifier.
These fields use the catalog's identifier rules.
The requester is a local label. This example provides no authentication or authorization service.
Optional CLI fields are priority, seed, and repeat number.
Their defaults are `1`, `0`, and `0`.

Priority ranges from `0` through `3`, with `0` highest.
The seed ranges from zero through `9007199254740991`.
The repeat number ranges from zero through `100000`.
One submission selects one repeat number, not a count of repeated runs.

The request identifier is the submission identity within one database.
Every submission field participates in its identity check.
An identical retry returns the saved outcome, including any resolution failures.
Changing any submission field while keeping the identifier is a conflict.
The accepted snapshot also keeps its original execution-source revision after a later software update.

## Snapshot contents and hashes

The [snapshot model](../internal/request/model.go) owns the saved format.
The snapshot includes these values:

- Submission details, including requester, priority, seed, and repeat number.
- The selected collection and its ordered suite references.
- Each selected suite and its ordered test references.
- Each unique test and its scenario, run template, analysis template, and simulator reference.
- The selected controller reference and the pinned execution-source record.
- Execution identifiers, resolution statuses, and failure reasons.

Each frozen reference stores its catalog identifier, content, and SHA-256 hash.
The hash identifies the exact compact JSON bytes in that reference's `content` field.
Catalog identifiers are separate metadata and are excluded from that content hash.
Changing scenario values changes its content hash. Changing only its catalog identifier does not.
Membership content keeps ordered member identifiers, so a membership edit changes its hash.

The encoding uses Go's JSON encoder, sorted top-level content keys, and the declared nested model fields.
Input-file whitespace does not affect these hashes because imports first decode typed records.
Snapshot version `1` identifies this encoding and structure.
The outer record stores separate submission and snapshot hashes.
The snapshot hash covers the exact compact bytes of the whole snapshot object.

Controller content is a named implementation and version, as defined by the catalog.
The snapshot also freezes the reviewed Yamata source revision from [the source record](../compatibility/yamata.json).
This version selects built-in controllers. It does not import arbitrary controller binaries.
The hashes describe frozen inputs, not proof that execution occurred.
The [adapter](../internal/adapter/jobs.go) calculates separate public input and file hashes after translating the frozen inputs.

Execution identifiers derive deterministically from submission details, the test identifier, and the frozen input hashes.
Distinct tests keep distinct execution identifiers even when their input content matches.
Repeated membership of the same test produces one execution record.
Read and retry commands return stored bytes instead of reconstructing snapshots from the live catalog.

## Resolution and persistence

The [resolver](../internal/request/resolve.go) checks the pinned lane example's supported types, implementations, metric versions, and input bounds.
Unsupported scenario types and scenario/template mismatches become explicit per-test resolution failures.
Unsupported controllers, simulators, metrics, geometry, and limits also become resolution failures.
A missing collection, missing controller, broken reference, or empty selection rejects creation without accepting a request.
The file adapter validates complete jobs against the pinned schema before saving them.
The publisher validates their saved bytes again before publication.

The [request store](../internal/store/requests.go) reads the catalog and saves the snapshot within one SQLite write transaction.
It saves the request, all execution records, and compatible job bytes together.
Suite edits use the same transaction discipline, so resolution sees one complete membership version.
Concurrent identical submissions return one accepted request and one set of execution records.
A failed write or process exit before commit rolls back the request, execution records, and outgoing jobs.

The [schema migration](../internal/store/requests-v2.sql) adds request storage to existing catalog databases without changing catalog records.
Current write commands upgrade supported older schemas to version `6` in one transaction.
The [outbox migration](../internal/store/outbox-v3.sql) adds jobs from existing frozen snapshots.

Catalog inspection can still read version `1` without upgrading it.
Request inspection accepts versions `2` through `6` through a read-only connection.
[Progress queries](lifecycle.md#completion-counts) require schema four or later.
Unknown schema versions remain rejected.

Database triggers prevent changing or deleting saved snapshots and initial execution resolution fields.
Read commands verify the stored hashes and matching execution records.
Suite membership remains editable through `catalog set-suite`. Invalid edits roll back without changing the suite.

Other catalog imports still add records and reject duplicate identifiers.
Use `--tests=` explicitly to clear a suite. Omitting `--tests` is an error.

A request can contain at most 1,000 unique tests and 16 mebibytes of snapshot JSON.
The local request store permits at most 1,000 requests and 64 mebibytes of snapshot JSON.
Commands keep the existing 30-second deadline and three-second database lock wait.
These bounds limit local storage and work. They are not team execution budgets.

If confirmation output fails after saving, the error explicitly says the request was saved.
Use `request show` or repeat the identical submission to retrieve the accepted outcome.
The [store tests](../internal/store/requests_test.go) verify transaction rollback, process exit, concurrent submissions, migration, and snapshot preservation.
The [command tests](../cmd/copernicus/requests_test.go) verify the CLI workflow and output-failure recovery.

## Browser suite selection

The [review interface](review-guide.md) accepts an optional ordered `suite_ids` array in a submission.
Every selected suite must belong to the selected collection.
Empty arrays, duplicate identifiers, and unknown memberships are rejected.
The snapshot freezes only the selected suites and their unique tests, keeping selection order.

Omitting `suite_ids` selects the entire collection and keeps the earlier submission encoding.
Selected suites participate in submission identity, so changing them requires a new request identifier.
The browser sends priority `1`, repeat `0`, requester `reviewer`, and a visible editable seed.
The HTTP boundary requires explicit scalar fields and limits the submission to 131,072 bytes.

[Reanalysis](reanalysis.md) adds immutable scoring selections without changing the request snapshot or its executions.

[Admission limits](decisions.md#budget-decisions) reserve maximum simulation ticks in the request transaction.
The optional `team_id` selects a configured local budget. Omission keeps earlier submission bytes and uses team `local`.
