package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
	"github.com/Matthew-Eucaristo/rustkill/internal/size"
)

// tools caches lookups of rustup and cargo for one scan.
type tools struct {
	env         Env
	cargoHome   string
	rustupHome  string
	mu          sync.Mutex
	path        map[string]string
	cargoVers   [][3]int // versions of every installed cargo, for the registry check
	cargoKnown  bool     // cargoVers is complete
	cargoVersMu sync.Once
}

func newTools(env Env) *tools {
	t := &tools{env: env, path: map[string]string{}}
	t.cargoHome = homeDir(env, "CARGO_HOME", ".cargo")
	t.rustupHome = homeDir(env, "RUSTUP_HOME", ".rustup")
	return t
}

// homeDir is $VAR when it is an absolute path, else ~/def.
func homeDir(env Env, v, def string) string {
	if p := env.Getenv(v); filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if env.Home == "" {
		return ""
	}
	return filepath.Join(env.Home, def)
}

// look finds rustup or cargo on PATH, else in $CARGO_HOME/bin, where
// rustup installs its proxies even when the shell's PATH lacks them.
func (t *tools) look(name string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.path[name]; ok {
		return p
	}
	p, err := t.env.LookPath(name)
	if err != nil {
		p = ""
		exe := name
		if t.env.GOOS == "windows" {
			exe += ".exe"
		}
		if c := filepath.Join(t.cargoHome, "bin", exe); t.cargoHome != "" && regularFile(c) {
			p = c
		}
	} else if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	t.path[name] = p
	return p
}

func regularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

func isDir(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.IsDir()
}

func hasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// globals lists the machine-wide Rust footprint: Cargo's caches, shared
// build directories, sccache, tools installed with cargo install and
// rustup's toolchains, components and targets.
func globals(env Env, table *procs.Table) ([]*Item, []string) {
	t := newTools(env)
	var (
		mu    sync.Mutex
		items []*Item
		warns []string
		wg    sync.WaitGroup
	)
	for _, f := range []func(*tools) ([]*Item, []string){registry, gitCaches, sharedTargets, sccache, rustupLeftovers, installs, toolchains} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			its, ws := f(t)
			mu.Lock()
			items, warns = append(items, its...), append(warns, ws...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.SortStableFunc(items, func(a, b *Item) int { return strings.Compare(a.ID, b.ID) })
	cargoBusy := table.Running("cargo", "rustc") || lockHeld(filepath.Join(t.cargoHome, ".package-cache"))
	rustupBusy := table.Running("rustup", "rustup-init")
	for _, it := range items {
		if it.Safety.Level == "" {
			it.Safety.Level = Safe
		}
		if len(it.Command) > 0 {
			// Run from home, where nothing pins a toolchain, so the command
			// acts on what was shown, never a project's override.
			it.CommandDir = env.Home
		} else if it.Path != "" && !DeepInHome(it.Path, env.Home) && it.Kind != "shared-target" {
			it.Safety.add(ReasonBroadPath, Protected, it.Path+" is not a folder well inside your home")
		}
		switch {
		case it.Category == CatCache && it.Kind != "sccache" && it.Kind != "rustup-downloads" && cargoBusy:
			it.Safety.add(ReasonToolRunning, Live, "cargo is running; Cargo's caches wait until it stops")
		case (it.Category == CatRuntime || it.Kind == "rustup-downloads") && rustupBusy:
			it.Safety.add(ReasonToolRunning, Live, "rustup is running; toolchains wait until it stops")
		case it.Kind == "sccache" && table.Running("sccache"):
			it.Safety.add(ReasonToolRunning, Live, "the sccache server is running; stop it first with `sccache --stop-server`")
		case it.Category == CatGlobal && cargoBusy:
			it.Safety.add(ReasonToolRunning, Live, "cargo is running; cargo uninstall waits until it stops")
		}
		it.fingerprint()
	}
	return items, warns
}

// DeepInHome: p is a clean absolute path at least two levels below home, so
// removing it cannot reach home or one of its top folders.
func DeepInHome(p, home string) bool {
	if home == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return false
	}
	rel, err := filepath.Rel(home, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return strings.Contains(rel, string(filepath.Separator))
}

// newGlobal builds a machine-wide item. With exe == "" the owning tool is
// missing: the item is shown, protected, with the command to run.
func newGlobal(cat Category, kind, name, tool, path string, cmd []string, exe string) *Item {
	it := &Item{ID: id("glb", kind+"\x00"+path+"\x00"+strings.Join(cmd, " ")), Kind: kind, Category: cat, Name: name, Tool: tool, Path: path, Safety: Safety{Level: Safe}}
	if fi, err := os.Lstat(path); err == nil {
		it.dev, it.ino = size.Identity(fi)
	}
	if len(cmd) > 0 {
		if exe == "" {
			it.Note = "run `" + strings.Join(cmd, " ") + "` once " + tool + " is installed"
			it.Safety.add(ReasonNotInstalled, Protected, tool+" is not installed, so its official command cannot run here")
		} else {
			it.Command = append([]string{exe}, cmd[1:]...)
		}
	}
	return it
}

// ── Cargo's caches ───────────────────────────────────────────────────────

// Registry folders are named <host>-<hash>. Cargo 1.85 changed the hash, so
// the folders older Cargo versions used are never read by 1.85 or later.
var oldRegistries = map[string]string{
	"index.crates.io-6f17d22bba15001f": "crates.io (sparse index, Cargo before 1.85)",
	"github.com-1ecc6299db9ec823":      "crates.io (git index, Cargo before 1.85)",
}

var registryName = regexp.MustCompile(`^(.+)-[0-9a-f]{16}$`)

func friendlyRegistry(dir string) string {
	if n, ok := oldRegistries[dir]; ok {
		return n
	}
	m := registryName.FindStringSubmatch(dir)
	if m == nil {
		return dir
	}
	switch m[1] {
	case "index.crates.io", "github.com":
		return "crates.io"
	}
	return m[1]
}

func registry(t *tools) ([]*Item, []string) {
	if t.cargoHome == "" {
		return nil, nil
	}
	areas := []struct {
		area, kind, what, explain, regen string
		level                            Level
	}{
		{"src", "registry-src", "sources", "Crates Cargo unpacked from registry/cache so rustc can compile them. Cargo unpacks them again from the downloaded .crate files when a build needs them, without the network.", "cargo unpacks them again from registry/cache", Safe},
		{"cache", "registry-cache", "downloads", "The .crate archives Cargo downloaded. Since Rust 1.88 Cargo deletes downloads unused for 3 months on its own, but only when it runs.", "cargo downloads the crates again (builds need the network once)", Review},
		{"index", "registry-index", "index", "Cargo's copy of the registry index (crate versions and dependencies).", "cargo downloads the index entries again", Review},
	}
	var items []*Item
	for _, a := range areas {
		root := filepath.Join(t.cargoHome, "registry", a.area)
		for _, d := range readDirs(root) {
			path := filepath.Join(root, d)
			if !hasEntries(path) {
				continue
			}
			it := newGlobal(CatCache, a.kind, friendlyRegistry(d)+" "+a.what, "cargo", path, nil, "")
			it.Explain, it.Regenerate = a.explain+" The Cargo book: any part of this cache can be removed and Cargo restores what a build needs.", a.regen
			if a.level == Review {
				it.Safety.add(ReasonCatalogReview, Review, "removing it means downloading again")
			}
			if fi, err := os.Lstat(path); err == nil {
				it.LastUsed, it.LastUsedSource = fi.ModTime(), "modified"
			}
			if _, old := oldRegistries[d]; old {
				oldCargo, known := t.olderCargo(1, 85)
				switch {
				case !known:
					it.Safety.add(ReasonOldCargo, Review, "rustkill could not read every installed Cargo version; one older than 1.85 would still use it")
				case oldCargo != "":
					it.Safety.add(ReasonOldCargo, Review, "toolchain "+oldCargo+" is older than Rust 1.85 and still uses it")
				default:
					it.Safety = Safety{Level: Safe}
					it.Explain = "Left behind by Cargo before 1.85, which named this folder differently. Every installed Cargo is 1.85 or later and uses index.crates.io-1949cf8c6b5b557f instead, so nothing reads it."
					it.Regenerate = "only an older Cargo would use it again"
				}
			}
			items = append(items, it)
		}
	}
	return items, nil
}

func gitCaches(t *tools) ([]*Item, []string) {
	if t.cargoHome == "" {
		return nil, nil
	}
	var items []*Item
	if p := filepath.Join(t.cargoHome, "git", "checkouts"); hasEntries(p) {
		it := newGlobal(CatCache, "git-checkouts", "git dependency checkouts", "cargo", p, nil, "")
		it.Explain = "Working copies of git dependencies, one per commit used. Cargo checks them out again from git/db when a build needs them, without the network."
		it.Regenerate = "cargo checks them out again from git/db"
		items = append(items, it)
	}
	if p := filepath.Join(t.cargoHome, "git", "db"); hasEntries(p) {
		it := newGlobal(CatCache, "git-db", "git dependency clones", "cargo", p, nil, "")
		it.Explain = "Bare clones of the git repositories your projects depend on. Since Rust 1.88 Cargo deletes ones unused for 3 months on its own, but only when it runs."
		it.Regenerate = "cargo clones them again (builds need the network once)"
		it.Safety.add(ReasonCatalogReview, Review, "removing it means cloning again")
		items = append(items, it)
	}
	return items, nil
}

// sharedTargets lists build directories that CARGO_TARGET_DIR,
// CARGO_BUILD_BUILD_DIR or the user's Cargo config point every project at,
// and the per-workspace build directories under $CARGO_HOME/build that
// build.build-dir's {cargo-cache-home} template creates.
func sharedTargets(t *tools) ([]*Item, []string) {
	var cands []string
	for _, v := range []string{"CARGO_TARGET_DIR", "CARGO_BUILD_TARGET_DIR", "CARGO_BUILD_BUILD_DIR"} {
		if p := t.env.Getenv(v); filepath.IsAbs(p) && !strings.Contains(p, "{") {
			cands = append(cands, filepath.Clean(p))
		}
	}
	var warns []string
	for _, name := range []string{"config.toml", "config"} {
		cfg := filepath.Join(t.cargoHome, name)
		b, err := readSmall(cfg, 1<<20)
		if err != nil {
			continue
		}
		var c struct {
			Build struct {
				TargetDir string `toml:"target-dir"`
				BuildDir  string `toml:"build-dir"`
			} `toml:"build"`
		}
		if _, err := toml.Decode(string(b), &c); err != nil {
			warns = append(warns, "could not read Cargo's config ("+name+"); shared build directories it sets are not listed")
			break
		}
		for _, p := range []string{c.Build.TargetDir, c.Build.BuildDir} {
			p = strings.ReplaceAll(p, "{cargo-cache-home}", t.cargoHome)
			if p == "" || strings.Contains(p, "{") {
				continue
			}
			if !filepath.IsAbs(p) { // relative to the folder holding .cargo
				p = filepath.Join(filepath.Dir(t.cargoHome), p)
			}
			cands = append(cands, filepath.Clean(p))
		}
		break // cargo reads config.toml and ignores config when both exist
	}
	for _, d := range readDirs(filepath.Join(t.cargoHome, "build")) {
		cands = append(cands, filepath.Join(t.cargoHome, "build", d))
	}
	var items []*Item
	seen := map[string]bool{}
	for _, p := range cands {
		if seen[norm(p)] {
			continue
		}
		seen[norm(p)] = true
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() {
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		names := map[string]bool{}
		for _, e := range entries {
			names[e.Name()] = true
		}
		if !names["CACHEDIR.TAG"] || !cargoOutput(p, names) {
			continue // not proven to be cargo's: never offered
		}
		it := buildItem(&buildDir{Path: p}, fi)
		if repo, wt := findRepo(p); repo != "" {
			it.Repo, it.Worktree = repo, wt
			gitEvidence([]*Item{it})
		}
		it.classify(time.Now())
		if !DeepInHome(p, t.env.Home) {
			it.Safety.add(ReasonBroadPath, Protected, p+" is not a folder well inside your home")
		}
		items = append(items, it)
	}
	return items, warns
}

func sccache(t *tools) ([]*Item, []string) {
	dir := t.env.Getenv("SCCACHE_DIR")
	if !filepath.IsAbs(dir) {
		switch t.env.GOOS {
		case "darwin":
			dir = filepath.Join(t.env.Home, "Library", "Caches", "Mozilla.sccache")
		case "windows":
			if la := t.env.Getenv("LOCALAPPDATA"); la != "" {
				dir = filepath.Join(la, "Mozilla", "sccache", "cache")
			}
		default:
			if x := t.env.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(x) {
				dir = filepath.Join(x, "sccache")
			} else {
				dir = filepath.Join(t.env.Home, ".cache", "sccache")
			}
		}
	}
	if dir == "" || !hasEntries(dir) {
		return nil, nil
	}
	it := newGlobal(CatCache, "sccache", "sccache local cache", "sccache", filepath.Clean(dir), nil, "")
	it.Explain = "sccache's local compiler cache. It is capped by SCCACHE_CACHE_SIZE (10 GB by default) and evicts the oldest entries itself; sccache has no command to empty it."
	it.Regenerate = "sccache fills it again as you build (builds miss the cache once)"
	it.Safety.add(ReasonCatalogReview, Review, "builds that used it compile from scratch once")
	return []*Item{it}, nil
}

func rustupLeftovers(t *tools) ([]*Item, []string) {
	if t.rustupHome == "" {
		return nil, nil
	}
	var items []*Item
	for _, d := range []string{"downloads", "tmp"} {
		p := filepath.Join(t.rustupHome, d)
		if !hasEntries(p) {
			continue
		}
		it := newGlobal(CatCache, "rustup-downloads", "rustup "+d, "rustup", p, nil, "")
		it.Explain = "Partial or leftover files from rustup installs and updates. rustup normally empties this folder when an install finishes."
		it.Regenerate = "rustup downloads again what it needs"
		items = append(items, it)
	}
	return items, nil
}

// ── cargo install ────────────────────────────────────────────────────────

// installs lists the packages cargo install (and cargo binstall, which
// keeps the same records) put in $CARGO_HOME/bin, read from .crates2.json.
func installs(t *tools) ([]*Item, []string) {
	if t.cargoHome == "" {
		return nil, nil
	}
	rec := filepath.Join(t.cargoHome, ".crates2.json")
	b, err := readSmall(rec, 16<<20)
	if err != nil {
		return nil, nil
	}
	var doc struct {
		Installs map[string]struct {
			Bins []string `json:"bins"`
		} `json:"installs"`
	}
	if json.Unmarshal(b, &doc) != nil || doc.Installs == nil {
		return nil, []string{"could not read Cargo's install records (.crates2.json); installed tools are not listed"}
	}
	exe := t.look("cargo")
	var items []*Item
	for key, in := range doc.Installs {
		f := strings.Fields(key)
		if len(f) < 2 || len(in.Bins) == 0 || !crateName.MatchString(f[0]) {
			continue
		}
		name, version := f[0], f[1]
		var bins []string
		var used time.Time
		for _, bin := range in.Bins {
			if t.env.GOOS == "windows" && !strings.HasSuffix(bin, ".exe") {
				bin += ".exe"
			}
			p := filepath.Join(t.cargoHome, "bin", bin)
			fi, err := os.Lstat(p)
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			bins = append(bins, p)
			if at := accessTime(fi); at.After(used) {
				used = at
			}
		}
		if len(bins) == 0 {
			continue
		}
		it := newGlobal(CatGlobal, "cargo-install", name+" "+version, "cargo", bins[0],
			[]string{"cargo", "uninstall", "--root", t.cargoHome, name}, exe)
		it.Files = bins
		it.Explain = "A command-line tool installed with cargo install (or cargo binstall): " + strings.Join(in.Bins, ", ") + ". Removing it removes the command."
		it.Regenerate = "cargo install " + name
		it.Safety.add(ReasonCatalogReview, Review, "a tool you may still use")
		if !used.IsZero() {
			it.LastUsed, it.LastUsedSource = used, "atime"
		}
		if strings.Contains(key, "(path+file://") {
			it.Note = "installed from a local folder"
		}
		items = append(items, it)
	}
	return items, nil
}

var crateName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ── versions ─────────────────────────────────────────────────────────────

// olderCargo names an installed toolchain whose Cargo is older than
// major.minor; known is false when some version could not be read.
func (t *tools) olderCargo(major, minor int) (name string, known bool) {
	t.cargoVersMu.Do(func() { t.readCargoVersions() })
	for i, v := range t.cargoVers {
		if v[0] < major || (v[0] == major && v[1] < minor) {
			return t.cargoVersName(i), t.cargoKnown
		}
	}
	return "", t.cargoKnown
}

func (t *tools) cargoVersName(i int) string {
	names := t.toolchainDirs()
	if i < len(names) {
		return names[i]
	}
	return "cargo on PATH"
}

// readCargoVersions reads the Rust release of every toolchain from
// rustup's files, never by running it (running rustc or cargo would move
// the access time rustkill reads as "last used"); without rustup, it asks
// the cargo on PATH.
func (t *tools) readCargoVersions() {
	t.cargoKnown = true
	dirs := t.toolchainDirs()
	for _, d := range dirs {
		v := toolchainVersion(filepath.Join(t.rustupHome, "toolchains", d), d)
		if v == [3]int{} {
			t.cargoKnown = false
		}
		t.cargoVers = append(t.cargoVers, v)
	}
	if len(dirs) == 0 {
		if exe := t.look("cargo"); exe != "" {
			v := parseVersion(t.query(exe, "--version"))
			t.cargoKnown = v != [3]int{}
			t.cargoVers = append(t.cargoVers, v)
		}
	}
}

// toolchainDirs are the real toolchain folders rustup installed.
func (t *tools) toolchainDirs() []string {
	if t.rustupHome == "" {
		return nil
	}
	return readDirs(filepath.Join(t.rustupHome, "toolchains"))
}

func (t *tools) query(exe string, args ...string) string {
	if !regularFile(exe) {
		return ""
	}
	b, err := t.env.Query(t.env.Home, exe, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// parseVersion reads "cargo 1.99.1 (…)" into numbers; an unreadable
// version is all zero.
func parseVersion(s string) [3]int {
	var v [3]int
	m := versionNumber.FindStringSubmatch(s)
	for i := 1; m != nil && i < len(m); i++ {
		v[i-1], _ = strconv.Atoi(m[i])
	}
	return v
}

var versionNumber = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)
