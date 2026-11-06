// Package gitx runs read-only git queries. rustkill never changes a
// repository: no optional locks, no index refresh, no fsmonitor hooks, and
// no inherited GIT_DIR or GIT_WORK_TREE that could point a query elsewhere.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func env() []string {
	var out []string
	for _, e := range os.Environ() {
		if k, _, _ := strings.Cut(e, "="); strings.HasPrefix(k, "GIT_") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
}

var base = []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotepath=off"}

// Read runs a query in dir and returns stdout. A non-zero exit is an error.
func Read(dir string, args ...string) ([]byte, error) {
	out, code, err := run(dir, "", args...)
	if err == nil && code != 0 {
		err = fmt.Errorf("git %s: exit %d", strings.Join(args, " "), code)
	}
	return out, err
}

// ReadIn runs a query with stdin and returns stdout and the exit code, for
// commands like `check-ignore` whose exit code 1 means "no match".
func ReadIn(dir, stdin string, args ...string) ([]byte, int, error) {
	return run(dir, stdin, args...)
}

func run(dir, stdin string, args ...string) ([]byte, int, error) {
	full := append(append([]string{}, base...), "-C", dir)
	cmd := exec.Command("git", append(full, args...)...)
	cmd.Env = env()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if code == 1 {
			return out, 1, nil
		}
		return out, code, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return out, -1, err
	}
	return out, 0, nil
}

// Available reports whether git is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
