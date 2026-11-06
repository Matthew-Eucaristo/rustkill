package scan

import (
	"io/fs"
	"syscall"
)

// sfDataless is SF_DATALESS from <sys/stat.h>: the file's contents live in
// the cloud (iCloud Drive, File Provider) and reading them downloads them.
const sfDataless = 0x40000000

func dataless(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Flags&sfDataless != 0
}
