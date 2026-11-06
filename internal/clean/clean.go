// Package clean is the only code in rustkill that removes anything. Every
// action re-reads its item, refuses it if anything safety-relevant changed
// since the scan, and passes the guards in guard.go first.
package clean

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
)

// TrashName is what the OS calls its trash, for messages.
var TrashName = "Trash"

// moveFunc moves a path to the trash; tests replace it.
var moveFunc = moveToTrash

// Mode selects what removing a directory means. Official commands are run
// in Trash and Permanent mode alike: the tool deletes by its own design.
type Mode int

const (
	Trash     Mode = iota // move to the OS trash (default, recoverable)
	Permanent             // delete directories instead
	DryRun                // check everything, change nothing
)

func (m Mode) String() string { return [...]string{"trash", "permanent", "dry-run"}[m] }

// Status of one action.
type Status string

const (
	StatusDone    Status = "done"
	StatusDryRun  Status = "dry_run"
	StatusRefused Status = "refused" // rules said no, or the item changed; nothing was touched
	StatusFailed  Status = "failed"  // an error after the checks passed
)

// Outcome is the result of acting on one item.
type Outcome struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Action   string   `json:"action"` // trash, delete, command
	Path     string   `json:"path"`
	Status   Status   `json:"status"`
	Command  []string `json:"command,omitempty"`
	Reasons  []string `json:"reasons,omitempty"`
	Error    string   `json:"error,omitempty"`
	Estimate int64    `json:"estimated_bytes"`
	// Freed is the measured change in free space on the item's filesystem
	// (-1 unknown). After a move to the trash it stays near zero until the
	// trash is emptied.
	Freed       int64  `json:"freed_bytes_measured"`
	RestoreHint string `json:"restore_hint,omitempty"`
}

// Options for acting.
type Options struct {
	Mode Mode
	// Review allows items at review level. Interactive selection and
	// explicit IDs set it; `--yes` alone does not.
	Review bool
	Scan   scan.Options
}

// Act removes one item the way its kind requires: an official command, or
// a move to the trash (or a deletion with --permanent).
func Act(it *scan.Item, fingerprint string, o Options) *Outcome {
	out := &Outcome{ID: it.ID, Kind: it.Kind, Path: it.Path, Action: action(it, o.Mode), Command: it.Command, Freed: -1}
	fresh, err := scan.Refresh(it, o.Scan)
	if err != nil {
		return refuse(out, nil, "could not re-check it: "+err.Error())
	}
	if fingerprint == "" {
		fingerprint = it.Fingerprint
	}
	if !fresh.Safety.Level.Allowed() || (fresh.Safety.Level == scan.Review && !o.Review) {
		var codes, details []string
		for _, r := range fresh.Safety.Reasons {
			if r.Level != scan.Safe && (r.Level != scan.Review || !o.Review) {
				codes, details = append(codes, r.Code), append(details, r.Detail)
			}
		}
		return refuse(out, codes, strings.Join(details, "; "))
	}
	// Checked after the level, so a refusal names what blocks it now (a
	// build that started, a toolchain made the default) when it can.
	if fresh.Fingerprint != fingerprint {
		return refuse(out, nil, "it changed since the scan; rescan and try again")
	}
	// What removing it frees: shared blocks (store clones, hard links) stay.
	out.Estimate = fresh.Size.Exclusive()
	if err := Check(fresh); err != nil {
		return refuse(out, nil, err.Error())
	}
	if o.Mode == DryRun {
		out.Status = StatusDryRun
		return out
	}
	before := FreeBytes(volumeOf(fresh))
	if len(fresh.Command) > 0 {
		err = run(fresh)
	} else {
		err = remove(fresh.Path, o.Mode)
	}
	if after := FreeBytes(volumeOf(fresh)); before >= 0 && after >= 0 {
		out.Freed = after - before
	}
	if err != nil {
		out.Status, out.Error = StatusFailed, err.Error()
		return out
	}
	out.Status = StatusDone
	switch {
	case len(fresh.Command) > 0:
		out.RestoreHint = fresh.Regenerate
	case o.Mode == Trash:
		out.RestoreHint = "restore from the " + TrashName + ", or run: " + fresh.Regenerate
	default:
		out.RestoreHint = fresh.Regenerate
	}
	return out
}

func action(it *scan.Item, m Mode) string {
	switch {
	case len(it.Command) > 0:
		return "command"
	case m == Permanent:
		return "delete"
	}
	return "trash"
}

func refuse(o *Outcome, codes []string, msg string) *Outcome {
	o.Status, o.Reasons, o.Error = StatusRefused, codes, msg
	return o
}

// remove moves path to the trash, or deletes it in Permanent mode, and
// confirms it is gone. os.RemoveAll never follows symlinks, so workspace
// links inside node_modules cannot reach their targets.
func remove(path string, m Mode) error {
	if m == Permanent {
		return os.RemoveAll(path)
	}
	if err := moveFunc(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s still exists after moving it to the %s", path, TrashName)
	}
	return nil
}

// run executes an official command with a fixed argument list (never a
// shell), a timeout and the quiet environment.
func run(it *scan.Item) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, it.Command[0], it.Command[1:]...)
	cmd.Dir = it.CommandDir
	cmd.Env = append(os.Environ(), scan.QuietEnv...)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return fmt.Errorf("%s: %v: %s", strings.Join(it.Command, " "), err, msg)
	}
	return nil
}

// volumeOf is an existing directory on the item's filesystem, for statfs.
func volumeOf(it *scan.Item) string {
	for p := it.Path; p != ""; p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return string(filepath.Separator)
}

// FreeBytes is the space available on path's filesystem, or -1.
func FreeBytes(path string) int64 { return freeBytes(path) }
