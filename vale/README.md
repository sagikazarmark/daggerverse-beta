# Vale

Lint prose with [Vale](https://vale.sh/), using the official
`jdkato/vale:v3.22.0` image by default.

Requires Dagger `v1.0.0-beta.14` or later.

```sh
# Check the workspace, automatically syncing configured packages first.
dagger -m vale check

# Export JSON diagnostics, including when linting fails.
dagger -m vale api call run json export --path vale-report.json

# Select a configuration relative to the workspace root.
dagger -m vale api call --config ci/vale.ini run check

# Use existing, vendored styles without downloading packages.
dagger -m vale api call --auto-sync=false run check

# Export the complete synchronized styles tree.
dagger -m vale api call sync export --path prepared-styles
```

## Configuration and workspace layout

The constructor accepts `ws` (`Workspace`), `version` (image tag), an optional
custom `container`, `config`, `autoSync`, `gitignore`, `include`, and `exclude`.
Dagger supplies the current workspace automatically for CLI calls.

Documents are mounted at `/work/src`, and the container cwd corresponds to
`ws.cwd`. An explicit `config` path is relative to the **workspace root**.
Otherwise, the module searches from cwd up to that root, checking `.vale`,
`_vale`, `vale.ini`, `.vale.ini`, and `_vale.ini` in each directory, matching
Vale 3.22's search order. Missing configuration is an error.

`StylesPath` is resolved relative to the configuration file. It must identify a
separate directory within the workspace; absolute host paths are not supported.
Without `StylesPath`, the module adds an isolated `/work/styles` default to the
container's copy of the configuration. Global configuration is disabled with
`--no-global`. The original workspace is never modified.

`Packages` can name library styles, remote archives, local zip archives, or local
directories. Local paths resolve relative to the **workspace cwd**, including
transitive local package references. Package-provided configuration, styles,
vocabularies, dictionaries, and other assets are retained together.

Documents respect `.gitignore` by default. `include` and `exclude` are
workspace-relative document filters and do not filter configuration dependencies.
The selected config and explicitly referenced local packages are loaded even
when ignored. With automatic sync enabled, local styles respect `gitignore`;
commit your own styles and assets, and ignore downloaded styles and
`.vale-config/` selectively. With `autoSync = false`, the configured styles
directory is loaded including ignored files, supporting vendored styles.

The custom container must provide `vale` and a POSIX shell. The default image
includes support for Markdown, HTML, AsciiDoc, and reStructuredText; additional
format tools can be installed in a custom container.

## Sync and caching

`autoSync` defaults to `true`. Both `run` and `check` use `sync` before linting.
Calling `sync` explicitly always synchronizes packages, even with `autoSync`
disabled.

Preparation runs with a minimal input tree containing configuration, local
styles/assets, and local package dependencies. Its complete output is an
immutable Dagger `Directory`, including `.vale-config/`. The document tree is
mounted only in the lint stage, followed by the prepared styles directory.

| Change | Result |
| --- | --- |
| Document contents | Reuse prepared styles; rerun lint. |
| Excluded document | Reuse preparation and lint. |
| Config, local styles/assets, or local package contents | Rerun preparation and lint. |
| Vale container/version | Rerun preparation and lint. |
| No inputs changed | Reuse both stages. |

Dagger's execution cache stores the downloaded-and-prepared styles; there is no
shared mutable styles volume. Each preparation starts from workspace inputs,
so results from earlier package lists cannot leave stale installed styles behind.
Existing styles supplied by the workspace remain workspace inputs.

Cached sync does not poll upstream or force a refresh on every call. Package
names and floating URLs resolve when preparation actually executes. Prefer
versioned package URLs in `.vale.ini` and update those URLs to change versions:

```ini
StylesPath = .vale/styles
Packages = https://github.com/vale-cli/Google/releases/download/v0.7.0/Google.zip

[*.md]
BasedOnStyles = Google
```

An internal Go helper reads dependency metadata using the same INI parser as
Vale 3.22. Vale itself loads and validates the full lint configuration. Local
dependencies can be discovered from project configuration and local packages;
a remotely downloaded package should keep its own dependencies remote rather
than referring to additional caller-workspace files.

## Checks and reports

`check` is annotated `@check`, so it is discovered by `dagger check`. It accepts
the same `inputs`, `glob`, and `minAlertLevel` options as `run`:

- `inputs` are literal file/directory paths relative to cwd, defaulting to `.`.
  Empty lists also mean `.`. Missing paths fail instead of being interpreted as
  literal prose by Vale. Paths may reach elsewhere inside the workspace.
- `glob` narrows files scanned by Vale; use it for patterns rather than putting
  shell globs in `inputs`.
- `minAlertLevel` optionally overrides `.vale.ini`, with `SUGGESTION`, `WARNING`,
  or `ERROR`.

Vale's native exit status determines success: error-level alerts fail, while
warnings and suggestions alone pass. `MinAlertLevel` controls reporting, **not**
the failure threshold. Configuration and sync failures fail the check too.

`run` returns a runner with `exitCode`, `report`, `stdout`, `stderr`,
`stdoutFile`, `stderrFile`, and `outputDirectory`. Output is stored under
`/work/out` and remains available on lint failures. `runner.check` raises on a
nonzero exit code and includes the diagnostics.

`runner.json` and `runner.line` return reports from cached format-specific Vale
executions. The default report uses CLI formatting without color or wrapping.
Sync and input-validation failures happen before a runner is created and return
their diagnostics directly.

## Tests

```sh
dagger -m vale/tests check --no-generate
```

Integration checks cover lint failures and reports, warning semantics, nested
configuration, local/transitive zip packages, ignored and vendored styles, input
filtering, and cache reuse. The download test serves a package from a controlled
HTTP service and counts actual downloads across document and dependency edits.
