# Copernicus

## In plain language

Copernicus is a learning project about choosing driving tests and understanding their results.
Imagine checking two braking rules against the same obstacle: one stops the vehicle early, while the other brakes too late.
The project will help a reviewer compare what happened and trace each result back to the test instructions.

You can import test definitions, organize them into groups, and inspect which tests a collection selects.
A browser introduction explains the driving example.
You can save a request that preserves the selected instructions, even after a group changes.
You can deliver the saved instructions as job files for Yamata to run.
Delivery can resume after an interruption.

You can load published results and check how many tests have finished.
Missing files remain incomplete, and failed tests stay visible.
You can compare two saved requests to see which measurements improved or became worse.

## Technical summary

Copernicus separates test selection and review from simulation execution.
The Go command imports validated test catalogs into SQLite and expands collections into ordered, unique tests.
Request creation freezes those selections and their content hashes in one transaction.
Help and catalog inspection start no background services.
The React and TypeScript package builds a static introduction with Vite.
Both packages build independently and require no other checkout.

The outbox exchanges versioned job files with Yamata.
The [execution source record](compatibility/yamata.json) pins the reviewed revision and public contract version.
The [copied public contract](compatibility/contract/v1/) supplies schemas and examples without requiring another checkout.
Compatible jobs commit with their request, then publish through a separate command.
Publication does not start workers or import results.

The result importer validates published files and maps exact accepted jobs back to requests.
A read-only local HTTP API exposes snapshots, progress, and the result index.
DuckDB queries selected result files for compatible comparisons, with explicit missing and unmatched rows.

## Build and read the command help

Prerequisites: Go 1.25.13, Node.js 22.18.0, npm 10.9.3, GNU Make 3.81 or later, and a POSIX shell.
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
Go downloads its dependencies into the module cache; no C compiler is required.
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

## Open the browser introduction

Prerequisites: the installed web dependencies above and a modern browser.
From the repository root, run:

```sh
make preview
```

Open [the local introduction](http://127.0.0.1:4173/) in your browser.
Select **Explore the example** to read the two braking outcomes.
The page labels them as an explanation, not loaded test results.
It shows no request controls or invented progress.

The preview serves built files on the loopback address only.
It fails if port `4173` is occupied; it does not silently choose another port.
It starts no simulation worker and sends no application data to another service.
Stop the preview with `Ctrl+C` before cleanup.
This preview is a development tool; a Go-served review interface belongs to later work.

## Verify and clean up

Prerequisites: the tools and installed dependencies above.
From the repository root, run:

```sh
make check
make clean
```

The check runs Go tests, static checks, TypeScript checks, typed lint rules, formatting checks, and both builds.
Expect exit code `0`.
Cleanup removes only `bin/copernicus` and `web/dist/`.
It preserves source files, dependency installations, and other data directories.
To remove installed web dependencies separately, run `rm -rf web/node_modules` from the repository root.

## Read the code

Start with the [code tour](docs/code-tour.md), [glossary](docs/glossary.md), and [contribution guide](CONTRIBUTING.md).
The [writing guide](docs/writing.md) defines the public documentation and naming rules.
The command stores frozen requests; the browser introduction remains static.
The interactive review interface belongs to later work.

## License

Copernicus uses the [Apache License 2.0](LICENSE).
