# Collections in the rust module

Research date: 2026-09-30, target matrix 2026-10-02 (Dagger `v1.0.0-beta.15`, which contains
[dagger/dagger#14221](https://github.com/dagger/dagger/pull/14221))

## Conclusion

**A matrix of target triples × packages carries the checks.** The outer
dimension is the target: `host` (cargo's default, no `--target`) plus the
triples from `rust-toolchain.toml` and the module's configuration. Under each target, the package keys are the member names
from `cargo metadata --no-deps`, limited to members at or below the current
directory. The checks (`check`, `clippy`, `test`, `doc`, `build`) are defined
on the packages collection only, so the engine calls each **once per target for
all selected packages**. They run one cargo command with a `-p` flag per package
(and `--target` for a cross target), and cargo-chef cooks with the same flags.
Without configured targets, `dagger check` costs the same as before.
`--rust-target` and `--rust-package` narrow it.

Targets are a second, **check-free** level, one collection per kind, keyed by
target name: `binaries` and `examples` build (`File`, or `Directory` for a
selection), `examples` also run, and `tests` (integration tests) run with
`cargo test --test NAME`. They have plain functions, not checks: the package
`test` check already runs every integration test, and a check here would
run once per package.

```
rust
├── audit              @check  (Cargo.lock, workspace-wide)
├── fmt / fix.clippy   @generate (workspace-wide)
├── check/test/...     plain functions with all cargo flags (no @check)
├── package(name, target = "host")  shortcut (takes an argument: not a dimension)
└── targets            @collection  key: host | triple  --rust-target
    └── get(name) → Target { name }
        └── packages   @collection  key: package name   --rust-package
            ├── check, clippy, test, doc, build   @check (batch: cargo … [--target T] -p a -p b)
            └── get(name) → Package { name, path, target; check, clippy, test, doc, build @check }
                ├── binaries   @collection  key: bin name       --rust-binary
                │   ├── build → Directory (batch: cargo build -p P --bin a --bin b)
                │   └── get(name) → Binary { build → File }
                ├── integrationTests @collection key: test name  --rust-integration-test
                │   ├── run → String (batch: cargo test -p P --test a --test b)
                │   └── get(name) → IntegrationTest { run → String }
                └── examples   @collection  key: example name   --rust-example
                    ├── build → Directory (batch: cargo build -p P --example a …)
                    └── get(name) → Example { build → File, run(args) → String }
```

Binaries and examples are per target: `targets/packages/binaries/build` is a
binary per triple (file names get `.exe`/`.wasm` where cargo adds them).
Running is `host` only: `integrationTests` is empty for other targets (the package `test`
check already builds them there), and `Example.run` raises.

API callers that don't care about the target use `rust.package(name)`, the
host package (or `target:` another one). It takes a required argument, so
discovery skips it and it adds no dimension. The package's own checks (for API
callers: `rust.package(name: "a").test.pass`) have the same name and type as
the collection's, so `dagger check` still runs the collection's once per target
(verified: one `cargo test` per target). A no-argument `host` field would add a second packages dimension
(see "Target matrix" for why that hurts selection).

## How collections work (engine, beta.15)

- `@collection` type + `@keys` field + `@get(key)` function. The engine adds
  `keys`, `get`, `list`, `subset(keys)` and a `batch` object holding every
  other function of the collection type.
- Discovery walks `collection → get(key) → item type`, and a collection under an
  item adds another key dimension. The walk is cycle-checked **by type name**,
  so an item type cannot contain a collection of itself.
- Batching: a `@check`/`@generate` on the collection with the same name and
  return type as the item's replaces the item's; one declared **only** on the
  collection is still addressed per item. Either way it runs **once per parent
  item** on `subset(selected keys)`. Nothing batches across parents: a check on
  a nested collection runs once per parent.
- `@delta` (optional `CollectionDelta`) tells a batch what was selected out.
  It isn't needed here: the keys are the selection.
- Dimension/flag names come from the item type: `Package` in module `rust` →
  `--rust-package`. The same item type at a second level gets a qualified flag
  (see "Target matrix"). The probe module gave `dag://proto/packages/test?package=b&package=c`.
- A failing batch fails as one artifact covering every selected key.
- **Every `@check` must return `Void`** once the module's `engineVersion` is
  `v1.0.0` (`core/module.go`, `validateObjectFunction`). This applies to the
  root too. The previous `String!`/`Directory!` checks stopped the module from
  loading on beta.15. A consuming module sees a dependency's check as an
  unrun `Check` (`sync`, `pass`, `error`).
- CLI gap: `dagger call` on a collection fails (`typedef "[RustPackage]" not
  found` for `rust packages keys`, in an earlier layout). `dagger check -l --all` and API calls from Dang work.

## Candidate segmentations

| Unit | Keys cheap? | Batchable? | Verdict |
|---|---|---|---|
| Cargo workspaces (multiple roots in a repo) | yes (`findUp`/glob) | no: different targets and locks | Not now. The module binds to one cargo root (env, chef, manifest config). Possible top level later. |
| **Packages (workspace members)** | `cargo metadata --no-deps` | **yes, `-p` flags** | **Checks live here.** |
| Targets: bins, integration tests, examples | from metadata | within one package | **Collections without checks**: build/run/test single targets or a selection. As checks they would duplicate the package `test` and run once per package. |
| Targets: lib, benches | from metadata | within one package | Not added. The lib is one per package, so the package covers it. Benches aren't CI checks. Same pattern if needed. |
| Test functions | **no**: listing needs a compile (`cargo test -- --list`) | `--exact` filters | Rejected: key discovery would cost a full build. |
| Rust modules (`mod foo`) | no (name paths, compile to list) | name filter | Rejected: not a compilation unit. |
| **Target triples** | `host` + `rustup target list --installed` (toolchain file's + configured) | **yes**, `-p` under one `--target` | **Outer dimension of the checks** (see "Target matrix"). |
| Features | from metadata, or `cargo hack --print-command-list` | only per package | Left to cargo-hack (see "Target matrix"). |
| Toolchains (msrv/stable) | fixed | no | Module instances (`toolchain`), as today. |

## Target matrix

The collections announcement presents nested collections as sparse matrices.
Whether a dimension is worth it here depends on the batching rule (once per
parent) and on the target directory not being cached:

- **Targets go outside packages.** One `cargo … --target T -p a -p b` per
  target compiles the dependencies once per target. Under packages, they
  would compile once per package and target. With only `host`, the outer
  order costs exactly what the single packages level cost.
- **One packages dimension for every package check.** Two collections with
  the same item type at different levels (`/rust/packages` and
  `/rust/targets/packages`) are allowed: the nested one gets a qualified flag
  (`--rust-targets-package`). But dimension filters are ANDed, and an artifact
  must carry every filtered dimension (measured on a probe module). With
  target-free checks in `packages` and target-specific ones in
  `targets/packages`, `--rust-package foo` would select only the first group,
  `--rust-targets-package foo` only the second, both together nothing, and
  `--rust-target host` would drop the target-free checks. The selector's
  alternatives (`FilterDimensions`) OR on dimension presence, not on keys. So
  every package check is under targets, `test` included.
- **`host` is a key, not the host triple.** It runs without `--target`, so
  host behavior is unchanged: `RUSTFLAGS` still apply to build scripts, the
  `target/debug` layout and the chef cook are the same, and so is
  `build.target` from `.cargo/config.toml`. Addresses also stay the same
  across machines (an arm Mac and an x86 CI runner). The host's own triple,
  configured or listed by a member, stands for `host` (`rustc -vV`, only run
  when targets are configured).
- **Cross `test` only builds the tests** (`cargo test --no-run --target T`,
  wrapped by cargo-hack like the host's), since they can't run here.
  `IntegrationTest.run` and `Example.run` raise for a target other than
  `host`. Both `test` checks pass `--no-fail-fast`, so one failing crate
  doesn't hide the others' results.
- **Linking may need a cross linker.** `check`, `clippy` and `doc` only need
  the rustup target. `build` and cross `test` link: wasm and rust-lld
  targets work as is, other architectures need a linker (overlay,
  `.cargo/config.toml`, or the rust-cross module); a failure on a cross target
  says so.
- **Targets come from `rust-toolchain.toml` first.** Its `[toolchain]
  targets` is where Rust projects already declare targets, works the same
  locally, and rustup installs them with the toolchain. A `toolchain` override
  ignores the file, as rustup does (`RUSTUP_TOOLCHAIN`): the instance only
  has its configured targets. The keys are what rustup reports as installed,
  run on the environment's container (no source, so source edits don't
  recompute them): the file's targets, the configured ones, and any the base
  image comes with. Cargo.toml has no standard
  place (per-package `forced-target` is unstable), and `.cargo/config.toml`
  `build.target` changes what `host` builds rather than adding a dimension.
- **Sparse by configuration.** The module-level set adds the constructor
  `targets` and `targets` in this module's entry of the **workspace root**
  manifest (the Cargo root: the nearest `Cargo.toml` with `[workspace]`, found
  without relying on `Cargo.lock`), whichever directory the module is called
  from; reading the nearest manifest made the matrix change with the current
  directory. A member's own entry restricts it (`targets = ["host"]` for a
  native-only app, `["wasm32-unknown-unknown"]` for a wasm-only frontend). It
  is read from `cargo metadata`, which carries `[package.metadata]`. A member
  can only restrict: an unconfigured target or an empty list is an error
  while listing keys, never a silently dropped package.
- **Installed up front.** Configured targets are installed by the environment
  for every command, like the toolchain file's. Adding one changes every
  command's layers, and a misspelled one fails them all (rustup suggests the
  fix). A per-command install (tried: `installTargets` in rust-environment)
  avoided both, but with the toolchain file as the main source a target change
  is a toolchain change anyway, so it wasn't worth the plumbing through every
  container builder.
- **Everything runs from the Cargo root.** rust-environment places the
  toolchain files and `.cargo` directories of the Cargo root and its parents
  (a filtered `ws.directory`) at their paths and sets the workdir there, so
  rustup and cargo resolve them natively; the source mounted later replaces
  them. The chef cook runs from the same directory as every command, so
  `--manifest-path` is gone, and the cook and the build always agree on the
  toolchain and config. The current directory only selects packages: the keys
  (members at or below it), and for commands without `-p`, cargo's own rule
  (the nearest `Cargo.toml`'s package, or the root default). Member-level
  toolchain and `.cargo` files don't apply.

Features are not a dimension:

- **Features are per package**, so they would nest under packages and run
  once per package. `cargo hack … --each-feature -p a -p b` runs every
  combination of every package in **one** container, sharing the target
  directory, so dependencies whose resolved features don't change compile
  once.
- **chef doesn't help.** Each combination needs its own cooked layer (multiple
  GB each), which grows combinatorially with `--feature-powerset`.
- **cargo-hack plans better than we would.** It handles `--depth`,
  `--group-features`, `--exclude-features`, `--mutually-exclusive-features`
  and `--no-dev-deps`. A collection would have to rebuild that logic or take
  its keys from `--print-command-list`.
- **What a collection would add:** listing combinations, rerunning one, and
  engine splitting. A batch that loops over the selected combinations in one
  container keeps the sharing, but a failing batch still fails as one unit.
  Worth revisiting once the engine splits collections across workers.

## Why checks are not per item (caching)

- The target directory is not cached (see `cargo-chef.md`), so each separate
  cargo invocation compiles the dependencies of its selection from scratch. A
  check per package, binary, test or example would compile shared dependencies
  once per item. On the package collection the batch keeps a full run at
  **one** invocation per target triple (one in total with only `host`, as
  before).
- Per-item result caching would require narrowing each package's source to
  its own path-dependency closure. That can't be done content-addressed here:
  only `Workspace.directory` results are content-addressed, and a filtered
  `Directory` is keyed by its call chain, which includes the full source
  (measured in `cargo-chef.md`). Doing it in the constructor would mean
  `cargo metadata` on every construction. Any source edit therefore re-runs
  the batch, which is no worse than before.
- cargo-chef: the batch passes `package: names` to the existing command
  builders, so `CargoCommand.chefCook` mirrors `-p a -p b` and feature
  unification matches exactly. Measured on the fixture: after a source-only
  edit, `cargo chef cook … --package greeting --tests` was `CACHED [0.0s]`, the
  planner re-ran (0.1s), and `cargo test --package greeting` compiled only the
  workspace crate. Each distinct selection is its own cook layer, cached
  across source edits until a manifest changes.

## Key discovery

`cargo metadata --no-deps --format-version 1 --offline` in the module's build
container (same layers as every other command, so no extra setup). With
`--no-deps`, `packages` is exactly the workspace members, with cargo's own
target discovery (`autobins`, `[[bin]]`, `src/bin/*`). The key set changes
with the source, but the exec takes about 0.1s. Keys are only computed when
listing with `--all` or running.
A directory without a `Cargo.toml` above it gives an empty collection (no exec).

Members are filtered to those whose directory is at or below the current
directory, mirroring `cargo` run from that directory and `dagger/go`'s
"modules at or below the working directory".

## References

- [dagger/dagger#14221: Artifact Collections](https://github.com/dagger/dagger/pull/14221)
- [dagger/dang-sdk#18: collection authoring in Dang](https://github.com/dagger/dang-sdk/pull/18) (`docs/collections.md`)
- [dagger/go `collections` branch, `go.dang`](https://github.com/dagger/go/blob/collections/go.dang): modules → packages → tests, binaries → platforms
- [kpenfound/greetings-api `collections` branch](https://github.com/kpenfound/greetings-api/tree/collections): workspace consuming collection modules
- Engine source at `v1.0.0-beta.15`: `core/artifact_batch.go`, `core/artifact_collection.go`, `core/artifact/dimension.go`, `core/modtree.go`, `core/module.go`, `core/schema/checks.go`
