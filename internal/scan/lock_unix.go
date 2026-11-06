//go:build unix

package scan

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockHeld reports whether another process holds an exclusive flock on p,
// the way cargo locks a build directory while it builds. It asks for a
// shared lock without waiting and releases it at once, so a cargo that
// starts meanwhile waits a moment at most. It never creates the file.
func lockHeld(p string) bool {
	f, err := os.OpenFile(p, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		return err == unix.EWOULDBLOCK
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false
}
