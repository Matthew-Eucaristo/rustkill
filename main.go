// Command rustkill finds what Rust leaves on disk (target folders, Cargo's
// caches, tools from cargo install, rustup toolchains and components),
// shows how safe each is to remove, and removes it the official way:
// folders to the Trash, toolchains and installed tools with rustup's and
// cargo's own commands. Run `rustkill -h` for usage.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Matthew-Eucaristo/rustkill/internal/clean"
	"github.com/Matthew-Eucaristo/rustkill/internal/contract"
	"github.com/Matthew-Eucaristo/rustkill/internal/format"
	"github.com/Matthew-Eucaristo/rustkill/internal/report"
	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
	"github.com/Matthew-Eucaristo/rustkill/internal/ui"
)

var version = "" // set by -ldflags at release time

const usage = `rustkill - reclaim disk space from Rust projects, Cargo caches and toolchains, safely.

Usage:
  rustkill [flags] [root...]               interactive; roots default to ~/dev
  rustkill --older-than 30d --yes [root]   move target folders of idle projects to the Trash
  rustkill scan [--json|--ndjson] [root…]  list everything with evidence (read-only)
  rustkill apply --plan <file|-> [root…]   act on ids + fingerprints from a scan
  rustkill capabilities --json             describe actions, reason codes and exit codes

Folders cargo tagged as build output (target) and Cargo's caches go to the
Trash. Toolchains, components, targets and installed tools are removed only
by rustup's and cargo's own commands (rustup toolchain uninstall, rustup
component remove, cargo uninstall). Anything git tracks or does not ignore,
anything in use and anything rustkill cannot verify is left alone.

Flags:
`

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func main() {
	err := run(os.Args[1:])
	var ee *exitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		if ee.err != nil {
			fmt.Fprintln(os.Stderr, "rustkill:", ee.err)
		}
		os.Exit(ee.code)
	case errors.Is(err, flag.ErrHelp):
	default:
		fmt.Fprintln(os.Stderr, "rustkill:", err)
		os.Exit(contract.ExitError)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "scan":
			return runScan(args[1:])
		case "apply":
			return runApply(args[1:])
		case "capabilities":
			return runCapabilities(args[1:])
		case "version":
			fmt.Println("rustkill", buildVersion())
			return nil
		}
	}
	return runDefault(args)
}

func usageErr(f string, a ...any) error { return &exitError{contract.ExitUsage, fmt.Errorf(f, a...)} }

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return &exitError{contract.ExitUsage, nil}
	}
	return nil
}

func mode(dryRun, permanent bool) (clean.Mode, error) {
	switch {
	case dryRun && permanent:
		return 0, usageErr("--dry-run and --permanent are mutually exclusive")
	case dryRun:
		return clean.DryRun, nil
	case permanent:
		return clean.Permanent, nil
	}
	return clean.Trash, nil
}

func runDefault(args []string) error {
	fs := newFlags("rustkill")
	var (
		dryRun    = fs.Bool("dry-run", false, "never change anything; show what would be done")
		permanent = fs.Bool("permanent", false, "delete directories instead of moving them to the Trash")
		jsonOut   = fs.Bool("json", false, "print the scan as JSON and exit (same as: rustkill scan --json)")
		list      = fs.Bool("list", false, "print the scan as a table and exit (default when not a terminal)")
		yes       = fs.Bool("yes", false, "non-interactive: clean every safe target folder (never caches, installed tools or toolchains)")
		olderThan = fs.String("older-than", "", "only projects idle for at least this long, e.g. 30d, 6w, 3mo, 1y")
		noGlobal  = fs.Bool("no-global", false, "skip Cargo caches, installed tools and toolchains")
		showVer   = fs.Bool("version", false, "print version")
	)
	if err := parse(fs, args); err != nil {
		return err
	}
	if *showVer {
		fmt.Println("rustkill", buildVersion())
		return nil
	}
	m, err := mode(*dryRun, *permanent)
	if err != nil {
		return err
	}
	var minAge time.Duration
	if *olderThan != "" {
		if minAge, err = format.ParseAge(*olderThan); err != nil {
			return usageErr("%v", err)
		}
	}
	opts, err := scanOptions(fs.Args(), !*noGlobal && !*yes)
	if err != nil {
		return err
	}
	interactive := isTerminal(os.Stdout) && isTerminal(os.Stdin)
	if *yes || *jsonOut || *list || !interactive {
		res, err := scan.Scan(opts)
		if err != nil {
			return err
		}
		res.MeasureAll()
		filterAge(res, minAge)
		switch {
		case *yes:
			return headless(res, opts, m, *jsonOut)
		case *jsonOut:
			return contract.Encode(os.Stdout, contract.NewScanDoc(res, contract.NewTool(buildVersion())))
		}
		report.Text(os.Stdout, res)
		return nil
	}
	model := ui.New(opts, clean.Options{Mode: m, Review: true, Scan: opts}, minAge)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return err
	}
	report.Receipt(os.Stdout, model.Outcomes(), m)
	return nil
}

// filterAge keeps project items of projects idle for at least d; items of
// projects with unknown activity are dropped, never assumed old.
func filterAge(res *scan.Result, d time.Duration) {
	if d <= 0 {
		return
	}
	var keep []*scan.Item
	for _, it := range res.Items {
		p := it.Project
		if p == nil || (!p.LastActivity.IsZero() && time.Since(p.LastActivity) >= d) {
			keep = append(keep, it)
		}
	}
	res.Items = keep
}

// headless cleans every safe project build directory, one at a time.
func headless(res *scan.Result, opts scan.Options, m clean.Mode, jsonOut bool) error {
	co := clean.Options{Mode: m, Scan: opts}
	var outs []*clean.Outcome
	for _, it := range res.Items {
		if it.Category != scan.CatProject || it.Safety.Level != scan.Safe {
			continue
		}
		outs = append(outs, clean.Act(it, it.Fingerprint, co))
	}
	return finish(outs, m, jsonOut, func() { report.Receipt(os.Stdout, outs, m) })
}

func runScan(args []string) error {
	fs := newFlags("rustkill scan")
	var (
		jsonOut  = fs.Bool("json", false, "print one JSON document")
		ndjson   = fs.Bool("ndjson", false, "stream newline-delimited JSON: a header, one item as each is measured, a summary")
		noGlobal = fs.Bool("no-global", false, "skip Cargo caches, installed tools and toolchains")
	)
	if err := parse(fs, args); err != nil {
		return err
	}
	opts, err := scanOptions(fs.Args(), !*noGlobal)
	if err != nil {
		return err
	}
	res, err := scan.Scan(opts)
	if err != nil {
		return err
	}
	tool := contract.NewTool(buildVersion())
	if *ndjson {
		return streamScan(os.Stdout, res, tool)
	}
	res.MeasureAll()
	if *jsonOut {
		return contract.Encode(os.Stdout, contract.NewScanDoc(res, tool))
	}
	report.Text(os.Stdout, res)
	return nil
}

func streamScan(w io.Writer, res *scan.Result, tool contract.Tool) error {
	head := contract.NewScanDoc(&scan.Result{Roots: res.Roots, Warnings: res.Warnings}, tool)
	if err := contract.Line(w, "header", struct {
		SchemaVersion int      `json:"schema_version"`
		Tool          any      `json:"tool"`
		Roots         []string `json:"roots"`
		Warnings      []string `json:"warnings"`
		Items         int      `json:"items"`
	}{head.SchemaVersion, head.Tool, head.Roots, head.Warnings, len(res.Items)}); err != nil {
		return err
	}
	ch := make(chan scan.Measurement)
	go res.MeasureInto(ch)
	var items []contract.Item
	emit := func(it *scan.Item) error {
		ci := contract.NewItem(it, head.GeneratedAt)
		items = append(items, ci)
		return contract.Line(w, "item", ci)
	}
	for m := range ch {
		it := res.Find(m.ID)
		it.Apply(m)
		if err := emit(it); err != nil {
			return err
		}
	}
	for _, it := range res.Items {
		if it.Path == "" { // nothing to measure (e.g. an entry known only by name)
			if err := emit(it); err != nil {
				return err
			}
		}
	}
	sum := contract.Summarize(items)
	sum.Projects = len(res.Projects)
	return contract.Line(w, "summary", sum)
}

func runCapabilities(args []string) error {
	fs := newFlags("rustkill capabilities")
	fs.Bool("json", true, "print JSON (the only format)")
	if err := parse(fs, args); err != nil {
		return err
	}
	return contract.Encode(os.Stdout, contract.NewCapabilities(contract.NewTool(buildVersion())))
}

func runApply(args []string) error {
	fs := newFlags("rustkill apply")
	var (
		planPath  = fs.String("plan", "", "plan file, or - for stdin")
		dryRun    = fs.Bool("dry-run", false, "check everything and report, change nothing")
		permanent = fs.Bool("permanent", false, "delete directories instead of moving them to the Trash")
		jsonOut   = fs.Bool("json", false, "print results as JSON")
	)
	if err := parse(fs, args); err != nil {
		return err
	}
	if *planPath == "" {
		return usageErr("apply needs --plan <file|->")
	}
	m, err := mode(*dryRun, *permanent)
	if err != nil {
		return err
	}
	var in io.Reader = os.Stdin
	if *planPath != "-" {
		f, err := os.Open(*planPath)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	plan, err := contract.ReadPlan(in)
	if err != nil {
		return &exitError{contract.ExitUsage, err}
	}
	opts, err := scanOptions(fs.Args(), true)
	if err != nil {
		return err
	}
	res, err := scan.Scan(opts)
	if err != nil {
		return err
	}
	// An item named in a plan was chosen explicitly, so review items may go.
	co := clean.Options{Mode: m, Review: true, Scan: opts}
	var outs []*clean.Outcome
	for _, pi := range plan.Items {
		it := res.Find(pi.ID)
		if it == nil {
			outs = append(outs, &clean.Outcome{ID: pi.ID, Status: clean.StatusRefused, Error: "not found under the scanned roots; rescan", Freed: -1})
			continue
		}
		outs = append(outs, clean.Act(it, pi.Fingerprint, co))
	}
	return finish(outs, m, *jsonOut, func() { report.Receipt(os.Stdout, outs, m) })
}

func finish(outs []*clean.Outcome, m clean.Mode, jsonOut bool, text func()) error {
	doc := contract.NewApplyDoc(contract.NewTool(buildVersion()), m, outs)
	if jsonOut {
		if err := contract.Encode(os.Stdout, doc); err != nil {
			return err
		}
	} else if len(outs) == 0 {
		fmt.Println("rustkill: nothing safe to clean.")
	} else {
		text()
	}
	if code := doc.ExitCode(); code != contract.ExitOK {
		return &exitError{code, nil}
	}
	return nil
}

// scanOptions resolves roots (default ~/dev, else the current directory).
func scanOptions(args []string, global bool) (scan.Options, error) {
	env := scan.DefaultEnv()
	opts := scan.Options{Home: env.Home, Global: global, Env: env}
	if len(args) == 0 {
		def := filepath.Join(env.Home, "dev")
		if fi, err := os.Stat(def); err != nil || !fi.IsDir() {
			def = "."
		}
		args = []string{def}
	}
	seen := map[string]bool{}
	for _, a := range args {
		abs, err := filepath.Abs(contract.Expand(a))
		if err != nil {
			return opts, err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return opts, &exitError{contract.ExitUsage, err}
		}
		if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
			return opts, usageErr("%s is not a directory", a)
		}
		if !seen[strings.ToLower(real)] {
			seen[strings.ToLower(real)] = true
			opts.Roots = append(opts.Roots, real)
		}
	}
	return opts, nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}
