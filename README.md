# Copernicus

## In plain language

Copernicus is a learning project about choosing driving tests and understanding their results.
Imagine checking two braking rules against the same obstacle: one stops the vehicle early, while the other brakes too late.
The project helps a reviewer compare what happened and trace each result back to the test instructions.

You can import test definitions, organize them into groups, and inspect which tests a collection selects.
The browser lets you choose test groups, save requests, and inspect their results.
You can save a request that preserves the selected instructions, even after a group changes.
You can deliver the saved instructions as job files for Yamata to run.
Delivery can resume after an interruption.

You can load published results and check how many tests have finished.
Missing files remain incomplete, and failed tests stay visible.
You can compare two saved requests to see which measurements improved or became worse.
You can score saved recordings again, preserve the original results, and choose which scores to compare.
Team allowances limit new simulation work. Saved comparisons make repeated reviews reusable while checking that their evidence remains available.

## Technical summary

Copernicus separates test selection and review from simulation execution.
The Go command imports validated test catalogs into SQLite and expands collections into ordered, unique tests.
Request creation freezes those selections and their content hashes in one transaction.
Help and catalog inspection start no background services.
The React and TypeScript package builds the review interface with Vite.
Both packages build independently and require no other checkout.

The outbox exchanges versioned job files with Yamata.
The [execution source record](compatibility/yamata.json) pins the reviewed revision and public contract version.
The [copied public contract](compatibility/contract/v1/) supplies schemas and examples without requiring another checkout.
Compatible jobs commit with their request, then publish through a separate command.
Publication does not start workers or import results.

The result importer validates published files and maps exact accepted jobs back to requests.
A local HTTP API exposes snapshots, progress, evidence, and the result index.
Review-server mode also accepts repeat-safe request and analysis creation.
DuckDB queries selected result files for compatible comparisons, with explicit missing and unmatched rows.

## Build and read the command help

Prerequisites: Go 1.25.13, Node.js 22.18.0, npm 10.9.3, GNU Make 3.81 or later, Python 3.12 or later, and a POSIX shell.
Use an ordinary account with write access to the checkout on macOS or Linux.
The [web manifest](web/package.json) records supported runtime versions and exact direct dependencies.
The lockfile pins the complete dependency tree.

From the repository root, install the Go and web dependencies:

```sh
go mod download
cd web
npm ci --ignore-scripts
cd ..
make install-duckdb
```

Installation downloads public dependencies into `web/node_modules/` and the npm cache.
The Go command uses the pure-Go SQLite driver pinned in [go.mod](go.mod).
Go downloads its dependencies into the module cache. No C compiler is required.
The [comparison setup](docs/comparisons.md#install-the-query-tool) installs the pinned DuckDB executable using `curl`, `gzip`, and a SHA-256 tool.

Keep installed dependencies for subsequent builds.
After installation, builds and command help need no internet access.

From the repository root, run:

```sh
make build
./bin/copernicus --help
```

The build creates `bin/copernicus` and `web/dist/`.
Help prints the usage and explains the current scope, then returns exit code `0`.
Running the command without arguments, with `help`, or with `-h` prints the same help.
Unsupported arguments and output failures return exit code `1`.
Help creates no data directory, database, or service.

## Run the standalone demo

Prerequisites: the installed tools and dependencies above.
No Yamata checkout or executable is required.
From the repository root, run:

```sh
make demo
```

The demo imports bundled synthetic results through the normal file contract.
It prints incomplete counts, the stopped-obstacle collision regression, incompatible scoring versions, and restored comparison after matching versions.
The collision count changes from `0` to `1`, with delta `+1`.
Second scores use the same six recordings and keep the original results.
See [reference provenance](examples/reference/README.md) for the recorded inputs and regeneration procedure.

Output stays in `.copernicus/standalone/` for review.
The command refuses an existing demo directory instead of replacing it.
Use the cleanup procedure below before another run.

## Run the combined demo

Prerequisites: standalone setup, Git, and a Yamata repository containing the [pinned revision](compatibility/yamata.json).
Place that repository beside Copernicus, or set `YAMATA_SOURCE` to its path.
During setup, run `go mod download` from a checkout of the recorded Yamata revision.
Keep its Go dependencies available for offline builds.
From the Copernicus repository root, run:

```sh
make demo-pair YAMATA_SOURCE=../yamata
```

This builds the recorded commit in a temporary directory, without changing the engine checkout.
It generates fresh recordings and results through public Yamata commands.
It verifies the same outcomes as the standalone demo, including unchanged recordings during new scoring.
Output stays in `.copernicus/pair/`, separate from standalone state.
Both demos run without internet access after dependency setup.

## Review the demo

Prerequisites: a completed demo, a modern browser, and an available port `8080`.
From the repository root, run:

```sh
make demo-serve
```

Use `make demo-serve MODE=pair` to review the combined demo.
Expect the printed address `http://127.0.0.1:8080`.
Open [the local review interface](http://127.0.0.1:8080/).

1. Select **Compare**.
2. Choose `demo-baseline` and `demo-candidate`, with **Original scores** on both sides.
3. Select **Compare requests**.
4. Find the stopped-obstacle collision regression.
5. Open **Candidate: stopped-obstacle**.
6. Open the collision evidence at **Tick 22**.
7. Expand **Scenario** to inspect the frozen inputs.
8. Select **Back to comparison**.
9. Set **Candidate scores** to `edge-v2`.
10. Select **Compare requests**. Expect three incompatible groups.
11. Set **Baseline scores** to `edge-v2`.
12. Select **Compare requests**. Expect three compared groups.
13. Select `demo-pending` as candidate, with **Original scores** on both sides.
14. Select **Compare requests**. Expect three incomplete groups.

The demo already requested both second analyses.
On a request page, **Score these recordings again** can repeat an accepted selection without duplicating it.
Additional calculations require the combined engine workflow in the [reanalysis guide](docs/reanalysis.md).
The standalone bundle supplies only its documented results.

Stop the server with `Ctrl+C` after review.
From the repository root, run:

```sh
make demo-clean
make clean
```

Demo cleanup removes only marked standalone and combined demo directories, including their databases and exchanges.
It keeps unrelated data and installed dependencies.
Build cleanup removes only `bin/copernicus` and `web/dist/`.
See [troubleshooting](docs/troubleshooting.md) for existing output, missing tools, ports, and incomplete results.

## Import test definitions

Follow the [test-model walkthrough](docs/test-model.md#import-and-inspect-the-example) to import the synthetic catalog and expand its collection.
It includes the empty-lane, stopped-obstacle, and moving-obstacle scenarios, plus baseline and candidate controller references.
The walkthrough creates a temporary database and includes cleanup commands.

## Freeze a request

Follow the [request walkthrough](docs/requests.md#create-a-request-and-change-a-suite) to save inputs, edit a suite, and verify the unchanged snapshot.
The guide also covers repeat-safe submissions, incompatible tests, and cleanup.

## Deliver saved jobs

Follow the [exchange walkthrough](docs/exchange.md#save-a-request-before-publication) to publish saved jobs and resume after an interruption.
The guide explains delivery status, conflicts, the pinned contract, and optional worker handoff.

## Import results and check progress

Follow the [lifecycle walkthrough](docs/lifecycle.md) to import results, inspect completion, rebuild the index, and use the local HTTP API.
The first procedure uses copied contract examples and requires no execution service.

## Compare saved requests

Follow the [comparison walkthrough](docs/comparisons.md) to inspect deltas, changed membership, completion counts, and the comparison API.
It includes a direct DuckDB query and cleanup commands.

## Open the browser review interface

Follow the [review guide](docs/review-guide.md) to create both requests in the browser and compare real synthetic results.
The guide includes server setup, worker commands, evidence inspection, keyboard controls, verification, and cleanup.
Use `serve --web-dir web/dist` to serve the built interface and its API from one local address.
`make preview` serves static assets only. Review actions require the Go review server.

## Score saved recordings again

Follow the [reanalysis guide](docs/reanalysis.md) to request new scores and select a scoring configuration for each comparison side.
It verifies unchanged recordings, kept original results, and restored comparison after matching metric versions.

## Limit work and reuse comparisons

Follow the [admission and saved-comparison guide](docs/decisions.md) to configure team budgets, verify safe retries, and reuse complete comparison pages.
Partial results remain live and always bypass the cache.

## Verify and clean up

Prerequisites: the tools and installed dependencies above.
From the repository root, run:

```sh
cd web
npx playwright install chromium
cd ..
make verify YAMATA_SOURCE=../yamata
make clean
```

Browser installation needs internet access once. Subsequent verification needs only local dependencies.
Full verification also requires the combined-demo source and its downloaded Go modules.
The check runs Go tests, race checks, static checks, TypeScript checks, lint rules, formatting checks, and both builds.
It checks public contract fixtures, document links, reference drift, both walkthroughs, browser acceptance, and scoped cleanup.

Temporary verification state is removed automatically. Test failure output stays in `web/test-results/`.

Use `make check` for the smaller build and static-check loop.
Expect exit code `0`.
Cleanup removes only `bin/copernicus` and `web/dist/`.
It keeps source files, dependency installations, and other data directories.
To remove installed web dependencies separately, run `rm -rf web/node_modules` from the repository root.

## Read the code

Start with the [code tour](docs/code-tour.md), [glossary](docs/glossary.md), and [contribution guide](CONTRIBUTING.md).
The [writing guide](docs/writing.md) defines the public documentation and naming rules.
The browser reviews frozen inputs and freshly validated result evidence.

## License

Copernicus uses the [Apache License 2.0](LICENSE).
