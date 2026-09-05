# cargo-chef as an optional dependency-cache layer for the Rust modules

Research date: 2026-09-05

## Conclusion

**Adopt cargo-chef, optional and off by default, as a linear step in the
existing container chain.** It fills the gap left when the shared `/target`
cache volume was dropped (`63dd5d1`, after `ce70590` had to serialize it with
`CacheSharingMode.LOCKED`): today `CARGO_TARGET_DIR=/target` is set but never
cached, so any source edit recompiles every dependency. cargo-chef replaces the
mutable, contended volume with an immutable, content-addressed layer that
Dagger's ordinary exec cache reuses whenever the dependency *recipe* is
unchanged.

The shape that works:

```
base → rustEnv.apply → overlay → hook → buildEnv.applyEnvironment ─┬─→ [planner] mount src, `cargo chef prepare` → recipe.json
                                                                   │                                                    │
                                                                   └─→ withFile(recipe.json) → `cargo chef cook` ←──────┘
                                                                        → buildEnv.applySource → cargo build/test/clippy
```

- The planner is a **side branch** off the same container; only the recipe's
  *content* survives it. The cook runs on the **main chain**, which never sees
  the source, so source edits cannot invalidate it.
- **Embed the recipe by content, not as a `File`.** Dagger keys `withFile`,
  `withMountedFile` and `withDirectory` by the *producing call chain*, which
  includes the source, so a content-identical `recipe.json` from an edited
  source still misses the cache. Reading `File.contents` and writing it back
  with `withNewFile(path, contents)` puts the content itself in the call, and
  the cook is reused (measured below).[^dagger-caching]
- The cook runs **after environment variables and before the source mount**,
  in the same container that later builds. Anything that changes cargo
  fingerprints (`RUSTFLAGS`, `CARGO_*`, toolchain, native-lib env) must be
  identical between cook and build, otherwise the layer is wasted (or, for
  native libraries without `rerun-if-changed`, subtly wrong).
- `/target` stays a rootfs layer produced by the cook exec. Do **not** extract
  it with `Container.directory("/target")` and remount it: that is a full copy
  of a multi-GB tree per cook variant and buys nothing, because a mount added
  after an exec never affects that exec's cache key.
- Cook arguments are **derived per command** (selection, features, profile,
  target, mode) so cargo's fingerprints match exactly; a fixed `chefArgs`
  override is the escape hatch. `--all-features` is never a valid superset.
  Where a tool drives cargo itself (dx), hydrate the skeleton with
  `cook --no-build` and let the tool compile it: the flags match by
  construction.
- Pin the cargo-chef version (`0.1.78` today) and make it settable. The binary
  is upstream of the cook exec, so "latest" would invalidate every consumer's
  dependency layer whenever upstream releases.

## What `cargo chef prepare` captures

All of this is read relative to the **current directory** (`base_path`); chef
does not walk parent directories.[^read]

| Captured | Details |
|---|---|
| Every workspace-member `Cargo.toml` | Discovered via `cargo metadata` (with `--no-deps` when `Cargo.lock` exists).[^no-deps] Normalized: implicit `[[bin]]`/`[lib]`/tests/benches/examples made explicit via `complete_from_path`, `[lints]` and `[workspace.lints]` stripped, bins sorted, **local crate versions masked to `0.0.1`**.[^manifests][^masking] |
| `Cargo.lock` | If present; local crate versions masked.[^lockfile] |
| `.cargo/config` or `.cargo/config.toml` | **Only** `<cwd>/.cargo/`. Not parent directories, not `$CARGO_HOME/config.toml`.[^config] |
| `rust-toolchain` / `rust-toolchain.toml` | Only `<cwd>/`.[^toolchain] |

Consequences for this repository:

- `prepare` needs the **full source tree** (target auto-discovery stats
  `src/main.rs`, `tests/*.rs`, `build.rs`, ...) and must run from the **cargo
  workspace root**. From a subdirectory of a virtual workspace the root
  `[workspace]` manifest is silently omitted.[^root-manifest]
  `ws.findUp("Cargo.lock")` locates the root (already used in
  `cargo-lock/main.dang`).
- `rust-environment` finds `.cargo/` and `rust-toolchain.toml` with `findUp`,
  which may resolve *above* the cargo root. Chef will not capture those, so the
  cook container must have them placed at the same relative path under the
  source root, or `RUSTFLAGS`/toolchain diverge between cook and build.
- Path dependencies outside the workspace are not captured; cook fails on them
  (upstream limitation).
- Recipe content is location-independent (relative paths only), so its content
  hash is stable across machines.

## What `cargo chef cook` does

`cook` writes the skeleton into the **current directory**, runs
`$CARGO build|check|clippy|zigbuild` with a fixed set of pass-through flags,
then deletes the dummy **library** and **build-script** outputs so the real
crates are rebuilt.[^cook][^remove-dummies] Supported flags include
`--release`, `--profile`, `--target`, `--features`, `--no-default-features`,
`--all-features`, `--tests`, `--benches`, `--examples`, `--all-targets`,
`--manifest-path`, `-p`, `--bin`, `--bins`, `--workspace`, `--locked`,
`--offline`, `--check`, `--clippy`, `--no-build`.[^cook-flags] Not supported:
`--lib`, `--example NAME`, `--test NAME`, `--bench NAME`, `--exclude`,
`--config`.

`cook` and the real build must agree on: workspace root path,
`CARGO_TARGET_DIR`, `CARGO_HOME`, toolchain, `RUSTFLAGS`, profile, target,
features, and package selection. Always invoke as `cargo chef ...` (never the
binary directly): cook reads the `CARGO` environment variable that cargo sets
for subcommands to pick the right toolchain.[^cargo-env]

## Rust-version compatibility

**No hard coupling.** cargo-chef is a standalone binary that shells out to
`$CARGO`; it does not link rustc or cargo.[^cargo-env] The README's "same Rust
version in all stages" warning is about *target-dir reuse* (the rustc version
is in every fingerprint), which is automatically satisfied when cook and build
run in the same container.[^readme]

Verified: cargo-chef 0.1.78 (static `aarch64-unknown-linux-musl` release
binary) against `rust:1-slim-trixie` (rustc 1.98.0) with no issues.

Soft coupling only: chef's own parsers (`cargo_manifest`, `guppy`) occasionally
need updates for new manifest syntax (the `[lints]` stripping is the recent
example).[^changelog] Pin the latest release; allow override.

Install from GitHub releases. Assets are cargo-dist archives named
`cargo-chef-<triple>.tar.xz` with the binary at
`cargo-chef-<triple>/cargo-chef`; `x86_64` and `aarch64` are published for both
`-unknown-linux-gnu` and `-unknown-linux-musl`.[^release] Prefer musl: static,
works on any base image. Avoid `cargo install cargo-chef` (compiles for
minutes).

## Dagger caching: what actually gets reused

BuildKit computes content-based cache keys for file copies, so the classic
Dockerfile pattern (`COPY recipe.json`, `RUN cargo chef cook`) is keyed on the
recipe's content.[^buildkit-filecopy] **Dagger v1.0 does not behave the same
way.** Measured on `v1.0.0-beta.11` with a probe that copies an identical file
from two directories differing only in an unrelated file, then runs an exec that
prints random bytes:

| Recipe injected with | Downstream `withExec` reused? |
|---|---|
| `withFile(path, file)` | no |
| `withMountedFile(path, file)` | no |
| `withDirectory(path, dir.filter(include: [...]))` | no |
| `withNewFile(path, file.contents)` | **yes** |

Dagger keys operations by the digest of the call chain that produced their
inputs; `File`/`Directory` IDs are not content-addressed, so only an operation
whose *argument* is the content itself is stable across sources.

Confirmed end to end on the `cargo-artifacts` fixture (`cook --release`,
33 dependencies): with `withFile`, a source-only change (an added, unreferenced
file) re-ran the cook (`Finished ... in 3.93s`, then `3.82s`); with
`withNewFile(recipe.contents)`, the same change left the cook
`CACHED [0.0s]` while the planner re-ran in `0.1s`.

Cost of the approach: the recipe (manifests + lockfile as JSON, typically tens
to a few hundred KB) is embedded in the call ID. Acceptable; noted.

## Correctness: the mtime question

`remove_compiled_dummies` deletes lib and build-script artifacts but **not bin
artifacts**.[^remove-dummies] Cargo's freshness check for path crates is
mtime-based (a source file older than the fingerprint's dep-info counts as
fresh), and BuildKit's file copy preserves source mtimes unless a timestamp
override is set.[^fsutil] So a bin-only crate whose mounted source is older
than the cook layer looked like it could ship the dummy `fn main() {}` binary.

Tested in Docker with a bin-only, zero-dependency crate whose `main.rs` was
dated 2020: **the real binary was built**. The reason is version masking. The
dummy compiles as `app v0.0.1` (`-C metadata` hash `a8b77cf...`); the real crate
as `app v0.1.0` (`c258d9f...`). Different fingerprint directories, no collision,
mtimes irrelevant.[^masking]

Edge case: a workspace crate with **no `version` field** (allowed since Rust
1.75; defaults to `0.0.0` and is not masked because masking only rewrites an
existing string value) or literally `version = "0.0.1"` *would* collide with
its dummy. Document this; consider guarding for it.

## Feature unification and package selection

Cargo unifies the features of a shared dependency across **the packages
selected by the invocation**.[^unification] A cook that selects a different
package set than the real build (e.g. cook at the root with all members, build
`cd api && cargo build`) resolves different feature sets for shared
dependencies, which changes their `-C metadata` hash: those dependencies and
everything above them recompile. Correctness is never at stake; only the hit
rate.

Cargo selects packages from three inputs; chef can mirror all three:

| Real command selects via | Mirror in `cargo chef cook` with |
|---|---|
| CWD (nearest `Cargo.toml` upward) | `--manifest-path /work/src/<ws.findUp("Cargo.toml")>` |
| `-p` | same `-p` flags |
| `--workspace` | `--workspace` (`--exclude` unsupported; accept the superset) |

Rule: cook always runs from `cargoRoot = ws.findUp("Cargo.lock")` (correct
skeleton layout) and always passes
`--manifest-path /work/src/<manifestPath>` where
`manifestPath = ws.findUp("Cargo.toml")` (which package cargo would pick from
the workdir). When the workdir *is* the root this is a no-op, so no branching.

A fixed `--workspace` cook is a legitimate superset for *selection*, but it
does not address features (`--features X` at build time must still be
mirrored), it cooks the dependencies of every member, and it can **fail** when a
member only compiles for another target (e.g. a wasm-only `web` crate in a
fullstack workspace).[^issue-166] Hence: derive per command; offer `chefArgs`
as an explicit override.

Cargo's `resolver.feature-unification` config would make selection irrelevant,
but it is still unstable (`-Z`).[^feature-unification]

Two more CWD-relative discoveries to document: `.cargo/config.toml` (cargo)
and `rust-toolchain.toml` (rustup) are found upward from **CWD**, not from the
workspace root. A member-level config with `rustflags` is seen by a build in
that member but not by a root-level cook.

## Speedup and its limits

- Dominant case, **source changed / dependencies unchanged**: the cook exec is
  a cache hit; only workspace crates compile. Dependencies are typically
  70-95% of a clean build.
- Intra-run: `build`/`test`/`clippy` today each compile dependencies
  independently; commands sharing the same cook arguments share one layer.
- Cold path: one extra exec plus a multi-GB `/target` snapshot per distinct
  cook variant (profile x target x features). Watch engine disk / GC.
- **No persistent engine cache, no cross-run benefit.** This repository's CI
  has no Dagger Cloud token; consumers with Cloud or a persistent engine get the
  full benefit.
- Any cook/build mismatch (flags, env, toolchain) is safe but yields no gain.

Compared with the dropped target cache volume: content-addressed and immutable,
no `LOCKED` serialization, safe under concurrency; workspace crates are always
rebuilt from scratch (already the case with `CARGO_INCREMENTAL=0`).

## Dioxus

`dx` invokes `cargo rustc --profile <bundle>-<dev|release>` (`wasm-`, `server-`,
`desktop-`, ...), defining the profile ad hoc via `--config
profile.X.inherits=...`, `strip=false`, and `opt-level="s"` for web release;
plus `--target <triple> --no-default-features --features ... -p <pkg> --bin
<pkg>`.[^dx-args][^dx-profile-name][^dx-profile-args] It sets
`RUSTC_WORKSPACE_WRAPPER=dx`, which affects workspace members only, not
dependencies.[^dx-wrapper]

Mirroring those flags in `cargo chef cook` would mean reproducing dx internals
(chef has no `--config`; the ad hoc profile would have to be rebuilt through
`CARGO_PROFILE_*` environment variables) and would drift with dx versions.
**Instead, let dx supply its own flags:** `cargo chef cook --no-build` only
hydrates the skeleton (`--no-build` exists for exactly this: "projects that
rely on a custom build system"[^cook-flags]); then `dx build` runs on the
skeleton with the same options as the real `bundle`/`serve` command. dx
compiles every dependency with exactly the flags it will use later, then fails
on the dummy binary (wasm-bindgen finds no bindings) — harmless, the artifacts
are in `/target`, so the step runs with `expect: ANY`. Version masking keeps
the skeleton's own artifacts from colliding with the real ones. `Dioxus.toml`
is not part of the recipe and is embedded by content alongside the skeleton.

Measured on the `dx init` Bare-Bones web template (253 crates, dx 0.7.10):
the skeleton `dx build` compiles 175 crates in ~18s; the real `dx bundle`
afterwards compiles **only the workspace crate** (`Compiled [175/253]: test`)
and finishes in 2.3s. With a source-only change, `cook --no-build` and the
skeleton `dx build` are both `CACHED [0.0s]`; the bundle again compiles only
the workspace crate (3.2s).

Not covered: fixtures without a `Cargo.lock` (chef requires one; dx generates
it in-container otherwise), and fullstack builds (client + server are two dx
invocations; the skeleton `dx build` mirrors whatever the command passes, so
it should follow, but it is unmeasured).

## worker-build

`worker-build` runs `cargo build --lib [--release|--profile X] --target
wasm32-unknown-unknown` **with `WASM_BINDGEN_USE_JS_SYS=1`**, an environment
variable read by wasm-bindgen's proc-macro at expansion time.[^wb-cargo] Cargo
does not track environment reads made inside proc-macros, so a plain `cook`
without that variable would produce dependency artifacts compiled differently
yet considered fresh by the real build — a silent miscompile, not just a cache
miss. This makes "hydrate the skeleton, let the tool compile it" the *safe*
option for any tool that drives cargo, not merely the convenient one; it is
factored into `RustEnvironment.cookWith(container, source, sourceRoot, workdir,
command)` and used by both `dioxus` and `worker-build`.

worker-build also validates the `worker` crate version against its own
(`MIN_WORKER_LIB_VERSION`).[^wb-versions] In the workers-rs repository itself
`worker` is a workspace member, whose version cargo-chef masks to `0.0.1`, so
the skeleton build is rejected before compiling. User projects depend on
`worker` from crates.io (not masked) and are unaffected; the test uses a
standalone copy of the digest example for that reason.

Measured on the standalone digest example (`worker = "=0.8.5"`, 81 crates):
skeleton `worker-build --release` compiles 81 crates in 11.1s, then fails at
wasm-bindgen ("no function table found in module") as expected; the real
`worker-build` compiles **only** `digest-stream-on-workers` (cargo: 0.18s).
With a source-only change, the skeleton build is `CACHED [0.0s]`.

## Design summary

- **`build-environment`**: split `apply` into `applyEnvironment` (env file,
  epoch, env vars) and `applySource` (mount + workdir); `apply` calls both.
- **`rust-environment`**: `chef: Boolean! = false`, `chefVersion: String`
  (null → pinned `defaultChefVersion`, `0.1.78`); record
  `cargoRoot`/`manifestPath` via `findUp`; install the pinned musl binary in
  `apply` when enabled; `recipe(container, source)` runs the planner;
  `cook(container, source, args)` embeds `recipe.contents` with `withNewFile`,
  places `dotCargo`/toolchain file at their real relative paths, and runs
  `cook --locked --manifest-path ...` plus `args`. `apply` never cooks.
- **`rust`**: `chef`/`chefVersion`/`chefArgs` constructor arguments; a pure
  `CargoCommand.chefCook(...)` arg builder mirroring selection, features,
  profile, target, and mode (`--tests` for `test`, `--clippy` for `clippy`,
  `--check` for `doc`; `fmt`/`audit` don't cook); cook slotted between
  `applyEnvironment` and `applySource` per command. The plain `container`
  accessor cooks only when `chefArgs` is set.
- **`dioxus`**: `chef`/`chefVersion` constructor arguments; the cook is
  `rustEnv.cookWith(...)` running `dx build` with the command's options
  (`DioxusCommand.build`), for `bundle`, `bundleAsContainer` and `serve`.
  No `chefArgs`: dx derives the flags itself.
- **`worker-build`**: `chef`/`chefVersion` constructor arguments; the cook is
  `rustEnv.cookWith(...)` running `worker-build` with the same options into a
  scratch `--out-dir`.

## Verification (2026-09-05, Dagger v1.0.0-beta.11)

- `rust-environment/tests`: recipe digest identical across a `main.rs` edit
  and different across a manifest edit; bin-only crate cooked then built from
  the real source prints the real output; from a workspace member the cook runs
  at the root with `--manifest-path` and builds only that member;
  `cargo-artifacts` cook yields `libclap-*` and no `libcargo_artifacts*`.
- End to end through `rust.build(release: true)` on `cargo-artifacts` with a
  source-only change: cook `CACHED [0.0s]`, `cargo build --release` compiles
  only `cargo-artifacts v0.2.0` and finishes in 1.56s (all 33 dependencies
  reused).
- End to end through `dioxus.bundle(platform: WEB)` on the `dx init` template
  with a source-only change: skeleton `dx build` `CACHED [0.0s]`, `dx bundle`
  compiles only the workspace crate (see "Dioxus").
- End to end through `worker-build.build` on a standalone digest example with
  a source-only change: skeleton `worker-build` `CACHED [0.0s]`, the real
  build compiles only the workspace crate (see "worker-build").

## Primary sources

[^read]: [cargo-chef v0.1.78 source: `read.rs` reads config, manifests, lockfile and toolchain relative to `base_path`](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/read.rs)
[^config]: [cargo-chef v0.1.78 source: `.cargo/config` / `config.toml` read only at `base_path/.cargo`](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/read.rs#L11-L38)
[^manifests]: [cargo-chef v0.1.78 source: manifests completed with `complete_from_path`, `[lints]` removed, bins sorted](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/read.rs#L66-L109)
[^root-manifest]: [cargo-chef v0.1.78 source: a non-package workspace root manifest is added as `base_path/Cargo.toml`](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/read.rs#L56-L64)
[^lockfile]: [cargo-chef v0.1.78 source: `Cargo.lock` read from `base_path`](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/read.rs#L163-L178)
[^toolchain]: [cargo-chef v0.1.78 source: `rust-toolchain` / `rust-toolchain.toml` read from `base_path`](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/read.rs#L180-L193)
[^no-deps]: [cargo-chef v0.1.78 source: `cargo metadata --no-deps` when `Cargo.lock` exists](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/mod.rs#L51-L55)
[^masking]: [cargo-chef v0.1.78 source: local crate versions masked to `0.0.1` in manifests and lockfile](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/version_masking.rs#L12-L68)
[^cook]: [cargo-chef v0.1.78 source: `cook` writes the skeleton into the current directory, builds, then removes dummies](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/recipe.rs#L59-L77)
[^remove-dummies]: [cargo-chef v0.1.78 source: `remove_compiled_dummies` globs `lib{name}.*`, `lib{name}-*` and build-script outputs only](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/skeleton/mod.rs#L212-L293)
[^cook-flags]: [cargo-chef v0.1.78 source: `Cook` CLI flags](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/main.rs#L66-L160)
[^cargo-env]: [cargo-chef v0.1.78 source: cook executes the binary named by the `CARGO` environment variable](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/src/recipe.rs#L123-L131)
[^readme]: [cargo-chef README: usage, "same Rust version in all stages", limitations](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/README.md)
[^changelog]: [cargo-chef CHANGELOG: "Remove lints from manifests in recipe.json"](https://github.com/LukeMathWalker/cargo-chef/blob/dd01a86e7427fe2ceebf0eec13d0c6ad7c21def8/CHANGELOG.md)
[^release]: [cargo-chef v0.1.78 release assets (cargo-dist archives for `x86_64`/`aarch64`, `gnu`/`musl`)](https://github.com/LukeMathWalker/cargo-chef/releases/tag/v0.1.78)
[^issue-166]: [cargo-chef issue #166: chef builds backend (musl) deps for wasm when cooking a frontend](https://github.com/LukeMathWalker/cargo-chef/issues/166)
[^fsutil]: [fsutil source: `copyFileTimestamp` preserves source atime/mtime unless an override is set](https://github.com/tonistiigi/fsutil/blob/83cac42c1c5296d6bbc4017ec1ee3c6701f49938/copy/copy_unix.go#L58-L73)
[^buildkit-filecopy]: BuildKit source: [file operations compute content-based cache keys for their inputs](https://github.com/moby/buildkit/blob/703866e5f2e4af295b485b181b447a7755d5099c/solver/llbsolver/ops/file.go#L170), whereas [an exec's root filesystem is explicitly excluded from content-based hashing](https://github.com/moby/buildkit/blob/703866e5f2e4af295b485b181b447a7755d5099c/solver/llbsolver/ops/exec.go#L349-L356). This is what makes the Dockerfile `COPY recipe.json` + `RUN cook` pattern work; Dagger layers its own call-chain cache on top (see "Dagger caching" above).
[^dagger-caching]: Measured, not documented: the probe and the end-to-end A/B described under "Dagger caching: what actually gets reused" were run against Dagger `v1.0.0-beta.11` (engine `registry.dagger.io/engine:v1.0.0-beta.11`) on 2026-09-05.
[^unification]: [Cargo reference, resolver: "When building multiple packages in a workspace (such as with `--workspace` or multiple `-p` flags), the features of the dependencies of all of those packages are unified"](https://doc.rust-lang.org/cargo/reference/resolver.html#features)
[^feature-unification]: [Cargo reference, unstable features: `feature-unification`](https://doc.rust-lang.org/cargo/reference/unstable.html#feature-unification)
[^dx-args]: [Dioxus CLI source: `cargo_build_arguments` (`--profile`, `--target`, `--no-default-features`, `--features`, `-p`, `--bin`)](https://github.com/DioxusLabs/dioxus/blob/74a49736c029e0e6f6fc4a79b5def923f0d587b8/packages/cli/src/build/request.rs#L1689-L1747)
[^dx-profile-name]: [Dioxus CLI source: `profile_name` yields `<bundle>-<dev|release>`](https://github.com/DioxusLabs/dioxus/blob/74a49736c029e0e6f6fc4a79b5def923f0d587b8/packages/cli/src/platform.rs#L306-L318)
[^dx-profile-args]: [Dioxus CLI source: `profile_args` defines the ad hoc profile via `--config` (`strip=false`, `inherits`, `opt-level="s"`)](https://github.com/DioxusLabs/dioxus/blob/74a49736c029e0e6f6fc4a79b5def923f0d587b8/packages/cli/src/build/request.rs#L3077-L3117)
[^dx-wrapper]: [Dioxus CLI source: `RUSTC_WORKSPACE_WRAPPER=dx` (workspace members only)](https://github.com/DioxusLabs/dioxus/blob/74a49736c029e0e6f6fc4a79b5def923f0d587b8/packages/cli/src/build/request.rs#L1665-L1677)
[^wb-cargo]: [worker-build v0.8.5 source: `cargo_build_wasm` (`cargo build --lib --target wasm32-unknown-unknown`, `WASM_BINDGEN_USE_JS_SYS=1` read by the proc-macro at expansion time)](https://github.com/cloudflare/workers-rs/blob/v0.8.5/worker-build/src/build/target.rs)
[^wb-versions]: [worker-build v0.8.5 source: `MIN_WORKER_LIB_VERSION` derived from worker-build's own version](https://github.com/cloudflare/workers-rs/blob/v0.8.5/worker-build/src/versions.rs)
