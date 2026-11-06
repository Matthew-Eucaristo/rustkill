package scan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
	"github.com/Matthew-Eucaristo/rustkill/internal/testfix"
)

// scanFix scans root with the global part off and sizes measured.
func scanFix(t *testing.T, root string) *Result {
	t.Helper()
	res, err := Scan(Options{Roots: []string{root}, Home: t.TempDir(), Env: DefaultEnv()})
	if err != nil {
		t.Fatal(err)
	}
	res.MeasureAll()
	return res
}

func item(t *testing.T, res *Result, path string) *Item {
	t.Helper()
	for _, it := range res.Items {
		if it.Path == path {
			return it
		}
	}
	var got []string
	for _, it := range res.Items {
		got = append(got, it.Path)
	}
	t.Fatalf("no item at %s; have %v", path, got)
	return nil
}

func noItem(t *testing.T, res *Result, path string) {
	t.Helper()
	for _, it := range res.Items {
		if it.Path == path {
			t.Fatalf("unexpected item at %s (%s)", path, it.Kind)
		}
	}
}

func codes(it *Item) []string {
	var c []string
	for _, r := range it.Safety.Reasons {
		c = append(c, r.Code)
	}
	return c
}

func want(t *testing.T, it *Item, level Level, code ...string) {
	t.Helper()
	if it.Safety.Level != level {
		t.Errorf("%s: level %s, want %s (reasons %v)", it.Path, it.Safety.Level, level, codes(it))
	}
	for _, c := range code {
		if !it.Safety.Has(c) {
			t.Errorf("%s: missing reason %s (have %v)", it.Path, c, codes(it))
		}
	}
}

// project writes an idle, committed crate with a build directory.
func project(t *testing.T, root, name string, extra map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	files := testfix.Target(testfix.Crate(name), "target")
	for k, v := range extra {
		files[k] = v
	}
	testfix.Write(t, dir, files)
	testfix.Repo(t, dir, testfix.Old)
	return dir
}

func TestIdleTargetIsSafe(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	app := project(t, root, "shop", nil)
	res := scanFix(t, root)
	it := item(t, res, filepath.Join(app, "target"))
	want(t, it, Safe)
	if it.Kind != "target" || it.Category != CatProject || it.Action() != "trash" || !it.Ignored || it.Tracked {
		t.Errorf("item %+v", it)
	}
	p := it.Project
	if p.Name != "shop" || p.Edition != "2024" || !slices.Contains(p.Tools, "axum") || p.LastActivity.IsZero() {
		t.Errorf("project %+v", p)
	}
	if !it.Measured || it.Size.Allocated <= 0 || it.LastUsedSource != "last build" {
		t.Errorf("measure %+v %s", it.Size, it.LastUsedSource)
	}
}

func TestOnlyCargoOutputCounts(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	dir := filepath.Join(root, "app")
	files := testfix.Crate("app")
	// A folder named target without cargo's tag: someone's data.
	files["target/notes.md"] = "mine"
	// The tag alone, without a build layout (Cargo's registry cache looks like this).
	files["data/CACHEDIR.TAG"] = testfix.CargoTag
	files["data/crate.crate"] = "x"
	// Another tool's CACHEDIR.TAG.
	files["other/CACHEDIR.TAG"] = "Signature: 8a477f597d28d172789f06886806bc55\n# created by someone else\n"
	files["other/debug/.cargo-lock"] = ""
	testfix.Write(t, dir, files)
	testfix.Repo(t, dir, testfix.Old)
	res := scanFix(t, root)
	if len(res.Items) != 0 {
		t.Fatalf("items %v", res.Items[0].Path)
	}
	if len(res.Projects) != 1 {
		t.Errorf("projects %d", len(res.Projects))
	}
}

func TestSplitBuildDirLayoutCounts(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	dir := filepath.Join(root, "app")
	files := testfix.Crate("app")
	// A target-dir when build-dir lives elsewhere: tag and locks, no rustc_info.
	files["target/CACHEDIR.TAG"] = testfix.CargoTag
	files["target/debug/.cargo-artifact-lock"] = ""
	files["target/debug/app"] = "bin"
	testfix.Write(t, dir, files)
	testfix.Repo(t, dir, testfix.Old)
	item(t, scanFix(t, root), filepath.Join(dir, "target"))
}

func TestGitEvidence(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	notIgnored := filepath.Join(root, "a")
	files0 := testfix.Crate("a")
	files0[".gitignore"] = "*.log\n"
	testfix.Write(t, notIgnored, files0)
	testfix.Repo(t, notIgnored, testfix.Old)
	testfix.Write(t, notIgnored, testfix.Target(map[string]string{}, "target"))
	testfix.Age(t, notIgnored, testfix.Old)
	tracked := filepath.Join(root, "b")
	files := testfix.Target(testfix.Crate("b"), "target")
	files[".gitignore"] = ""
	testfix.Write(t, tracked, files)
	testfix.Repo(t, tracked, testfix.Old)
	os.WriteFile(filepath.Join(tracked, ".gitignore"), []byte("/target\n"), 0o644)
	testfix.Age(t, tracked, testfix.Old)
	outside := filepath.Join(root, "c")
	testfix.Write(t, outside, testfix.Target(testfix.Crate("c"), "target"))
	testfix.Age(t, outside, testfix.Old)

	res := scanFix(t, root)
	want(t, item(t, res, filepath.Join(notIgnored, "target")), Protected, ReasonNotIgnored)
	want(t, item(t, res, filepath.Join(tracked, "target")), Protected, ReasonTracked)
	// Outside git the tag is the evidence: cargo made it.
	want(t, item(t, res, filepath.Join(outside, "target")), Safe)
}

func TestRecentAndBuiltRecently(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	recent := filepath.Join(root, "recent")
	testfix.Write(t, recent, testfix.Target(testfix.Crate("recent"), "target"))
	testfix.Repo(t, recent, time.Now())
	built := project(t, root, "built", nil)
	os.WriteFile(filepath.Join(built, "target", "debug", "app"), []byte("rebuilt"), 0o644)
	res := scanFix(t, root)
	want(t, item(t, res, filepath.Join(recent, "target")), Review, ReasonRecent)
	it := item(t, res, filepath.Join(built, "target"))
	want(t, it, Review, ReasonRecent)
	if it.Safety.Reasons[0].Detail != "built in the last 7 days" {
		t.Errorf("detail %q", it.Safety.Reasons[0].Detail)
	}
}

func TestWorkspaceTakesMemberActivity(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	ws := filepath.Join(root, "ws")
	files := testfix.Target(map[string]string{
		"Cargo.toml": "[workspace]\nmembers = [\"crates/*\"]\nresolver = \"3\"\n\n[workspace.package]\nedition = \"2021\"\n\n[workspace.dependencies]\nbevy = \"0.16\"\n",
		".gitignore": "/target\n",
	}, "target")
	for k, v := range testfix.Crate("game") {
		files["crates/game/"+k] = v
	}
	testfix.Write(t, ws, files)
	testfix.Repo(t, ws, testfix.Old)
	// A member file edited today (uncommitted) makes the whole workspace recent.
	os.WriteFile(filepath.Join(ws, "crates", "game", "src", "main.rs"), []byte("fn main() { }\n"), 0o644)
	res := scanFix(t, root)
	it := item(t, res, filepath.Join(ws, "target"))
	want(t, it, Review, ReasonRecent)
	if p := it.Project; !p.Workspace || p.Members != 1 || p.Edition != "2021" || p.Framework() != "Bevy" {
		t.Errorf("workspace %+v", p)
	}
}

func TestWorktreeBundleNestedRepo(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	main := project(t, root, "main", nil)
	wt := filepath.Join(root, "wt")
	testfix.Git(t, main, testfix.Old, "worktree", "add", "-q", wt)
	testfix.Write(t, wt, testfix.Target(map[string]string{}, "target"))
	testfix.Age(t, wt, testfix.Old)
	bundle := project(t, root, "desk", map[string]string{"target/release/bundle/dmg/Desk.dmg": "installer"})
	nested := project(t, root, "nested", map[string]string{"target/debug/build/x/out/vendored/.git/HEAD": "ref"})
	res := scanFix(t, root)
	want(t, item(t, res, filepath.Join(wt, "target")), Review, ReasonWorktree)
	want(t, item(t, res, filepath.Join(bundle, "target")), Review, ReasonDeployable)
	want(t, item(t, res, filepath.Join(nested, "target")), Protected, ReasonNestedRepo)
}

func TestSharedBuildDirUnderRoot(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	shared := filepath.Join(root, "cargo-target")
	testfix.Write(t, shared, testfix.Target(map[string]string{}, "."))
	testfix.Age(t, shared, testfix.Old)
	res := scanFix(t, root)
	it := item(t, res, shared)
	if it.Kind != "shared-target" || it.Category != CatCache || it.Project != nil {
		t.Errorf("item %+v", it)
	}
	want(t, it, Review, ReasonSharedTarget)
}

func TestLiveProcessAndSymlinks(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	app := project(t, root, "app", nil)
	idle := project(t, root, "idle", nil)
	if err := os.Symlink(filepath.Join(idle, "target"), filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	old := Snapshot
	Snapshot = func() *procs.Table {
		return &procs.Table{CwdKnown: true, Procs: []procs.Proc{
			{PID: 7, Command: "cargo", Cwd: filepath.Join(app, "src")},
			{PID: 8, Command: "zsh", Cwd: idle},
		}}
	}
	t.Cleanup(func() { Snapshot = old })
	res := scanFix(t, root)
	want(t, item(t, res, filepath.Join(app, "target")), Live, ReasonInUse)
	want(t, item(t, res, filepath.Join(idle, "target")), Safe) // a shell is not a build
	noItem(t, res, filepath.Join(root, "link"))
	if len(res.Items) != 2 {
		t.Errorf("%d items", len(res.Items))
	}
}

func TestToolchainPins(t *testing.T) {
	for _, c := range []struct {
		pin, dir string
		ok       bool
	}{
		{"stable", "stable-aarch64-apple-darwin", true},
		{"nightly", "nightly-aarch64-apple-darwin", true},
		{"nightly", "nightly-2024-05-01-aarch64-apple-darwin", false},
		{"nightly-2024-05-01", "nightly-2024-05-01-aarch64-apple-darwin", true},
		{"1.80", "1.80.1-x86_64-unknown-linux-gnu", true},
		{"1.80", "1.81.0-x86_64-unknown-linux-gnu", false},
		{"1.80.0", "1.80.0-aarch64-apple-darwin", true},
		{"1.8", "1.80.0-aarch64-apple-darwin", false},
	} {
		if got := pinMatches(c.pin, c.dir); got != c.ok {
			t.Errorf("pinMatches(%q, %q) = %v", c.pin, c.dir, got)
		}
	}
	dir := t.TempDir()
	testfix.Write(t, dir, map[string]string{"a/rust-toolchain.toml": "[toolchain]\nchannel = \"1.82.0\"\ncomponents = [\"clippy\"]\n", "b/rust-toolchain": "nightly-2025-01-01\n"})
	if got := toolchainPin(filepath.Join(dir, "a"), map[string]bool{"rust-toolchain.toml": true}); got != "1.82.0" {
		t.Errorf("toml pin %q", got)
	}
	if got := toolchainPin(filepath.Join(dir, "b"), map[string]bool{"rust-toolchain": true}); got != "nightly-2025-01-01" {
		t.Errorf("legacy pin %q", got)
	}
}

func TestBrokenManifestStillListed(t *testing.T) {
	root := testfix.Real(t, t.TempDir())
	app := project(t, root, "broken", map[string]string{"Cargo.toml": "[package\nname="})
	res := scanFix(t, root)
	it := item(t, res, filepath.Join(app, "target"))
	if it.Project.Name != "broken" || len(it.Project.Errors) == 0 {
		t.Errorf("project %+v", it.Project)
	}
}
