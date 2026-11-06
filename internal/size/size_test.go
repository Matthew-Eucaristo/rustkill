package size

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestHardLinksCountOnceAndOutsideLinksAreShared(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard links are not detected on Windows")
	}
	root := t.TempDir()
	tree := filepath.Join(root, "node_modules")
	os.MkdirAll(filepath.Join(tree, "a"), 0o755)
	big := make([]byte, 64*1024)
	os.WriteFile(filepath.Join(tree, "a", "own.js"), big, 0o644)
	os.WriteFile(filepath.Join(tree, "a", "twice.js"), big, 0o644)
	os.Link(filepath.Join(tree, "a", "twice.js"), filepath.Join(tree, "a", "twice-again.js"))
	store := filepath.Join(root, "store")
	os.MkdirAll(store, 0o755)
	os.WriteFile(filepath.Join(store, "pkg.js"), big, 0o644)
	os.Link(filepath.Join(store, "pkg.js"), filepath.Join(tree, "a", "from-store.js"))

	r := Measure(tree, Options{})
	if r.Size.Files != 3 {
		t.Errorf("files %d, want 3 (one hard link counted once)", r.Size.Files)
	}
	if r.Size.Shared < 64*1024 || r.Size.Exclusive() < 2*64*1024 {
		t.Errorf("size %+v: the store-linked file must be shared, the rest exclusive", r.Size)
	}
}

func TestNewestSkipsAndRepo(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".cache"), 0o755)
	os.MkdirAll(filepath.Join(root, "dep", ".git"), 0o755)
	old := time.Now().Add(-48 * time.Hour)
	os.WriteFile(filepath.Join(root, "dep", "index.js"), []byte("x"), 0o644)
	os.Chtimes(filepath.Join(root, "dep", "index.js"), old, old)
	os.WriteFile(filepath.Join(root, ".cache", "c"), []byte("x"), 0o644)
	r := Measure(root, Options{SkipNewest: func(rel string, _ bool) bool { return rel == ".cache" }})
	if !r.Repo {
		t.Error("nested .git not reported")
	}
	if r.Newest.After(old.Add(time.Second)) {
		t.Errorf("newest %v includes the skipped cache", r.Newest)
	}
}

func TestSymlinksNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(outside, "big"), make([]byte, 1<<20), 0o644)
	tree := filepath.Join(root, "tree")
	os.MkdirAll(tree, 0o755)
	os.Symlink(outside, filepath.Join(tree, "link"))
	if r := Measure(tree, Options{}); r.Size.Allocated > 512*1024 {
		t.Errorf("followed a symlink: %+v", r.Size)
	}
}
