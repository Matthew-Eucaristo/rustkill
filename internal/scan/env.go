package scan

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
)

// Env is how a scan sees the machine. Tests replace it.
type Env struct {
	Home     string
	GOOS     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// Query runs a read-only command (a tool's "where is your cache" or
	// "list" query) and returns its stdout.
	Query func(dir, name string, args ...string) ([]byte, error)
}

// Snapshot reads the running processes. Tests on systems that cannot see
// process working directories (Windows) replace it.
var Snapshot = procs.Snapshot

// DefaultEnv is the real machine.
func DefaultEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Home: home, GOOS: runtime.GOOS, Getenv: os.Getenv, LookPath: exec.LookPath, Query: query}
}

// query runs a tool with a timeout, no stdin, and settings that keep it
// from prompting, updating itself or phoning home.
func query(dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), QuietEnv...)
	return cmd.Output()
}

// QuietEnv is added to every tool rustkill runs, for queries and cleanups
// alike: no colour, progress bars or prompts; rustup never installs a
// missing toolchain on the way (RUSTUP_AUTO_INSTALL), and cargo stays
// offline (`cargo uninstall` needs no network).
var QuietEnv = []string{
	"NO_COLOR=1", "CARGO_TERM_COLOR=never", "CARGO_TERM_PROGRESS_WHEN=never", "CARGO_NET_OFFLINE=true",
	"RUSTUP_AUTO_INSTALL=0", "RUSTUP_TERM_COLOR=never",
}
