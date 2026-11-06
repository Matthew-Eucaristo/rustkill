# rustkill machine contract (schema_version 1)

rustkill uses the same envelope as the other tools of the family (`fell`, `rustkill`, `flutterkill`), so an umbrella such as `decruft` can show what rustkill sees and hand removals back to it without bypassing its checks.

## Principles

1. **The tool that knows the domain does the removal.** A caller never deletes paths from a scan; it hands a plan back, and rustkill re-reads every item and re-applies its own rules.
2. **Plans are pinned to what the user saw.** Each item carries a `fingerprint` of its safety-relevant state (path, directory identity, git evidence, level, reasons, command). If it changed, the item is refused as stale.
3. **Evidence, not just bytes.** Items carry the reasons behind their level.
4. **No usernames in output.** Paths under home are written as `~/…`.
5. **Additive evolution.** Within a schema version, fields, reason codes and exit codes may be added but never renamed, removed or repurposed. Consumers ignore unknown fields.

## Discovery: `rustkill capabilities --json`

Returns `schema_version`, `tool {name: "rustkill", version, ecosystem: "rust"}`, `commands`, `actions` (`trash`, `delete`, `command`), `categories` (`project`, `cache`, `global`, `runtime`), `safety_levels` (`safe`, `review`, `protected`, `live`), `reason_codes` (`{code, level, effect}`), `artifact_kinds` (every item kind with its category, path, level, action and how it comes back), `exit_codes` and the OS's name for the trash.

## Scan: `rustkill scan --json [--no-global] [root...]`

Read-only. Roots default to `~/dev`, else the current directory. Unless `--no-global`, machine-wide items (Cargo's caches, shared build directories, sccache, `cargo install` tools, rustup toolchains and components) are included.

```json
{
  "schema_version": 1,
  "tool": { "name": "rustkill", "version": "0.1.0", "ecosystem": "rust" },
  "generated_at": "2026-10-27T05:00:00Z",
  "roots": ["~/dev"],
  "warnings": [],
  "size_note": "allocated blocks like du; hard links counted once; shared = …",
  "summary": {
    "items": 3, "projects": 1,
    "bytes": { "allocated": 1650000000, "shared": 0, "exclusive": 1650000000, "file_count": 52000 },
    "safe_bytes": { "allocated": 1550000000, "shared": 0, "exclusive": 1550000000, "file_count": 50000 },
    "by_safety_level": { "safe": 2, "review": 1 },
    "by_category": { "project": 2, "cache": 1 }
  },
  "items": [ /* Item */ ]
}
```

### Item

| Field | Meaning |
| --- | --- |
| `id` | Stable id (`tgt-…` build directory under a root, `glb-…` machine-wide, `cmp-…` toolchain component or target). Use it in plans. |
| `fingerprint` | Hash of the safety-relevant state. Sizes and times are excluded, so a build writing into `target` does not invalidate a plan. |
| `kind` | `target`, `shared-target`, `registry-src`, `registry-cache`, `registry-index`, `git-checkouts`, `git-db`, `sccache`, `rustup-downloads`, `cargo-install`, `toolchain`, `rust-docs`, `rustc-dev`, `rust-target` (see `artifact_kinds`). |
| `category` | `project`, `cache`, `global` or `runtime`. |
| `name`, `path` | Display name and path (`~/…`). |
| `project` | For a project's build directory: `{path, name, workspace, workspace_members, edition, toolchain, tools, repo, in_linked_worktree, last_activity{at, source, age_days}, processes_checked, processes[{pid, command}]}`; `null` otherwise. `workspace` is true for a `[workspace]` root (its members' activity counts for it); `toolchain` is the channel `rust-toolchain(.toml)` pins. |
| `tool` | Owning tool of a machine-wide item (`cargo`, `rustup`, `sccache`). |
| `action` | `command` (the owning tool's cleanup, which deletes by design) or `trash` (moved to the trash; deleted with `--permanent`). |
| `command` | The exact argv rustkill would run, when `action` is `command` (`rustup toolchain uninstall`, `rustup component remove`, `rustup target remove`, `cargo uninstall --root`). |
| `regenerate`, `explain`, `note` | How it comes back, what it is, and anything worth knowing (e.g. which projects pin a toolchain). |
| `measured`, `bytes` | `{allocated, shared, exclusive, file_count}`. `shared` is held by hard links outside the item or, on macOS, APFS clone/snapshot blocks; `exclusive` is what removal frees right away. |
| `last_used` | `{at, source, age_days}`: `last build` (newest file in a build directory), `atime` (when a toolchain's rustc/cargo or an installed binary last ran, on macOS/Linux; rustkill never runs them itself) or `modified`. A hint; only `last build` affects a level (built in the last 7 days → `recent`). |
| `git` | For build directories inside a repository: `{ignored, tracked_files, error}`. |
| `safety` | `{level, reasons[{code, level, detail}]}`. |
| `errors` | Non-fatal problems reading the item. |

### Levels and reason codes

| Level | Meaning |
| --- | --- |
| `safe` | Rebuildable and idle; may be bulk-selected (`--yes` takes safe project items only). |
| `review` | Allowed when chosen explicitly (TUI selection or a plan), never in bulk. |
| `protected` | rustkill will not act on it. |
| `live` | In use right now; nothing is allowed. |

`capabilities --json` lists every code with its effect. Current codes: `in_use`, `building`, `tool_running` (live); `not_ignored`, `tracked`, `git_error`, `nested_repo`, `not_installed`, `unknown_format`, `current`, `broad_path` (protected); `recent`, `unknown_age`, `liveness_unknown`, `deployable`, `catalog_review`, `shared_target`, `old_cargo`, `worktree`, `partial_read`, `ignored_project` (review).

### Streaming: `rustkill scan --ndjson`

```
{"type":"header","schema_version":1,"tool":{…},"roots":["~/dev"],"warnings":[],"items":3}
{"type":"item", …Item…}      ← one per item, as soon as it is measured
{"type":"summary", …Summary…}
```

## Plan and apply: `rustkill apply --plan <file|-> [--dry-run] [--permanent] [--json] [root...]`

```json
{ "schema_version": 1, "items": [ { "id": "art-c8922447aaf0", "fingerprint": "fp-7bc4c3f502b81124" } ] }
```

- The action follows from the item: its official command, or a move to the trash (`--permanent`: delete).
- Unknown fields are rejected, so a plan cannot smuggle options; consent comes only from flags on the command line.
- Items named in a plan count as chosen explicitly, so `review` items are allowed; `protected` and `live` never are.
- Pass the same roots as the scan. Items run one at a time: re-read, fingerprint check, level check, guards, then the action.

### Result (`--json`)

```json
{
  "schema_version": 1,
  "tool": { "name": "rustkill", "version": "0.1.0", "ecosystem": "rust" },
  "mode": "trash",
  "results": [
    { "id": "tgt-c8922447aaf0", "kind": "target", "action": "trash", "path": "~/dev/shop/target",
      "status": "done", "estimated_bytes": 831200000, "freed_bytes_measured": 0,
      "restore_hint": "restore from the Trash, or run: cargo build (from scratch, so the first build is slow)" }
  ],
  "summary": { "done": 1, "dry_run": 0, "refused": 0, "failed": 0, "trashed_bytes": 831200000,
               "command_bytes": 0, "deleted_bytes": 0, "estimated_bytes": 831200000, "freed_bytes_measured": 0 }
}
```

- `status`: `done`, `dry_run`, `refused` (rules said no, or the item is stale or gone; nothing changed; `reasons` and `error` say why), `failed` (an error after the checks passed).
- `estimated_bytes` (and `trashed_bytes`, `command_bytes`, `deleted_bytes`) is what removing the item frees according to the scan: its own blocks, without blocks it shares with a store clone or a hard link.
- `freed_bytes_measured` is the measured change in free space (`-1` unknown). After a move to the trash it stays near zero until the trash is emptied; that is reported, not hidden.

`rustkill --yes [--older-than D] [--json]` prints the same result document for its headless run.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every item done (or dry run). |
| 1 | Error before acting. |
| 2 | Usage error (bad flags, invalid plan). |
| 3 | At least one item refused, none failed. |
| 4 | At least one item failed. |
