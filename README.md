# rustkill

**Reclaim the disk space Rust leaves behind: safely, and the official way.**

**Website:** [matthew-eucaristo.github.io/rustkill](https://matthew-eucaristo.github.io/rustkill/), with a clickable demo fed by `rustkill scan --json` on the demo workspace.

rustkill finds every `target` folder cargo built under your projects, plus the machine-wide Rust footprint: Cargo's registry and git caches, shared build directories, sccache, tools from `cargo install`, and rustup's toolchains, offline docs and cross-compilation targets. It shows how much each one really frees and explains why it is safe or not. Then it removes what you pick. Build folders and caches go to the Trash. Toolchains, components and installed tools are removed only by their own tool (`rustup toolchain uninstall`, `rustup component remove`, `rustup target remove`, `cargo uninstall`).

It is a single Go binary for macOS (first-class), Linux and Windows (best effort). It needs no Rust of its own, never runs your toolchains, and never contacts the network itself.

![rustkill scanning Rust projects, Cargo caches, cargo install tools and rustup toolchains: it selects the idle safe target folders, groups them by framework and idle time, and shows exactly what a dry run would move to the Trash and which rustup and cargo commands it would run](demo/demo.gif)

## Install

```sh
brew install matthew-eucaristo/tap/rustkill
```

Or with Go 1.26+: `go install github.com/Matthew-Eucaristo/rustkill@latest`. Release archives for macOS, Linux and Windows are on the [releases page](https://github.com/Matthew-Eucaristo/rustkill/releases).

## Use

```sh
rustkill                          # interactive; scans ~/dev (else the current folder)
rustkill ~/code ~/side            # other roots
rustkill --list                   # plain table, read-only
rustkill --dry-run                # interactive, but nothing changes
rustkill --older-than 30d --yes   # headless: trash the safe target folders of projects idle 30+ days
rustkill scan --json              # machine-readable, read-only
```

In the TUI, `space` selects, `s` selects the safe items in projects and `a` every safe item. `enter` shows exactly what will happen and asks before doing it. `tab` groups by project, framework or idle time, `o` sorts by size, idle time or name, `/` filters, and `?` explains every colour, column and key. The layout fits terminals down to 60 columns, and `NO_COLOR` turns colour off.

```
$ rustkill --list
rustkill: 21 item(s) in 5 project(s), 3.6 GB on disk, 3.0 GB freeable; 1.2 GB safe to clean  (roots: ~/dev)

~/dev/game-engine  edition 2024 · Bevy · active 3mo ago  717.2 MB
  717.2 MB    safe       target  tgt-f2bcc2c750a0
~/dev/desktop-notes  edition 2024 · Tauri · active 4w ago  330.3 MB
  330.3 MB    review     target  deployable  tgt-cc56406a0f09
~/dev/firmware  edition 2024 · toolchain 1.82.0 · Embassy · active 2mo ago  73.4 MB
  73.4 MB     protected  target  not_ignored  tgt-2118389b49dd
…
Cargo caches and shared builds  548.4 MB
  136.3 MB    safe    crates.io sources                                                    glb-77ed5dcde1d2
  99.6 MB     review  crates.io (sparse index, Cargo before 1.85) sources    old_cargo     glb-98edfcee362c
  47.2 MB     safe    git dependency checkouts                                             glb-5eb2c66b3db7
…
Toolchains, components and targets  1.6 GB
  587.2 MB    protected  toolchain stable (1.99.1)  current via `rustup toolchain uninstall stable-aarch64-apple-darwin`
  461.4 MB    review     toolchain 1.82.0           catalog_review via `rustup toolchain uninstall 1.82.0-aarch64-apple-darwin`
  241.2 MB    review     rust-docs (stable)         catalog_review via `rustup component remove rust-docs --toolchain stable-aarch64-apple-darwin`
```

(The demo workspace from `demo/setup.sh`, trimmed. `firmware` does not ignore its `target`, so rustkill leaves it alone. Toolchain 1.82.0 is pinned by `firmware`'s `rust-toolchain.toml`, and as long as it is installed, Cargo before 1.85 may still read the old registry folders.)

At the end rustkill prints a receipt that keeps the numbers apart, because they mean different things (for example):

```
rustkill receipt
  ✓ Moved to Trash       1.1 GB  3 item(s); restore from the Trash
  ✓ Freed by tools     461.6 MB  2 official command(s)
  Free space changed by +461.6 MB (measured); another ~1.1 GB when you empty the Trash
```

## What it finds

**Build directories.** A folder counts only when cargo itself marked it: cargo writes a `CACHEDIR.TAG` with its own signature into every target and build directory, next to `.rustc_info.json` or profile folders holding its lock files and fingerprints. A folder merely named `target` is never touched, and Cargo's registry, which carries the same tag but not the layout, never counts. This finds `target` folders wherever `build.target-dir` put them.

| Item | Level | Notes |
|---|---|---|
| a project's `target` | safe | review when the project changed or was built in the last 7 days, sits in a linked git worktree, or holds installers you built (`target/release/bundle`, Tauri and cargo-bundle); protected when git tracks it or does not ignore it, or a repository lies inside; live while cargo holds its build lock |
| a shared build directory (`CARGO_TARGET_DIR`, `build.target-dir`, `build.build-dir`, `$CARGO_HOME/build/*`) | review | every project that builds into it starts from scratch |

**Cargo's home** (`$CARGO_HOME`, default `~/.cargo`). There is no stable command to clean it (`cargo clean gc` is nightly-only), and the Cargo book says any part of this cache can be removed: Cargo restores what a build needs. So these go to the Trash:

| Item | Level | Comes back |
|---|---|---|
| `registry/src/<registry>`, `git/checkouts` | safe | unpacked again from the local downloads, without the network |
| `registry/cache/<registry>`, `registry/index/<registry>`, `git/db` | review | downloaded or cloned again |
| registry folders of Cargo before 1.85 (`index.crates.io-6f17d22bba15001f`, `github.com-1ecc6299db9ec823`) | safe when every installed toolchain is 1.85 or newer, else review | Cargo 1.85 renamed them, so newer Cargo never reads the old ones |
| sccache's local cache | review | sccache has no command to empty it |
| rustup's leftover `downloads` and `tmp` | safe | |

**Tools and toolchains**, each removed only by its owner's command, which rustkill runs from your home folder so no project's `rust-toolchain.toml` applies:

| Item | Removed with | Level |
|---|---|---|
| each package from `cargo install` / `cargo binstall` (from `.crates2.json`), with when it last ran | `cargo uninstall --root $CARGO_HOME <package>` | review |
| each rustup toolchain, with which projects pin it | `rustup toolchain uninstall <toolchain>` | review; rustup's default and `RUSTUP_TOOLCHAIN` are protected |
| `rust-docs` (the offline docs `rustup doc` opens, often 900 MB per toolchain) | `rustup component remove rust-docs --toolchain <toolchain>` | review |
| `rustc-dev` | `rustup component remove rustc-dev --toolchain <toolchain>` | review |
| standard libraries for cross-compiling (`wasm32-unknown-unknown`, `thumbv7em-none-eabihf`, …) | `rustup target remove <target> --toolchain <toolchain>` | review |

If rustup or cargo is not installed, the item is shown with the command to run, and rustkill leaves it alone. rustkill reads toolchain versions from rustup's own files and never runs a toolchain, so "last used" stays honest. [docs/rules.md](docs/rules.md) links the source for every rule and command.

## Safety model

rustkill refuses anything it cannot verify. Every item gets a level and the evidence behind it:

- **safe**: rebuildable, cargo's own, git-ignored with no tracked files, project idle 7+ days, nothing running in it. These may be bulk-selected (empty folders are skipped).
- **review**: allowed, never bulk-selected. Reasons include:
  - the project changed or was built this week;
  - the folder holds installers or app bundles you built;
  - it is a build directory several projects share;
  - it sits in a linked git worktree that an agent may still use (fell manages the worktree itself);
  - git ignores the project folder itself (a vendored copy, test data);
  - removing it means downloading again, or it is a tool or toolchain you installed on purpose;
  - a Cargo older than 1.85 is installed and still reads the folder.
- **protected**: shown, never removed. Reasons include:
  - git tracks files inside, or git does not ignore it;
  - another repository lies inside;
  - it is rustup's default toolchain, or rustup's settings could not be read;
  - its tool is missing;
  - the folder is not well inside your home.
- **live**: cargo, rustc, rust-analyzer, bacon, cargo-watch and friends are working in the project, or cargo holds the build directory's lock right now (rustkill asks for the lock without waiting and never creates it). Cargo's caches wait while any cargo or rustc runs, and toolchains wait while rustup runs.

Right before acting, rustkill reads the item again and refuses it if anything safety-relevant changed since the scan, naming what blocks it when it can. It also re-checks that the folder still carries cargo's tag and layout; symlinks are never followed or removed through. Commands run with a fixed argument list, never through a shell, with `RUSTUP_AUTO_INSTALL=0` and `CARGO_NET_OFFLINE=true`, so a cleanup never downloads a toolchain. `--dry-run`, `--list`, `--json` and `scan` never remove anything.

The Trash is the default. `--permanent` deletes folders instead and turns the header red. Official commands delete by their tool's own design, so the confirmation screen lists them apart.

## How sizes are counted

Sizes are allocated blocks, like `du`. A hard-linked file counts once. **FREES** appears when much of an item is shared with something outside it (APFS clones, hard links), and shows what removing it frees right now; on macOS rustkill asks APFS for each file's private size. After acting, freed space is measured from the disk, never assumed. Moving to the Trash frees nothing until you empty it, and the receipt says so.

## Automation and the JSON contract

`rustkill scan --json` (or `--ndjson` to stream) prints every item with its id, fingerprint, sizes, evidence and the exact command it would run. Paths are home-relative. `rustkill apply --plan <file|->` takes ids and fingerprints back, re-checks each item and acts. `rustkill capabilities --json` lists actions, reason codes and exit codes. The format is shared with the other tools in this family and described in [docs/contract.md](docs/contract.md); [decruft](https://github.com/Matthew-Eucaristo/decruft) uses it to show rustkill's items in its storage view and hand removals back.

Exit codes: 0 ok · 1 error · 2 usage · 3 something was refused · 4 something failed.

## Platforms

- **macOS**: first-class. Sizes account for APFS clones. The Trash supports Put Back (`/usr/bin/trash` on macOS 15+, else Finder). Running processes are checked with `lsof`, build locks with `flock`.
- **Linux**: freedesktop Trash, `/proc` process checks, `flock` build-lock checks, hard-link-aware sizes.
- **Windows**: best effort, with the full test suite running in CI. Folders go to the Recycle Bin. There is no running-process or build-lock check, so nothing is bulk-selected.

## Demo

`demo/demo.tape` records `demo/demo.gif` with [VHS](https://github.com/charmbracelet/vhs) against generated fixtures: a fake home on a small disk image with cargo-tagged build folders, a Cargo home, two rustup toolchains and stand-in `rustup` and `cargo` commands. Nothing from the real machine appears. To try the same workspace yourself, run `source demo/setup.sh`, then `rustkill`.

## Uninstall

rustkill writes no configuration, cache, log or state anywhere. The only things it changes are the items you confirm. On Linux, the first move to the Trash creates the standard `~/.local/share/Trash` folders if they are missing.

```sh
brew uninstall rustkill
brew autoremove                     # removes Go if it was installed only to build rustkill
brew cleanup --prune=all            # optional: clears Homebrew's download and build cache
brew untap matthew-eucaristo/tap    # only if nothing else is installed from this tap
```

Installed with `go install`? Run `rm "$(go env GOPATH)/bin/rustkill"`. Anything rustkill moved to the Trash stays there until you empty it or put it back.

## Credits

Every rule was derived from Cargo's and rustup's documentation and behaviour, checked against real toolchains; [docs/rules.md](docs/rules.md) links them. Prior art studied: [cargo-sweep](https://github.com/holmgr/cargo-sweep) (MIT/Apache-2.0) and [cargo-cache](https://github.com/matthiaskrgr/cargo-cache) (MIT/Apache-2.0) for what a Rust machine accumulates; [kondo](https://github.com/tbillington/kondo) (MIT) and [npkill](https://github.com/voidcosmos/npkill) (MIT) for finding and choosing build folders. No code is shared with them. rustkill shares its safety model and JSON contract with [fell](https://github.com/Matthew-Eucaristo/fell), [nodekill](https://github.com/Matthew-Eucaristo/nodekill) and [flutterkill](https://github.com/Matthew-Eucaristo/flutterkill).

## License

[MIT](LICENSE)
