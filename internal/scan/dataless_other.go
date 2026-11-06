//go:build !darwin

package scan

import "io/fs"

func dataless(fs.FileInfo) bool { return false }
