package clean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Matthew-Eucaristo/rustkill/internal/pathx"
	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
)

// These checks run on freshly read evidence immediately before every
// removal, independently of how the item was chosen, so a stale or
// tampered plan cannot widen what is removed.

// officialCommands are the only commands rustkill runs to clean up: the
// executable's name and the subcommand that must follow it.
var officialCommands = map[string][][]string{
	"rustup": {{"toolchain", "uninstall"}, {"component", "remove"}, {"target", "remove"}},
	"cargo":  {{"uninstall"}},
}

// Check validates a freshly scanned item right before acting on it.
func Check(it *scan.Item) error {
	if len(it.Command) > 0 {
		return checkCommand(it)
	}
	if err := checkPath(it.Path); err != nil {
		return err
	}
	switch it.Kind {
	case "target", "shared-target":
		return checkBuildDir(it)
	}
	home, _ := os.UserHomeDir()
	if !scan.DeepInHome(it.Path, home) {
		return fmt.Errorf("refusing: %s is not a folder well inside your home", it.Path)
	}
	return nil
}

// checkPath refuses relative, broad or symlinked targets.
func checkPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("refusing %q: not a clean absolute path", path)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink %s", path)
	}
	if !fi.IsDir() {
		return fmt.Errorf("refusing: %s is not a directory", path)
	}
	home, _ := os.UserHomeDir()
	parent := filepath.Dir(path)
	if parent == path || filepath.Dir(parent) == parent || (home != "" && pathx.SameOrWithin(path, home)) {
		return fmt.Errorf("refusing broad path %s", path)
	}
	return nil
}

// checkBuildDir: the folder still carries cargo's tag and build layout,
// holds no repository, and a project's build directory still sits inside
// a project with a Cargo.toml.
func checkBuildDir(it *scan.Item) error {
	if !scan.IsCargoOutput(it.Path) {
		return fmt.Errorf("refusing: %s is no longer recognisably cargo's build output", it.Path)
	}
	if it.NestedRepo {
		return fmt.Errorf("%s contains a repository; remove it by hand", it.Path)
	}
	if it.Kind == "shared-target" {
		return nil
	}
	p := it.Project
	if p == nil {
		return errors.New("not a project's build directory")
	}
	if !pathx.Within(p.Path, it.Path) {
		return fmt.Errorf("%s is not inside %s", it.Path, p.Path)
	}
	if fi, err := os.Lstat(filepath.Join(p.Path, "Cargo.toml")); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s has no Cargo.toml any more", p.Path)
	}
	return nil
}

// checkCommand: the executable is rustup or cargo, resolved to an absolute
// path, running one of the subcommands rustkill's catalog uses.
func checkCommand(it *scan.Item) error {
	exe := it.Command[0]
	if !filepath.IsAbs(exe) {
		return fmt.Errorf("refusing %q: not an absolute executable path", exe)
	}
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), ".exe")
	subs, ok := officialCommands[base]
	if !ok {
		return fmt.Errorf("refusing to run %s: not rustup or cargo", exe)
	}
	if !slices.ContainsFunc(subs, func(s []string) bool {
		return len(it.Command) > len(s) && slices.Equal(it.Command[1:1+len(s)], s)
	}) {
		return fmt.Errorf("refusing: %s is not a cleanup command rustkill knows", strings.Join(it.Command, " "))
	}
	if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
		return fmt.Errorf("%s is not runnable", exe)
	}
	return nil
}
