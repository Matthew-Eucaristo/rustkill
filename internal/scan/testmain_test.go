package scan

import (
	"os"
	"runtime"
	"testing"

	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
)

// Windows cannot report process working directories, so every project
// would be "review" there. Tests of the other evidence pretend nothing runs.
func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		Snapshot = func() *procs.Table { return &procs.Table{CwdKnown: true} }
	}
	os.Exit(m.Run())
}
