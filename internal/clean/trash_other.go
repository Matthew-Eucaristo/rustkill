//go:build !darwin && !linux && !(windows && (amd64 || arm64))

package clean

import "errors"

func moveToTrash(string) error {
	return errors.New("moving to the trash is not supported on this OS; use --permanent")
}
