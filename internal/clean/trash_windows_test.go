//go:build windows && (amd64 || arm64)

package clean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveToTrashRecycleBin(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj", "build")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)

	if err := moveToTrash(dir); err != nil {
		if strings.Contains(err.Error(), "fixed drives") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
	if _, err := os.Lstat(filepath.Dir(dir)); err != nil {
		t.Fatalf("parent removed: %v", err)
	}
}

func TestMoveToTrashRefusesLongPaths(t *testing.T) {
	if err := moveToTrash(`C:\` + strings.Repeat("a", 300)); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v", err)
	}
}
