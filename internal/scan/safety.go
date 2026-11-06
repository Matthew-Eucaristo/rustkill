package scan

import (
	"errors"
	"slices"
)

// Level is how freely an item may be removed.
type Level string

const (
	Safe      Level = "safe"      // rebuildable; may be preselected
	Review    Level = "review"    // allowed, never preselected: it costs something or is worth a look
	Protected Level = "protected" // shown for context; rustkill will not remove it
	Live      Level = "live"      // in use right now; nothing is allowed
)

func (l Level) rank() int { return slices.Index([]Level{Safe, Review, Protected, Live}, l) }

// Allowed reports whether rustkill may act on an item at this level.
func (l Level) Allowed() bool { return l == Safe || l == Review }

// Reason codes, part of the JSON contract.
const (
	ReasonInUse           = "in_use"           // live: a Rust process (cargo, rustc, rust-analyzer, …) works in the project
	ReasonBuilding        = "building"         // live: cargo holds the build directory's lock right now
	ReasonToolRunning     = "tool_running"     // live: cargo or rustup is running
	ReasonNotIgnored      = "not_ignored"      // protected: git does not ignore it
	ReasonTracked         = "tracked"          // protected: git tracks files inside
	ReasonGitError        = "git_error"        // protected: git could not answer
	ReasonNestedRepo      = "nested_repo"      // protected: a .git lies inside
	ReasonNotInstalled    = "not_installed"    // protected: the owning tool is missing, so its official command cannot run
	ReasonUnknownFormat   = "unknown_format"   // protected: a layout rustkill does not recognise
	ReasonCurrent         = "current"          // protected: the default or active toolchain
	ReasonBroadPath       = "broad_path"       // protected: the folder is home itself, a top folder of it, or outside it
	ReasonRecent          = "recent"           // review: project changed or built in the last 7 days
	ReasonUnknownAge      = "unknown_age"      // review: activity could not be determined
	ReasonLivenessUnknown = "liveness_unknown" // review: running processes could not be checked
	ReasonDeployable      = "deployable"       // review: holds installers or bundles you built
	ReasonCatalogReview   = "catalog_review"   // review: the catalog marks it for a look
	ReasonSharedTarget    = "shared_target"    // review: a build directory several projects share
	ReasonOldCargo        = "old_cargo"        // review: an installed older Cargo may still read it
	ReasonWorktree        = "worktree"         // review: inside a linked git worktree, which an agent or app may still use
	ReasonPartialRead     = "partial_read"     // review: some files inside could not be read, so size and activity are incomplete
	ReasonIgnoredProject  = "ignored_project"  // review: git ignores the project folder itself (a vendored copy or data, not source)
)

// Reason explains one piece of evidence.
type Reason struct {
	Code   string `json:"code"`
	Level  Level  `json:"level"`
	Detail string `json:"detail"`
}

// Safety is the level and the evidence behind it.
type Safety struct {
	Level   Level    `json:"level"`
	Reasons []Reason `json:"reasons"`
}

func (s *Safety) add(code string, l Level, detail string) {
	for _, r := range s.Reasons {
		if r.Code == code {
			return
		}
	}
	s.Reasons = append(s.Reasons, Reason{code, l, detail})
	if l.rank() > s.Level.rank() {
		s.Level = l
	}
}

// Has reports whether a reason code is present.
func (s Safety) Has(code string) bool {
	for _, r := range s.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}

// ReasonInfo documents a reason code for `capabilities`.
type ReasonInfo struct {
	Code   string `json:"code"`
	Level  Level  `json:"level"`
	Effect string `json:"effect"`
}

// Reasons lists every code rustkill can emit.
var Reasons = []ReasonInfo{
	{ReasonInUse, Live, "blocks every action: a Rust process (cargo, rustc, rust-analyzer, bacon, …) works in the project"},
	{ReasonBuilding, Live, "blocks every action: cargo holds the build directory's lock (a build or check is running)"},
	{ReasonToolRunning, Live, "blocks while cargo or rustup runs, so a cache or toolchain is not pulled from under a build"},
	{ReasonNotIgnored, Protected, "blocks: git does not ignore the directory, so it may be source"},
	{ReasonTracked, Protected, "blocks: git tracks files inside"},
	{ReasonGitError, Protected, "blocks: git could not confirm it is ignored and untracked"},
	{ReasonNestedRepo, Protected, "blocks: another repository lies inside"},
	{ReasonNotInstalled, Protected, "blocks: rustup or cargo is not installed, so the official command cannot run; the item shows the command"},
	{ReasonUnknownFormat, Protected, "blocks: a file or layout rustkill does not recognise (rustup settings, Cargo install records)"},
	{ReasonCurrent, Protected, "blocks: rustup's default toolchain, or the one RUSTUP_TOOLCHAIN selects"},
	{ReasonBroadPath, Protected, "blocks: the folder is home itself, a top-level folder of it, or outside it"},
	{ReasonRecent, Review, "not preselected: the project changed or was built in the last 7 days"},
	{ReasonUnknownAge, Review, "not preselected: the project's last activity is unknown"},
	{ReasonLivenessUnknown, Review, "not preselected: running processes could not be checked"},
	{ReasonDeployable, Review, "not preselected: holds installers or app bundles you built (target/release/bundle)"},
	{ReasonCatalogReview, Review, "not preselected: removable, but it costs a download or a reinstall"},
	{ReasonSharedTarget, Review, "not preselected: several projects build into it, and all of them start from scratch afterwards"},
	{ReasonOldCargo, Review, "not preselected: an installed toolchain older than Rust 1.85 still reads this registry folder"},
	{ReasonWorktree, Review, "not preselected: it sits in a linked git worktree that an agent or app may still be using"},
	{ReasonPartialRead, Review, "not preselected: some files inside could not be read, so its size and last activity may be incomplete"},
	{ReasonIgnoredProject, Review, "not preselected: git ignores the project folder itself, so it may be a vendored copy or data rather than source"},
}

var (
	errNotDir = errors.New("is not a directory")
)
