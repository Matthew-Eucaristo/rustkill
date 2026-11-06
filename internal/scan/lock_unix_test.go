//go:build unix

package scan

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/Matthew-Eucaristo/rustkill/internal/testfix"
)

// TestBuildLockMakesTargetLive holds the lock the way cargo does while it
// builds, and checks the item turns live and stays live only while held.
func TestBuildLockMakesTargetLive(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	app := project(t, root, "app", nil)
	lock := filepath.Join(app, "target", "debug", ".cargo-lock")
	if lockHeld(lock) {
		t.Fatal("free lock reported held")
	}
	f, err := os.OpenFile(lock, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	res := scanFix(t, root)
	want(t, item(t, res, filepath.Join(app, "target")), Live, ReasonBuilding)
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	want(t, item(t, scanFix(t, root), filepath.Join(app, "target")), Safe)
}
