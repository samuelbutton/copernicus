# Code tour

The first version has two independent entry points: a Go command and a static browser introduction.
The command manages a SQLite catalog, frozen requests, and outgoing job files.
The browser introduction remains static.
Neither entry point reads simulation results.

| Path | Responsibility |
| --- | --- |
| [cmd/copernicus/main.go](../cmd/copernicus/main.go) | Routes commands, bounds command duration, and reports failures. |
| [cmd/copernicus/catalog.go](../cmd/copernicus/catalog.go) | Reads an import file and runs catalog commands. |
| [internal/catalog/model.go](../internal/catalog/model.go) | Defines records and validates their values. |
| [internal/catalog/json.go](../internal/catalog/json.go) | Rejects oversized or ambiguous JSON input. |
| [internal/store/catalog.go](../internal/store/catalog.go) | Owns SQLite transactions and consistent catalog reads. |
| [internal/store/schema.sql](../internal/store/schema.sql) | Enforces identifiers, references, and ordered memberships. |
| [internal/catalog/expand.go](../internal/catalog/expand.go) | Expands a collection with first-occurrence ordering. |
| [internal/request/resolve.go](../internal/request/resolve.go) | Resolves selected definitions into immutable snapshot content. |
| [internal/request/model.go](../internal/request/model.go) | Defines submissions, frozen references, and content hashes. |
| [internal/store/requests.go](../internal/store/requests.go) | Saves requests and executions together, with submission identity checks. |
| [internal/store/suites.go](../internal/store/suites.go) | Changes existing suite membership in one transaction. |
| [internal/store/requests-v2.sql](../internal/store/requests-v2.sql) | Adds request tables and snapshot immutability constraints. |
| [internal/adapter/jobs.go](../internal/adapter/jobs.go) | Translates frozen inputs into validated run jobs. |
| [internal/store/outbox.go](../internal/store/outbox.go) | Saves jobs, migrates accepted requests, and acknowledges durable publication. |
| [internal/store/outbox-v3.sql](../internal/store/outbox-v3.sql) | Enforces immutable job bytes and one exchange destination. |
| [internal/exchange/publish.go](../internal/exchange/publish.go) | Synchronizes files and publishes with an exclusive atomic rename. |
| [compatibility/contract.go](../compatibility/contract.go) | Validates job structure and public input hashes using the embedded schema. |
| [compatibility/contract/v1/](../compatibility/contract/v1/) | Preserves pinned public schemas, examples, and their checksum manifest. |
| [cmd/copernicus/outbox.go](../cmd/copernicus/outbox.go) | Inspects delivery state and runs a bounded publication batch. |
| [cmd/copernicus/requests.go](../cmd/copernicus/requests.go) | Creates and reads saved requests. |
| [compatibility/source.go](../compatibility/source.go) | Embeds the execution source record for independent CLI use. |
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
| [compatibility/yamata.json](../compatibility/yamata.json) | Records the reviewed execution source revision. |

The command passes an output writer to its argument handler.
Tests can observe help and writer failures without starting a child process.
The actual binary reports failure with exit code `1`.

The browser introduction uses local assets and no external fonts, analytics, or result service.
Its example describes earlier and later braking without pretending to be a saved comparison.
The [README procedure](../README.md#open-the-browser-introduction) explains how to build, open, and stop the preview.

The [test-model guide](test-model.md) explains catalog relationships, validation, failure behavior, and cleanup.

The [request guide](requests.md) demonstrates unchanged snapshots after suite edits and explains submission retries.

The [exchange guide](exchange.md) maps request fields to jobs and demonstrates restart recovery.
The [outbox tests](../internal/store/outbox_test.go) exercise process exits across the transaction and publication boundary.
