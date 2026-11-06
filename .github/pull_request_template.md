**What and why**

**Checklist**
- [ ] `gofmt -l .` is empty, `go vet ./...` and `go test -race ./...` pass
- [ ] New project folder: one `Rule` with its marker, a test, a README row and a source in `docs/rules.md`
- [ ] New machine-wide item: removed only by its tool's official command (or Trash for a plain download cache), with a source in `docs/rules.md`
- [ ] Removal paths changed: a test in `internal/clean` proves the refusal
- [ ] No real paths, usernames or project names in code, tests or docs
