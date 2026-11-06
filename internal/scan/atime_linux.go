package scan

import (
	"io/fs"
	"syscall"
	"time"
)

// accessTime is when the file was last read or run; with the default
// relatime mount option it is accurate to about a day.
func accessTime(fi fs.FileInfo) time.Time {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Atim.Sec, st.Atim.Nsec)
	}
	return time.Time{}
}
