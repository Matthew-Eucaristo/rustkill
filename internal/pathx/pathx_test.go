package pathx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWithin(t *testing.T) {
	if !Within("/a/b", "/a/b/c") || Within("/a/b", "/a/bc") || Within("/a/b", "/a/b") || !SameOrWithin("/a/b", "/a/b") {
		t.Error("Within basics")
	}
	if runtime.GOOS == "darwin" && !Within("/Users/x/WTS", "/Users/x/wts/wt") {
		t.Error("darwin folds case")
	}
}

func TestRealMissing(t *testing.T) {
	d := t.TempDir()
	real, _ := filepath.EvalSymlinks(d)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(d, link); err != nil {
		t.Skip(err)
	}
	if got := Real(filepath.Join(link, "gone", "x")); got != filepath.Join(real, "gone", "x") {
		t.Errorf("Real = %s", got)
	}
}
