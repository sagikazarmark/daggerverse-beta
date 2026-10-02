# Rust

Check, test, lint, document and build Rust projects (Cargo workspaces), with
optional cargo-chef dependency caching, cargo-hack feature matrices and
cargo-nextest.

Requires Dagger `v1.0.0-beta.15` or later.

```toml
# dagger.toml
[modules.rust]
source = "github.com/sagikazarmark/daggerverse-beta/rust"

[modules.rust.settings]
chef = true # optional: cache compiled dependencies
```

```sh
dagger check -l                       # checks, grouped
dagger check -l --all                 # one row per target and package
dagger check                          # everything
dagger check --rust-package my-crate  # one package, every target
dagger check --rust-target host       # the host only
dagger check rust:targets:packages:clippy --rust-target host --rust-package my-crate
```

## Checks: targets × packages

Checks (`check`, `clippy`, `test`, `doc`, `build`) run per **target** and
**package**:

- **Targets** are `host` (cargo's default target, no `--target` flag) and the
  other targets installed for the toolchain: those listed in
  `rust-toolchain.toml`, those configured in the module (see below), and any
  the base image comes with.
- **Packages** are the members of the Cargo workspace (from `cargo metadata`)
  at or below the current directory.

Selecting packages doesn't multiply the work: each check runs **one cargo
command per target** with a `-p` flag for every selected package, so
dependencies compile once per target. A failing command fails every package
it covered, and the cargo output names the failing crate. `test` runs every
selected package even after a failure (`--no-fail-fast`).

On a target other than `host`, `test` only builds the tests (they can't run
here). `build` and `test` link, so targets other than wasm need a linker for
that target (an overlay, `.cargo/config.toml`, or the rust-cross module).

`audit` (Cargo.lock vulnerabilities) covers the whole workspace; `fmt`
(rustfmt) and `fix:clippy` (`clippy --fix`) the package cargo would select
from the current directory (see below). `fix:clippy` is a generator: `dagger
check -l` lists its staleness check as `clippy/stale`, next to the `clippy`
check. Outside a Cargo project, there's nothing to check.

Run `dagger generate` (`fmt`, `fix:clippy`) from the workspace root: Dagger
`v1.0.0-beta.15` applies the generated changes relative to the current
directory, so from a subdirectory they land in a nested copy of the tree
(`crates/a/crates/a/...`). With the Rust code in a subdirectory of the
repository, that leaves no working place to run generators until it is fixed
in Dagger. Checking for stale files (`dagger check`) works from anywhere.

## Where commands run

Every command runs from the **Cargo workspace root**, as cargo finds it from
the current directory (the nearest `Cargo.toml` with a `[workspace]` table, or
the package's own directory), lockfile or not. The current directory
only selects packages, as cargo would: the package keys are the members at
or below it, and commands without a package selection (`fmt`, `fix:clippy`,
the root functions) take the package of the nearest `Cargo.toml`, or the
workspace default at the root. From a crate excluded from the workspace (no
`[workspace]` of its own, not a member), `fmt`, `fix:clippy` and the root
functions fail and there are no package checks: give it its own
`[workspace]` table.

The Cargo project is found from the current directory. In a repository with
the Rust code in a subdirectory (e.g. `rs/`), run Dagger from there (`cd rs &&
dagger check`): from the repository root there's no Cargo project, so
nothing is checked.

Toolchain files (`rust-toolchain`, `rust-toolchain.toml`) and cargo
configuration (`.cargo/config.toml`) apply as found from the Cargo root and
its parents. Files inside a member directory don't apply, as when building
from the workspace root locally.

## Targets

List the targets in `rust-toolchain.toml`, as for local development. rustup
installs them with the toolchain:

```toml
[toolchain]
channel = "1.90"
targets = ["wasm32-unknown-unknown"]
```

A `toolchain` setting overrides the file, which is then ignored, as rustup
does: such an instance (e.g. pinned to the MSRV) only checks the targets
configured for it.

Without a toolchain file, configure them in the module (the `targets` setting,
or the Cargo.toml entry below). Every configured target is installed in every
command's container, so adding one, or a misspelled one, affects all of them.

The targets come from what rustup reports as installed, so targets a custom
base image comes with are checked too.

## Configuration in Cargo.toml

The module reads an entry named after its name in `dagger.toml` from the
**workspace root** manifest, whichever directory it's called from:

```toml
[[workspace.metadata.dagger.modules]]
name = "rust"
targets = ["wasm32-unknown-unknown"]      # checked besides the host and the toolchain file's
hack.check = ["--feature-powerset"]       # wrap cargo check with cargo-hack
hack.test = ["--each-feature"]            # also: hack.clippy, hack.doc
```

A member can restrict the targets it's checked for, e.g. a native-only app or
a wasm-only frontend:

```toml
[[package.metadata.dagger.modules]]
name = "rust"
targets = ["host"]
```

A member can only list configured targets (`host`, or the host's own triple,
for the host). An unknown target or an empty list is an error. A member's
`hack` entry isn't read, since one cargo command covers every selected
package.

Tests run with cargo-nextest when the project has a `.config/nextest.toml`.

## API

From another module:

```dang
let rust = Dagger.rust(ws: ws)

# One package, for the host (or another target with target: "...").
rust.package(name: "my-crate").test                     # Check
rust.package(name: "cli").binaries.get(key: "cli").build # File
rust.package(name: "my-crate").integrationTests.get(key: "api").run # String
rust.package(name: "my-crate").examples.get(key: "demo").run(args: ["x"])

# Functions on a collection itself (several items in one cargo command) are
# under `batch`: packages' checks, binaries' and examples' `build`,
# integration tests' `run`.
rust.targets.get(key: "host").packages.subset(keys: ["a", "b"]).batch.test.pass
rust.package(name: "cli").binaries.batch.build              # Directory, all binaries

# Any cargo selection, returning cargo's output.
rust.test(package: ["a"], feature: ["extra"])
```

A dependency's checks return a `Check` (`pass`, `error`, `sync`). The root
`check`, `test`, `clippy`, `doc` and `build` functions take every cargo flag
and return the output (`build`: the artifacts). They aren't checks.

`dagger call` doesn't work on collections in Dagger `v1.0.0-beta.15`
(`typedef "[RustTarget]" not found`): use the API, `dagger -c` (the shell), or
`dagger check`.

## Upgrading from the pre-collections module

- Checks moved under targets and packages: `rust:test` is now
  `rust:targets:packages:test`, and so on. Checks return `Void` (required by
  Dagger `v1.0.0`).
- The root `check`, `test`, `clippy`, `doc` and `build` functions are no
  longer checks. They still return cargo's output.
- `audit` returns `Void`; its report is in the logs.
- Configuration (`hack`) is read from the workspace root manifest, not the
  nearest one.
- Targets (`rust-toolchain.toml`, the `targets` setting) now add checks for
  those targets.
- Commands run from the Cargo workspace root; the current directory selects
  packages as cargo would. Toolchain and `.cargo` files inside a member
  directory no longer apply.
- The `toolchain` setting no longer accepts `msrv`: pin the version instead.
