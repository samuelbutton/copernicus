# Code tour

The first version has two independent entry points: a Go command and a static browser introduction.
The command manages a SQLite catalog; the browser introduction remains static.
Neither entry point reads simulation results.

| Path | Responsibility |
| --- | --- |
| [cmd/copernicus/main.go](../cmd/copernicus/main.go) | Routes commands, bounds command duration, and reports failures. |
| [cmd/copernicus/catalog.go](../cmd/copernicus/catalog.go) | Reads an import file and runs catalog commands. |
| [internal/catalog/model.go](../internal/catalog/model.go) | Defines records and validates their values. |
| [internal/catalog/json.go](../internal/catalog/json.go) | Rejects oversized or ambiguous JSON input. |
| [internal/catalog/store.go](../internal/catalog/store.go) | Owns SQLite transactions and consistent catalog reads. |
| [internal/catalog/schema.sql](../internal/catalog/schema.sql) | Enforces identifiers, references, and ordered memberships. |
| [internal/catalog/expand.go](../internal/catalog/expand.go) | Expands a collection with first-occurrence ordering. |
| [examples/catalog.json](../examples/catalog.json) | Supplies synthetic definitions for the catalog walkthrough. |
| [cmd/copernicus/main_test.go](../cmd/copernicus/main_test.go) | Checks help, rejected arguments, and output errors. |
| [web/index.html](../web/index.html) | Defines the document, initial message, and local asset policy. |
| [web/src/main.tsx](../web/src/main.tsx) | Mounts the React application. |
| [web/src/App.tsx](../web/src/App.tsx) | Explains the driving example and the currently available actions. |
| [web/src/styles.css](../web/src/styles.css) | Owns semantic colors, spacing, type sizes, focus states, and responsive layout. |
| [web/tsconfig.json](../web/tsconfig.json) | Requires strict browser-side type checking. |
| [web/eslint.config.mjs](../web/eslint.config.mjs) | Checks typed source and React Hook rules. |
| [web/package.json](../web/package.json) | Owns web commands and pinned direct dependencies. |
| [Makefile](../Makefile) | Builds, checks, previews, formats, and cleans the two packages. |
| [compatibility/yamata.json](../compatibility/yamata.json) | Records the execution source revision for later contract integration. |

The command passes an output writer to its argument handler.
Tests can observe help and writer failures without starting a child process.
The actual binary reports failure with exit code `1`.

The browser introduction uses local assets and no external fonts, analytics, or result service.
Its example describes earlier and later braking without pretending to be a saved comparison.
The [README procedure](../README.md#open-the-browser-introduction) explains how to build, open, and stop the preview.

The [test-model guide](test-model.md) explains catalog relationships, validation, failure behavior, and cleanup.
