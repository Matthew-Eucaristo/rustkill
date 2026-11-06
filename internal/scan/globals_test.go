package scan

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
	"github.com/Matthew-Eucaristo/rustkill/internal/testfix"
)

const host = "aarch64-apple-darwin"

// machine builds a fake home with a Cargo home, a rustup home holding the
// given toolchains (folder → version) and stand-in rustup/cargo scripts.
func machine(t *testing.T, toolchains map[string]string, vars map[string]string) (Env, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stand-in tools are shell scripts")
	}
	home := testfix.Real(t, t.TempDir())
	ch, rh := filepath.Join(home, ".cargo"), filepath.Join(home, ".rustup")
	files := map[string]string{
		".cargo/bin/rustup": "#!/bin/sh\nexit 0\n",
		".cargo/bin/cargo":  "#!/bin/sh\nexit 0\n",
		".cargo/bin/rg":     "binary",
		".cargo/.crates2.json": `{"installs":{"ripgrep 14.1.1 (registry+https://github.com/rust-lang/crates.io-index)":{"bins":["rg"]},` +
			`"gone 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)":{"bins":["gone"]}}}`,
		".cargo/registry/src/index.crates.io-1949cf8c6b5b557f/serde-1.0.228/src/lib.rs": "pub fn x() {}",
		".cargo/registry/cache/index.crates.io-1949cf8c6b5b557f/serde-1.0.228.crate":    "crate",
		".cargo/registry/index/index.crates.io-1949cf8c6b5b557f/config.json":            "{}",
		".cargo/registry/src/index.crates.io-6f17d22bba15001f/serde-1.0.200/src/lib.rs": "pub fn x() {}",
		".cargo/registry/CACHEDIR.TAG":                      testfix.CargoTag,
		".cargo/git/checkouts/bevy-1a2b/abc1234/Cargo.toml": "[package]",
		".cargo/git/db/bevy-1a2b/HEAD":                      "ref",
		".rustup/settings.toml":                             "version = \"12\"\ndefault_toolchain = \"stable-" + host + "\"\ndefault_host_triple = \"" + host + "\"\n\n[overrides]\n",
		".rustup/downloads/abc.partial":                     "x",
	}
	for tc, v := range toolchains {
		d := ".rustup/toolchains/" + tc + "/"
		// Running a toolchain would move its "last used" time: the stand-ins
		// leave a mark if rustkill ever does.
		files[d+"bin/rustc"] = "#!/bin/sh\ntouch \"$0.ran\"\n"
		files[d+"bin/cargo"] = "#!/bin/sh\ntouch \"$0.ran\"\n"
		files[d+"lib/rustlib/multirust-channel-manifest.toml"] = "manifest-version = \"2\"\n\n[pkg.cargo]\nversion = \"0.100.0 (x)\"\n\n[pkg.rustc]\nversion = \"" + v + " (abc 2026-01-01)\"\n"
		files[d+"lib/rustlib/components"] = "rustc-" + host + "\ncargo-" + host + "\nrust-docs-" + host + "\nrust-std-" + host + "\nrust-std-wasm32-unknown-unknown\n"
		files[d+"lib/rustlib/manifest-rust-docs-"+host] = "dir:share/doc/rust/html\n"
		files[d+"lib/rustlib/manifest-rust-std-wasm32-unknown-unknown"] = "file:lib/rustlib/wasm32-unknown-unknown/lib/libstd.rlib\nfile:../../escape\n"
		files[d+"share/doc/rust/html/index.html"] = "<html>docs</html>"
		files[d+"lib/rustlib/wasm32-unknown-unknown/lib/libstd.rlib"] = "rlib"
	}
	testfix.Write(t, home, files)
	for _, p := range []string{".cargo/bin/rustup", ".cargo/bin/cargo"} {
		os.Chmod(filepath.Join(home, p), 0o755)
	}
	for tc := range toolchains {
		for _, b := range []string{"rustc", "cargo"} {
			os.Chmod(filepath.Join(rh, "toolchains", tc, "bin", b), 0o755)
		}
	}
	_ = ch
	env := Env{Home: home, GOOS: runtime.GOOS, Query: query,
		Getenv:   func(k string) string { return vars[k] },
		LookPath: func(string) (string, error) { return "", errors.New("not on PATH") }}
	return env, home
}

func byName(t *testing.T, items []*Item, kind, name string) *Item {
	t.Helper()
	for _, it := range items {
		if it.Kind == kind && it.Name == name {
			return it
		}
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Kind+"/"+it.Name)
	}
	t.Fatalf("no %s %q; have %v", kind, name, got)
	return nil
}

var idle = &procs.Table{CwdKnown: true}

func TestCargoCaches(t *testing.T) {
	env, home := machine(t, map[string]string{"stable-" + host: "1.99.1"}, nil)
	items, warns := globals(env, idle)
	if len(warns) > 0 {
		t.Fatal(warns)
	}
	src := byName(t, items, "registry-src", "crates.io sources")
	want(t, src, Safe)
	if src.Action() != "trash" || src.Path != filepath.Join(home, ".cargo", "registry", "src", "index.crates.io-1949cf8c6b5b557f") {
		t.Errorf("src %+v", src)
	}
	want(t, byName(t, items, "registry-cache", "crates.io downloads"), Review, ReasonCatalogReview)
	want(t, byName(t, items, "registry-index", "crates.io index"), Review)
	want(t, byName(t, items, "git-checkouts", "git dependency checkouts"), Safe)
	want(t, byName(t, items, "git-db", "git dependency clones"), Review)
	want(t, byName(t, items, "rustup-downloads", "rustup downloads"), Safe)
	// Every installed Cargo is 1.85+, so the pre-1.85 folder is unused.
	old := byName(t, items, "registry-src", "crates.io (sparse index, Cargo before 1.85) sources")
	want(t, old, Safe)
	if !strings.Contains(old.Explain, "Left behind") {
		t.Errorf("explain %q", old.Explain)
	}
	for _, it := range items {
		if strings.Contains(it.Path, "CACHEDIR") {
			t.Errorf("listed %s", it.Path)
		}
	}
}

func TestOldRegistryKeptForOldCargo(t *testing.T) {
	env, _ := machine(t, map[string]string{"stable-" + host: "1.99.1", "1.80.0-" + host: "1.80.0"}, nil)
	items, _ := globals(env, idle)
	want(t, byName(t, items, "registry-src", "crates.io (sparse index, Cargo before 1.85) sources"), Review, ReasonOldCargo)
}

func TestToolchainsAndComponents(t *testing.T) {
	env, home := machine(t, map[string]string{"stable-" + host: "1.99.1", "1.80.0-" + host: "1.80.0", "nightly-2025-06-01-" + host: "1.89.0"}, nil)
	items, _ := globals(env, idle)
	rustup := filepath.Join(home, ".cargo", "bin", "rustup")
	stable := byName(t, items, "toolchain", "toolchain stable (1.99.1)")
	want(t, stable, Protected, ReasonCurrent)
	old := byName(t, items, "toolchain", "toolchain 1.80.0")
	want(t, old, Review, ReasonCatalogReview)
	if !slices.Equal(old.Command, []string{rustup, "toolchain", "uninstall", "1.80.0-" + host}) || old.CommandDir != home || old.Version != "1.80.0" {
		t.Errorf("toolchain %+v", old)
	}
	byName(t, items, "toolchain", "toolchain nightly-2025-06-01 (1.89.0)")
	docs := byName(t, items, "rust-docs", "rust-docs (stable)")
	want(t, docs, Review)
	if !slices.Equal(docs.Command, []string{rustup, "component", "remove", "rust-docs", "--toolchain", "stable-" + host}) {
		t.Errorf("docs command %v", docs.Command)
	}
	wasm := byName(t, items, "rust-target", "wasm32-unknown-unknown target (stable)")
	if !slices.Equal(wasm.Command, []string{rustup, "target", "remove", "wasm32-unknown-unknown", "--toolchain", "stable-" + host}) || len(wasm.Files) != 1 {
		t.Errorf("target %+v", wasm)
	}
	m := Measure(wasm)
	if m.Result.Size.Allocated <= 0 {
		t.Error("component not measured from its manifest")
	}
	for _, it := range items {
		if it.Kind == "rust-target" && strings.Contains(it.Name, host) {
			t.Error("offered the host's own standard library")
		}
	}
	ran, _ := filepath.Glob(filepath.Join(home, ".rustup", "toolchains", "*", "bin", "*.ran"))
	if len(ran) > 0 {
		t.Errorf("ran toolchain binaries: %v", ran)
	}
}

func TestRustupToolchainEnvAndBrokenSettings(t *testing.T) {
	env, home := machine(t, map[string]string{"stable-" + host: "1.99.1", "1.80.0-" + host: "1.80.0"}, map[string]string{"RUSTUP_TOOLCHAIN": "1.80.0"})
	items, _ := globals(env, idle)
	want(t, byName(t, items, "toolchain", "toolchain 1.80.0"), Protected, ReasonCurrent)

	os.WriteFile(filepath.Join(home, ".rustup", "settings.toml"), []byte("default_toolchain = [broken"), 0o644)
	items, warns := globals(env, idle)
	if len(warns) == 0 {
		t.Error("no warning for unreadable settings")
	}
	for _, it := range items {
		if it.Kind == "toolchain" {
			want(t, it, Protected, ReasonUnknownFormat)
		}
	}
}

func TestRustupMissing(t *testing.T) {
	env, home := machine(t, map[string]string{"1.80.0-" + host: "1.80.0"}, nil)
	os.Remove(filepath.Join(home, ".cargo", "bin", "rustup"))
	items, _ := globals(env, idle)
	it := byName(t, items, "toolchain", "toolchain 1.80.0")
	want(t, it, Protected, ReasonNotInstalled)
	if len(it.Command) != 0 || !strings.Contains(it.Note, "rustup toolchain uninstall") {
		t.Errorf("item %+v", it)
	}
}

func TestCargoInstalls(t *testing.T) {
	env, home := machine(t, nil, nil)
	items, _ := globals(env, idle)
	rg := byName(t, items, "cargo-install", "ripgrep 14.1.1")
	want(t, rg, Review)
	ch := filepath.Join(home, ".cargo")
	if !slices.Equal(rg.Command, []string{filepath.Join(ch, "bin", "cargo"), "uninstall", "--root", ch, "ripgrep"}) || rg.Path != filepath.Join(ch, "bin", "rg") {
		t.Errorf("install %+v", rg)
	}
	for _, it := range items {
		if strings.HasPrefix(it.Name, "gone") {
			t.Error("listed an install whose binary is missing")
		}
	}
	os.WriteFile(filepath.Join(ch, ".crates2.json"), []byte("{not json"), 0o644)
	if _, warns := globals(env, idle); len(warns) == 0 {
		t.Error("no warning for unreadable install records")
	}
}

func TestBusyToolsMakeItemsLive(t *testing.T) {
	env, _ := machine(t, map[string]string{"1.80.0-" + host: "1.80.0"}, nil)
	busy := &procs.Table{CwdKnown: true, Procs: []procs.Proc{{PID: 1, Command: "cargo"}, {PID: 2, Command: "rustup"}}}
	items, _ := globals(env, busy)
	want(t, byName(t, items, "registry-src", "crates.io sources"), Live, ReasonToolRunning)
	want(t, byName(t, items, "toolchain", "toolchain 1.80.0"), Live, ReasonToolRunning)
	want(t, byName(t, items, "cargo-install", "ripgrep 14.1.1"), Live, ReasonToolRunning)
}

func TestSharedTargetFromEnv(t *testing.T) {
	home := testfix.Real(t, t.TempDir())
	shared := filepath.Join(home, "build", "cargo-target")
	testfix.Write(t, shared, testfix.Target(map[string]string{}, "."))
	notCargo := filepath.Join(home, "build", "other")
	testfix.Write(t, notCargo, map[string]string{"a": "x"})
	env := Env{Home: home, GOOS: runtime.GOOS, Query: query, LookPath: func(string) (string, error) { return "", errors.New("no") },
		Getenv: func(k string) string {
			return map[string]string{"CARGO_TARGET_DIR": shared, "CARGO_BUILD_BUILD_DIR": notCargo}[k]
		}}
	items, _ := globals(env, idle)
	if len(items) != 1 {
		t.Fatalf("items %d", len(items))
	}
	want(t, items[0], Review, ReasonSharedTarget)
	if items[0].Kind != "shared-target" || items[0].Path != shared {
		t.Errorf("item %+v", items[0])
	}
}

func TestDeepInHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "someone")
	for p, ok := range map[string]bool{
		filepath.Join(home, ".cargo", "registry"): true, filepath.Join(home, ".cargo"): false, home: false,
		filepath.Join(filepath.Dir(home), "x", "y"): false, filepath.Join("rel", "a"): false,
	} {
		if DeepInHome(p, home) != ok {
			t.Errorf("DeepInHome(%q) != %v", p, ok)
		}
	}
}
