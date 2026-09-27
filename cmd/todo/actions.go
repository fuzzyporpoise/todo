package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/polymorcodeus/todo/internal/fs"
	"github.com/polymorcodeus/todo/internal/git"
	"github.com/polymorcodeus/todo/internal/registry"
	"github.com/polymorcodeus/todo/internal/todo"
)

// appConfig carries the resolved per-run dependencies for all commands.
type appConfig struct {
	repoRoot   string
	todoPath   string
	notesDir   string
	archiveDir string
}

// addOptions carries the flag values for the add command.
type addOptions struct {
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
}

// listOptions carries the flag values for the list command.
type listOptions struct {
	asJSON      bool
	all         bool
	archive     bool
	state       string
	stale       int
	sort        string
	sortReverse bool
}

// archiveOptions carries the flag values for the archive command.
type archiveOptions struct {
	name     string
	synopsis string
}

// completeOptions carries the flag values for the complete command.
type completeOptions struct {
	clear bool
	park  bool
}

// clearOptions carries the flag values for the clear command.
type clearOptions struct {
	all bool
}

// removeOptions carries the flag values for the remove command.
type removeOptions struct {
	deleteNote bool
}

func outWriter(cmd *cli.Command) io.Writer {
	if cmd.Root().Writer != nil {
		return cmd.Root().Writer
	}
	return os.Stdout
}

func inReader(cmd *cli.Command) io.Reader {
	if cmd.Root().Reader != nil {
		return cmd.Root().Reader
	}
	return os.Stdin
}

func runAdd(cmd *cli.Command, cfg appConfig, opts addOptions) error {
	if opts.summary == "" {
		opts.summary = strings.TrimSpace(cmd.Args().First())
	}
	if opts.summary == "" {
		return exitError(errors.New("task summary required: provide as first argument or --summary"))
	}

	// Content flags imply note creation, so --note-content/--note-file do not
	// require a separate --note.
	createNote := opts.create || opts.noteContent != "" || opts.noteFile != ""

	// Disposition flags only make sense when a note is being created.
	if !createNote && (opts.kind != "" || opts.category != "" || opts.synopsis != "" || opts.source != "") {
		return exitError(errors.New("note disposition flags (--kind/--category/--synopsis/--source) require --note"))
	}

	// Resolve note content: --note-content (or '-') takes precedence, then
	// --note-file copy (handled internally by Add), else read note content
	// from stdin.
	content := opts.noteContent
	if content == "-" || (content == "" && createNote && opts.noteFile == "") {
		data, err := io.ReadAll(inReader(cmd))
		if err != nil {
			return exitError(fmt.Errorf("read note stdin: %w", err))
		}
		content = string(data)
	}

	result, err := todo.Add(todo.AddOptions{
		TodoPath:    cfg.todoPath,
		NotesDir:    cfg.notesDir,
		Priority:    opts.priority,
		Summary:     opts.summary,
		CreateNote:  createNote,
		NoteContent: content,
		NoteFile:    opts.noteFile,
		DryRun:      opts.dryRun,
		Kind:        opts.kind,
		Category:    opts.category,
		Synopsis:    opts.synopsis,
		Source:      opts.source,
	})
	if err != nil {
		return exitError(err)
	}

	out := outWriter(cmd)
	if opts.dryRun {
		_, _ = fmt.Fprintf(out, "would add:         %s\n", result.Line)
		if result.NotePath != "" {
			_, _ = fmt.Fprintf(out, "would create note: %s\n", result.NotePath)
			if result.NoteContent != "" {
				_, _ = fmt.Fprintln(out, "staged note content:")
				_, _ = fmt.Fprint(out, result.NoteContent)
				if !strings.HasSuffix(result.NoteContent, "\n") {
					_, _ = fmt.Fprintln(out)
				}
			}
		}
		return nil
	}

	noteMsg := ""
	if result.NotePath != "" {
		noteMsg = fmt.Sprintf(" + note %s", result.NotePath)
	}
	_, _ = fmt.Fprintf(out, "Created %s [priority:%s]%s\n", result.ID, result.Priority, noteMsg)
	return nil
}

func runInit(cmd *cli.Command, cfg appConfig) error {
	exists, err := fs.VerifyExists(cfg.todoPath)
	if err != nil {
		return exitError(err)
	}
	if !exists {
		if err := todo.Init(cfg.todoPath); err != nil {
			return exitError(err)
		}
	}

	entries, err := registry.Load(registryPath())
	if err != nil {
		return exitError(err)
	}

	entries = registerRepo(entries, cfg.repoRoot)

	if err := registry.Save(registryPath(), entries); err != nil {
		return exitError(err)
	}

	out := outWriter(cmd)
	if !exists {
		_, _ = fmt.Fprintf(out, "created %s\n", cfg.todoPath)
	}
	_, _ = fmt.Fprintf(out, "registered %s\n", cfg.repoRoot)
	return nil
}

func runList(cmd *cli.Command, cfg appConfig, opts listOptions) error {
	filter := todo.ListFilter{
		StaleDays:   opts.stale,
		SortField:   todo.SortField(opts.sort),
		SortReverse: opts.sortReverse,
	}
	if opts.state != "" {
		s := parseState(opts.state)
		filter.State = &s
	}

	out := outWriter(cmd)
	errOut := cmd.Root().ErrWriter
	if errOut == nil {
		errOut = os.Stderr
	}

	var listed []listedTask
	if opts.all {
		entries, err := registry.Load(registryPath())
		if err != nil {
			return exitError(err)
		}
		for _, e := range entries {
			if _, err := os.Stat(e.Path); err != nil {
				_, _ = fmt.Fprintf(errOut, "warning: skipping missing repo %s: %v\n", e.Path, err)
				continue
			}
			todoPath := filepath.Join(e.Path, ".todo", "todo.md")
			tasks, err := todo.List(todoPath, filter)
			if err != nil {
				_, _ = fmt.Fprintf(errOut, "warning: cannot read %s: %v\n", todoPath, err)
				continue
			}
			for _, t := range tasks {
				listed = append(listed, listedTask{
					Task:        t,
					repoPath:    e.Path,
					repoProject: projectFromEntry(e),
				})
			}
		}
	} else {
		tasks, err := todo.List(cfg.todoPath, filter)
		if err != nil {
			return exitError(err)
		}
		listed = make([]listedTask, len(tasks))
		for i, t := range tasks {
			listed[i] = listedTask{
				Task:     t,
				notesDir: cfg.notesDir,
			}
		}
	}

	if opts.asJSON {
		return writeJSON(out, listed)
	}

	if len(listed) == 0 {
		_, _ = fmt.Fprintln(out, "No tasks found.")
		return nil
	}

	showRepo := opts.all
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if showRepo {
		_, _ = fmt.Fprintln(w, "\tID\tPRIORITY\tOPENED\tCLAIMED\tAGE\tREPO\tSUMMARY")
		_, _ = fmt.Fprintln(w, "\t--\t--------\t------\t-------\t---\t----\t-------")
	} else {
		_, _ = fmt.Fprintln(w, "\tID\tPRIORITY\tOPENED\tCLAIMED\tAGE\tSUMMARY")
		_, _ = fmt.Fprintln(w, "\t--\t--------\t------\t-------\t---\t-------")
	}
	for _, lt := range listed {
		t := lt.Task
		age := "-"
		if t.AgeDays() >= 0 {
			age = fmt.Sprintf("%d day/s", t.AgeDays())
		}
		repo := lt.repoProject
		if repo == "" && showRepo {
			repo = filepath.Base(lt.repoPath)
		}
		if showRepo {
			_, _ = fmt.Fprintf(w, "[%s]\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				t.Status, t.ID, t.Priority, t.Opened, dashIfEmpty(t.Claimed), age, repo, t.Summary)
		} else {
			_, _ = fmt.Fprintf(w, "[%s]\t%s\t%s\t%s\t%s\t%s\t%s\n",
				t.Status, t.ID, t.Priority, t.Opened, dashIfEmpty(t.Claimed), age, t.Summary)
		}
	}
	if err := w.Flush(); err != nil {
		return exitError(err)
	}
	return nil
}

// archiveEntry augments an archived note with cross-repo metadata for output.
type archiveEntry struct {
	todo.ArchivedNote
	repoPath    string
	repoProject string
}

// runListArchived renders the compact archived-notes view. It scans each target
// repo's .todo/archive directory rather than task lines, so archived content
// never appears in the default list.
func runListArchived(cmd *cli.Command, cfg appConfig, opts listOptions) error {
	out := outWriter(cmd)
	errOut := cmd.Root().ErrWriter
	if errOut == nil {
		errOut = os.Stderr
	}

	var entries []archiveEntry
	collect := func(root, project string, includeRepo bool) {
		archiveDir := filepath.Join(root, ".todo", "archive")
		notes, err := todo.ListArchived(archiveDir)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "warning: cannot read %s: %v\n", archiveDir, err)
			return
		}
		for _, n := range notes {
			e := archiveEntry{ArchivedNote: n, repoProject: project}
			if includeRepo {
				e.repoPath = root
			}
			entries = append(entries, e)
		}
	}

	if opts.all {
		reg, err := registry.Load(registryPath())
		if err != nil {
			return exitError(err)
		}
		for _, e := range reg {
			if _, err := os.Stat(e.Path); err != nil {
				_, _ = fmt.Fprintf(errOut, "warning: skipping missing repo %s: %v\n", e.Path, err)
				continue
			}
			collect(e.Path, projectFromEntry(e), true)
		}
	} else {
		collect(cfg.repoRoot, "", false)
	}

	if opts.asJSON {
		return writeJSONArchive(out, entries)
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "No archived notes found.")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if opts.all {
		_, _ = fmt.Fprintln(w, "NAME\tREPO\tSYNOPSIS")
		_, _ = fmt.Fprintln(w, "----\t----\t--------")
	} else {
		_, _ = fmt.Fprintln(w, "NAME\tSYNOPSIS")
		_, _ = fmt.Fprintln(w, "----\t--------")
	}
	for _, e := range entries {
		syn := e.Synopsis
		if syn == "" {
			syn = "-"
		}
		if opts.all {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, e.repoProject, syn)
		} else {
			_, _ = fmt.Fprintf(w, "%s\t%s\n", e.Name, syn)
		}
	}
	if err := w.Flush(); err != nil {
		return exitError(err)
	}
	return nil
}

// jsonArchived is the machine-readable representation of one archived note.
type jsonArchived struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Synopsis    string `json:"synopsis"`
	RepoPath    string `json:"repo_path,omitempty"`
	RepoProject string `json:"repo_project,omitempty"`
}

// jsonArchiveEnvelope wraps the list --archive --json output in a versioned
// contract so consumers can detect schema drift.
type jsonArchiveEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	Archived      []jsonArchived `json:"archived"`
}

func writeJSONArchive(out io.Writer, entries []archiveEntry) error {
	items := make([]jsonArchived, 0, len(entries))
	for _, e := range entries {
		items = append(items, jsonArchived{
			Name:        e.Name,
			Path:        e.Path,
			Synopsis:    e.Synopsis,
			RepoPath:    e.repoPath,
			RepoProject: e.repoProject,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonArchiveEnvelope{SchemaVersion: 1, Archived: items})
}

// listedTask augments a task with optional cross-repo metadata for list output.
type listedTask struct {
	todo.Task
	repoPath    string
	repoProject string
	notesDir    string // set for local tasks so disposition can be resolved
}

func projectFromEntry(e registry.Entry) string {
	if e.Project != "" {
		return e.Project
	}
	return filepath.Base(e.Path)
}

// registerRepo upserts a repo folder into the registry, stamping its git
// remote, parsed host/owner, and project name. init and doctor both go through
// here so a registered repo looks the same however it was discovered.
func registerRepo(entries []registry.Entry, path string) []registry.Entry {
	remote := git.RemoteURLAt(path, "origin")
	host, owner := git.ParseRemote(remote)

	entries = registry.Upsert(entries, path, remote, filepath.Base(path))
	for i := range entries {
		if filepath.Clean(entries[i].Path) == filepath.Clean(path) {
			entries[i].Host = host
			entries[i].Owner = owner
			break
		}
	}
	return entries
}

func runPickup(cmd *cli.Command, cfg appConfig) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}
	res, err := todo.Pickup(todo.RefOptions{TodoPath: cfg.todoPath, NotesDir: cfg.notesDir, Ref: ref})
	if err != nil {
		return exitError(err)
	}
	printTaskLine(outWriter(cmd), res.Line, res.Note)
	return nil
}

// detailOptions carries the flag values for the detail command.
type detailOptions struct {
	lines  int
	noNote bool
	asJSON bool
	full   bool
}

// jsonDetail is the machine-readable representation of a task detail.
type jsonDetail struct {
	ID                   string `json:"id"`
	Status               string `json:"status"`
	StatusSymbol         string `json:"status_symbol"`
	Priority             string `json:"priority"`
	Opened               string `json:"opened"`
	OpenedDays           int    `json:"opened_days"`
	Claimed              string `json:"claimed,omitempty"`
	AgeDays              *int   `json:"age_days,omitempty"`
	Summary              string `json:"summary"`
	Disposition          string `json:"disposition"`
	NotePath             string `json:"note_path"`
	NoteExists           bool   `json:"note_exists"`
	NotePreview          string `json:"note_preview,omitempty"`
	NotePreviewTruncated bool   `json:"note_preview_truncated,omitempty"`
	NoteBody             string `json:"note_body,omitempty"`
}

func runDetail(cmd *cli.Command, cfg appConfig, opts detailOptions) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}

	lines := opts.lines
	if lines <= 0 {
		lines = 20
	}

	res, err := todo.Detail(todo.DetailOptions{
		TodoPath: cfg.todoPath,
		NotesDir: cfg.notesDir,
		Ref:      ref,
		Lines:    lines,
		NoNote:   opts.noNote,
		Full:     opts.full,
	})
	if err != nil {
		return exitError(err)
	}

	out := outWriter(cmd)
	if opts.asJSON {
		jd := jsonDetail{
			ID:           res.Task.ID,
			Status:       res.Task.Status.StatusName(),
			StatusSymbol: string(res.Task.Status),
			Priority:     string(res.Task.Priority),
			Opened:       res.Task.Opened,
			OpenedDays:   res.Task.OpenedDays(),
			Claimed:      res.Task.Claimed,
			Summary:      res.Task.Summary,
			Disposition:  string(res.Disposition),
			NotePath:     res.NotePath,
			NoteExists:   res.NoteExists,
			NotePreview:  res.NotePreview,
		}
		if opts.full && !opts.noNote {
			jd.NoteBody = res.NoteBody
		} else if res.NotePreview != "" {
			jd.NotePreviewTruncated = res.NoteTruncated
		}
		if age := res.Task.AgeDays(); age >= 0 {
			jd.AgeDays = &age
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(jd)
	}

	claimed := "-"
	if res.Task.Claimed != "" {
		if age := res.Task.AgeDays(); age >= 0 {
			claimed = fmt.Sprintf("%s (%d day/s)", res.Task.Claimed, age)
		} else {
			claimed = res.Task.Claimed
		}
	}

	_, _ = fmt.Fprintf(out, "%s (priority: %s)\n", res.Task.ID, res.Task.Priority)
	_, _ = fmt.Fprintf(out, "status: %s | opened: %s (%d day/s) | claimed: %s\n",
		res.Task.Status.Description(), res.Task.Opened, res.Task.OpenedDays(), claimed)
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "summary:")
	_, _ = fmt.Fprintln(out, res.Task.Summary)
	_, _ = fmt.Fprintln(out)

	if res.NoteExists {
		_, _ = fmt.Fprintf(out, "note: %s\n", res.NotePath)
		if opts.full && res.NoteBody != "" {
			for i, line := range strings.Split(res.NoteBody, "\n") {
				_, _ = fmt.Fprintf(out, "  %2d | %s\n", i+1, line)
			}
		} else if res.NotePreview != "" {
			for i, line := range strings.Split(res.NotePreview, "\n") {
				_, _ = fmt.Fprintf(out, "  %2d | %s\n", i+1, line)
			}
			if res.NoteTruncated {
				_, _ = fmt.Fprintln(out, "       ...")
			}
		}
	} else {
		_, _ = fmt.Fprintln(out, "note: none")
	}

	return nil
}

func runRelease(cmd *cli.Command, cfg appConfig) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}
	res, err := todo.Release(todo.RefOptions{TodoPath: cfg.todoPath, NotesDir: cfg.notesDir, Ref: ref})
	if err != nil {
		return exitError(err)
	}
	printTaskLine(outWriter(cmd), res.Line, res.Note)
	return nil
}

func runBump(cmd *cli.Command, cfg appConfig, down bool) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}
	res, err := todo.Bump(cfg.todoPath, cfg.notesDir, ref, down)
	if err != nil {
		return exitError(err)
	}
	out := outWriter(cmd)
	if res.NoOp {
		dir := "up"
		if down {
			dir = "down"
		}
		_, _ = fmt.Fprintf(out, "%s is already at the %s boundary\n", res.Line, dir)
		return nil
	}
	printTaskLine(out, res.Line, res.Note)
	return nil
}

func runReopen(cmd *cli.Command, cfg appConfig) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}
	res, err := todo.Reopen(todo.RefOptions{TodoPath: cfg.todoPath, NotesDir: cfg.notesDir, Ref: ref})
	if err != nil {
		return exitError(err)
	}
	out := outWriter(cmd)
	if res.NoOp {
		_, _ = fmt.Fprintf(out, "%s is already open\n", res.Line)
		return nil
	}
	printTaskLine(out, res.Line, res.Note)
	return nil
}

func runRemove(cmd *cli.Command, cfg appConfig, opts removeOptions) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}
	res, err := todo.Remove(todo.RefOptions{TodoPath: cfg.todoPath, NotesDir: cfg.notesDir, Ref: ref})
	if err != nil {
		return exitError(err)
	}
	if opts.deleteNote && res.Note != "" {
		if err := fs.RemoveFollowingSymlink(res.Note); err != nil {
			return exitError(fmt.Errorf("delete note: %w", err))
		}
	}
	printTaskLine(outWriter(cmd), res.Line, res.Note)
	return nil
}

// runArchive retires a completed task into the repo-local .todo/archive.
func runArchive(cmd *cli.Command, cfg appConfig, opts archiveOptions) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}
	res, err := todo.Archive(todo.ArchiveOptions{
		TodoPath:   cfg.todoPath,
		NotesDir:   cfg.notesDir,
		ArchiveDir: cfg.archiveDir,
		Ref:        ref,
		Name:       opts.name,
		Synopsis:   opts.synopsis,
	})
	if err != nil {
		return exitError(err)
	}
	out := outWriter(cmd)
	_, _ = fmt.Fprintln(out, res.Line)
	_, _ = fmt.Fprintf(out, "archived: %s\n", res.ArchivePath)
	return nil
}

func runComplete(cmd *cli.Command, cfg appConfig, opts completeOptions) error {
	ref, err := requireTaskRef(cmd)
	if err != nil {
		return exitError(err)
	}
	res, err := todo.Complete(todo.CompleteOptions{TodoPath: cfg.todoPath, NotesDir: cfg.notesDir, Ref: ref, Clear: opts.clear})
	if err != nil {
		return exitError(err)
	}
	out := outWriter(cmd)
	_, _ = fmt.Fprintln(out, res.Line)
	if opts.park {
		if res.Note != "" {
			_, _ = fmt.Fprintln(out, "note:", res.Note)
		} else {
			_, _ = fmt.Fprintf(out, "no note to park for %s\n", ref)
		}
	}
	return nil
}

func runClear(cmd *cli.Command, cfg appConfig, opts clearOptions) error {
	out := outWriter(cmd)
	errOut := cmd.Root().ErrWriter
	if errOut == nil {
		errOut = os.Stderr
	}

	type target struct {
		cfg  appConfig
		repo string
	}
	var targets []target

	if opts.all {
		entries, err := registry.Load(registryPath())
		if err != nil {
			return exitError(err)
		}
		for _, e := range entries {
			if _, err := os.Stat(e.Path); err != nil {
				_, _ = fmt.Fprintf(errOut, "warning: skipping missing repo %s: %v\n", e.Path, err)
				continue
			}
			targets = append(targets, target{
				cfg:  appConfigFor(e.Path),
				repo: projectFromEntry(e),
			})
		}
	} else {
		targets = append(targets, target{cfg: cfg, repo: ""})
	}

	var printed bool
	for _, tgt := range targets {
		res, err := todo.Clear(tgt.cfg.todoPath, tgt.cfg.notesDir)
		if err != nil {
			if opts.all {
				_, _ = fmt.Fprintf(errOut, "warning: cannot clear %s: %v\n", tgt.cfg.todoPath, err)
				continue
			}
			return exitError(err)
		}

		if len(res.RemovedClear) == 0 && len(res.RemovedWorkOrder) == 0 && len(res.Parked) == 0 && len(res.Recorded) == 0 && len(res.Float) == 0 {
			if !opts.all {
				_, _ = fmt.Fprintln(out, "No completed tasks to clear.")
				printed = true
			}
			continue
		}

		prefix := ""
		if opts.all {
			prefix = tgt.repo + ": "
		}

		if len(res.RemovedClear) > 0 {
			_, _ = fmt.Fprintf(out, "%sremoved (no note): %s\n", prefix, strings.Join(res.RemovedClear, ", "))
			printed = true
		}
		if len(res.RemovedWorkOrder) > 0 {
			_, _ = fmt.Fprintf(out, "%sremoved work-order: %s\n", prefix, strings.Join(res.RemovedWorkOrder, ", "))
			printed = true
		}
		if len(res.Parked) > 0 {
			_, _ = fmt.Fprintf(out, "%sparked: %s\n", prefix, strings.Join(res.Parked, ", "))
			printed = true
		}
		if len(res.Recorded) > 0 {
			_, _ = fmt.Fprintf(out, "%spreserved record: %s\n", prefix, strings.Join(res.Recorded, ", "))
			printed = true
		}
		if len(res.Float) > 0 {
			_, _ = fmt.Fprintf(out, "%sfloat (review): %s\n", prefix, strings.Join(res.Float, ", "))
			printed = true
		}
	}

	if !printed && opts.all {
		_, _ = fmt.Fprintln(out, "No completed tasks to clear across registered repos.")
	}

	return nil
}

// doctorOptions carries the flag values for the doctor command.
type doctorOptions struct {
	all   bool
	fix   bool
	depth int
	roots []string
}

func runDoctor(cmd *cli.Command, opts doctorOptions) error {
	entries, err := registry.Load(registryPath())
	if err != nil {
		return exitError(err)
	}

	kept, stale, err := registry.DropMissing(entries)
	if err != nil {
		return exitError(err)
	}

	roots := opts.roots
	if len(roots) == 0 {
		if opts.all {
			// Scan every registered repo directory downward for nested
			// .todo folders, rather than walking up to sibling directories.
			for _, e := range kept {
				roots = append(roots, e.Path)
			}
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return exitError(err)
			}
			roots = []string{cwd}
		}
	}

	unregistered, err := registry.FindUnregistered(roots, opts.depth, kept)
	if err != nil {
		return exitError(err)
	}

	out := outWriter(cmd)
	for _, e := range stale {
		_, _ = fmt.Fprintf(out, "stale\t%s\n", e.Path)
	}
	for _, p := range unregistered {
		_, _ = fmt.Fprintf(out, "unregistered\t%s\n", p)
	}

	if opts.fix {
		for _, p := range unregistered {
			kept = registerRepo(kept, p)
		}
		if err := registry.Save(registryPath(), kept); err != nil {
			return exitError(err)
		}
		_, _ = fmt.Fprintf(out, "reconciled: %d kept, %d dropped, %d added\n", len(kept), len(stale), len(unregistered))
	} else {
		_, _ = fmt.Fprintf(out, "summary: %d ok, %d stale, %d unregistered\n", len(kept), len(stale), len(unregistered))
	}

	return nil
}

// requireTaskRef validates and returns the first positional argument as a task
// reference.
func requireTaskRef(cmd *cli.Command) (string, error) {
	ref := strings.TrimSpace(cmd.Args().First())
	if ref == "" {
		return "", errors.New("task number required: provide a task reference such as TSK-001")
	}
	return ref, nil
}

// printTaskLine prints the updated task line and, when present, its companion
// note path.
func printTaskLine(out io.Writer, line, note string) {
	_, _ = fmt.Fprintln(out, line)
	if note != "" {
		_, _ = fmt.Fprintln(out, "note:", note)
	}
}

// runSchema prints the current list --json schema contract: the version and
// the stable status enum values.
func runSchema(out io.Writer) error {
	type schemaOutput struct {
		SchemaVersion int      `json:"schema_version"`
		StatusEnum    []string `json:"status_enum"`
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(schemaOutput{
		SchemaVersion: 1,
		StatusEnum:    []string{"open", "in_progress", "complete"},
	})
}

// parseState maps the --state flag value to a task Status filter.
func parseState(s string) todo.Status {
	switch s {
	case "progress":
		return todo.StatusInProgress
	case "done":
		return todo.StatusDone
	default:
		return todo.StatusOpen
	}
}

// dashIfEmpty returns "-" for empty strings, used to keep columns aligned.
func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// jsonTask is the machine-readable representation of a task for --json.
type jsonTask struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	StatusSymbol string `json:"status_symbol"`
	Priority     string `json:"priority"`
	Opened       string `json:"opened"`
	Claimed      string `json:"claimed,omitempty"`
	AgeDays      *int   `json:"age_days,omitempty"`
	Summary      string `json:"summary"`
	Disposition  string `json:"disposition,omitempty"`
	RepoPath     string `json:"repo_path,omitempty"`
	RepoProject  string `json:"repo_project,omitempty"`
}

// jsonListEnvelope wraps the list --json output in a versioned contract so
// consumers can detect schema drift.
type jsonListEnvelope struct {
	SchemaVersion int        `json:"schema_version"`
	Tasks         []jsonTask `json:"tasks"`
}

// writeJSON emits tasks in a versioned envelope with a stable schema for
// machine consumers.
func writeJSON(out io.Writer, tasks []listedTask) error {
	outTasks := make([]jsonTask, 0, len(tasks))
	for _, lt := range tasks {
		t := lt.Task
		disp, err := dispositionFor(lt)
		if err != nil {
			return err
		}
		jt := jsonTask{
			ID:           t.ID,
			Status:       t.Status.StatusName(),
			StatusSymbol: string(t.Status),
			Priority:     string(t.Priority),
			Opened:       t.Opened,
			Claimed:      t.Claimed,
			Summary:      t.Summary,
			RepoPath:     lt.repoPath,
			RepoProject:  lt.repoProject,
			Disposition:  string(disp),
		}
		if age := t.AgeDays(); age >= 0 {
			jt.AgeDays = &age
		}
		outTasks = append(outTasks, jt)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonListEnvelope{
		SchemaVersion: 1,
		Tasks:         outTasks,
	})
}

// dispositionFor returns the companion-note disposition for a listed task.
// A missing note returns DispositionClear; any other read error is returned
// so the caller can surface it instead of silently reporting "clear".
func dispositionFor(lt listedTask) (todo.Disposition, error) {
	var notePath string
	if lt.repoPath != "" {
		notePath = filepath.Join(lt.repoPath, ".todo", "notes", lt.ID+".md")
	} else if lt.notesDir != "" {
		notePath = filepath.Join(lt.notesDir, lt.ID+".md")
	} else {
		return todo.DispositionClear, nil
	}
	disp, err := todo.NoteDisposition(notePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return todo.DispositionClear, nil
		}
		return todo.DispositionClear, fmt.Errorf("read note disposition for %s: %w", notePath, err)
	}
	return disp, nil
}
