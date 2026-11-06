//go:build !darwin && !linux

package scan

import (
	"io/fs"
	"time"
)

// accessTime is unknown here: Windows usually does not keep last-access
// times on large volumes, so items show their install time instead.
func accessTime(fs.FileInfo) time.Time { return time.Time{} }
