//go:build !unix

package scan

// lockHeld cannot ask for advisory locks here; running builds are found
// through the process list instead.
func lockHeld(string) bool { return false }
