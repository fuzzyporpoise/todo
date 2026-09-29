// Package cmd defines the todo CLI command tree and wiring.
package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	validation "github.com/urfave/cli-validation"
	"github.com/urfave/cli/v3"

	"go.fuzzyporpoise.dev/todo/internal/fs"
	"go.fuzzyporpoise.dev/todo/internal/git"
	"go.fuzzyporpoise.dev/todo/internal/registry"
	"go.fuzzyporpoise.dev/todo/internal/todo"
)

var (
	version   = "0.0.0"
	buildTime string
)

// SetVersion wires the build-time version into the command tree.
func SetVersion(v string) { version = v }

// SetBuildTime wires the build timestamp into the command tree.
func SetBuildTime(bt string) { buildTime = bt }

func buildVersion() string {
	v := version
	if buildTime != "" {
		v += " (" + buildTime + ")"
	}
	return v
}

// exitError returns a plain, uncolored error for non-interactive use.
func exitError(err error) error {
	return cli.Exit(err.Error(), 1)
}

// newApp builds the todo command tree. It is separate from Run so tests can
// inject writers/readers and call app.Run directly.
func newApp() *cli.Command {
	return &cli.Command{
		Name:                  "todo",
		Version:               buildVersion(),
		EnableShellCompletion: true,
		Usage:                 "manage ad-hoc tasks in .todo/todo.md",
		Commands: []*cli.Command{
			func() *cli.Command {
				var (
					summary     string
					priority    string
					create      bool
					noteContent string
					noteFile    string
					dryRun      bool
					kind        string
					category    string
					synopsis    string
					source      string
				)
				noteContentFlag := &cli.StringFlag{
					Name:        "note-content",
					Destination: &noteContent,
					Usage:       "content to write into the note (implies --note); use '-' to read from stdin",
				}
				noteFileFlag := &cli.StringFlag{
					Name:        "note-file",
					Destination: &noteFile,
					Usage:       "copy an existing file into the note (implies --note; copy, not move)",
				}
				kindFlag := &cli.StringFlag{
					Name:        "kind",
					Destination: &kind,
					Usage:       "note disposition marker (work-order)",
					Validator:   validation.Enum("work-order"),
				}
				categoryFlag := &cli.StringFlag{
					Name:        "category",
					Destination: &category,
					Usage:       "park record category (" + strings.Join(todo.ParkCategories(), ", ") + ")",
					Validator:   validation.Enum(todo.ParkCategories()...),
				}
				synopsisFlag := &cli.StringFlag{
					Name:        "synopsis",
					Destination: &synopsis,
					Usage:       "park record one-line synopsis",
				}
				sourceFlag := &cli.StringFlag{
					Name:        "source",
					Destination: &source,
					Usage:       "park record source (e.g. repo or chat)",
				}
				return &cli.Command{
					Name:      "add",
					Usage:     "Add a new todo to the todo list",
					ArgsUsage: "[summary]",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:        "priority",
							Aliases:     []string{"p"},
							Value:       "med",
							Destination: &priority,
							Usage:       "priority: low, med, high",
							Validator:   validation.Enum("low", "med", "high"),
						},
						&cli.StringFlag{
							Name:        "summary",
							Aliases:     []string{"s"},
							Destination: &summary,
							Usage:       "summary text (alternative to positional arg); over 120 runes truncates on the line with '...' and spills the full text into a work-order note (auto-created when no note is requested)",
						},
						&cli.BoolFlag{
							Name:        "create-note",
							Aliases:     []string{"n"},
							Destination: &create,
							Usage:       "create a companion note file (optional when --note-content/--note-file given; otherwise reads content from stdin)",
						},
						noteContentFlag,
						noteFileFlag,
						kindFlag,
						categoryFlag,
						synopsisFlag,
						sourceFlag,
						&cli.BoolFlag{
							Name:        "dry-run",
							Destination: &dryRun,
							Usage:       "preview the would-be task line and note without writing",
						},
					},
					MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
						{
							Category: "note source",
							// note-content and note-file are alternative note
							// sources; each in its own path so at most one may
							// be set.
							Flags: [][]cli.Flag{{noteContentFlag}, {noteFileFlag}},
						},
						{
							Category: "note disposition",
							// --kind (work-order) and the record flags are
							// alternative dispositions; category/synopsis/source
							// may be set together as a record.
							Flags: [][]cli.Flag{{kindFlag}, {categoryFlag, synopsisFlag, sourceFlag}},
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						return runAdd(cmd, cfg, addOptions{
							summary:     summary,
							priority:    priority,
							create:      create,
							noteContent: noteContent,
							noteFile:    noteFile,
							dryRun:      dryRun,
							kind:        kind,
							category:    category,
							synopsis:    synopsis,
							source:      source,
						})
					},
				}
			}(),
			{
				Name:  "init",
				Usage: "creates todo.md if missing and registers the repo",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					root, err := git.RepoRoot()
					if err != nil {
						return exitError(err)
					}
					return runInit(cmd, appConfigFor(root))
				},
			},
			func() *cli.Command {
				var (
					asJSON      bool
					state       string
					stale       int
					all         bool
					archive     bool
					sort        string
					sortReverse bool
				)
				return &cli.Command{
					Name:    "list",
					Aliases: []string{"ls"},
					Usage:   "lists existing todos in tabular format",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:        "json",
							Destination: &asJSON,
							Usage:       "output machine-readable JSON",
						},
						&cli.BoolFlag{
							Name:        "all",
							Destination: &all,
							Usage:       "list tasks across all registered repos",
						},
						&cli.BoolFlag{
							Name:        "archive",
							Destination: &archive,
							Usage:       "list archived notes instead of tasks (compact view)",
						},
						&cli.IntFlag{
							Name:        "stale",
							Destination: &stale,
							Usage:       "only show claimed tasks older than N day/s",
						},
						&cli.StringFlag{
							Name:        "state",
							Destination: &state,
							Usage:       "filter by status: open, progress, done",
							Validator:   validation.Enum("open", "progress", "done"),
						},
						&cli.StringFlag{
							Name:        "sort",
							Destination: &sort,
							Usage:       "sort by: priority, opened, claimed, age",
							Validator:   validation.Enum("priority", "opened", "claimed", "age"),
						},
						&cli.BoolFlag{
							Name:        "reverse",
							Destination: &sortReverse,
							Usage:       "reverse sort order",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						opts := listOptions{
							asJSON:      asJSON,
							all:         all,
							archive:     archive,
							state:       state,
							stale:       stale,
							sort:        sort,
							sortReverse: sortReverse,
						}
						if all {
							if archive {
								return runListArchived(cmd, appConfig{}, opts)
							}
							return runList(cmd, appConfig{}, opts)
						}
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						if archive {
							return runListArchived(cmd, cfg, opts)
						}
						return runList(cmd, cfg, opts)
					},
				}
			}(),
			{
				Name:      "pickup",
				Usage:     "pick up a task (mark as in progress)",
				ArgsUsage: "<task>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := requireRepoConfig()
					if err != nil {
						return exitError(err)
					}
					return runPickup(cmd, cfg)
				},
			},
			{
				Name:      "release",
				Usage:     "release a picked-up task back to open (drop its claim)",
				ArgsUsage: "<task>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := requireRepoConfig()
					if err != nil {
						return exitError(err)
					}
					return runRelease(cmd, cfg)
				},
			},
			{
				Name:      "reopen",
				Usage:     "reopen a completed task (restore it to open status)",
				ArgsUsage: "<task>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg, err := requireRepoConfig()
					if err != nil {
						return exitError(err)
					}
					return runReopen(cmd, cfg)
				},
			},
			func() *cli.Command {
				var down bool
				return &cli.Command{
					Name:      "bump",
					Usage:     "bump a task's priority up or down by one level",
					ArgsUsage: "<task>",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:        "down",
							Destination: &down,
							Usage:       "bump priority down instead of up",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						return runBump(cmd, cfg, down)
					},
				}
			}(),
			func() *cli.Command {
				var deleteNote bool
				return &cli.Command{
					Name:      "remove",
					Aliases:   []string{"rm"},
					Usage:     "remove a task line by reference (any status)",
					ArgsUsage: "<task>",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:        "note",
							Destination: &deleteNote,
							Usage:       "also delete the companion note file",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						return runRemove(cmd, cfg, removeOptions{deleteNote: deleteNote})
					},
				}
			}(),
			func() *cli.Command {
				var (
					name     string
					synopsis string
				)
				return &cli.Command{
					Name:      "archive",
					Usage:     "retire a completed task into .todo/archive (move its note, remove its line)",
					ArgsUsage: "<task>",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:        "name",
							Destination: &name,
							Usage:       "archived note basename (default <ID>.md)",
						},
						&cli.StringFlag{
							Name:        "synopsis",
							Destination: &synopsis,
							Usage:       "assert the archived one-line relevance why (defaults to the note's synopsis)",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						return runArchive(cmd, cfg, archiveOptions{name: name, synopsis: synopsis})
					},
				}
			}(),
			func() *cli.Command {
				var (
					lines  int
					noNote bool
					asJSON bool
					full   bool
				)
				return &cli.Command{
					Name:      "detail",
					Usage:     "show detailed information for a task, including a note preview",
					ArgsUsage: "<task>",
					Flags: []cli.Flag{
						&cli.IntFlag{
							Name:        "lines",
							Value:       20,
							Destination: &lines,
							Usage:       "number of note lines to preview",
						},
						&cli.BoolFlag{
							Name:        "no-note",
							Destination: &noNote,
							Usage:       "skip the companion note preview",
						},
						&cli.BoolFlag{
							Name:        "json",
							Destination: &asJSON,
							Usage:       "output machine-readable JSON",
						},
						&cli.BoolFlag{
							Name:        "full",
							Destination: &full,
							Usage:       "show the full note body instead of a preview (--lines is ignored)",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						return runDetail(cmd, cfg, detailOptions{
							lines:  lines,
							noNote: noNote,
							asJSON: asJSON,
							full:   full,
						})
					},
				}
			}(),
			func() *cli.Command {
				var clear, park bool
				return &cli.Command{
					Name:      "complete",
					Aliases:   []string{"done"},
					Usage:     "complete a task (mark done, optionally clear the line or park its note)",
					ArgsUsage: "<task>",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:        "clear",
							Destination: &clear,
							Usage:       "remove the task line instead of marking it done",
						},
						&cli.BoolFlag{
							Name:        "park",
							Destination: &park,
							Usage:       "print the companion note file name",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						return runComplete(cmd, cfg, completeOptions{
							clear: clear,
							park:  park,
						})
					},
				}
			}(),
			func() *cli.Command {
				var all bool
				return &cli.Command{
					Name:  "clear",
					Usage: "clear completed tasks by note disposition",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:        "all",
							Destination: &all,
							Usage:       "clear completed tasks across all registered repos",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						if all {
							return runClear(cmd, appConfig{}, clearOptions{all: true})
						}
						cfg, err := requireRepoConfig()
						if err != nil {
							return exitError(err)
						}
						return runClear(cmd, cfg, clearOptions{all: false})
					},
				}
			}(),
			func() *cli.Command {
				var (
					all   bool
					fix   bool
					depth int
				)
				return &cli.Command{
					Name:      "doctor",
					Usage:     "reconcile the tracked-folder registry against disk",
					ArgsUsage: "[paths...]",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:        "all",
							Destination: &all,
							Usage:       "scan every registered repo directory downward for unregistered .todo folders",
						},
						&cli.BoolFlag{
							Name:        "fix",
							Destination: &fix,
							Usage:       "drop stale entries and register unregistered folders",
						},
						&cli.IntFlag{
							Name:        "depth",
							Value:       4,
							Destination: &depth,
							Usage:       "max depth when scanning for .todo folders",
						},
					},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						// Positional arguments, if provided, override the scan roots.
						paths := cmd.Args().Slice()
						if len(paths) > 0 {
							return runDoctor(cmd, doctorOptions{
								all:   all,
								fix:   fix,
								depth: depth,
								roots: paths,
							})
						}
						return runDoctor(cmd, doctorOptions{all: all, fix: fix, depth: depth})
					},
				}
			}(),
			{
				Name:  "schema",
				Usage: "print the current list --json schema version and status enum",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runSchema(outWriter(cmd))
				},
			},
		},
	}
}

// Run runs the root command.
func Run() {
	app := newApp()
	if err := app.Run(context.Background(), os.Args); err != nil {
		// cli has already reported the error by the time Run returns:
		// HandleExitCoder prints ExitCoder messages (no timestamp) and usage
		// errors are shown as "Incorrect Usage". Only the exit code is left.
		code := 1
		if exitErr, ok := errors.AsType[cli.ExitCoder](err); ok {
			code = exitErr.ExitCode()
		}
		os.Exit(code)
	}
}

// appConfigFor builds the per-run configuration from the resolved repo root.
func appConfigFor(repoRoot string) appConfig {
	if r, err := filepath.EvalSymlinks(repoRoot); err == nil {
		repoRoot = r
	}
	return appConfig{
		repoRoot:   repoRoot,
		todoPath:   filepath.Join(repoRoot, ".todo", "todo.md"),
		notesDir:   filepath.Join(repoRoot, ".todo", "notes"),
		archiveDir: filepath.Join(repoRoot, ".todo", "archive"),
	}
}

// requireRepoConfig resolves the current git repo, builds its appConfig, and
// ensures .todo/todo.md exists. Per-repo commands call this; global commands
// such as init, list --all, and doctor skip it.
func requireRepoConfig() (appConfig, error) {
	root, err := git.RepoRoot()
	if err != nil {
		return appConfig{}, err
	}
	cfg := appConfigFor(root)
	exists, err := fs.VerifyExists(cfg.todoPath)
	if err != nil {
		return appConfig{}, err
	}
	if !exists {
		return appConfig{}, errors.New("repo not todo initialized - run `todo init`")
	}
	return cfg, nil
}

// registryPath returns the path to the machine-local registry. Tests can
// override it via the TODO_REGISTRY environment variable.
func registryPath() string {
	if p := os.Getenv("TODO_REGISTRY"); p != "" {
		return p
	}
	return registry.DefaultPath()
}
