package scan

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// rustupSettings is the part of $RUSTUP_HOME/settings.toml rustkill reads.
type rustupSettings struct {
	Version     string            `toml:"version"`
	Default     string            `toml:"default_toolchain"`
	DefaultHost string            `toml:"default_host_triple"`
	Overrides   map[string]string `toml:"overrides"`
}

// toolchains lists rustup's toolchains with the components and targets
// worth a look inside them. The default toolchain and the one
// RUSTUP_TOOLCHAIN selects are protected; the rest are offered for review
// with rustup's own uninstall.
func toolchains(t *tools) ([]*Item, []string) {
	dirs := t.toolchainDirs()
	if len(dirs) == 0 {
		return nil, nil
	}
	exe := t.look("rustup")
	var set rustupSettings
	settingsOK := true
	if b, err := readSmall(filepath.Join(t.rustupHome, "settings.toml"), 1<<20); err == nil {
		if _, err := toml.Decode(string(b), &set); err != nil {
			settingsOK = false
		}
	} else if !os.IsNotExist(err) {
		settingsOK = false
	}
	var warns []string
	if !settingsOK {
		warns = append(warns, "could not read rustup's settings.toml, so its default toolchain is unknown; toolchains are shown but protected")
	}
	active := t.env.Getenv("RUSTUP_TOOLCHAIN")
	var items []*Item
	for _, d := range dirs {
		path := filepath.Join(t.rustupHome, "toolchains", d)
		host := toolchainHost(path, d, set.DefaultHost)
		short := strings.TrimSuffix(d, "-"+host)
		if host == "" {
			short = d
		}
		it := newGlobal(CatRuntime, "toolchain", "toolchain "+short, "rustup", path, []string{"rustup", "toolchain", "uninstall", d}, exe)
		it.Toolchain = d
		if v := toolchainVersion(path, d); v != [3]int{} {
			it.Version = versionString(v)
			if short != it.Version {
				it.Name += " (" + it.Version + ")"
			}
		}
		it.Explain = "A Rust toolchain rustup installed: rustc, cargo, the standard library and its components."
		it.Regenerate = "rustup toolchain install " + short
		it.LastUsed, it.LastUsedSource = toolchainUsed(t, path)
		var overriddenFor []string
		for dir, tc := range set.Overrides {
			if sameToolchain(tc, d, host) && isDir(dir) {
				overriddenFor = append(overriddenFor, dir)
			}
		}
		switch {
		case !settingsOK:
			it.Safety.add(ReasonUnknownFormat, Protected, "rustup's settings.toml could not be read, so rustkill cannot tell whether this is the default")
		case active != "" && sameToolchain(active, d, host):
			it.Safety.add(ReasonCurrent, Protected, "RUSTUP_TOOLCHAIN selects it")
		case set.Default != "" && sameToolchain(set.Default, d, host):
			it.Safety.add(ReasonCurrent, Protected, "rustup's default toolchain")
		default:
			it.Safety.add(ReasonCatalogReview, Review, "check that no project still needs it")
		}
		if len(overriddenFor) > 0 {
			it.Note = "rustup override for " + strings.Join(overriddenFor, ", ")
		}
		items = append(items, it)
		items = append(items, components(t, path, d, short, host, exe)...)
	}
	return items, warns
}

func exeName(t *tools, n string) string {
	if t.env.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

func versionString(v [3]int) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// toolchainVersion is the Rust release of a toolchain: the rustc version
// in the channel manifest rustup keeps in it, else the version its folder
// is named after (1.82.0-<host>); zero when neither says.
func toolchainVersion(path, dir string) [3]int {
	if f, err := os.Open(filepath.Join(path, "lib", "rustlib", "multirust-channel-manifest.toml")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		in := false
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			switch {
			case strings.HasPrefix(l, "["):
				in = l == "[pkg.rustc]"
			case in && strings.HasPrefix(l, "version"):
				if _, v, ok := strings.Cut(l, "="); ok {
					return parseVersion(v)
				}
			}
		}
	}
	if m := dirVersion.FindStringSubmatch(dir); m != nil {
		return parseVersion(m[1])
	}
	return [3]int{}
}

var dirVersion = regexp.MustCompile(`^(\d+\.\d+\.\d+)-`)

// toolchainHost is the toolchain's host triple: the suffix of its rustc
// component (rustc-<host>), else rustup's default host when the folder name
// ends with it.
func toolchainHost(path, dir, defaultHost string) string {
	for _, c := range installedComponents(path) {
		if h, ok := strings.CutPrefix(c, "rustc-"); ok && strings.HasSuffix(dir, "-"+h) {
			return h
		}
	}
	if defaultHost != "" && strings.HasSuffix(dir, "-"+defaultHost) {
		return defaultHost
	}
	return ""
}

// sameToolchain: name (as settings.toml, RUSTUP_TOOLCHAIN or an override
// writes it, with or without the host) names the toolchain folder dir.
func sameToolchain(name, dir, host string) bool {
	name = strings.TrimSpace(name)
	return name != "" && (name == dir || (host != "" && name+"-"+host == dir))
}

// pinMatches: a rust-toolchain channel asks for the toolchain folder dir.
// "1.80" asks for the newest 1.80.x, so it matches 1.80.0, 1.80.1, ….
func pinMatches(channel, dir string) bool {
	if channel == "" {
		return false
	}
	if channel == dir || strings.HasPrefix(dir, channel+"-") && !strings.HasPrefix(strings.TrimPrefix(dir, channel+"-"), "20") {
		return true // "stable" ↔ stable-<host>, but "nightly" never ↔ nightly-2024-…
	}
	if strings.Count(channel, ".") == 1 && strings.HasPrefix(dir, channel+".") {
		return true
	}
	return false
}

// toolchainUsed: running rustc or cargo updates the binary's access time on
// macOS and Linux, which hints when the toolchain was last used.
func toolchainUsed(t *tools, path string) (time.Time, string) {
	var at time.Time
	for _, b := range []string{"rustc", "cargo"} {
		if fi, err := os.Stat(filepath.Join(path, "bin", exeName(t, b))); err == nil {
			if a := accessTime(fi); a.After(at) {
				at = a
			}
		}
	}
	if at.IsZero() {
		return at, ""
	}
	return at, "atime"
}

// installedComponents reads lib/rustlib/components, rustup's list of what
// it installed into the toolchain.
func installedComponents(path string) []string {
	b, err := readSmall(filepath.Join(path, "lib", "rustlib", "components"), 64<<10)
	if err != nil {
		return nil
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if c := strings.TrimSpace(sc.Text()); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// manifestFiles are the files and folders rustup installed for one
// component, from its manifest, made absolute. Entries that would leave the
// toolchain are dropped.
func manifestFiles(path, component string) []string {
	b, err := readSmall(filepath.Join(path, "lib", "rustlib", "manifest-"+component), 16<<20)
	if err != nil {
		return nil
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		_, rel, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if !ok || rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			continue
		}
		out = append(out, filepath.Join(path, filepath.FromSlash(rel)))
	}
	return out
}

// components offers the parts of a toolchain that are large and rarely
// needed: the offline docs (rust-docs), rustc's own libraries (rustc-dev)
// and standard libraries for other targets.
func components(t *tools, path, dir, short, host, exe string) []*Item {
	if host == "" {
		return nil
	}
	var items []*Item
	for _, c := range installedComponents(path) {
		files := manifestFiles(path, c)
		if len(files) == 0 {
			continue
		}
		var it *Item
		switch {
		case c == "rust-docs-"+host:
			it = newGlobal(CatRuntime, "rust-docs", "rust-docs ("+short+")", "rustup", filepath.Join(path, "share", "doc", "rust", "html"),
				[]string{"rustup", "component", "remove", "rust-docs", "--toolchain", dir}, exe)
			it.Explain = "The offline Rust documentation `rustup doc` opens: the book, std and every other Rust book as HTML. docs.rs and doc.rust-lang.org have the same. To leave it out of toolchains you install later: rustup set profile minimal."
			it.Regenerate = "rustup component add rust-docs --toolchain " + short
		case c == "rustc-dev-"+host:
			it = newGlobal(CatRuntime, "rustc-dev", "rustc-dev ("+short+")", "rustup", filepath.Join(path, "lib", "rustlib", host, "lib"),
				[]string{"rustup", "component", "remove", "rustc-dev", "--toolchain", dir}, exe)
			it.Explain = "The compiler's own libraries, needed only to build rustc drivers and tools such as Clippy forks or Miri from source."
			it.Regenerate = "rustup component add rustc-dev --toolchain " + short
		case strings.HasPrefix(c, "rust-std-") && c != "rust-std-"+host:
			target := strings.TrimPrefix(c, "rust-std-")
			it = newGlobal(CatRuntime, "rust-target", target+" target ("+short+")", "rustup", filepath.Join(path, "lib", "rustlib", target),
				[]string{"rustup", "target", "remove", target, "--toolchain", dir}, exe)
			it.Explain = "The standard library for cross-compiling to " + target + "."
			it.Regenerate = "rustup target add " + target + " --toolchain " + short
		default:
			continue
		}
		it.Toolchain, it.Files = dir, files
		it.ID = id("cmp", dir+"\x00"+c)
		it.Safety.add(ReasonCatalogReview, Review, "rustup installs it again in a moment if you need it")
		items = append(items, it)
	}
	return items
}
