package clean

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// moveToTrash uses /usr/bin/trash (macOS 15+), else asks Finder, so items can
// be restored with "Put Back".
func moveToTrash(path string) error {
	var cmd *exec.Cmd
	if _, err := os.Stat("/usr/bin/trash"); err == nil {
		cmd = exec.Command("/usr/bin/trash", "-s", path)
	} else {
		script := `on run argv
tell application "Finder" to delete (POSIX file (item 1 of argv) as alias)
end run`
		cmd = exec.Command("/usr/bin/osascript", "-e", script, path)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("move to trash: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
