# DPM

DPM generates `dpm.gen.dang` files from a small `dpm.json` package list.

This is a bundled package generator, not a general-purpose dependency manager. Packages are shipped with this module and copied into the consuming Dang module as normal Dang source.

## User Flow

Install the DPM module in your workspace:

```bash
dagger install github.com/sagikazarmark/daggerverse-beta/dpm
```

Create `dpm.json` in the Dang module directory that should receive generated package code:

```json
{"packages":["base64","lists","maps","path","strings"]}
```

Run Dagger code generation:

```bash
dagger generate
```

## Configure

The `dpm.json` file contains the bundled packages to generate:

```json
{"packages":["base64","lists","maps","path","strings"]}
```

The `packages` list is required. Package names must be unique, and unknown packages fail generation.

The selected packages are generated into `dpm.gen.dang` in the same directory as `dpm.json`. Commit the generated file, because Dang loads it as regular module source.

## Manual Generation

Preview the generated changes as a patch:

```bash
dagger call -m dpm generate as-patch
```

Write generated files back to the workspace:

```bash
dagger call -m dpm generate export --path .
```

By default, DPM scans the workspace for every `dpm.json` under non-gitignored files and updates each matching `dpm.gen.dang`.

To generate only for the current working directory, run from a directory containing `dpm.json` and pass `--here`:

```bash
dagger call -m dpm generate --here export --path .
```

## Use Packages

Generated packages are zero-argument namespace types. Call their functions directly:

```dang
let encoded = Base64.encode("hello")
let decoded = Base64.decode(encoded)

let sorted = Lists.sort([3, 1, 2])
let sortedStrings = Lists.sortWith(["bbb", "a", "cc"]) { left, right =>
  if (left < right) { -1 } else if (left > right) { 1 } else { 0 }
}

let sum = Maps.reduce(["a": 1, "b": 2], 0) { acc, key, value => acc + value }

let cleanPath = Path.clean("/src/../src/main.dang")
let joinedPath = Path.join(["src", "main.dang"])

let slug = Strings.slug(" Hello World ")
let title = Strings.truncate("hello world", maxLength: 8)
```

Available packages:

| Package | Type | API |
| --- | --- | --- |
| `base64` | `Base64` | `encode(s)`, `decode(s)` |
| `lists` | `Lists` | `sort(list)`, `sortWith(list) { left, right => ... }` |
| `maps` | `Maps` | `reduce(map, initial) { acc, key, value => ... }` |
| `path` | `Path` | `separator`, `clean(path)`, `join(parts)`, `isRelativeTo(path, base)` |
| `strings` | `Strings` | `slug(s)`, `truncate(s, maxLength)` |

`Base64` uses a pinned BusyBox container for encoding and decoding. `encode` emits unwrapped base64 output.

## Inspect

List bundled packages:

```bash
dagger call -m dpm packages
```

Validate bundled package source files:

```bash
dagger call -m dpm validate
```

## Development

Run DPM generator tests:

```bash
dagger check -m dpm/tests
```

Run generated package behavior tests:

```bash
dagger check -m dpm/package-tests
```

After changing bundled packages, regenerate `dpm/package-tests/dpm.gen.dang` and keep it current with `dpm/package-tests/dpm.json`.
