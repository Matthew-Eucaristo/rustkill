#!/usr/bin/env bash
# Builds a synthetic machine for the demo recording: a fake HOME (on macOS a
# small disk image, so "free" shows the image), Rust projects with cargo-
# tagged target folders, Cargo's registry and git caches (including the
# folders Cargo before 1.85 used), sccache, tools from cargo install and two
# rustup toolchains with components, plus stand-in rustup/cargo commands
# that change nothing. It ends in a shell whose environment holds nothing
# from the real machine.
# Usage: source demo/setup.sh   (from the repository root)
# Remove: hdiutil detach /tmp/rustkill-demo/home (macOS); rm -rf /tmp/rustkill-demo
# site/build.sh runs it as RUSTKILL_DEMO_DIR=<new empty dir> bash demo/setup.sh:
# the same workspace in that folder, no disk image, and instead of the shell
# it writes the self-checked scan to <dir>/scan.json.
set -e

if [ -n "${RUSTKILL_DEMO_DIR:-}" ]; then
  DEMO="$(cd "$RUSTKILL_DEMO_DIR" && pwd -P)"
  [ -z "$(ls -A "$DEMO")" ] || { echo "demo setup: $DEMO is not empty" >&2; exit 1; }
else
  DEMO="$(cd /tmp && pwd -P)/rustkill-demo"
  [ -d "$DEMO/home" ] && command -v hdiutil >/dev/null && hdiutil detach -quiet "$DEMO/home" 2>/dev/null
  [ -d "$DEMO" ] && chmod -R u+w "$DEMO" && rm -rf "$DEMO"
fi
mkdir -p "$DEMO/bin" "$DEMO/home"
go build -trimpath -o "$DEMO/bin/rustkill" .
if [ -z "${RUSTKILL_DEMO_DIR:-}" ] && command -v hdiutil >/dev/null; then
  hdiutil create -quiet -size 5g -fs APFS -volname demo -type SPARSE "$DEMO/home.sparseimage"
  hdiutil attach -quiet -nobrowse -mountpoint "$DEMO/home" "$DEMO/home.sparseimage"
fi
H="$DEMO/home"
case "$(uname)" in
  Darwin) SCCACHE="$H/Library/Caches/Mozilla.sccache" ;;
  *) SCCACHE="$H/.cache/sccache" ;;
esac
HOST=aarch64-apple-darwin # fixed, so the site's sample matches the recording
NEW=index.crates.io-1949cf8c6b5b557f OLD=index.crates.io-6f17d22bba15001f

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_OPTIONAL_LOCKS=0
export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com
blob() { mkdir -p "$1" && dd if=/dev/urandom of="$1/${3:-blob}" bs=1048576 count="$2" status=none; }
stamp_ago() { local t=$(($(date +%s) - $1 * 86400)); date -d "@$t" +%Y%m%d%H%M 2>/dev/null || date -r "$t" +%Y%m%d%H%M; }
iso_ago() { date -u -d "@$(($(date +%s) - $1 * 86400))" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v-"$1"d +%Y-%m-%dT%H:%M:%SZ; }
age() { find "$1" -exec touch -h -t "$(stamp_ago "$2")" {} + 2>/dev/null; }
# crate dir days deps gitignore [manifest-extra]
crate() {
  local dir="$1" days="$2"
  mkdir -p "$dir/src"
  printf '[package]\nname = "%s"\nversion = "0.1.0"\nedition = "2024"\n%b\n[dependencies]\n%b' "$(basename "$dir")" "${5:-}" "$3" > "$dir/Cargo.toml"
  printf '# lockfile\n' > "$dir/Cargo.lock"
  printf '%b' "$4" > "$dir/.gitignore"
  echo 'fn main() {}' > "$dir/src/main.rs"
  git init -q -b main "$dir"
  git -C "$dir" add -A
  GIT_AUTHOR_DATE=$(iso_ago "$days") GIT_COMMITTER_DATE=$(iso_ago "$days") git -C "$dir" commit -q -m init
}
# target dir debugMB releaseMB: a build directory as cargo leaves it.
target() {
  local t="$1"
  mkdir -p "$t/debug/.fingerprint" "$t/debug/incremental"
  printf 'Signature: 8a477f597d28d172789f06886806bc55\n# This file is a cache directory tag created by cargo.\n# For information about cache directory tags see https://bford.info/cachedir/\n' > "$t/CACHEDIR.TAG"
  echo '{}' > "$t/.rustc_info.json"
  : > "$t/debug/.cargo-lock"
  blob "$t/debug/deps" "$2" libdeps.rlib
  [ "${3:-0}" -gt 0 ] && { mkdir -p "$t/release"; : > "$t/release/.cargo-lock"; blob "$t/release/deps" "$3" libdeps.rlib; }
  return 0
}

# ── projects ──
P="$H/dev/game-engine"
crate "$P" 96 'bevy = "0.17"\n' '/target\n'
target "$P/target" 520 160
age "$P" 96

P="$H/dev/shop-api"
crate "$P" 44 'axum = "0.8"\ntokio = { version = "1", features = ["full"] }\nsqlx = "0.9"\n' '/target\n'
target "$P/target" 240
age "$P" 44

P="$H/dev/desktop-notes"
crate "$P" 31 'tauri = "2"\n' '/target\n'
target "$P/target" 150 120
blob "$P/target/release/bundle/dmg" 45 "Desktop Notes.dmg"
age "$P" 31

P="$H/dev/firmware"
crate "$P" 60 'embassy-executor = "0.9"\n' '*.bin\n'
printf '[toolchain]\nchannel = "1.82.0"\ntargets = ["thumbv7em-none-eabihf"]\n' > "$P/rust-toolchain.toml"
git -C "$P" add -A && GIT_AUTHOR_DATE=$(iso_ago 60) GIT_COMMITTER_DATE=$(iso_ago 60) git -C "$P" commit -q -m pin
target "$P/target" 70
age "$P" 60

P="$H/dev/cli-tool"
crate "$P" 1 'clap = "4"\n' '/target\n'
target "$P/target" 90
echo '// today' >> "$P/src/main.rs"

# ── Cargo home ──
C="$H/.cargo"
blob "$C/registry/src/$NEW/tokio-1.48.0" 130 lib.rs
blob "$C/registry/cache/$NEW" 70 tokio-1.48.0.crate
blob "$C/registry/index/$NEW/.cache" 18 index
blob "$C/registry/src/$OLD/tokio-1.38.0" 95 lib.rs
blob "$C/registry/cache/$OLD" 48 tokio-1.38.0.crate
age "$C/registry" 30; age "$C/registry/src/$OLD" 220; age "$C/registry/cache/$OLD" 220
blob "$C/git/checkouts/bevy-1a2b3c4d5e6f7a8b/9c0d1e2" 45 checkout
blob "$C/git/db/bevy-1a2b3c4d5e6f7a8b" 32 pack
age "$C/git" 96
mkdir -p "$C/bin"
for spec in "cargo-watch:8.5.3:cargo-watch:150" "ripgrep:14.1.1:rg:2" "sqlx-cli:0.9.0:sqlx:44"; do
  IFS=: read -r name ver bin days <<< "$spec"
  blob "$C/bin" 9 "$bin"
  touch -t "$(stamp_ago "$days")" "$C/bin/$bin"
  INSTALLS="${INSTALLS:+$INSTALLS,}\"$name $ver (registry+https://github.com/rust-lang/crates.io-index)\":{\"bins\":[\"$bin\"]}"
done
printf '{"installs":{%s}}\n' "$INSTALLS" > "$C/.crates2.json"
blob "$SCCACHE/0" 85 cache; age "$SCCACHE" 20

# ── rustup ──
R="$H/.rustup"
mkdir -p "$R"
printf 'version = "12"\ndefault_toolchain = "stable-%s"\nprofile = "default"\ndefault_host_triple = "%s"\n\n[overrides]\n' "$HOST" "$HOST" > "$R/settings.toml"
# toolchain dir version libMB docsMB [extra target]
toolchain() {
  local d="$R/toolchains/$1" comps="rustc-$HOST\ncargo-$HOST\nrust-std-$HOST\nrust-docs-$HOST\n"
  mkdir -p "$d/bin" "$d/lib/rustlib"
  printf '#!/bin/sh\necho "rustc %s (demo)"\n' "$2" > "$d/bin/rustc"
  printf '#!/bin/sh\necho "cargo %s (demo)"\n' "$2" > "$d/bin/cargo"
  chmod +x "$d/bin/rustc" "$d/bin/cargo"
  printf 'manifest-version = "2"\n\n[pkg.rustc]\nversion = "%s (demo)"\n' "$2" > "$d/lib/rustlib/multirust-channel-manifest.toml"
  blob "$d/lib" "$3" librustc_driver.dylib
  blob "$d/share/doc/rust/html" "$4" index.html
  echo "dir:share/doc/rust/html" > "$d/lib/rustlib/manifest-rust-docs-$HOST"
  if [ -n "${5:-}" ]; then
    comps="${comps}rust-std-$5\n"
    blob "$d/lib/rustlib/$5/lib" 70 libstd.rlib
    echo "dir:lib/rustlib/$5/lib" > "$d/lib/rustlib/manifest-rust-std-$5"
  fi
  printf '%b' "$comps" > "$d/lib/rustlib/components"
}
toolchain "stable-$HOST" 1.99.1 260 230 wasm32-unknown-unknown
toolchain "1.82.0-$HOST" 1.82.0 230 210
age "$R/toolchains/stable-$HOST" 1; age "$R/toolchains/1.82.0-$HOST" 60

# ── stand-in tools: rustkill finds them on PATH; they change nothing ──
printf '#!/bin/sh\nexit 0\n' > "$DEMO/bin/rustup"
printf '#!/bin/sh\nexit 0\n' > "$DEMO/bin/cargo"
chmod +x "$DEMO/bin/"*

# Self-checks: every path rustkill reports lies in the demo HOME, and
# nothing is live. Liveness reads this machine's process table, so a cargo
# build running here would show up in the recording.
ENV=(HOME="$H" PATH="$DEMO/bin:/usr/bin:/bin:/usr/sbin" TERM="${TERM:-xterm-256color}" LANG=en_US.UTF-8 GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1)
SCAN=$(env -i "${ENV[@]}" "$DEMO/bin/rustkill" scan --json "$H/dev")
if grep -E '"path": "/' <<< "$SCAN"; then
  echo "demo setup: rustkill reports a path outside the demo; not recording" >&2
  return 1 2>/dev/null || exit 1
fi
if grep -E '"level": "live"' <<< "$SCAN" >/dev/null; then
  echo "demo setup: cargo, rustc or rustup is running on this machine; stop it and record again" >&2
  return 1 2>/dev/null || exit 1
fi
if [ -n "${RUSTKILL_DEMO_DIR:-}" ]; then
  printf '%s\n' "$SCAN" > "$DEMO/scan.json"
  return 0 2>/dev/null || exit 0
fi
cd "$H"
set +e
exec env -i "${ENV[@]}" PS1='$ ' bash --norc --noprofile
