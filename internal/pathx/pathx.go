// Package pathx compares paths the way the filesystem does. macOS volumes
// are case-insensitive by default, and git, lsof and the user may spell
// the same directory differently, so on darwin comparisons fold case. That
// errs towards "same path", which makes in-use and nesting checks stricter,
// never looser.
package pathx

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var fold = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

func norm(p string) string {
	p = filepath.Clean(p)
	if fold {
		return strings.ToLower(p)
	}
	return p
}

// Same reports whether a and b name the same path.
func Same(a, b string) bool { return norm(a) == norm(b) }

// Within reports whether path is strictly inside dir.
func Within(dir, path string) bool {
	d, p := norm(dir), norm(path)
	if d == string(filepath.Separator) {
		return p != d && strings.HasPrefix(p, d)
	}
	return strings.HasPrefix(p, d+string(filepath.Separator))
}

// SameOrWithin reports whether path is dir or inside it.
func SameOrWithin(dir, path string) bool { return Same(dir, path) || Within(dir, path) }

// Real resolves symlinks. For a path that does not exist (a worktree whose
// checkout is gone) it resolves the deepest existing parent and keeps the
// rest, so /tmp/x and /private/tmp/x still compare equal.
func Real(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	var rest []string
	for cur := p; ; {
		parent := filepath.Dir(cur)
		rest = append([]string{filepath.Base(cur)}, rest...)
		if parent == cur {
			return p
		}
		if _, err := os.Lstat(parent); err == nil {
			r, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return p
			}
			return filepath.Join(append([]string{r}, rest...)...)
		}
		cur = parent
	}
}
