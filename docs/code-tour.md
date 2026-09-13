# Code tour

The Go command and browser package build independently.
The command manages a SQLite catalog, frozen requests, and outgoing job files.
The browser selects suites, creates requests, and reviews progress, comparisons, and cited evidence.
The command imports validated results and serves the local review interface.

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
| [compatibility/outcomes.go](../compatibility/outcomes.go) | Checks event identity, result relationships, metrics, and recording structure. |
| [internal/exchange/read.go](../internal/exchange/read.go) | Reads bounded public files within their declared folders. |
| [internal/store/results.go](../internal/store/results.go) | Imports events and results transactionally and rebuilds the derived index. |
| [internal/store/results-v4.sql](../internal/store/results-v4.sql) | Preserves publication identities, accepted events, and result references. |
| [internal/store/progress.go](../internal/store/progress.go) | Maps exact jobs to requests and revalidates results before counting completion. |
| [internal/httpapi/api.go](../internal/httpapi/api.go) | Exposes bounded JSON routes with local-origin checks. |
| [cmd/copernicus/serve.go](../cmd/copernicus/serve.go) | Owns loopback binding, HTTP timeouts, and graceful shutdown. |
| [cmd/copernicus/results.go](../cmd/copernicus/results.go) | Imports, lists, and rebuilds the result index. |
| [internal/comparison/model.go](../internal/comparison/model.go) | Pairs frozen selections and preserves complete denominators. |
| [internal/comparison/deltas.sql](../internal/comparison/deltas.sql) | Queries compatible metric values, pass flags, and deltas in DuckDB. |
| [internal/comparison/duckdb.go](../internal/comparison/duckdb.go) | Restricts the query process to private copies of validated results. |
| [internal/store/comparisons.go](../internal/store/comparisons.go) | Reads frozen selections and validates their exact selected outcomes. |
| [cmd/copernicus/compare.go](../cmd/copernicus/compare.go) | Runs comparison reads and optional terminal-page saving. |
| [scripts/install-duckdb.sh](../scripts/install-duckdb.sh) | Installs the pinned executable after verifying its archive checksum. |
| [cmd/copernicus/outbox.go](../cmd/copernicus/outbox.go) | Inspects delivery state and runs a bounded publication batch. |
| [cmd/copernicus/requests.go](../cmd/copernicus/requests.go) | Creates and reads saved requests. |
| [compatibility/source.go](../compatibility/source.go) | Embeds the execution source record for independent CLI use. |
| [examples/catalog.json](../examples/catalog.json) | Supplies synthetic definitions for the catalog walkthrough. |
| [cmd/copernicus/main_test.go](../cmd/copernicus/main_test.go) | Checks help, rejected arguments, and output errors. |
| [web/index.html](../web/index.html) | Defines the document, initial message, and local asset policy. |
| [web/src/main.tsx](../web/src/main.tsx) | Mounts the React application. |
| [web/src/App.tsx](../web/src/App.tsx) | Routes review views and manages navigation focus. |
| [web/src/styles.css](../web/src/styles.css) | Owns semantic colors, spacing, type sizes, focus states, and responsive layout. |
| [web/tsconfig.json](../web/tsconfig.json) | Requires strict browser-side type checking. |
| [web/eslint.config.mjs](../web/eslint.config.mjs) | Checks typed source and React Hook rules. |
| [web/package.json](../web/package.json) | Owns web commands and pinned direct dependencies. |
| [Makefile](../Makefile) | Builds, checks, previews, formats, and cleans the two packages. |
| [compatibility/yamata.json](../compatibility/yamata.json) | Records the reviewed execution source revision. |

The command passes an output writer to its argument handler.
Tests can observe help and writer failures without starting a child process.
The actual binary reports failure with exit code `1`.

The browser validates response data with Zod and manages HTTP state with TanStack Query.
The [review guide](review-guide.md) explains server startup and the complete browser workflow.

The [test-model guide](test-model.md) explains catalog relationships, validation, failure behavior, and cleanup.

The [request guide](requests.md) demonstrates unchanged snapshots after suite edits and explains submission retries.

The [exchange guide](exchange.md) maps request fields to jobs and demonstrates restart recovery.
The [outbox tests](../internal/store/outbox_test.go) exercise process exits across the transaction and publication boundary.

The [lifecycle guide](lifecycle.md) explains result validation, completion counts, replay, and index recovery.

The [comparison guide](comparisons.md) explains matching, metric deltas, unavailable values, and direct SQL inspection.
The [comparison tests](../internal/comparison/comparison_test.go) cover equal values, regressions, content conflicts, metric conflicts, ambiguity, and denominators.

| Review path | Responsibility |
| --- | --- |
| [internal/httpapi/review.go](../internal/httpapi/review.go) | Serves bounded built assets and validates same-origin request submissions. |
| [internal/store/review.go](../internal/store/review.go) | Joins saved inputs with current results and restricts evidence to cited ticks. |
| [web/src/api.ts](../web/src/api.ts) | Validates response contracts and owns query freshness and bounded request listing. |
| [web/src/CreateRequest.tsx](../web/src/CreateRequest.tsx) | Preserves drafts and submits selected suites with repeat-safe identities. |
| [web/src/Requests.tsx](../web/src/Requests.tsx) | Shows request history and complete progress denominators. |
| [web/src/Comparison.tsx](../web/src/Comparison.tsx) | Presents both selections, metric deltas, and paged evidence links. |
| [web/src/Execution.tsx](../web/src/Execution.tsx) | Displays immutable inputs and validated recording evidence. |
| [web/e2e/review.spec.ts](../web/e2e/review.spec.ts) | Exercises the browser against temporary databases and the public engine CLI. |

| Reanalysis path | Responsibility |
| --- | --- |
| [internal/adapter/analysis.go](../internal/adapter/analysis.go) | Creates public analysis jobs for pinned recordings and scoring configurations. |
| [internal/store/analysis.go](../internal/store/analysis.go) | Saves complete immutable selections, reuses equivalent jobs, and rejects missing original recordings. |
| [internal/store/analysis-v5.sql](../internal/store/analysis-v5.sql) | Preserves original jobs and adds separate saved scoring selections. |
| [internal/httpapi/analysis.go](../internal/httpapi/analysis.go) | Validates selected-analysis queries and same-origin analysis submissions. |
| [cmd/copernicus/analysis.go](../cmd/copernicus/analysis.go) | Creates, lists, and reads progress for scoring selections. |
| [web/src/Analysis.tsx](../web/src/Analysis.tsx) | Offers explicit scoring choices and requests new scores. |
| [examples/reanalysis-catalog.json](../examples/reanalysis-catalog.json) | Supplies body-edge gap scoring without changing original definitions. |

The [reanalysis guide](reanalysis.md) verifies preserved recordings and original results through the public execution boundary.

| Admission and saved comparisons | Responsibility |
| --- | --- |
| [internal/request/budget.go](../internal/request/budget.go) | Calculates maximum simulation tick reservations from frozen ready executions. |
| [internal/store/budgets.go](../internal/store/budgets.go) | Configures team limits, reserves admission atomically, and accounts for legacy requests. |
| [internal/store/admission-v6.sql](../internal/store/admission-v6.sql) | Enforces budget reservations and immutable saved comparisons. |
| [cmd/copernicus/budget.go](../cmd/copernicus/budget.go) | Sets cumulative limits and shows remaining team allowances. |
| [internal/comparison/view.go](../internal/comparison/view.go) | Defines cache identity and filters, orders, and pages complete comparisons. |
| [internal/store/saved_comparisons.go](../internal/store/saved_comparisons.go) | Revalidates evidence before cache lookup and bounds saved-page storage. |

The [decisions guide](decisions.md) explains admission, safe retries, terminal caching, partial bypass, and the local analytical boundary.
