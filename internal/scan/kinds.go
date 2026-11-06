package scan

// KindInfo documents one item kind for `capabilities`.
type KindInfo struct {
	Kind       string `json:"kind"`
	Category   string `json:"category"`
	Path       string `json:"path"`
	Level      string `json:"level"`
	Action     string `json:"action"`
	Regenerate string `json:"regenerate"`
}

// Kinds is the catalog, in display order. Sources for each entry are listed
// in docs/rules.md.
var Kinds = []KindInfo{
	{"target", "project", "<project>/target (any folder cargo tagged as its build output)", "safe", "trash", "cargo build"},
	{"shared-target", "cache", "CARGO_TARGET_DIR, build.target-dir, build.build-dir, $CARGO_HOME/build/*", "review", "trash", "the next cargo build of every project that uses it"},
	{"registry-src", "cache", "$CARGO_HOME/registry/src/<registry>", "safe", "trash", "cargo re-extracts crates from registry/cache"},
	{"registry-cache", "cache", "$CARGO_HOME/registry/cache/<registry>", "review", "trash", "cargo downloads the crates again"},
	{"registry-index", "cache", "$CARGO_HOME/registry/index/<registry>", "review", "trash", "cargo downloads the index entries again"},
	{"git-checkouts", "cache", "$CARGO_HOME/git/checkouts", "safe", "trash", "cargo checks them out again from git/db"},
	{"git-db", "cache", "$CARGO_HOME/git/db", "review", "trash", "cargo clones the git dependencies again"},
	{"sccache", "cache", "SCCACHE_DIR or sccache's default cache folder", "review", "trash", "sccache fills it again as you build"},
	{"rustup-downloads", "cache", "$RUSTUP_HOME/downloads, $RUSTUP_HOME/tmp", "safe", "trash", "rustup downloads again what it needs"},
	{"cargo-install", "global", "$CARGO_HOME/bin/<binary> (from .crates2.json)", "review", "command: cargo uninstall --root $CARGO_HOME <package>", "cargo install <package>"},
	{"toolchain", "runtime", "$RUSTUP_HOME/toolchains/<toolchain>", "review", "command: rustup toolchain uninstall <toolchain>", "rustup toolchain install <toolchain>"},
	{"rust-docs", "runtime", "<toolchain>/share/doc/rust/html", "review", "command: rustup component remove rust-docs --toolchain <toolchain>", "rustup component add rust-docs"},
	{"rustc-dev", "runtime", "files of the rustc-dev component", "review", "command: rustup component remove rustc-dev --toolchain <toolchain>", "rustup component add rustc-dev"},
	{"rust-target", "runtime", "<toolchain>/lib/rustlib/<target>", "review", "command: rustup target remove <target> --toolchain <toolchain>", "rustup target add <target>"},
}

// noDescend are directory names the project walk never enters: they hold
// no projects of their own, only dependencies, caches and other tools' data.
// Cargo build directories are recognised by their tag, not by name.
var noDescend = map[string]bool{
	".git": true, "node_modules": true, ".svn": true, ".hg": true, ".jj": true, ".Trash": true,
	".venv": true, "__pycache__": true, ".gradle": true, "Pods": true, "DerivedData": true,
	".next": true, ".turbo": true, ".cache": true, ".dart_tool": true, ".pnpm-store": true,
}
