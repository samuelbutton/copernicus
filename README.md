# Copernicus

## In plain language

Copernicus is a learning project about choosing driving tests and understanding their results.
Imagine checking two braking rules against the same obstacle: one stops the vehicle early, while the other brakes too late.
The project will help a reviewer compare what happened and trace each result back to the test instructions.

The first version provides command help and a browser introduction to this example.
It does not create tests, start driving simulations, or load results yet.

## Technical summary

Copernicus separates test selection and review from simulation execution.
The Go command currently exposes help without file writes or background services.
The React and TypeScript package builds a static introduction with Vite.
Both packages build independently and require no other checkout.

Future request handling will exchange versioned files with Yamata.
The [execution source record](compatibility/yamata.json) pins the reviewed revision and public contract version.
It records the integration baseline; this version does not import schemas, jobs, results, or engine packages.

## Build and read the command help

Prerequisites: Go 1.25.13, Node.js 22.18.0, npm 10.9.3, GNU Make 3.81 or later, and a POSIX shell.
Use an ordinary account with write access to the checkout on macOS or Linux.
The [web manifest](web/package.json) records supported runtime versions and exact direct dependencies.
The lockfile pins the complete dependency tree.

From the repository root, install the web dependencies:

```sh
cd web
npm ci --ignore-scripts
cd ..
```

Installation downloads public dependencies into `web/node_modules/` and the npm cache.
The Go command uses only the standard library and needs no third-party Go modules.
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
The command and browser introduction contain no request store, result importer, or comparison engine yet.

## License

Copernicus uses the [Apache License 2.0](LICENSE).
