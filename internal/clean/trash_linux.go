package clean

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// moveToTrash implements the freedesktop.org Trash spec for the home trash:
// write Trash/info/<name>.trashinfo, then rename into Trash/files/<name>.
// Cross-device moves are refused rather than copied.
func moveToTrash(path string) error {
	trash := filepath.Join(dataHome(), "Trash")
	files, info := filepath.Join(trash, "files"), filepath.Join(trash, "info")
	for _, d := range []string{files, info} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}

	base := filepath.Base(path)
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = base + "." + strconv.Itoa(i)
		}
		dest := filepath.Join(files, name)
		if _, err := os.Lstat(dest); err == nil {
			continue // orphan in files/ without an info file
		}
		infoPath := filepath.Join(info, name+".trashinfo")
		f, err := os.OpenFile(infoPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		u := url.URL{Path: path}
		_, err = fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", u.EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(path, dest)
		}
		if err != nil {
			os.Remove(infoPath)
			if errors.Is(err, syscall.EEXIST) || errors.Is(err, syscall.ENOTEMPTY) {
				continue // dest appeared meanwhile; try the next name
			}
			if errors.Is(err, syscall.EXDEV) {
				return fmt.Errorf("%s is on another filesystem than the trash in %s; rustkill only moves within one filesystem", path, trash)
			}
			return err
		}
		return nil
	}
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// stagingParent is where rescue folders are assembled: next to the trash,
// so moving them in is a rename.
func stagingParent() string { return dataHome() }
