//go:build !unix

package size

import "io/fs"

// diskUsage falls back to apparent sizes; hard links are not detected.
func diskUsage(info fs.FileInfo) (int64, fileID, uint64) {
	return info.Size(), fileID{}, 1
}

// Identity is unknown here; fingerprints then rely on paths and times.
func Identity(fs.FileInfo) (dev, ino uint64) { return 0, 0 }
