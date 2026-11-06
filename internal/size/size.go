// Package size measures directory trees the way removal would free them:
// allocated blocks like `du`, symlinks never followed, each hard-linked file
// counted once, and bytes shared with something outside the tree (a pnpm
// store's hard links, APFS clones from a pnpm or Bun store) reported apart.
package size

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Size is disk usage. Shared is the part of Allocated that removing the tree
// would not free: hard-linked files with links outside it and, on APFS,
// blocks shared with clones or snapshots. Exclusive is therefore a lower
// bound on macOS and an upper bound on filesystems whose reflinks cannot be
// seen (Btrfs, XFS); freed space is measured after acting, never assumed.
type Size struct {
	Allocated int64 `json:"allocated"`
	Shared    int64 `json:"shared"`
	Files     int64 `json:"file_count"`
}

// Exclusive is what removing the tree can free right away.
func (s Size) Exclusive() int64 { return s.Allocated - s.Shared }

// Add sums two sizes.
func (s Size) Add(o Size) Size {
	return Size{s.Allocated + o.Allocated, s.Shared + o.Shared, s.Files + o.Files}
}

// Result is one measured tree.
type Result struct {
	Size Size
	// Newest is the newest modification time of a regular file, ignoring
	// entries Options.SkipNewest rejects.
	Newest time.Time
	// Repo is set when a .git lies below the root: someone's checkout, not
	// a generated directory.
	Repo bool
	// Unreadable counts directories that could not be listed (permissions),
	// so Size and Newest are incomplete when it is not zero.
	Unreadable int64
}

// Options tune a measurement.
type Options struct {
	// SkipNewest, given a slash-separated path relative to the root, says
	// whether that file (or everything in that directory) is left out of
	// Newest.
	SkipNewest func(rel string, dir bool) bool
	Workers    int // parallel directory readers; 0 = a default
}

type fileID struct{ dev, ino uint64 }

type link struct{ nlink, seen uint64 }

type walker struct {
	root    string
	dev     uint64
	opts    Options
	sem     chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	links   map[fileID]*link
	linkLen map[fileID]int64
	alloc   atomic.Int64
	shared  atomic.Int64
	files   atomic.Int64
	newest  atomic.Int64
	repo    atomic.Bool
	unread  atomic.Int64
}

// Measure walks root. It never follows symlinks and stays on root's device.
// Unreadable directories are counted in Result.Unreadable, not fatal.
func Measure(root string, opts Options) Result {
	if opts.Workers <= 0 {
		opts.Workers = max(4, runtime.NumCPU())
	}
	w := &walker{root: root, opts: opts, sem: make(chan struct{}, opts.Workers), links: map[fileID]*link{}, linkLen: map[fileID]int64{}}
	fi, err := os.Lstat(root)
	if err != nil {
		return Result{}
	}
	n, id, _ := diskUsage(fi)
	w.dev = id.dev
	if !fi.IsDir() {
		w.file(root, fi, n, false)
	} else {
		w.alloc.Add(n)
		w.dir(root, "", false)
	}
	w.wg.Wait()
	for id, l := range w.links {
		if l.seen < l.nlink {
			w.shared.Add(w.linkLen[id])
		}
	}
	r := Result{Size: Size{w.alloc.Load(), w.shared.Load(), w.files.Load()}, Repo: w.repo.Load(), Unreadable: w.unread.Load()}
	if ns := w.newest.Load(); ns > 0 {
		r.Newest = time.Unix(0, ns)
	}
	return r
}

// dir reads one directory; rel is its path below the root and skip means
// its files do not count for Newest.
func (w *walker) dir(path, rel string, skip bool) {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			w.unread.Add(1)
		}
		return
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		w.unread.Add(1)
	}
	for _, e := range entries {
		p := filepath.Join(path, e.Name())
		r := e.Name()
		if rel != "" {
			r = rel + "/" + r
		}
		if e.Name() == ".git" {
			w.repo.Store(true)
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		n, id, _ := diskUsage(info)
		skip := skip || (w.opts.SkipNewest != nil && w.opts.SkipNewest(r, info.IsDir()))
		if info.IsDir() {
			if id.dev != w.dev && id.dev != 0 {
				continue // another filesystem mounted inside
			}
			w.alloc.Add(n)
			select {
			case w.sem <- struct{}{}:
				w.wg.Add(1)
				go func() {
					defer func() { <-w.sem; w.wg.Done() }()
					w.dir(p, r, skip)
				}()
			default:
				w.dir(p, r, skip)
			}
			continue
		}
		w.file(p, info, n, skip)
	}
}

func (w *walker) file(p string, info fs.FileInfo, n int64, skipNewest bool) {
	_, id, nlink := diskUsage(info)
	if nlink > 1 {
		w.mu.Lock()
		l := w.links[id]
		if l != nil {
			l.seen++
			w.mu.Unlock()
			return
		}
		w.links[id] = &link{nlink: nlink, seen: 1}
		w.linkLen[id] = n
		w.mu.Unlock()
	}
	w.alloc.Add(n)
	w.files.Add(1)
	if !info.Mode().IsRegular() {
		return
	}
	if !skipNewest {
		ns := info.ModTime().UnixNano()
		for {
			cur := w.newest.Load()
			if ns <= cur || w.newest.CompareAndSwap(cur, ns) {
				break
			}
		}
	}
	if nlink <= 1 && n > 0 {
		if private, ok := privateSize(p); ok && private < n {
			w.shared.Add(n - private)
		}
	}
}
