package clean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveToTrashFreedesktop(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "proj", "build") // same filesystem as the trash
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)

	if err := moveToTrash(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("source still exists")
	}
	if _, err := os.Stat(filepath.Join(data, "Trash", "files", "build", "f")); err != nil {
		t.Fatal(err)
	}
	info, err := os.ReadFile(filepath.Join(data, "Trash", "info", "build.trashinfo"))
	if err != nil || !strings.Contains(string(info), "Path="+dir) {
		t.Fatalf("trashinfo = %q, %v", info, err)
	}

	// A second item with the same name gets a unique slot.
	os.MkdirAll(dir, 0o755)
	if err := moveToTrash(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "Trash", "files", "build.1")); err != nil {
		t.Fatal(err)
	}

	// An orphan in files/ (no .trashinfo) is skipped, not overwritten.
	os.MkdirAll(filepath.Join(data, "Trash", "files", "build.2"), 0o755)
	os.MkdirAll(dir, 0o755)
	if err := moveToTrash(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "Trash", "info", "build.3.trashinfo")); err != nil {
		t.Fatal(err)
	}
}
