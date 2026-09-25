# Lychee

Check links with [Lychee](https://lychee.cli.rs/), using the official
`lycheeverse/lychee:0.24.2` image by default.

```sh
# Check the current workspace (automatically discovered by dagger check).
dagger -m lychee check

# Inspect or export a report, including when links fail.
dagger -m lychee api call run json export --path lychee-report.json

# Use an explicit configuration file and a Dagger secret.
dagger -m lychee api call --config lychee.toml --github-token env:GITHUB_TOKEN \
  run check

# Export the report using the format selected in the config file.
dagger -m lychee api call run report export --path lychee-report
```

## Configuration

The constructor accepts `ws` (`Workspace`) and loads the entire workspace root
with `gitignore: true`. The source lives at `/work/src`, and the container's cwd
corresponds to `ws.cwd` beneath that root, matching the Rust module's workspace
layout. From a nested cwd, links can still reference files elsewhere in the
workspace. Dagger supplies the current workspace automatically for CLI calls.

Other constructor settings are `version`, `container`, `config` (`File`),
`githubToken` (`Secret`), `noCache`, `cache` (`CacheVolume`), and `cookieJar` (`File`).

Without an explicit config file, Lychee discovers configuration in the workspace
cwd, including `lychee.toml`. `.lycheeignore` and relative input paths also work
from that cwd. Explicit config files retain their filenames, allowing Lychee's
`Cargo.toml`, `pyproject.toml`, and `package.json` loaders to work too.

Ordinary checking options remain under Lychee's config/default control unless
explicitly set through the module. For example, omitting `offline` preserves the
config value; `offline: false` explicitly overrides `offline = true`.
Exclusions, accepted status codes, request headers, base URL, timeout, retries,
concurrency, cache age, and other Lychee options can be set in the config file.

The module controls these runtime settings through CLI flags and environment variables:

| Setting | Behavior |
| --- | --- |
| GitHub token | A supplied secret is exposed as `GITHUB_TOKEN`, which Lychee reads directly; Dagger redacts it. |
| Local absolute-link root | `--root-dir` always points to the mounted workspace root, `/work/src`, regardless of cwd. |
| Report output | Always `/work/out/report`, available as `runner.report`. |
| Cookies | Always use a writable `/work/out/cookies.jar`, optionally seeded from the supplied File. |
| Cache | Enabled when a cache volume is present; `noCache` forces it off even if config enables it. |
| Progress / dump mode | Progress is disabled and link dumping is disabled so runs actually check links. |

`inputs` accepts filenames, globs, or URLs. With no inputs, the module checks `.`
from the workspace cwd. Inputs are passed literally to Lychee, without shell
interpolation.

## Runner and report formats

`run` returns a `Runner`. Its `report` function runs Lychee without a format
override, preserving the config's `format` setting or Lychee's default.

For an explicit format, call `runner.compact`, `runner.detailed`, `runner.json`,
`runner.junit`, or `runner.markdown`. Each returns a `File` from a separate Lychee
execution with the corresponding `--format` override. These functions use the
same inputs and settings, and leave `runner.report` in the configured format.

The runner also exposes `exitCode`, `stdout`, `stderr`, log files, `cookieJar`,
and `outputDirectory` for the default-format execution, including on broken-link
failures. `runner.check` raises
on failure and includes the configured-format report and logs in the error.
The module's `check` function is annotated `@check` and defaults to checking the
current workspace directory with gitignored files excluded.

## Cache outside the workdir

Yes: the cache volume is mounted at `/work/cache`, with the cache stored at
`/work/cache/lychee.csv`. Lychee 0.24.2 hardcodes `.lycheecache`, so the module
creates a `.lycheecache` symlink in the container's cwd to that file. The original source
directory is unchanged. The mount uses locked sharing to serialize cache writers.

The default volume is shared by module runs. Supply a dedicated `CacheVolume`
for project-specific caches. `max_cache_age` and `cache_exclude_status` in config
remain effective.

Each call to `run` creates a fresh runner that bypasses previous runners' Dagger
execution cache entries so Lychee can recheck links and apply its own cache
expiry.
`noCache` performs fresh checks for each new runner even with unchanged inputs.

## Tests

```sh
dagger -m lychee/tests check --no-generate
```
