<p align="center">
  <source media="(prefers-color-scheme: dark)" srcset="images/todo-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="images/todo-light.png">
  <img alt="Project Logo" src="images/todo-dark.png" width="128">
</p>

# todo

[![Go Version](https://img.shields.io/github/go-mod/go-version/fuzzyporpoise/todo)](https://go.dev/) [![Build Status](https://img.shields.io/github/actions/workflow/status/fuzzyporpoise/todo/ci.yml?branch=main)](https://github.com/fuzzyporpoise/todo/actions) [![License](https://img.shields.io/github/license/fuzzyporpoise/todo)](./LICENSE)

**An ad-hoc task list that lives in a plain Markdown file, `.todo/todo.md`, inside your git repo.**

Part of the [fuzzyporpoise](https://github.com/fuzzyporpoise) suite of CLI tooling for dotfile and knowledge management.

Manage an ad-hoc task list in a plain Markdown file, `.todo/todo.md`, stored in the root of a git repo. Designed for agents and scripts: everything is a version-controllable text file, with optional companion notes. Every read command has a `--json` mode, task IDs are never recycled, companion notes at `.todo/notes/<ID>.md` carry the full brief for a task, and retired whys live in the repo-local `.todo/archive/`.

## Install

Quick install (downloads the latest release to `/usr/local/bin`):

```sh
curl -sSL https://raw.githubusercontent.com/fuzzyporpoise/todo/main/install.sh | bash
```

The `go install` and source builds require Go 1.26.4+.

Or install via Go:

```sh
go install go.fuzzyporpoise.dev/todo@latest
```

Or build from source:

```sh
git clone https://github.com/fuzzyporpoise/todo.git
cd todo
make build
```

`make install` builds the binary and copies it to `/usr/local/bin`.

## Usage

`todo` operates on the current git repo. Run `todo init` once to create the `.todo/todo.md` file, then manage tasks:

```sh
todo init                                   # create .todo/todo.md if missing and register the repo
todo add "fix the thing"                    # add a task (default priority: med)
todo add -p high "urgent issue"             # add with priority (low|med|high)
todo add -s "summaries also via flag"       # summary via flag
todo add --note-content "body" "task"       # create a note with content (no -n needed)
echo "body" | todo add -n "task"            # ...or read note content from stdin
todo add --note-file ./draft.md "task"      # ...or copy a file's body (its frontmatter is dropped; not moved; no -n needed)
todo add --dry-run -s "x" --note-content "y" # preview the would-be line + note, no write
todo add -n --kind work-order "task"        # note is a disposable work order
todo add -n --synopsis "one line" "task"    # note is a todo-native record (default when -n is given)
todo add -n --category areas --synopsis "one line" --source repo "task"  # note is a park record (interop)
todo add "an over-long summary (over 120 chars) ..."                    # long summaries truncate on the line and spill into a work-order note
todo list                                   # list tasks (alias: todo ls)
todo list --archive                         # compact view of retired notes in .todo/archive
todo list --archive --all                   # ...across all registered repos
todo list --archive --json                  # machine-readable archived-note listing
todo list --state open                      # filter by status: open|progress|done
todo list --stale 5                         # only claimed tasks older than 5 day/s
todo list --sort priority                   # sort by priority, opened, claimed, or age
todo list --sort opened --reverse           # newest first
todo list --json                            # machine-readable JSON output
todo list --all                             # list tasks across all registered repos
todo list --all --json                      # ...with repo_path/repo_project and disposition
todo list --all --sort priority             # sort global results
todo doctor                                 # report stale/unregistered registry entries
todo doctor --all                           # scan every registered repo directory downward
todo doctor --fix                           # drop stale entries and register missing repos
todo doctor --depth 3 /code                 # scan /code downward up to depth 3 for unregistered .todo folders
todo detail TSK-001                         # show task details + 20-line note preview
todo detail --lines 5 TSK-001               # preview first 5 lines of the note
todo detail --no-note TSK-001               # show task details without note preview
todo detail --full TSK-001                  # show the complete note body
todo detail --json TSK-001                  # machine-readable detail output
todo detail --json --full TSK-001           # ...with the complete note body in note_body
todo pickup TSK-001                         # mark a task in progress (adds claimed date)
todo release TSK-001                        # release a picked-up task back to open (drop claim)
todo complete TSK-001                       # mark a task done (drops claimed)
todo complete --clear TSK-001               # remove the task line entirely
todo complete --park TSK-001                # done + print the companion note path
todo archive TSK-001                        # retire a done task: move its note to .todo/archive, remove its line
todo archive TSK-001 --synopsis "the why"   # assert/rewrite the archived one-line relevance why
todo archive TSK-001 --name retry-loop      # name the archived note retry-loop.md (default: <ID>.md)
todo clear                                  # bulk-clear completed tasks by note disposition
todo clear --all                            # ...across all registered repos
todo remove TSK-001                         # remove a task line by ref (any status, e.g. [x])
todo remove --note TSK-001                  # ...and delete its companion note file
todo reopen TSK-001                         # restore a completed [x] task to open [ ]
todo bump TSK-001                           # bump priority up: low->med, med->high, high->(no-op)
todo bump --down TSK-001                    # bump priority down: high->med, med->low, low->(no-op)
```

Task references accept `TSK-001`, `tsk-001`, `001`, `1`, or `#1`.

State rules:

- `pickup` only works on an open `[ ]` task.
- `complete` only works on an in-progress `[o]` task (i.e. one you picked up).
- `release` also only works on an in-progress `[o]` task; it returns it to `[ ]` and drops the claim.
- `remove` works on any status line (including `[x]` done lines) and deletes it; `--note` also deletes the companion note file. When that note is a symlink, the target it points at is deleted first and the link after, so lnk-managed notes leave neither an orphaned target nor a dangling link.
- `reopen` works on a completed `[x]` task and restores it to `[ ]`; it is a no-op if the task is already open.
- `archive` works on a completed `[x]` task that has a companion note. It asserts the note's one-line relevance why (the note's existing `synopsis`, overridable with `--synopsis`; an empty result is an error), writes the note into `.todo/archive/`, removes the task line, and removes the source note entry.
- `bump` works on any task regardless of status; it cycles priority up (`low->med->high->no-op`) or down (`high->med->low->no-op`) via `--down`. No-op at boundaries writes nothing.
- `pickup` records the `claimed:` date; `complete`/`release`/`reopen` drop it.

## Data layout

Tasks are stored as lines under a YAML-like frontmatter header in `.todo/todo.md`:

```
---
project: todo
last_updated: 2026-08-18T10:37
configured: 2026-08-18
legacy_source: none
next_id: TSK-005
---

- [ ] [TSK-001][priority:high][opened:2026-08-01] fix the thing
- [o] [TSK-002][priority:med][opened:2026-08-02][claimed:2026-08-10] refactor parser
- [x] [TSK-003][priority:low][opened:2026-08-03] write docs
```

Status is `[ ]` open, `[o]` in progress, `[x]` done. `claimed:` records when a task was picked up (present only on in-progress tasks). Optional companion notes live at `.todo/notes/<ID>.md`; retired whys live at `.todo/archive/` (see Archive below).

`next_id:` is a monotonic high-water mark: `add` never reuses it, so removing all tasks still lets new tasks resume at the next ID rather than restarting at TSK-001. It is backfilled automatically on any write, so files created before this field existed converge without manual action.

## Registry

Every `todo init` registers the repo in a machine-local JSON cache at `$XDG_CACHE_HOME/todo/registry.json` (falling back to `~/.cache/todo/registry.json`). The registry stores the absolute repo path, project name, git remote URL, parsed host/owner, and a `last_seen` timestamp. `todo list --all` and `todo doctor` use this cache to operate across tracked folders without `cd`ing.

## Note disposition

Every companion note carries a write-time disposition in its frontmatter so clear-time tooling knows whether to preserve, delete, or float it. Todo defines its own note contract and does not import park's schema; a park-shaped record is emitted only for interop.

- **record** (default): todo-native frontmatter with `kind: record`, `created`, `source`, and `synopsis`. Stamped whenever `-n` creates a note without a `--kind` or `--category` flag; `synopsis` defaults to the task summary and `source` to `repo`.
- **work order**: `kind: work-order`, stamped by `--kind work-order`. Disposable.
- **park**: park-native frontmatter with `category`, `created`, `source`, and `synopsis`, stamped by `--category` (interop with the park store). Recognized by the mere presence of `category:`.

### Note creation owns the block

`add` writes exactly one frontmatter block, whatever the body it was handed looks like:

- A body from `--note-content` or stdin that already opens with a block has that block stripped and reconciled: its `synopsis`/`source` fill whichever fields the flags leave silent, and its disposition is adopted when no flag names one (so a body carrying `category: areas` becomes a park record).
- A body declaring a disposition that the flags contradict is an error, never a second stacked block. `--note-content '---\nkind: record\n---' --kind work-order` refuses instead of writing both.
- `--note-file` copies a free-standing doc, so the doc's frontmatter (title, status, and friends) describes the doc and is dropped; only the body crosses into the note.
- Stacked blocks in one body collapse into a single block, which is how the older doubled notes (todo's `kind: record` stapled over an agent-authored `category:` block) heal when their text is fed back in.
- `created` is always the write date, and a body that merely opens with a horizontal rule is treated as prose, never as frontmatter, so no supplied text is ever eaten.

`todo clear` bulk-removes completed `[x]` tasks based on that disposition:

- no note file: remove the task line (nothing to preserve or delete).
- `work-order`: remove the task line and delete the disposable note (symlink-aware, exactly like `remove --note`).
- `park`: remove the task line but keep the park record note.
- `record`: remove the task line but keep the todo-native record note (retire it with `todo archive` to move it into `.todo/archive/`).
- `float` (note exists with no recognized disposition, or the note cannot be read): leave the task line and list it for review. Only a genuinely missing note file counts as "no note", so an unreadable note is never discarded by accident.

`todo clear --all` applies the same rules across every registered repo. The registry is updated by `todo init` and reconciled with `todo doctor`. A registry entry whose directory cannot be checked (for example a permissions problem) fails the run with an error instead of counting as stale, so `doctor --fix` never drops a reachable entry by accident.

Summaries must fit on a single line: embedded newlines are rejected. The same holds for the `synopsis` and `source` note fields, so `add` and `archive` can never write a malformed frontmatter block. Summaries longer than 120 characters are truncated on the task line (with a trailing `...`) and spilled into a `kind: work-order` note so the full text is preserved. When no note is requested, that note is created automatically. Park records are exempt: their `synopsis` carries the full summary.

`todo detail --json` exposes the derived `disposition` field: `park` (has `category`), `record` (has `kind: record`), `work-order` (has `kind: work-order`), `clear` (no note), or `float` (note exists, none of the markers). `todo detail` itself reports an unreadable note as an error rather than a disposition.

## Archive

`.todo/archive/` is the repo-local home for retired whys: the reasoning behind work that is done, kept where the work happened so a "how did we fix this before" search lands in the right repo. Retire a completed task with `todo archive <ref>`:

- the task must be `[x]` and must have a companion note;
- the note's one-line relevance why (`synopsis`) is asserted at archive time. It defaults to the note's existing synopsis; `--synopsis` sets or rewrites it, and an empty result is an error, never a silently unlabeled entry;
- the note is rewritten as a todo-native record and moved to `.todo/archive/<ID>.md` (`--name` overrides the basename);
- the task line and the source note entry are removed.

The archived note's `synopsis` is the field a non-author reads, so it is the one field with a standing maintenance duty: review it as the cleanup surface. `todo list` never shows archived content; `todo list --archive` is the compact view, and `todo list --archive --json` emits a versioned `{schema_version, archived[]}` envelope.

Work orders stay disposable: `clear` deletes them. When a completed work order changed behavior, run `todo archive` first so its why survives.

## JSON output

Both `todo list --json` and `todo detail --json` emit stable, machine-readable JSON.

- `status`: canonical status designation (`open`, `in progress`, `complete`)
- `status_symbol`: raw checkbox character (` `, `o`, `x`)

`todo list --json` returns an array of tasks with fields: `id`, `status`, `status_symbol`, `priority`, `opened`, `claimed`, `age_days`, `summary`, and `disposition` (`park`, `record`, `work-order`, `clear`, or `float`). A companion note that exists but cannot be read aborts the listing with an error rather than reporting the task as `clear`, matching `todo detail`. When using `todo list --all --json`, each task also includes `repo_path` and `repo_project`.

`todo list --archive --json` returns `{schema_version, archived[]}`, where each archived note has `name`, `path`, and `synopsis` (plus `repo_path`/`repo_project` with `--all`).

`todo detail --json` returns a single object with fields: `id`, `status`, `status_symbol`, `priority`, `opened`, `opened_days`, `claimed`, `age_days`, `summary`, `disposition`, `note_path`, `note_exists`, `note_preview`, `note_preview_truncated`.

`todo detail --json --full` adds the complete note text in `note_body` and omits `note_preview` and `note_preview_truncated`. `--lines` is ignored when `--full` is set.

`claimed`, `age_days`, `note_preview`, `note_preview_truncated`, and `note_body` are omitted when empty or not applicable.

## Development

```sh
make check       # fmt + vet + lint + test
make build       # build ./todo binary
make test        # run tests
make lint        # golangci-lint (only if installed)
make deps        # install golangci-lint and goreleaser
```

Release tooling:

```sh
make cross-compile         # build for linux/darwin/windows (legacy, no archives)
make release               # cross-compile plus checksums in dist/
make goreleaser-check      # validate .goreleaser.yml
make goreleaser-snapshot   # build a snapshot release with GoReleaser
```

Tags matching `v*` trigger the release workflow, which publishes archives, checksums, and a changelog to GitHub Releases. See [CONTRIBUTING.md](./CONTRIBUTING.md) for package boundaries and conventions.

## Versioning

Version comes from the `VERSION` file for local/`go install` builds, and is overridden by `-ldflags` at release build time.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). Run `make check` before opening a PR.

## License

[MIT](./LICENSE) (c) Aaron Martell
