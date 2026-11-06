package clean

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
	"github.com/Matthew-Eucaristo/rustkill/internal/testfix"
)

// fakeTrash replaces the trash with a folder in the test's temp dir.
func fakeTrash(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	old := moveFunc
	moveFunc = func(p string) error { return os.Rename(p, filepath.Join(bin, filepath.Base(p))) }
	t.Cleanup(func() { moveFunc = old })
	return bin
}

func setup(t *testing.T, files map[string]string, commit bool) (string, *scan.Result, scan.Options) {
	t.Helper()
	root := testfix.Real(t, t.TempDir())
	app := filepath.Join(root, "app")
	testfix.Write(t, app, files)
	if commit {
		testfix.Repo(t, app, testfix.Old)
	} else {
		testfix.Age(t, app, testfix.Old)
	}
	opts := scan.Options{Roots: []string{root}, Home: t.TempDir(), Env: scan.DefaultEnv()}
	res, err := scan.Scan(opts)
	if err != nil {
		t.Fatal(err)
	}
	res.MeasureAll()
	return app, res, opts
}

func base() map[string]string { return testfix.Target(testfix.Crate("app"), "target") }

func TestTrashSafeTarget(t *testing.T) {
	bin := fakeTrash(t)
	app, res, opts := setup(t, base(), true)
	it := res.Find(filepath.Join(app, "target"))
	if it == nil || it.Safety.Level != scan.Safe {
		t.Fatalf("item %+v", it)
	}
	out := Act(it, it.Fingerprint, Options{Mode: Trash, Scan: opts})
	if out.Status != StatusDone || out.Action != "trash" {
		t.Fatalf("outcome %+v", out)
	}
	if _, err := os.Lstat(filepath.Join(app, "target")); !os.IsNotExist(err) {
		t.Error("target still there")
	}
	if _, err := os.Lstat(filepath.Join(bin, "target", "debug", "app")); err != nil {
		t.Error("not in the trash")
	}
	if _, err := os.Lstat(filepath.Join(app, "src", "main.rs")); err != nil {
		t.Error("source touched")
	}
	if out.Estimate <= 0 || out.RestoreHint == "" {
		t.Errorf("estimate %d hint %q", out.Estimate, out.RestoreHint)
	}
}

func TestDryRunAndPermanent(t *testing.T) {
	fakeTrash(t)
	app, res, opts := setup(t, base(), true)
	it := res.Find(filepath.Join(app, "target"))
	if out := Act(it, it.Fingerprint, Options{Mode: DryRun, Scan: opts}); out.Status != StatusDryRun {
		t.Fatalf("dry run: %+v", out)
	}
	if _, err := os.Lstat(filepath.Join(app, "target")); err != nil {
		t.Fatal("dry run removed it")
	}
	if out := Act(it, it.Fingerprint, Options{Mode: Permanent, Scan: opts}); out.Status != StatusDone || out.Action != "delete" {
		t.Fatalf("permanent: %+v", out)
	}
	if _, err := os.Lstat(filepath.Join(app, "target")); !os.IsNotExist(err) {
		t.Error("target still there")
	}
}

func TestRefusals(t *testing.T) {
	fakeTrash(t)
	app, res, opts := setup(t, base(), true)
	it := res.Find(filepath.Join(app, "target"))

	// Stale: the scan's fingerprint no longer matches.
	if out := Act(it, "fp-0000", Options{Mode: Trash, Scan: opts}); out.Status != StatusRefused {
		t.Errorf("stale plan: %+v", out)
	}
	// Changed since the scan: git stops ignoring it.
	os.WriteFile(filepath.Join(app, ".gitignore"), []byte("*.log\n"), 0o644)
	if out := Act(it, it.Fingerprint, Options{Mode: Trash, Scan: opts}); out.Status != StatusRefused {
		t.Errorf("changed item: %+v", out)
	}
	os.WriteFile(filepath.Join(app, ".gitignore"), []byte("/target\n"), 0o644)
	// No longer cargo's: the tag is gone.
	os.Remove(filepath.Join(app, "target", "CACHEDIR.TAG"))
	if out := Act(it, it.Fingerprint, Options{Mode: Trash, Review: true, Scan: opts}); out.Status != StatusRefused {
		t.Errorf("untagged folder: %+v", out)
	}
	if _, err := os.Lstat(filepath.Join(app, "target", "debug", "app")); err != nil {
		t.Fatal("refused item was removed")
	}
	// Vanished since the scan.
	os.RemoveAll(filepath.Join(app, "target"))
	if out := Act(it, it.Fingerprint, Options{Mode: Trash, Scan: opts}); out.Status == StatusDone || out.Error == "" {
		t.Errorf("vanished item: %+v", out)
	}
}

func TestReviewNeedsConsent(t *testing.T) {
	fakeTrash(t)
	files := base()
	files["target/release/bundle/dmg/App.dmg"] = "installer" // → review: deployable
	app, res, opts := setup(t, files, true)
	it := res.Find(filepath.Join(app, "target"))
	if it.Safety.Level != scan.Review {
		t.Fatalf("level %s", it.Safety.Level)
	}
	if out := Act(it, it.Fingerprint, Options{Mode: Trash, Scan: opts}); out.Status != StatusRefused {
		t.Errorf("review without consent: %+v", out)
	}
	if out := Act(it, it.Fingerprint, Options{Mode: Trash, Review: true, Scan: opts}); out.Status != StatusDone {
		t.Errorf("review with consent: %+v", out)
	}
}

func TestProtectedNeverRemoved(t *testing.T) {
	fakeTrash(t)
	files := base()
	files[".gitignore"] = "*.log\n" // git does not ignore target, so it may be source
	app, _, opts := setup(t, files, false)
	os.MkdirAll(filepath.Join(app, ".git"), 0o755)
	testfix.Git(t, app, testfix.Old, "init", "-q")
	res, _ := scan.Scan(opts)
	res.MeasureAll()
	it := res.Find(filepath.Join(app, "target"))
	if it.Safety.Level != scan.Protected {
		t.Fatalf("level %s", it.Safety.Level)
	}
	for _, m := range []Mode{Trash, Permanent} {
		if out := Act(it, it.Fingerprint, Options{Mode: m, Review: true, Scan: opts}); out.Status != StatusRefused {
			t.Errorf("%s: %+v", m, out)
		}
	}
	if _, err := os.Lstat(filepath.Join(app, "target", "debug", "app")); err != nil {
		t.Fatal("protected target removed")
	}
}

func TestPermanentDoesNotFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	fakeTrash(t)
	app, res, opts := setup(t, base(), true)
	// A link inside target (an out-dir pointing at sources) must not be followed.
	os.MkdirAll(filepath.Join(app, "assets"), 0o755)
	os.WriteFile(filepath.Join(app, "assets", "logo.png"), []byte("source"), 0o644)
	os.Symlink(filepath.Join(app, "assets"), filepath.Join(app, "target", "debug", "assets"))
	it := res.Find(filepath.Join(app, "target"))
	fresh, err := scan.Refresh(it, opts)
	if err != nil {
		t.Fatal(err)
	}
	if out := Act(fresh, fresh.Fingerprint, Options{Mode: Permanent, Scan: opts}); out.Status != StatusDone {
		t.Fatalf("%+v", out)
	}
	if _, err := os.Lstat(filepath.Join(app, "assets", "logo.png")); err != nil {
		t.Fatal("followed a symlink out of target")
	}
}

func TestCheckRefusesBroadAndForeign(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, p := range []string{"/", home, "relative/path", filepath.Dir(home)} {
		if err := checkPath(p); err == nil {
			t.Errorf("checkPath(%q) allowed", p)
		}
	}
	if err := Check(&scan.Item{Command: []string{"/bin/rm", "-rf", "/"}}); err == nil {
		t.Error("ran a foreign tool")
	}
	if err := Check(&scan.Item{Command: []string{"rustup", "toolchain", "uninstall", "x"}}); err == nil {
		t.Error("ran a relative executable")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	rustup, cargo := filepath.Join(dir, "rustup"), filepath.Join(dir, "cargo")
	if os.Symlink(exe, rustup) != nil || os.Symlink(exe, cargo) != nil {
		t.Skip("no symlinks")
	}
	for _, cmd := range [][]string{
		{rustup, "self", "uninstall", "-y"}, {rustup, "toolchain"}, {cargo, "clean", "gc"}, {cargo, "install", "x"},
	} {
		if err := Check(&scan.Item{Command: cmd}); err == nil {
			t.Errorf("allowed %v", cmd)
		}
	}
	for _, cmd := range [][]string{
		{rustup, "toolchain", "uninstall", "1.80.0-x"}, {rustup, "component", "remove", "rust-docs", "--toolchain", "x"},
		{rustup, "target", "remove", "wasm32-unknown-unknown", "--toolchain", "x"}, {cargo, "uninstall", "--root", "/x", "ripgrep"},
	} {
		if err := Check(&scan.Item{Command: cmd}); err != nil {
			t.Errorf("refused %v: %v", cmd, err)
		}
	}
	// A cache folder must lie well inside home.
	if err := Check(&scan.Item{Kind: "registry-src", Path: t.TempDir()}); err == nil {
		t.Error("would trash a folder outside home")
	}
}
