//go:build windows && (amd64 || arm64)

package clean

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() { TrashName = "Recycle Bin" }

var shFileOperation = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// shFileOpStruct is SHFILEOPSTRUCTW with 64-bit (natural) alignment.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete           = 0x3
	fofSilent          = 0x4
	fofNoConfirmation  = 0x10
	fofAllowUndo       = 0x40
	fofNoErrorUI       = 0x400
	fofWantNukeWarning = 0x4000 // ask before deleting anything that cannot be recycled
	fofNoConnected     = 0x2000 // never take "X_files" along with "X.htm"
	recycleOverhead    = 60     // C:\$Recycle.Bin\<SID>\$Rxxxxxx adds about this much to every path
	driveFixed         = 3
	maxPath            = 260
)

// moveToTrash sends path to the Recycle Bin, so it can be restored from there.
// Only fixed drives have a Recycle Bin; elsewhere use --permanent. If Windows
// would delete instead of recycling (e.g. the item is larger than the bin),
// it asks first.
func moveToTrash(path string) error {
	// Deep node_modules trees overflow MAX_PATH once moved under
	// C:\$Recycle.Bin, and the shell then fails half-way. Refuse up front.
	if n := longestPath(path); n+recycleOverhead >= maxPath {
		return fmt.Errorf("%s holds paths too long for the Recycle Bin (%d characters); use --permanent", path, n)
	}
	root, err := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	if err != nil {
		return err
	}
	if windows.GetDriveType(root) != driveFixed {
		return errors.New("the Recycle Bin is only available on fixed drives; use --permanent")
	}
	from, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0) // the list is double-NUL terminated
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI | fofWantNukeWarning | fofNoConnected,
	}
	r, _, _ := shFileOperation.Call(uintptr(unsafe.Pointer(&op)))
	runtime.KeepAlive(from)
	if r != 0 {
		return fmt.Errorf("move to Recycle Bin: error 0x%x", r)
	}
	if op.fAnyOperationsAborted != 0 {
		return errors.New("move to Recycle Bin was cancelled")
	}
	return nil
}

// longestPath is the length of the longest path at or below root.
func longestPath(root string) int {
	n := len(root)
	_ = filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err == nil && len(p) > n {
			n = len(p)
		}
		return nil
	})
	return n
}
