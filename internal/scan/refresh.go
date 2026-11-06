package scan

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
)

var (
	procsMu   sync.Mutex
	procsAt   time.Time
	procsLast *procs.Table
)

// freshProcs is a process snapshot at most a few seconds old, so a batch
// of actions does not run lsof once per item.
func freshProcs() *procs.Table {
	procsMu.Lock()
	defer procsMu.Unlock()
	if procsLast == nil || time.Since(procsAt) > 3*time.Second {
		procsLast, procsAt = Snapshot(), time.Now()
	}
	return procsLast
}

// Refresh re-reads one item from disk, measures it and classifies it again,
// so an action decides on current evidence. It returns an error when the
// item no longer exists where the scan saw it.
func Refresh(old *Item, opts Options) (*Item, error) {
	var fresh *Item
	if old.Category == CatProject {
		fresh = refreshBuild(old)
	} else {
		items, _ := globals(opts.Env, freshProcs())
		for _, it := range items {
			if it.ID == old.ID {
				fresh = it
			}
		}
	}
	if fresh == nil && old.Kind == "shared-target" {
		fresh = refreshShared(old)
	}
	if fresh == nil {
		return nil, errors.New("not found any more")
	}
	if fresh.Path != "" {
		fresh.Apply(Measure(fresh))
	}
	return fresh, nil
}

// refreshBuild re-reads a project's build directory: the project's
// Cargo.toml must still be there and the folder must still be cargo's.
func refreshBuild(old *Item) *Item {
	if old.Project == nil {
		return nil
	}
	entries, err := os.ReadDir(old.Project.Path)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["Cargo.toml"] {
		return nil
	}
	p := newProject(old.Project.Path, names)
	p.Repo, p.Worktree = findRepo(p.Path)
	p.newest = old.Project.newest
	bnames := map[string]bool{}
	bentries, err := os.ReadDir(old.Path)
	if err != nil {
		return nil
	}
	for _, e := range bentries {
		bnames[e.Name()] = true
	}
	if !bnames["CACHEDIR.TAG"] || !cargoOutput(old.Path, bnames) {
		return nil
	}
	b := &buildDir{Path: old.Path, Owner: p, Repo: p.Repo, Worktree: p.Worktree}
	for _, it := range evaluate([]*Project{p}, []*buildDir{b}, freshProcs(), time.Now()) {
		return it
	}
	return nil
}

// refreshShared re-reads a shared build directory found under a root.
func refreshShared(old *Item) *Item {
	fi, err := os.Lstat(old.Path)
	if err != nil || !fi.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(old.Path)
	if err != nil {
		return nil
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["CACHEDIR.TAG"] || !cargoOutput(old.Path, names) {
		return nil
	}
	repo, wt := findRepo(old.Path)
	it := buildItem(&buildDir{Path: old.Path, Repo: repo, Worktree: wt}, fi)
	gitEvidence([]*Item{it})
	it.classify(time.Now())
	return it
}

// findRepo walks up to the directory holding .git.
func findRepo(dir string) (string, bool) {
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d, linkedWorktree(filepath.Join(d, ".git"))
		}
		if filepath.Dir(d) == d {
			return "", false
		}
	}
}
