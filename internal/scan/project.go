package scan

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Matthew-Eucaristo/rustkill/internal/procs"
)

// Project is a directory with a Cargo.toml: a package, a workspace root, or
// both.
type Project struct {
	ID   string
	Path string
	Name string

	Edition   string
	Workspace bool     // declares [workspace]
	Members   int      // workspace members listed (globs count once)
	Tools     []string // frameworks in use, for display
	Toolchain string   // channel pinned by rust-toolchain(.toml), if any

	Repo     string // git work tree root; "" outside git
	Worktree bool   // a linked git worktree (fell owns the worktree itself)
	// GitIgnored: git ignores the project folder itself, so it is not
	// source (a vendored crate, a copy, test data).
	GitIgnored bool

	LastActivity   time.Time
	ActivitySource string // "commit", "files" or ""

	Live     []procs.Proc // Rust processes working in it
	CwdKnown bool

	Errors []string

	deps   map[string]bool
	names  map[string]bool // top-level entries
	newest time.Time       // newest source file seen while walking
}

// cargoManifest is the part of Cargo.toml rustkill reads.
type cargoManifest struct {
	Package *struct {
		Name    string `toml:"name"`
		Edition any    `toml:"edition"` // a string, or {workspace = true}
	} `toml:"package"`
	Workspace *struct {
		Members      []string       `toml:"members"`
		Dependencies map[string]any `toml:"dependencies"`
		Package      *struct {
			Edition string `toml:"edition"`
		} `toml:"package"`
	} `toml:"workspace"`
	Dependencies      map[string]any `toml:"dependencies"`
	DevDependencies   map[string]any `toml:"dev-dependencies"`
	BuildDependencies map[string]any `toml:"build-dependencies"`
}

// toolCatalog names a project by the framework its dependencies show, for
// display and grouping only; nothing is decided by it.
var toolCatalog = []struct{ name, dep string }{
	{"Tauri", "tauri"}, {"Bevy", "bevy"}, {"Leptos", "leptos"}, {"Dioxus", "dioxus"}, {"Yew", "yew"},
	{"egui", "eframe"}, {"egui", "egui"}, {"Iced", "iced"}, {"Slint", "slint"}, {"GPUI", "gpui"},
	{"axum", "axum"}, {"Actix Web", "actix-web"}, {"Rocket", "rocket"}, {"Loco", "loco-rs"},
	{"PyO3", "pyo3"}, {"napi-rs", "napi"}, {"wasm-bindgen", "wasm-bindgen"}, {"Embassy", "embassy-executor"},
	{"Solana", "anchor-lang"}, {"Tokio", "tokio"},
}

func newProject(dir string, names map[string]bool) *Project {
	p := &Project{ID: id("prj", dir), Path: dir, Name: filepath.Base(dir), names: names, deps: map[string]bool{}}
	b, err := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	if err != nil {
		p.Errors = append(p.Errors, "Cargo.toml unreadable: "+err.Error())
		return p
	}
	var m cargoManifest
	if _, err := toml.Decode(string(b), &m); err != nil {
		p.Errors = append(p.Errors, "Cargo.toml is not valid TOML")
		return p
	}
	if m.Package != nil {
		if m.Package.Name != "" {
			p.Name = m.Package.Name
		}
		if e, ok := m.Package.Edition.(string); ok {
			p.Edition = e
		}
	}
	for _, deps := range []map[string]any{m.Dependencies, m.DevDependencies, m.BuildDependencies} {
		for d := range deps {
			p.deps[d] = true
		}
	}
	if w := m.Workspace; w != nil {
		p.Workspace, p.Members = true, len(w.Members)
		for d := range w.Dependencies {
			p.deps[d] = true
		}
		if p.Edition == "" && w.Package != nil {
			p.Edition = w.Package.Edition
		}
	}
	for _, t := range toolCatalog {
		if p.deps[t.dep] && !slices.Contains(p.Tools, t.name) {
			p.Tools = append(p.Tools, t.name)
		}
	}
	p.Toolchain = toolchainPin(dir, names)
	return p
}

// toolchainPin reads the channel a rust-toolchain.toml or legacy
// rust-toolchain file pins, the way rustup does.
func toolchainPin(dir string, names map[string]bool) string {
	for _, f := range []string{"rust-toolchain.toml", "rust-toolchain"} {
		if !names[f] {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || len(b) > 64<<10 {
			return ""
		}
		var t struct {
			Toolchain struct {
				Channel string `toml:"channel"`
			} `toml:"toolchain"`
		}
		if _, err := toml.Decode(string(b), &t); err == nil && t.Toolchain.Channel != "" {
			return t.Toolchain.Channel
		}
		if line := strings.TrimSpace(string(b)); f == "rust-toolchain" && line != "" && !strings.ContainsAny(line, "\n=[") {
			return line
		}
		return ""
	}
	return ""
}

// Framework is the first detected framework, for grouping.
func (p *Project) Framework() string {
	if len(p.Tools) == 0 {
		return "Rust"
	}
	return p.Tools[0]
}
