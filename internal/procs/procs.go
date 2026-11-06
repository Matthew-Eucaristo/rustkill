// Package procs lists the user's running processes and their working
// directories, so nodekill can tell whether a project is in use. It only reads.
package procs

import (
	"bufio"
	"bytes"
	"github.com/Matthew-Eucaristo/rustkill/internal/pathx"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Proc is one running process.
type Proc struct {
	PID     int    `json:"pid"`
	Command string `json:"command"` // executable name, e.g. "zsh", "node", "Claude"
	Cwd     string `json:"-"`       // empty when unknown
}

// Table is a snapshot of processes, taken once per scan.
type Table struct {
	Procs []Proc
	// CwdKnown is false when working directories could not be read (no
	// lsof on macOS); callers then report liveness as unknown.
	CwdKnown bool
}

// Snapshot reads the process table. It never fails; missing data is
// reported through CwdKnown and empty fields.
func Snapshot() *Table {
	if runtime.GOOS == "linux" {
		return fromProc("/proc")
	}
	return fromPS()
}

// fromPS uses ps for names and lsof for working directories (macOS, BSD).
func fromPS() *Table {
	t := &Table{}
	byPID := map[int]int{}
	if out, err := exec.Command(tool("ps", "/bin/ps"), "-axo", "pid=,comm=").Output(); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) < 2 {
				continue
			}
			pid, err := strconv.Atoi(f[0])
			if err != nil {
				continue
			}
			byPID[pid] = len(t.Procs)
			t.Procs = append(t.Procs, Proc{PID: pid, Command: filepath.Base(strings.Join(f[1:], " "))})
		}
	}
	// -d cwd: only working directories; -F pn: machine-readable pid + name.
	// lsof exits 1 when some processes cannot be inspected, so use the
	// output whenever there is any.
	out, _ := exec.Command(tool("lsof", "/usr/sbin/lsof"), "-n", "-P", "-w", "-d", "cwd", "-F", "pn").Output()
	if len(out) == 0 {
		return t
	}
	t.CwdKnown = true
	pid := -1
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if i, ok := byPID[pid]; ok {
				t.Procs[i].Cwd = line[1:]
			} else if pid > 0 {
				byPID[pid] = len(t.Procs)
				t.Procs = append(t.Procs, Proc{PID: pid, Cwd: line[1:]})
			}
		}
	}
	return t
}

// fromProc reads /proc/<pid>/{comm,cwd}; other users' cwds are unreadable
// and skipped, which matches what lsof shows without root.
func fromProc(root string) *Table {
	t := &Table{CwdKnown: true}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.CwdKnown = false
		return t
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(root, e.Name())
		comm, _ := os.ReadFile(filepath.Join(dir, "comm"))
		cwd, _ := os.Readlink(filepath.Join(dir, "cwd"))
		t.Procs = append(t.Procs, Proc{PID: pid, Command: strings.TrimSpace(string(comm)), Cwd: cwd})
	}
	return t
}

// Inside returns the processes whose working directory is dir or below it.
// dir must be a real (symlink-resolved) path.
func (t *Table) Inside(dir string) []Proc {
	var out []Proc
	self := os.Getpid()
	for _, p := range t.Procs {
		if p.Cwd == "" || p.PID == self {
			continue
		}
		if pathx.SameOrWithin(dir, p.Cwd) {
			out = append(out, p)
		}
	}
	return out
}

// Running reports whether a process with one of these executable names is
// running. Names match case-insensitively: Linux builds of the same apps
// often use lowercase executables ("code", "cursor").
func (t *Table) Running(names ...string) bool {
	for _, p := range t.Procs {
		for _, n := range names {
			if strings.EqualFold(p.Command, n) {
				return true
			}
		}
	}
	return false
}

// Alive reports whether pid is in the snapshot.
func (t *Table) Alive(pid int) bool {
	for _, p := range t.Procs {
		if p.PID == pid {
			return true
		}
	}
	return false
}

// tool finds a system command on PATH, else at its usual location (lsof is
// in /usr/sbin on macOS, which minimal PATHs leave out).
func tool(name, fallback string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return fallback
}
