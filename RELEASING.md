# Releasing

Releases are cut by pushing a `v*` tag. The `release` workflow runs the tests and publishes release archives for macOS, Linux and Windows with GoReleaser, using only the built-in `GITHUB_TOKEN` (no secrets to maintain). Release notes come from the commits since the previous tag; `docs:`, `test:`, `ci:` and `chore:` commits are left out.

The Homebrew formula lives in [Matthew-Eucaristo/homebrew-tap](https://github.com/Matthew-Eucaristo/homebrew-tap) as `Formula/rustkill.rb` and is bumped by hand, which takes a minute and needs no token.

## One-time setup

- Enable **Settings → Code security → Private vulnerability reporting** (referenced by SECURITY.md).

## Each release

If the TUI changed visibly, re-record `demo/demo.gif` first (`vhs demo/demo.tape`, then check the frames) and commit it.

```sh
V=0.1.0
git tag v$V && git push origin v$V
SHA=$(curl -sL https://github.com/Matthew-Eucaristo/rustkill/archive/refs/tags/v$V.tar.gz | shasum -a 256 | cut -d' ' -f1)
```

In a clone of the tap, create or update `Formula/rustkill.rb` from `packaging/homebrew/rustkill.rb` with the new version in `url` and `$SHA` in `sha256`, add a row to the tap README, commit `rustkill $V`, and push. Then check it:

```sh
brew update
brew upgrade matthew-eucaristo/tap/rustkill || brew install matthew-eucaristo/tap/rustkill
brew test rustkill
brew audit --strict --online matthew-eucaristo/tap/rustkill
```

When the contract changes, bump `schema_version` only for breaking changes, update `docs/contract.md`, and say so in the release notes.
