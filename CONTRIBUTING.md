# Contributing

## Prerequisites

- Go 1.26.4+
- `golangci-lint` for `make lint` (install with `make deps`)

## Getting started

```bash
git clone https://github.com/fuzzyporpoise/todo.git
cd todo
make build
```

## Before opening a PR

```bash
make check    # fmt, vet, lint, test
```

- Keep PRs focused: one behavior change per PR.
- Update `README.md` and `CONTRIBUTING.md` when behavior, commands, or flags change; docs are part of done.
- Follow the conventions in this file (task file format, state guards, package boundaries).

## Package boundaries

| Package | Does | Imports |
|---------|------|---------|
| `cmd/todo` | CLI command tree (`add`, `init`, `list`, `pickup`, `release`, `remove`, `detail`, `complete`, `reopen`, `bump`, `clear`, `doctor`), flag parsing, all terminal I/O | everything below |
| `internal/todo` | domain logic: task model, Markdown parse/serialize, ID allocation, filtering, note disposition | `internal/fs`, `internal/git` |
| `internal/registry` | machine-local cache of tracked repo folders (`$XDG_CACHE_HOME/todo/registry.json`, falling back to `~/.cache/todo/registry.json`) | stdlib only |
| `internal/git` | git CLI helpers: repo root, remote URL, remote parsing | stdlib only |
| `internal/fs` | filesystem helpers (exists check) | stdlib only |

Control flow: per-repo commands resolve the repo root through `requireRepoConfig()`, which fails hard when `.todo/todo.md` is missing. Global commands (`init`, `list --all`, `doctor`) skip that guard so they work outside a tracked repo.

## Conventions

### The task file is machine-owned

- `.todo/todo.md` is rewritten in full on every mutation, and any line that is not a strict task line is silently dropped. Never hand-write prose, section headers, or comments into it. Implementation notes and design docs belong in `.todo/` as separate Markdown files.
- The task line format is defined by `taskRegex` in `internal/todo/todo.go`. Changing the format means changing `Task.String()` and `taskRegex` together, plus the fixtures in `internal/todo/todo_test.go`.
- `next_id` is a monotonic high-water mark. IDs are never recycled and never decremented, including by `remove` and `complete --clear`. `normalizeHeader` is the single choke point that backfills it on every write, so there is no standalone repair pass.
- Unknown header lines round-trip verbatim through `header.otherLines`. Preserve that behavior: it is what keeps hand-added metadata alive across writes.

### State changes

- Every mutation stamps `last_updated` through the `now` function. Tests stub it with `setNow`; always `defer setNow(time.Now)` after stubbing.
- State guards run before any write: `pickup` and `release` accept only `[o]`, `complete` only `[o]`, `reopen` only `[x]`, and `bump` accepts any status. A failed guard must leave the file untouched.
- Mutations return the serialized line they produced so the CLI can print it without re-reading. `complete --clear` deliberately returns the line as it was before removal.

### Errors and output

- Errors are wrapped with `fmt.Errorf("...: %w", err)` when crossing package boundaries, using context like `"read todo file: %w"`.
- CLI-facing errors go through `exitError`, which returns `cli.Exit(err.Error(), 1)`.
- Mutation APIs take an options struct and return a result struct (`Add(AddOptions) (AddResult, error)`, `Pickup(RefOptions) (Result, error)`, and so on). Read APIs such as `Detail` never write.
- The `--json` structs (`jsonTask`, `jsonDetail`) are stable contracts for scripts and agents. Add fields freely; renaming or removing one is a breaking change worth calling out in the README.

### Companion notes

- Notes live at `.todo/notes/<ID>.md` and are plain Markdown, edited by hand or by the CLI. The binary only deletes a note on explicit request (`remove --note`) or when `clear` identifies it as a disposable work order.
- Note deletion always goes through `fs.RemoveFollowingSymlink`, which removes a symlink's resolved target before the link itself. That is deliberate: notes in this suite are commonly lnk-managed symlinks into a central store, so unlinking alone would orphan the target. Do not swap it for a plain `os.Remove`, and keep the tests in `cmd/todo/cli_test.go` (symlinked note) and `internal/fs` covering it.
- `clear` separates a missing note from an unreadable one: `os.ErrNotExist` means "no note" and the task line is removed, while any other read error leaves the line in place and reports the task as `float`. An unreadable note must never be deleted by accident.
- Every note carries a write-time disposition in its frontmatter (`kind: work-order`, or park record fields `category`/`created`/`source`/`synopsis`). `clear` and `detail --json` both read it, so changes to the markers in `internal/todo/note.go` must update `README.md` and the tests together.
- Summaries longer than 120 runes are truncated on the task line and spilled into a work-order note. Park records are exempt: their `synopsis` carries the full text.

### Tests

- Tests use plain stdlib `testing` with table-driven cases; there is no testify dependency.
- Fixtures are built on disk with `t.TempDir()` and `writeTestTodo`, so tests need no network, no git remote, and no execution environment.
- `internal/registry` tests isolate the cache through the `TODO_REGISTRY` environment variable.

## Implementation notes

- The only external dependencies are `urfave/cli/v3` (plus `urfave/cli-validation`). todo does not import `park/schema`: it defines its own durable note contract and duplicates the park category enum locally (`ParkCategories()` in `internal/todo/note.go`), emitting a park-shaped record only for interop. Keep `internal/*` free of new dependencies where stdlib will do.
- The registry path defaults to `$XDG_CACHE_HOME/todo/registry.json` (or `~/.cache/todo/registry.json`) and is overridable via `TODO_REGISTRY`.
- Version is dual-sourced: the embedded `VERSION` file serves local and `go install` builds, while `main.version`/`main.buildTime` ldflags (set by GoReleaser, and by the Makefile for `make build`) win when present.
- `AGENTS.md` is a private, symlinked file and is not part of this repository. Contributor-facing context belongs in this file and in `README.md`.
