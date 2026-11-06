package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Matthew-Eucaristo/rustkill/internal/pathx"
	"github.com/Matthew-Eucaristo/rustkill/internal/size"
)

// walker finds projects (directories with a Cargo.toml) and Cargo build
// directories below the roots. It never follows symlinks, stays on each
// root's filesystem, and never enters build directories, dependency folders,
// other tools' homes, app bundles, cloud-only placeholders or the
// privacy-protected home folders (unless a root is inside one).
type walker struct {
	skip    map[string]bool // normalised absolute paths never entered
	private map[string]bool // home folders that trigger macOS privacy prompts
	sem     chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	found   []*Project
	builds  []*buildDir
	dirs    int
	denied  []string // folders that could not be listed
}

func newWalker(home string) *walker {
	w := &walker{skip: map[string]bool{}, private: map[string]bool{}, sem: make(chan struct{}, max(8, 2*runtime.NumCPU()))}
	var skip []string
	if home != "" {
		for _, rel := range []string{
			"Library", ".Trash", ".npm", ".nvm", ".fnm", ".volta", ".bun", ".deno", ".yarn", ".pnpm-store", ".cache",
			".config", ".local", ".vscode", ".vscode-server", ".cursor", ".windsurf", ".gradle", ".m2", ".cargo",
			".rustup", ".pub-cache", ".android", ".docker", ".orbstack", "go/pkg", ".asdf", ".n", ".node-gyp",
			".sccache", ".solana", ".foundry", ".svm", "AppData", "Applications",
		} {
			skip = append(skip, filepath.Join(home, rel))
		}
		for _, rel := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Movies", "Music", "Public"} {
			w.private[norm(filepath.Join(home, rel))] = true
		}
	}
	skip = append(skip, "/Applications", "/System", "/Library", "/opt/homebrew", "/usr", "/private", "/proc", "/sys", "/dev", "/nix",
		`C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`)
	for _, p := range skip {
		w.skip[norm(p)] = true
	}
	return w
}

func norm(p string) string { return strings.ToLower(filepath.Clean(p)) }

// walk visits root and everything below it.
func (w *walker) walk(root string) {
	fi, err := os.Lstat(root)
	if err != nil {
		return
	}
	dev, _ := size.Identity(fi)
	// A root inside a protected folder was chosen on purpose.
	for p := range w.private {
		if pathx.SameOrWithin(p, root) {
			delete(w.private, p)
		}
	}
	w.wg.Add(1)
	w.visit(root, dev, "", false, nil)
	w.wg.Wait()
}

func (w *walker) visit(dir string, dev uint64, repo string, worktree bool, owner *Project) {
	defer w.wg.Done()
	f, err := os.Open(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			w.mu.Lock()
			w.denied = append(w.denied, dir)
			w.mu.Unlock()
		}
		return
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		w.mu.Lock()
		w.denied = append(w.denied, dir)
		w.mu.Unlock()
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	w.mu.Lock()
	w.dirs++
	w.mu.Unlock()

	if names[".git"] {
		repo, worktree = dir, linkedWorktree(filepath.Join(dir, ".git"))
	}
	if names["CACHEDIR.TAG"] && cargoOutput(dir, names) {
		w.mu.Lock()
		w.builds = append(w.builds, &buildDir{Path: dir, Owner: owner, Repo: repo, Worktree: worktree && repo != ""})
		w.mu.Unlock()
		return // cargo's own output: nothing to find inside
	}
	if fi, err := os.Lstat(filepath.Join(dir, "Cargo.toml")); err == nil && fi.Mode().IsRegular() {
		p := newProject(dir, names)
		p.Repo, p.Worktree = repo, worktree && repo != ""
		w.mu.Lock()
		w.found = append(w.found, p)
		w.mu.Unlock()
		owner = p
	}

	var newest time.Time
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		if !e.IsDir() {
			if owner != nil && e.Type().IsRegular() {
				if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
					newest = info.ModTime()
				}
			}
			continue
		}
		if noDescend[name] || strings.HasSuffix(name, ".app") || w.skip[norm(path)] || w.private[norm(path)] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if d, _ := size.Identity(info); d != dev && d != 0 {
			continue // another filesystem
		}
		if dataless(info) {
			continue // iCloud or File Provider placeholder: reading would download it
		}
		w.wg.Add(1)
		select {
		case w.sem <- struct{}{}:
			go func() {
				defer func() { <-w.sem }()
				w.visit(path, dev, repo, worktree, owner)
			}()
		default:
			w.visit(path, dev, repo, worktree, owner)
		}
	}
	if owner != nil && !newest.IsZero() {
		w.mu.Lock()
		if newest.After(owner.newest) {
			owner.newest = newest
		}
		w.mu.Unlock()
	}
}

// buildDir is a directory cargo writes its build output to (a target or
// build directory), found by the tag cargo puts in it.
type buildDir struct {
	Path     string
	Owner    *Project // nearest project above it; nil for a shared directory
	Repo     string
	Worktree bool
}

// cargoTag is the start of the CACHEDIR.TAG cargo writes into every target
// and build directory (and its registry cache): the standard signature
// followed by cargo's own comment.
const cargoTag = "Signature: 8a477f597d28d172789f06886806bc55\n# This file is a cache directory tag created by cargo."

// buildLocks are the lock files cargo keeps in each profile directory.
var buildLocks = []string{".cargo-lock", ".cargo-build-lock", ".cargo-artifact-lock"}

// cargoOutput: dir carries cargo's CACHEDIR.TAG and the layout of a build
// directory (rustc's version cache, or a profile directory with cargo's
// locks or fingerprints). Cargo's registry cache has the tag but not the
// layout, so it never counts.
func cargoOutput(dir string, names map[string]bool) bool {
	b, err := readSmall(filepath.Join(dir, "CACHEDIR.TAG"), 4096)
	if err != nil || !strings.HasPrefix(strings.ReplaceAll(string(b), "\r\n", "\n"), cargoTag) {
		return false
	}
	if names[".rustc_info.json"] {
		return true
	}
	return len(profileDirs(dir)) > 0
}

// profileDirs are the directories of a build directory that hold cargo's
// locks or fingerprints: <dir>/<profile> and <dir>/<triple>/<profile>.
func profileDirs(dir string) []string {
	var out []string
	marked := func(d string) bool {
		for _, n := range append([]string{".fingerprint"}, buildLocks...) {
			if _, err := os.Lstat(filepath.Join(d, n)); err == nil {
				return true
			}
		}
		return false
	}
	for _, e := range readDirs(dir) {
		d := filepath.Join(dir, e)
		if marked(d) {
			out = append(out, d)
			continue
		}
		for _, e2 := range readDirs(d) {
			if d2 := filepath.Join(d, e2); marked(d2) {
				out = append(out, d2)
			}
		}
	}
	return out
}

// readDirs lists the real (non-symlink) subdirectories of dir.
func readDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Type()&fs.ModeSymlink == 0 {
			out = append(out, e.Name())
		}
	}
	return out
}

// readSmall reads a file of at most limit bytes, refusing symlinks.
func readSmall(p string, limit int64) ([]byte, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > limit {
		return nil, &fs.PathError{Op: "read", Path: p, Err: errNotDir}
	}
	return os.ReadFile(p)
}

// linkedWorktree: a .git file pointing into <common>/worktrees/<name>.
func linkedWorktree(dotgit string) bool {
	fi, err := os.Lstat(dotgit)
	if err != nil || fi.IsDir() {
		return false
	}
	b, err := os.ReadFile(dotgit)
	if err != nil || len(b) > 4096 {
		return false
	}
	gd, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	return ok && filepath.Base(filepath.Dir(strings.TrimSpace(gd))) == "worktrees"
}

// IsCargoOutput re-reads a folder and reports whether it is still a cargo
// build directory, for the check right before removal.
func IsCargoOutput(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names["CACHEDIR.TAG"] && cargoOutput(dir, names)
}
