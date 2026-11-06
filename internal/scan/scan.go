// Package scan finds Rust projects, the build directories cargo leaves in
// and around them, Cargo's caches, tools installed with cargo install and
// rustup's toolchains, and decides how freely each may go. It only reads;
// all removal lives in internal/clean.
package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Matthew-Eucaristo/rustkill/internal/gitx"
	"github.com/Matthew-Eucaristo/rustkill/internal/pathx"
	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
	"github.com/Matthew-Eucaristo/rustkill/internal/size"
)

// Category groups items in the UI and in JSON.
type Category string

const (
	CatProject Category = "project" // a project's build directory
	CatCache   Category = "cache"   // Cargo's caches, shared build directories, sccache
	CatGlobal  Category = "global"  // tools installed with cargo install
	CatRuntime Category = "runtime" // rustup toolchains, components and targets
)

// Item is one thing rustkill can remove.
type Item struct {
	ID          string
	Fingerprint string
	Kind        string
	Category    Category
	Name        string
	Path        string
	Project     *Project // nil outside projects
	Repo        string   // git work tree holding Path; "" outside git
	Worktree    bool     // Repo is a linked worktree
	Tool        string   // owning tool for caches, installs and toolchains
	// Command is the owning tool's official removal command. Items with a
	// command are removed by running it (which deletes permanently, by the
	// tool's design); items without one move to the Trash.
	Command    []string
	CommandDir string
	Toolchain  string   // toolchains and components: the toolchain's folder name
	Version    string   // toolchains: the Rust version
	Files      []string // components: what rustup installed, absolute (measured instead of Path)
	Regenerate string
	Explain    string
	Note       string

	Measured bool
	Size     size.Size
	Newest   time.Time

	LastUsed       time.Time
	LastUsedSource string // "last build", "atime", "modified"

	Ignored, Tracked bool
	GitErr           string
	NestedRepo       bool
	Unreadable       int64 // folders inside that could not be listed

	Safety Safety
	Errors []string

	dev, ino uint64
}

// Action is "command" or "trash".
func (it *Item) Action() string {
	if len(it.Command) > 0 {
		return "command"
	}
	return "trash"
}

// Freeable is what removal is expected to free right away (the measured
// exclusive bytes), or -1 before measuring.
func (it *Item) Freeable() int64 {
	if !it.Measured {
		return -1
	}
	return it.Size.Exclusive()
}

// Options control a scan.
type Options struct {
	Roots  []string // absolute, symlink-free
	Home   string
	Global bool // also list caches, installed tools and toolchains
	Env    Env
}

// Result is a scan.
type Result struct {
	Roots    []string
	Projects []*Project
	Items    []*Item
	Warnings []string
	Dirs     int
}

// Find returns the item with this id or path.
func (r *Result) Find(ref string) *Item {
	for _, it := range r.Items {
		if it.ID == ref || (it.Path != "" && pathx.Same(it.Path, ref)) {
			return it
		}
	}
	return nil
}

func id(prefix, path string) string {
	h := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))
	return prefix + "-" + hex.EncodeToString(h[:6])
}

// Scan discovers projects and build directories below the roots and, with
// Global, Cargo's caches, installed tools and rustup's toolchains. Sizes
// come later (Measure).
func Scan(opts Options) (*Result, error) {
	res := &Result{Roots: opts.Roots}
	if !gitx.Available() {
		res.Warnings = append(res.Warnings, "git is not on PATH: build directories inside repositories cannot be checked and are protected")
	}
	w := newWalker(opts.Home)
	for _, root := range opts.Roots {
		w.walk(root)
	}
	res.Dirs = w.dirs
	if n := len(w.denied); n > 0 {
		slices.Sort(w.denied)
		first := w.denied[0]
		if rel, err := filepath.Rel(opts.Home, first); err == nil && opts.Home != "" && !strings.HasPrefix(rel, "..") {
			first = filepath.Join("~", rel)
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d folder(s) could not be read (permission denied), e.g. %s; projects inside them are not listed", n, first))
	}
	seen := map[string]bool{}
	for _, p := range w.found {
		if !seen[norm(p.Path)] {
			seen[norm(p.Path)] = true
			res.Projects = append(res.Projects, p)
		}
	}
	slices.SortFunc(res.Projects, func(a, b *Project) int { return strings.Compare(a.Path, b.Path) })
	table := Snapshot()
	res.Items = evaluate(res.Projects, w.builds, table, time.Now())
	if opts.Global {
		g, warns := globals(opts.Env, table)
		res.Warnings = append(res.Warnings, warns...)
		for _, it := range g {
			if res.Find(it.Path) == nil || it.Path == "" { // a shared target inside a root is already listed
				res.Items = append(res.Items, it)
			}
		}
		pinned(res.Items, res.Projects)
	}
	if !table.CwdKnown {
		res.Warnings = append(res.Warnings, "could not read process working directories: running builds cannot be detected, so nothing is preselected")
	}
	return res, nil
}

// pinned notes which projects ask for each toolchain through
// rust-toolchain(.toml), so an unused one stands out. It changes notes
// only, never levels, so a plan's fingerprints do not depend on the roots.
func pinned(items []*Item, projects []*Project) {
	for _, it := range items {
		if it.Kind != "toolchain" {
			continue
		}
		var by []string
		for _, p := range projects {
			if p.Toolchain != "" && pinMatches(p.Toolchain, it.Toolchain) {
				by = append(by, p.Name)
			}
		}
		sort.Strings(by)
		by = slices.Compact(by)
		switch {
		case len(by) > 0:
			it.Note = strings.TrimPrefix(it.Note+"; pinned by "+strings.Join(by, ", ")+" (rustup installs it again when you build there)", "; ")
		case it.Safety.Level == Review:
			it.Note = strings.TrimPrefix(it.Note+"; no project under the scanned folders pins it", "; ")
		}
	}
}

// evaluate turns the build directories found into items and gathers git,
// activity and liveness evidence.
func evaluate(projects []*Project, builds []*buildDir, table *procs.Table, now time.Time) []*Item {
	var items []*Item
	for _, b := range builds {
		fi, err := os.Lstat(b.Path)
		if err != nil || !fi.IsDir() {
			continue
		}
		items = append(items, buildItem(b, fi))
	}
	gitEvidence(items)
	activity(projects, now)
	for _, p := range projects {
		p.CwdKnown = table.CwdKnown
		p.Live = liveIn(table, p)
	}
	for _, it := range items {
		it.classify(now)
	}
	return items
}

func buildItem(b *buildDir, fi os.FileInfo) *Item {
	it := &Item{ID: id("tgt", b.Path), Kind: "target", Category: CatProject, Path: b.Path, Project: b.Owner,
		Repo: b.Repo, Worktree: b.Worktree, Name: filepath.Base(b.Path), Regenerate: "cargo build (from scratch, so the first build is slow)"}
	it.dev, it.ino = size.Identity(fi)
	if p := b.Owner; p != nil {
		if rel, err := filepath.Rel(p.Path, b.Path); err == nil {
			it.Name = filepath.ToSlash(rel)
		}
		it.Explain = "Cargo's build output for " + p.Name + ": compiled dependencies, incremental caches, test and doc builds, and the binaries in debug/ and release/. `cargo clean` removes the same folder."
	} else {
		it.Kind, it.Category, it.Tool = "shared-target", CatCache, "cargo"
		it.Name = "shared build dir " + filepath.Base(b.Path)
		it.Regenerate = "the next cargo build of every project that uses it"
		it.Explain = "A Cargo build directory outside any project, the kind CARGO_TARGET_DIR or build.target-dir points several projects at."
	}
	return it
}

// gitEvidence asks git, once per repository, which build directories it
// ignores and which hold tracked files.
func gitEvidence(items []*Item) {
	byRepo := map[string][]*Item{}
	for _, it := range items {
		if it.Repo != "" {
			byRepo[it.Repo] = append(byRepo[it.Repo], it)
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for repo, its := range byRepo {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			checkRepo(repo, its)
		}()
	}
	wg.Wait()
}

func checkRepo(repo string, its []*Item) {
	rels := make([]string, len(its))
	var in strings.Builder
	for i, it := range its {
		rel, err := filepath.Rel(repo, it.Path)
		if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
			it.GitErr = "outside its repository"
			continue
		}
		rels[i] = filepath.ToSlash(rel)
		in.WriteString(rels[i] + "/\x00")
	}
	// The project folders too: a project git ignores is not source.
	projects := map[*Project]string{}
	for _, it := range its {
		p := it.Project
		if p == nil || it.GitErr != "" {
			continue
		}
		if _, seen := projects[p]; seen {
			continue
		}
		rel, err := filepath.Rel(repo, p.Path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			projects[p] = ""
			continue
		}
		projects[p] = filepath.ToSlash(rel)
		in.WriteString(projects[p] + "/\x00")
	}
	if !gitx.Available() {
		for _, it := range its {
			it.GitErr = "git is not installed"
		}
		return
	}
	out, _, err := gitx.ReadIn(repo, in.String(), "check-ignore", "--stdin", "-z")
	if err != nil {
		for _, it := range its {
			it.GitErr = err.Error()
		}
		return
	}
	ignored := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			ignored[strings.TrimSuffix(p, "/")] = true
		}
	}
	for p, rel := range projects {
		p.GitIgnored = rel != "" && ignored[rel]
	}
	for i := 0; i < len(its); i += 200 {
		chunk := its[i:min(i+200, len(its))]
		args := []string{"ls-files", "-z", "--"}
		for j, it := range chunk {
			if rels[i+j] != "" && it.GitErr == "" {
				args = append(args, rels[i+j])
			}
		}
		out, err := gitx.Read(repo, args...)
		for j, it := range chunk {
			rel := rels[i+j]
			if rel == "" || it.GitErr != "" {
				continue
			}
			if err != nil {
				it.GitErr = err.Error()
				continue
			}
			it.Ignored = ignored[rel]
			for _, f := range strings.Split(string(out), "\x00") {
				if f == rel || strings.HasPrefix(f, rel+"/") {
					it.Tracked = true
					break
				}
			}
		}
	}
}

// activity: the newer of the last commit touching the project and the
// newest source file seen while walking (build output excluded). A
// workspace root takes the activity of its members, since their builds all
// land in its target directory.
func activity(projects []*Project, now time.Time) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, p := range projects {
		if p.Repo == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			out, err := gitx.Read(p.Path, "log", "-1", "--format=%ct", "--", ".")
			if sec, perr := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil && perr == nil {
				p.LastActivity, p.ActivitySource = time.Unix(sec, 0), "commit"
			}
		}()
	}
	wg.Wait()
	for _, p := range projects {
		if p.newest.After(p.LastActivity) && !p.newest.After(now.Add(time.Hour)) {
			p.LastActivity, p.ActivitySource = p.newest, "files"
		}
	}
	// Longest paths first, so activity rises through nested projects.
	byDepth := slices.Clone(projects)
	slices.SortFunc(byDepth, func(a, b *Project) int { return len(b.Path) - len(a.Path) })
	for _, p := range byDepth {
		for _, q := range projects {
			if q != p && pathx.Within(q.Path, p.Path) && p.LastActivity.After(q.LastActivity) {
				q.LastActivity, q.ActivitySource = p.LastActivity, p.ActivitySource
			}
		}
	}
}

// rustProcs are executables that build in, check or watch a project while
// they run. A shell or editor sitting in the folder does not count.
var rustProcs = []string{"cargo", "rustc", "rustdoc", "clippy-driver", "rust-analyzer", "rust-analyzer-proc-macro-srv",
	"cargo-watch", "bacon", "cargo-nextest", "cargo-tauri", "trunk", "dx", "cargo-leptos", "cargo-shuttle", "wasm-pack",
	"cargo-llvm-cov", "cargo-tarpaulin", "cargo-miri", "miri"}

// liveIn: Rust processes working in the project or below it.
func liveIn(t *procs.Table, p *Project) []procs.Proc {
	var out []procs.Proc
	for _, pr := range t.Procs {
		if pr.Cwd == "" || !slices.ContainsFunc(rustProcs, func(n string) bool { return strings.EqualFold(n, pr.Command) }) {
			continue
		}
		if pathx.SameOrWithin(p.Path, pr.Cwd) {
			out = append(out, pr)
		}
	}
	return out
}

// lockedBy reports the first profile directory whose build lock cargo
// holds right now.
func lockedBy(dir string) string {
	for _, d := range profileDirs(dir) {
		for _, l := range buildLocks {
			if lockHeld(filepath.Join(d, l)) {
				rel, _ := filepath.Rel(dir, filepath.Join(d, l))
				return filepath.ToSlash(rel)
			}
		}
	}
	return ""
}

// bundles lists the folders inside a build directory that hold installers
// or app bundles a packager built (Tauri, cargo-bundle, cargo-packager).
func bundles(dir string) []string {
	var out []string
	for _, pat := range []string{"release/bundle", "*/release/bundle", "release/packager", "release/bundle-*"} {
		m, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pat)))
		for _, x := range m {
			if fi, err := os.Lstat(x); err == nil && fi.IsDir() {
				rel, _ := filepath.Rel(dir, x)
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	return out
}

// classify turns evidence into a level. It runs again after measuring.
func (it *Item) classify(now time.Time) {
	partial := func(s *Safety) {
		if it.Unreadable > 0 {
			s.add(ReasonPartialRead, Review, fmt.Sprintf("%d folder(s) inside could not be read, so its size and last activity may be incomplete", it.Unreadable))
		}
	}
	if it.Kind != "target" && it.Kind != "shared-target" {
		partial(&it.Safety)
		it.fingerprint()
		return
	}
	s := Safety{Level: Safe}
	if it.Measured && !it.Newest.IsZero() {
		it.LastUsed, it.LastUsedSource = it.Newest, "last build"
	}
	if it.Kind == "shared-target" {
		s.add(ReasonSharedTarget, Review, "several projects may build into it; each starts from scratch afterwards")
	}
	switch {
	case it.Repo == "":
	case it.GitErr != "":
		s.add(ReasonGitError, Protected, "git could not check it: "+it.GitErr)
	case it.Tracked:
		s.add(ReasonTracked, Protected, "git tracks files inside "+it.Name)
	case !it.Ignored:
		s.add(ReasonNotIgnored, Protected, "git does not ignore "+it.Name+", so it may be source")
	}
	if it.NestedRepo {
		s.add(ReasonNestedRepo, Protected, "another repository lies inside")
	}
	if l := lockedBy(it.Path); l != "" {
		s.add(ReasonBuilding, Live, "cargo is building in it right now (it holds "+l+")")
	}
	p := it.Project
	if p != nil && p.GitIgnored {
		s.add(ReasonIgnoredProject, Review, "git ignores the project folder itself, so it is not source: it may be a vendored copy or test data")
	}
	if it.Worktree {
		s.add(ReasonWorktree, Review, "in a linked git worktree that an agent or app may still be using (fell manages the worktree itself)")
	}
	if p != nil {
		if len(p.Live) > 0 {
			var names []string
			for _, pr := range p.Live {
				names = append(names, fmt.Sprintf("%s (pid %d)", pr.Command, pr.PID))
			}
			s.add(ReasonInUse, Live, "in use by "+strings.Join(names, ", "))
		} else if !p.CwdKnown {
			s.add(ReasonLivenessUnknown, Review, "running processes could not be checked")
		}
		switch {
		case p.LastActivity.IsZero() && it.LastUsed.IsZero():
			s.add(ReasonUnknownAge, Review, "last activity unknown")
		case now.Sub(p.LastActivity) < 7*24*time.Hour:
			s.add(ReasonRecent, Review, "project changed in the last 7 days")
		case !it.LastUsed.IsZero() && now.Sub(it.LastUsed) < 7*24*time.Hour:
			s.add(ReasonRecent, Review, "built in the last 7 days")
		}
	}
	if b := bundles(it.Path); len(b) > 0 {
		s.add(ReasonDeployable, Review, "holds installers or app bundles you built: "+strings.Join(b, ", "))
	}
	partial(&s)
	it.Safety = s
	it.fingerprint()
}

// fingerprint hashes the safety-relevant state. Sizes and times are left
// out so a build writing into target does not invalidate a plan; a changed
// level or reason does.
func (it *Item) fingerprint() {
	var codes []string
	for _, r := range it.Safety.Reasons {
		codes = append(codes, r.Code)
	}
	sort.Strings(codes)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%v\x00%v\x00%s\x00%s\x00%s", it.Kind, strings.ToLower(it.Path), it.dev, it.ino,
		it.Ignored, it.Tracked, it.Safety.Level, strings.Join(codes, ","), strings.Join(it.Command, "\x01"))
	it.Fingerprint = "fp-" + hex.EncodeToString(h.Sum(nil)[:8])
}

// Measurement is a finished size measurement, applied with Item.Apply on
// the goroutine that owns the result.
type Measurement struct {
	ID     string
	Result size.Result
}

// Measure sizes one item: its folder, or for a rustup component the files
// rustup installed for it.
func Measure(it *Item) Measurement {
	opts := size.Options{Workers: 8}
	if len(it.Files) == 0 {
		return Measurement{ID: it.ID, Result: size.Measure(it.Path, opts)}
	}
	var r size.Result
	for _, f := range it.Files {
		m := size.Measure(f, opts)
		r.Size = r.Size.Add(m.Size)
		r.Unreadable += m.Unreadable
		if m.Newest.After(r.Newest) {
			r.Newest = m.Newest
		}
	}
	return Measurement{ID: it.ID, Result: r}
}

// Apply records a measurement and re-checks the item.
func (it *Item) Apply(m Measurement) {
	it.Measured, it.Size, it.Newest = true, m.Result.Size, m.Result.Newest
	it.Unreadable = m.Result.Unreadable
	it.NestedRepo = m.Result.Repo && (it.Kind == "target" || it.Kind == "shared-target")
	it.classify(time.Now())
}

// MeasureInto measures every item, a few at a time, and sends each result.
// It closes ch when done.
func (r *Result) MeasureInto(ch chan<- Measurement) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, it := range r.Items {
		if it.Measured || it.Path == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			ch <- Measure(it)
		}()
	}
	wg.Wait()
	close(ch)
}

// MeasureAll measures everything in place.
func (r *Result) MeasureAll() {
	ch := make(chan Measurement)
	go r.MeasureInto(ch)
	for m := range ch {
		if it := r.Find(m.ID); it != nil {
			it.Apply(m)
		}
	}
}
