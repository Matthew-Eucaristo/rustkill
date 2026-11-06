package procs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromProcAndInside(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "wt")
	os.MkdirAll(filepath.Join(work, "sub"), 0o755)
	for pid, cwd := range map[string]string{"101": filepath.Join(work, "sub"), "102": root, "103": work + "-other"} {
		dir := filepath.Join(root, "proc", pid)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "comm"), []byte("node\n"), 0o644)
		os.Symlink(cwd, filepath.Join(dir, "cwd"))
	}
	tb := fromProc(filepath.Join(root, "proc"))
	in := tb.Inside(work)
	if len(in) != 1 || in[0].PID != 101 || in[0].Command != "node" {
		t.Fatalf("Inside = %+v", in)
	}
	if !tb.Running("node") || tb.Running("Claude") || !tb.Alive(102) || tb.Alive(9) {
		t.Error("Running/Alive wrong")
	}
}
