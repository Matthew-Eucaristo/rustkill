package scan

import (
	"io/fs"
	"syscall"
	"time"
)

// accessTime is when the file was last read or run. On APFS executing a
// file updates it, plain reads only while it is older than the last change,
// so it is a hint for "last run", never a guarantee.
func accessTime(fi fs.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec)
	}
	return time.Time{}
}
