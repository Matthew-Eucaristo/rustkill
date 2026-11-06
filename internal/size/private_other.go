//go:build !darwin

package size

// privateSize is only known on APFS; elsewhere reflinked (Btrfs, XFS)
// extents count in full and freed space is measured after acting.
func privateSize(string) (int64, bool) { return 0, false }
