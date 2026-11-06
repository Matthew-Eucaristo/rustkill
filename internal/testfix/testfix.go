// Package testfix builds throwaway Rust projects, Cargo build directories
// and git repositories for tests. Nothing here touches real user directories.
package testfix

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Old is a time well outside every "recent" window.
var Old = time.Now().Add(-120 * 24 * time.Hour)

// Write creates files (path → content) below root, making parents.
func Write(t testing.TB, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Age sets every file and directory below root to time at.
func Age(t testing.TB, root string, at time.Time) {
	t.Helper()
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			os.Chtimes(p, at, at)
		}
		return nil
	})
}

// Git runs git in dir with a fixed identity and commit date.
func Git(t testing.TB, dir string, at time.Time, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	date := at.Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// Repo initialises a repository at dir, commits everything not ignored at
// time at, and ages the files to match.
func Repo(t testing.TB, dir string, at time.Time) {
	t.Helper()
	Git(t, dir, at, "init", "-q", "-b", "main")
	Git(t, dir, at, "add", "-A")
	Git(t, dir, at, "commit", "-q", "-m", "init")
	Age(t, dir, at)
}

// Real resolves symlinks in a temp dir (macOS /var → /private/var).
func Real(t testing.TB, dir string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// CargoTag is the CACHEDIR.TAG cargo writes into its build directories.
const CargoTag = "Signature: 8a477f597d28d172789f06886806bc55\n# This file is a cache directory tag created by cargo.\n# For information about cache directory tags see https://bford.info/cachedir/\n"

// Crate is a minimal package with the .gitignore cargo new writes.
func Crate(name string) map[string]string {
	return map[string]string{
		"Cargo.toml":  "[package]\nname = \"" + name + "\"\nversion = \"0.1.0\"\nedition = \"2024\"\n\n[dependencies]\naxum = \"0.8\"\n",
		"src/main.rs": "fn main() {}\n",
		".gitignore":  "/target\n",
	}
}

// Target adds the files of a cargo build directory at rel to files.
func Target(files map[string]string, rel string) map[string]string {
	for k, v := range map[string]string{
		"CACHEDIR.TAG": CargoTag, ".rustc_info.json": "{}", "debug/.cargo-lock": "",
		"debug/.fingerprint/app-1/bin-app": "x", "debug/deps/libserde-1.rlib": "rlib", "debug/app": "binary",
	} {
		files[rel+"/"+k] = v
	}
	return files
}
