//go:build !unix && !windows

package clean

func freeBytes(string) int64 { return -1 }
