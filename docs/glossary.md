# Glossary

Use each term with the meaning below.
The README explains the complete local review workflow.

| Term | Meaning |
| --- | --- |
| Acknowledgment | A saved confirmation that a complete job file was published durably. |
| Admission | Accepting a request only when its team can reserve the maximum simulation ticks. |
| Budget reservation | The immutable simulation tick allowance charged to one accepted request. |
| Cache | Bounded saved calculations that can be reused when their exact inputs still match. |
| FIFO | First in, first out: remove the oldest saved entry first. |
| Analysis | One scoring operation on a recording with a complete metric configuration. |
| Analysis selection | A saved scoring configuration and its exact job for every test in a request. |
| Analysis template | Metric names, versions, and settings used to score a recording. |
| Atomic rename | A filesystem operation that makes a complete file visible under its final name at once. |
| Bag | A saved sequence of simulation records. |
| Baseline | The reference controller or request used for a comparison. |
| Binary | The executable file produced by the Go build. |
| Chromium | The browser engine used by the automated review tests. |
| Candidate | The controller or request compared with the baseline. |
| Catalog | The stored test definitions, templates, controller references, and ordered groups. |
| CLI | Command-line interface: commands and options entered in a shell. |
| Collection | A group of suites. |
| Content hash | A digest that identifies specified file bytes. |
| Contract | Versioned rules for files exchanged between independent programs. |
| Correlation identifier | A request label carried through related public files. |
| Controller | A rule that selects acceleration or braking during a simulation. |
| Delta | The candidate metric value minus the baseline value in the same unit. |
| Denominator | The total number of selected items against which a count is interpreted. |
| DuckDB | The local SQL query engine used to compare published metric data. |
| Exchange directory | A local directory containing public job, event, recording, and result files. |
| Evidence tick | A recording step cited by a measured result. |
| Event | An immutable notice describing one observed execution transition. |
| Execution | One identified simulation run. |
| Exit code | The number that reports command success or failure to the shell. |
| Go | The language and toolchain used for the command. |
| GNU Make | The tool that runs the repository's build and check targets. |
| Job | Complete instructions for an identified run or analysis operation. |
| HTTP API | Local URLs that return structured data for another program to read. |
| Index | Derived records that locate validated published results. |
| Incomparable | A matched group whose pairing or scoring rules do not permit metric deltas. |
| Git archive | A source snapshot exported from one identified Git commit. |
| Ownership marker | A fixed file that identifies a directory created by the demo. |
| Python | The runtime used by the public walkthrough and verification scripts. |
| Reference bundle | Reviewed synthetic jobs, recordings, results, and their checksum manifest. |
| JSON | A text format for structured data. |
| Mebibyte | A unit containing 1,048,576 bytes. |
| Membership | A test’s position in a suite, or a suite’s position in a collection. |
| Millimeter | One thousandth of a meter. |
| Millisecond | One thousandth of a second. |
| Matching key | Frozen fields that identify comparable test selections independently of the controller. |
| Loopback address | An address that accepts connections from the same computer. |
| Metric | A named calculation used to score a recording. |
| Node.js | The runtime used by the web build tools. |
| npm | The package manager used to install and run the web tools. |
| Outbox | Saved outgoing files that remain pending until durable publication is acknowledged. |
| Playwright | The tool that runs browser acceptance tests. |
| Package | A group of code files with a shared build and dependency boundary. |
| React | The library used to render the browser interface. |
| Publication | Making a complete, immutable file visible under its final name. |
| Regression | A result that is worse than the baseline result under compatible scoring rules. |
| Request | A saved selection of tests and a controller build. |
| Replay | Importing published events again after an interruption or missed scan. |
| Run template | Simulator settings, limits, and accepted scenario type. |
| Schema | Machine-readable rules for a document’s structure and allowed values. |
| Scenario | The initial vehicle state, goal, and obstacle behavior. |
| Resolution | Checking a selection and copying its complete inputs into a request. |
| SHA-256 | The hash algorithm used to identify frozen JSON content. |
| Submission identity | A request identifier paired with the command fields accepted for that request. |
| Snapshot | An immutable copy of resolved request inputs. |
| SQLite | The embedded database that stores the local catalog. |
| Transaction | A group of database changes accepted together or rolled back together. |
| Unassigned result | A result without an exact matching job in the local request outbox. |
| UTF-8 | The character encoding required for import files. |
| STE | Simplified Technical English, the writing method defined by ASD-STE100. |
| Suite | An ordered group of tests. |
| Synthetic example | An example created without private recordings or customer data. |
| TanStack Query | The browser library that manages HTTP reads, retries, and data freshness. |
| Test | One scenario, one run template, and one analysis template. |
| TypeScript | The language used to check browser code before it runs. |
| Vite | The tool that builds and previews the web package. |
| Zod | The library that validates browser response data before use. |
| Yamata | The independent execution project that owns simulations, recordings, and scores. |
