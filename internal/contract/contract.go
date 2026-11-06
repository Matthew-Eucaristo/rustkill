// Package contract is rustkill's stable machine interface (schema_version
// 1): the scan document, capabilities, plans and apply results, in the same
// envelope as the other tools of the family. docs/contract.md describes it.
// Fields may be added; existing fields and codes are never renamed or
// repurposed within a schema version.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Matthew-Eucaristo/rustkill/internal/clean"
	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
)

// SchemaVersion of every document rustkill emits or accepts.
const SchemaVersion = 1

// Tool identifies the emitter.
type Tool struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

// NewTool describes rustkill at version v.
func NewTool(v string) Tool { return Tool{Name: "rustkill", Version: v, Ecosystem: "rust"} }

// Bytes is a clone- and hard-link-aware size.
type Bytes struct {
	Allocated int64 `json:"allocated"`
	Shared    int64 `json:"shared"`
	Exclusive int64 `json:"exclusive"`
	Files     int64 `json:"file_count"`
}

// Item is one removable thing.
type Item struct {
	ID          string        `json:"id"`
	Fingerprint string        `json:"fingerprint"`
	Kind        string        `json:"kind"`
	Category    scan.Category `json:"category"`
	Name        string        `json:"name"`
	Path        string        `json:"path"`
	Project     *Project      `json:"project"`
	Tool        string        `json:"tool,omitempty"`
	Action      string        `json:"action"` // trash or command
	Command     []string      `json:"command,omitempty"`
	Regenerate  string        `json:"regenerate"`
	Explain     string        `json:"explain"`
	Note        string        `json:"note,omitempty"`
	Measured    bool          `json:"measured"`
	Bytes       Bytes         `json:"bytes"`
	LastUsed    *Time         `json:"last_used"`
	Git         *Git          `json:"git,omitempty"`
	Safety      scan.Safety   `json:"safety"`
	Errors      []string      `json:"errors,omitempty"`
}

// Project describes the Cargo.toml directory a build directory belongs to.
type Project struct {
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	Workspace bool     `json:"workspace"`
	Members   int      `json:"workspace_members"`
	Edition   string   `json:"edition"`
	Toolchain string   `json:"toolchain"`
	Tools     []string `json:"tools"`
	Repo      string   `json:"repo"`
	Worktree  bool     `json:"in_linked_worktree"`
	Activity  *Time    `json:"last_activity"`
	CwdKnown  bool     `json:"processes_checked"`
	Processes []Proc   `json:"processes"`
}

// Time is a timestamp with its source and age.
type Time struct {
	At      time.Time `json:"at"`
	Source  string    `json:"source"`
	AgeDays int       `json:"age_days"`
}

type Proc struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

type Git struct {
	Ignored bool   `json:"ignored"`
	Tracked bool   `json:"tracked_files"`
	Error   string `json:"error,omitempty"`
}

// Tilde shows home as "~" so documents do not carry the username.
func Tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home || strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// Expand reverses Tilde.
func Expand(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

func stamp(t time.Time, src string, now time.Time) *Time {
	if t.IsZero() {
		return nil
	}
	return &Time{At: t.UTC(), Source: src, AgeDays: int(now.Sub(t).Hours() / 24)}
}

// NewItem converts a scanned item.
func NewItem(it *scan.Item, now time.Time) Item {
	out := Item{
		ID: it.ID, Fingerprint: it.Fingerprint, Kind: it.Kind, Category: it.Category, Name: it.Name, Path: Tilde(it.Path),
		Tool: it.Tool, Action: it.Action(), Regenerate: it.Regenerate, Explain: it.Explain, Note: it.Note,
		Measured: it.Measured, Safety: it.Safety, Errors: it.Errors, LastUsed: stamp(it.LastUsed, it.LastUsedSource, now),
	}
	if out.Safety.Reasons == nil {
		out.Safety.Reasons = []scan.Reason{}
	}
	for _, a := range it.Command {
		out.Command = append(out.Command, Tilde(a))
	}
	if it.Measured {
		out.Bytes = Bytes{it.Size.Allocated, it.Size.Shared, it.Size.Exclusive(), it.Size.Files}
	}
	if p := it.Project; p != nil {
		cp := &Project{Path: Tilde(p.Path), Name: p.Name, Workspace: p.Workspace, Members: p.Members, Edition: p.Edition,
			Toolchain: p.Toolchain, Tools: nonNil(p.Tools), Repo: Tilde(p.Repo), Worktree: p.Worktree,
			Activity: stamp(p.LastActivity, p.ActivitySource, now), CwdKnown: p.CwdKnown, Processes: []Proc{}}
		for _, pr := range p.Live {
			cp.Processes = append(cp.Processes, Proc{pr.PID, pr.Command})
		}
		out.Project = cp
	}
	if it.Repo != "" {
		out.Git = &Git{Ignored: it.Ignored, Tracked: it.Tracked, Error: it.GitErr}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Summary totals a scan.
type Summary struct {
	Items      int            `json:"items"`
	Projects   int            `json:"projects"`
	Bytes      Bytes          `json:"bytes"`
	SafeBytes  Bytes          `json:"safe_bytes"`
	ByLevel    map[string]int `json:"by_safety_level"`
	ByCategory map[string]int `json:"by_category"`
}

// ScanDoc is `rustkill scan --json`.
type ScanDoc struct {
	SchemaVersion int       `json:"schema_version"`
	Tool          Tool      `json:"tool"`
	GeneratedAt   time.Time `json:"generated_at"`
	Roots         []string  `json:"roots"`
	Warnings      []string  `json:"warnings"`
	SizeNote      string    `json:"size_note"`
	Summary       Summary   `json:"summary"`
	Items         []Item    `json:"items"`
}

// SizeNote explains how bytes are counted.
const SizeNote = "allocated blocks like du; hard links counted once; shared = bytes removal would not free (hard links outside the item, and on macOS blocks shared with APFS clones or snapshots); exclusive = allocated - shared; Btrfs/XFS reflinks count in full; freed space is measured after each action"

// NewScanDoc converts a scan.
func NewScanDoc(r *scan.Result, tool Tool) ScanDoc {
	now := time.Now()
	doc := ScanDoc{SchemaVersion: SchemaVersion, Tool: tool, GeneratedAt: now.UTC(), Warnings: nonNil(r.Warnings), SizeNote: SizeNote, Items: []Item{}, Roots: []string{}}
	for _, root := range r.Roots {
		doc.Roots = append(doc.Roots, Tilde(root))
	}
	for _, it := range r.Items {
		doc.Items = append(doc.Items, NewItem(it, now))
	}
	doc.Summary = Summarize(doc.Items)
	doc.Summary.Projects = len(r.Projects)
	return doc
}

// Summarize totals items.
func Summarize(items []Item) Summary {
	s := Summary{Items: len(items), ByLevel: map[string]int{}, ByCategory: map[string]int{}}
	add := func(a *Bytes, b Bytes) {
		a.Allocated += b.Allocated
		a.Shared += b.Shared
		a.Exclusive += b.Exclusive
		a.Files += b.Files
	}
	for _, it := range items {
		add(&s.Bytes, it.Bytes)
		if it.Safety.Level == scan.Safe {
			add(&s.SafeBytes, it.Bytes)
		}
		s.ByLevel[string(it.Safety.Level)]++
		s.ByCategory[string(it.Category)]++
	}
	return s
}

// Encode writes v as indented JSON.
func Encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Line writes one NDJSON record of the given type.
func Line(w io.Writer, typ string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	b := bytes.TrimRight(buf.Bytes(), "\n")
	if len(b) > 1 && b[0] == '{' {
		sep := ","
		if b[1] == '}' {
			sep = ""
		}
		b = append([]byte(fmt.Sprintf(`{"type":%q%s`, typ, sep)), b[1:]...)
	}
	_, err := w.Write(append(b, '\n'))
	return err
}

// Plan is what a caller asks `rustkill apply` to do. The action follows
// from the item: its official command, or a move to the trash.
type Plan struct {
	SchemaVersion int        `json:"schema_version"`
	Items         []PlanItem `json:"items"`
}

// PlanItem selects one item, pinned to its scan fingerprint.
type PlanItem struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}

// ReadPlan parses and validates a plan. Unknown fields are rejected so a
// plan cannot smuggle options; consent comes only from command-line flags.
func ReadPlan(r io.Reader) (*Plan, error) {
	var p Plan
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if p.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("plan: schema_version %d is not supported (want %d)", p.SchemaVersion, SchemaVersion)
	}
	if len(p.Items) == 0 {
		return nil, errors.New("plan: no items")
	}
	for i, it := range p.Items {
		if it.ID == "" || it.Fingerprint == "" {
			return nil, fmt.Errorf("plan: item %d needs id and fingerprint", i)
		}
	}
	return &p, nil
}

// Result is one apply outcome with paths shown relative to home.
type Result struct{ clean.Outcome }

// MarshalJSON tilde-shortens paths.
func (r Result) MarshalJSON() ([]byte, error) {
	o := r.Outcome
	o.Path = Tilde(o.Path)
	var cmd []string
	for _, a := range o.Command {
		cmd = append(cmd, Tilde(a))
	}
	o.Command = cmd
	return json.Marshal(o)
}

// ApplyDoc is `rustkill apply --json` and the result of headless cleaning.
type ApplyDoc struct {
	SchemaVersion int          `json:"schema_version"`
	Tool          Tool         `json:"tool"`
	Mode          string       `json:"mode"`
	Results       []Result     `json:"results"`
	Summary       ApplySummary `json:"summary"`
}

type ApplySummary struct {
	Done     int   `json:"done"`
	DryRun   int   `json:"dry_run"`
	Refused  int   `json:"refused"`
	Failed   int   `json:"failed"`
	Trashed  int64 `json:"trashed_bytes"`   // moved to the trash: freed when it is emptied
	ByTool   int64 `json:"command_bytes"`   // removed by official commands
	Deleted  int64 `json:"deleted_bytes"`   // deleted with --permanent
	Estimate int64 `json:"estimated_bytes"` // all of the above: what removal frees by the scan (shared blocks excluded)
	Freed    int64 `json:"freed_bytes_measured"`
}

// NewApplyDoc totals outcomes.
func NewApplyDoc(tool Tool, mode clean.Mode, outs []*clean.Outcome) ApplyDoc {
	d := ApplyDoc{SchemaVersion: SchemaVersion, Tool: tool, Mode: mode.String(), Results: []Result{}}
	for _, o := range outs {
		d.Results = append(d.Results, Result{*o})
		switch o.Status {
		case clean.StatusDone, clean.StatusDryRun:
			if o.Status == clean.StatusDone {
				d.Summary.Done++
			} else {
				d.Summary.DryRun++
			}
			d.Summary.Estimate += o.Estimate
			switch o.Action {
			case "command":
				d.Summary.ByTool += o.Estimate
			case "delete":
				d.Summary.Deleted += o.Estimate
			default:
				d.Summary.Trashed += o.Estimate
			}
		case clean.StatusRefused:
			d.Summary.Refused++
		case clean.StatusFailed:
			d.Summary.Failed++
		}
		if o.Freed > 0 {
			d.Summary.Freed += o.Freed
		}
	}
	return d
}

// Exit codes, part of the contract.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitUsage   = 2
	ExitRefused = 3 // at least one item refused, none failed
	ExitPartial = 4 // at least one item failed
)

// ExitCode for an apply.
func (d ApplyDoc) ExitCode() int {
	switch {
	case d.Summary.Failed > 0:
		return ExitPartial
	case d.Summary.Refused > 0:
		return ExitRefused
	}
	return ExitOK
}

// Capabilities is `rustkill capabilities --json`.
type Capabilities struct {
	SchemaVersion int               `json:"schema_version"`
	Tool          Tool              `json:"tool"`
	Commands      map[string]string `json:"commands"`
	Actions       []ActionInfo      `json:"actions"`
	Categories    []string          `json:"categories"`
	Levels        []scan.Level      `json:"safety_levels"`
	Reasons       []scan.ReasonInfo `json:"reason_codes"`
	Kinds         []scan.KindInfo   `json:"artifact_kinds"`
	ExitCodes     map[string]int    `json:"exit_codes"`
	Trash         string            `json:"trash"`
}

type ActionInfo struct {
	Name        string `json:"name"`
	Reversible  bool   `json:"reversible"`
	Description string `json:"description"`
}

// NewCapabilities describes what this build can do.
func NewCapabilities(tool Tool) Capabilities {
	c := Capabilities{
		SchemaVersion: SchemaVersion, Tool: tool, Trash: clean.TrashName,
		Commands: map[string]string{
			"scan":         "rustkill scan --json [--ndjson] [--no-global] [root...]",
			"apply":        "rustkill apply --plan <file|-> [--dry-run] [--permanent] [--json] [root...]",
			"capabilities": "rustkill capabilities --json",
		},
		Actions: []ActionInfo{
			{"trash", true, "Move a directory to the " + clean.TrashName + " (build directories and Cargo's caches, which have no stable cleanup command)."},
			{"delete", false, "With --permanent: delete the directory instead."},
			{"command", false, "Run rustup or cargo's own command (rustup toolchain uninstall, rustup component remove, rustup target remove, cargo uninstall); the tool deletes by its own design."},
		},
		Categories: []string{string(scan.CatProject), string(scan.CatCache), string(scan.CatGlobal), string(scan.CatRuntime)},
		Levels:     []scan.Level{scan.Safe, scan.Review, scan.Protected, scan.Live},
		Reasons:    scan.Reasons,
		ExitCodes:  map[string]int{"ok": ExitOK, "error": ExitError, "usage": ExitUsage, "refused": ExitRefused, "partial": ExitPartial},
	}
	c.Kinds = scan.Kinds
	return c
}
