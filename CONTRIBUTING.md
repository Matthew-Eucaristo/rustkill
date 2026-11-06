# Contributing

Thanks for helping. rustkill aims to stay small, safe and easy to audit.

## Development

```sh
gofmt -l .
go vet ./...
GOOS=linux go vet ./...
GOOS=windows go vet ./...
go test -race ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go run . --dry-run
```

To try the TUI on a synthetic machine, run `source demo/setup.sh`, then `rustkill`. When the TUI changes visibly, re-record the README GIF locally with `vhs demo/demo.tape` (see the Demo section of `AGENTS.md`), and check the frames before committing.

Tests build throwaway projects and git repositories in temp dirs (`internal/testfix`) and swap the trash for a temp folder. Never test removal against real directories; use `--dry-run`, `--list` or `scan --json` there.

## Website

`site/` is the GitHub Pages site: plain HTML, CSS and JS. Preview it with `site/build.sh /tmp/site/rustkill && python3 -m http.server -d /tmp/site 8000`, then open http://localhost:8000/rustkill/. The build needs Go: it runs `demo/setup.sh` in a temp folder and keeps its read-only `scan --json` as `sample.js`, so the page's replica shows real output. It uses ffmpeg for the recording when installed, and deploys from `main` through `.github/workflows/pages.yml`.

## Ground rules

- **The official way only.** A toolchain, component, target or installed tool is removed by rustup's or cargo's own command. Build directories and Cargo's caches, which have no stable cleanup command, move to the Trash. When the tool is missing, show the command and leave the item alone.
- **Read, never write, other tools' state.** Parse configs and lockfiles read-only. When a format is unknown, fail closed: protect the item and say why.
- **Evidence, not names.** A build directory counts only with cargo's own tag and layout. A new item kind is an entry in `internal/scan/kinds.go`, the code that finds it, a test (including what must never match), a README row and a source link in `docs/rules.md`. A kind whose level is not backed by a source is review at most.
- All removal goes through `internal/clean`, and every target is re-read and checked right before acting. Keep it that way.
- The JSON contract (`internal/contract`, `docs/contract.md`) only grows within a schema version.
- Keep dependencies to Charm modules and `golang.org/x/sys`.
- No real paths, usernames or project names in code, tests, docs or screenshots. Use placeholders such as `~/dev/my-app`.
- Pull requests are squash-merged, so the PR title becomes the release-notes entry. Use a conventional prefix: `feat:`, `fix:`, `docs:`, `test:`, `ci:` or `chore:` (the last four are left out of release notes).

## Reporting security issues

See [SECURITY.md](SECURITY.md).
