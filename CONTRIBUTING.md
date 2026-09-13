# Contributing

Keep changes small, explicit, and easy to explain.
Use new code and synthetic examples.
Follow the [writing guide](docs/writing.md) for documents, command help, and interface text.

## Code boundaries

Keep command arguments, configuration, and process startup under `cmd/`.
Pass explicit values into packages that own application behavior.
Separate calculations from file access, clocks, and other side effects.
Prefer standard-library code and concrete types until a real boundary requires an abstraction.
Preserve error causes and report errors at the command boundary.

Keep the web package independent of other checkouts.
Use strict TypeScript, semantic HTML, and the shared CSS tokens in `web/src/styles.css`.
Keep component rendering pure and expose only real actions.
Use native links and controls before adding a component dependency.
Do not show missing results as passing scores.

Execution integration must use the pinned public file contract.
Do not import another project's internal packages or open its private database.
Review the source revision and compatibility examples together when changing that boundary.

## Verify a change

Prerequisites: the [README tools](README.md#build-and-read-the-command-help) and installed web dependencies.
From the repository root, run:

```sh
make fmt
make check
```

Formatting updates Go source and web package files.
The check verifies formatting, types, lint rules, Go tests, static checks, and both builds.
Expect exit code `0`.
Keep tests focused on observable behavior, including invalid arguments and output failures.

For interface changes, run the README preview and check keyboard focus, narrow screens, and browser errors.
Check the built page without external network access after loading its local assets.
Record tested operating-system and tool versions with the change.
Stop the preview, then clean up from the repository root:

```sh
make clean
```

The command removes generated builds and preserves source files and installed dependencies.

## Public names

Use Copernicus as the project name and Yamata only when explaining the public execution boundary.
Use generic descriptions for other organizations, systems, people, and examples.
Relevant public technology names are permitted.
Preserve required repository coordinates, license notices, and dependency attribution.

Do not include former employer names, private system names, aliases, private source notes, or private links.
Apply this rule to code, comments, filenames, test data, output, documents, and publication text.
Review direct and indirect references before publication.
