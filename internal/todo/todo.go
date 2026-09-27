// Package todo models and persists the ad-hoc task list in a Markdown file.
package todo

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/polymorcodeus/todo/internal/fs"
	"github.com/polymorcodeus/todo/internal/git"
)

// now is the clock; tests override it via setNow to make behavior deterministic.
var now = time.Now

func setNow(f func() time.Time) { now = f }

// Status is the on-disk checkbox state of a task.
type Status string

const (
	StatusOpen       Status = " "
	StatusInProgress Status = "o"
	StatusDone       Status = "x"
)

// parseStatus normalizes a checkbox char (accepting uppercase legacy forms)
// into a canonical Status.
func parseStatus(s string) Status {
	switch strings.ToLower(s) {
	case "o":
		return StatusInProgress
	case "x":
		return StatusDone
	default:
		return StatusOpen
	}
}

// describeStatus returns a human-readable description of a task's status for
// error messages.
func describeStatus(s Status) string {
	switch s {
	case StatusOpen:
		return "open"
	case StatusInProgress:
		return "in progress"
	case StatusDone:
		return "done"
	default:
		return "unknown"
	}
}

// Description returns a human-readable description of the status.
func (s Status) Description() string { return describeStatus(s) }

// StatusName returns the canonical status designation used in machine-readable
// output: "open", "in_progress", or "complete".
func (s Status) StatusName() string {
	switch s {
	case StatusOpen:
		return "open"
	case StatusInProgress:
		return "in_progress"
	case StatusDone:
		return "complete"
	default:
		return "unknown"
	}
}

// Priority is the tri-level importance of a task.
type Priority string

const (
	PriorityLow  Priority = "low"
	PriorityMed  Priority = "med"
	PriorityHigh Priority = "high"
)

// parsePriority validates a priority string.
func parsePriority(s string) (Priority, error) {
	switch Priority(s) {
	case PriorityLow, PriorityMed, PriorityHigh:
		return Priority(s), nil
	default:
		return "", fmt.Errorf("invalid priority %q: want low, med, or high", s)
	}
}

// Task is a single todo modeled by the fields in its serialized line.
type Task struct {
	ID       string
	Priority Priority
	Opened   string
	Status   Status
	Summary  string
	Claimed  string // date the task was picked up; empty until pickup
}

// String serializes a task back into its on-disk line format.
func (t Task) String() string {
	claimed := ""
	if t.Claimed != "" {
		claimed = "[claimed:" + t.Claimed + "]"
	}
	return fmt.Sprintf("- [%s] [%s][priority:%s][opened:%s]%s %s",
		t.Status, t.ID, t.Priority, t.Opened, claimed, t.Summary)
}

type header struct {
	project      string
	repo         string
	lastUpdated  string
	configured   string
	legacySource string
	nextID       string // monotonic high-water mark (TSK-NNN); lazy-backfilled on write
	otherLines   []string
}

// taskRegex matches a serialized task line. The claimed field is optional so
// files written before claimed existed still parse.
var taskRegex = regexp.MustCompile(`^- \[(x|X|o|O|\s+)\] \[(TSK-\d{3,})\]\[priority:(high|med|low)\]\[opened:(\d{4}-\d{2}-\d{2})\](?:\[claimed:(\d{4}-\d{2}-\d{2})\])? (.+)$`)

// parseTask parses a serialized task line. It returns false for lines that do
// not match the task format.
func parseTask(line string) (Task, bool) {
	m := taskRegex.FindStringSubmatch(line)
	if len(m) < 6 {
		return Task{}, false
	}
	claimed := ""
	if m[5] != "" {
		claimed = m[5]
	}
	return Task{
		ID:       m[2],
		Priority: Priority(m[3]),
		Opened:   m[4],
		Status:   parseStatus(m[1]),
		Summary:  m[6],
		Claimed:  claimed,
	}, true
}

// AddResult describes a task created by Add.
type AddResult struct {
	ID          string
	Priority    Priority
	Line        string // serialized task line (also the would-be line in dry-run)
	NotePath    string
	NoteContent string
}

// Result describes the outcome of a task mutation: the affected task's
// serialized line and the full path of its companion note, if any.
type Result struct {
	Line string
	Note string
}

// RefOptions configures a single-task mutation (Pickup, Release, Remove).
type RefOptions struct {
	TodoPath string
	NotesDir string
	Ref      string // any form accepted by normalizeTaskRef
}

// CompleteOptions configures Complete. Clear removes the task line instead of
// marking it done.
type CompleteOptions struct {
	TodoPath string
	NotesDir string
	Ref      string
	Clear    bool
}

// Clear removes completed tasks according to their note disposition:
//   - no note: delete the task line (nothing to preserve or delete).
//   - work-order: delete the task line and its disposable note.
//   - park: delete the task line but preserve the park record note.
//   - record: delete the task line but preserve the todo-native record note.
//   - float (a note exists but has no recognized disposition, or cannot be
//     read at all): leave the task line for manual review.
//
// The todo file is rewritten before any work-order notes are deleted so a
// write failure cannot leave task lines pointing to missing notes.
func Clear(todoPath, notesDir string) (ClearResult, error) {
	tasks, h, err := parseTodoFile(todoPath)
	if err != nil {
		return ClearResult{}, fmt.Errorf("read todo file: %w", err)
	}

	var remaining []Task
	var result ClearResult
	var notesToDelete []string
	wrote := false

	for _, t := range tasks {
		if t.Status != StatusDone {
			remaining = append(remaining, t)
			continue
		}

		notePath := filepath.Join(notesDir, t.ID+".md")
		disp, err := NoteDisposition(notePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// No companion note exists: nothing to preserve or delete.
				result.RemovedClear = append(result.RemovedClear, t.ID)
				wrote = true
				continue
			}
			// The note exists but cannot be read (permissions, I/O). Keep the
			// line so an unreadable note is never discarded by accident.
			remaining = append(remaining, t)
			result.Float = append(result.Float, t.ID)
			continue
		}

		switch disp {
		case DispositionWorkOrder:
			notesToDelete = append(notesToDelete, notePath)
			result.RemovedWorkOrder = append(result.RemovedWorkOrder, t.ID)
			wrote = true
		case DispositionPark:
			result.Parked = append(result.Parked, t.ID)
			wrote = true
		case DispositionRecord:
			result.Recorded = append(result.Recorded, t.ID)
			wrote = true
		default:
			remaining = append(remaining, t)
			result.Float = append(result.Float, t.ID)
		}
	}

	if wrote {
		h.lastUpdated = now().Format("2006-01-02T15:04")
		if err := writeTodoFile(todoPath, h, remaining); err != nil {
			return ClearResult{}, fmt.Errorf("write todo file: %w", err)
		}
	}

	for _, notePath := range notesToDelete {
		if err := fs.RemoveFollowingSymlink(notePath); err != nil {
			return ClearResult{}, fmt.Errorf("delete note %s: %w", notePath, err)
		}
	}

	return result, nil
}

// AddOptions configures Add.
type AddOptions struct {
	TodoPath    string
	NotesDir    string
	Priority    string
	Summary     string
	CreateNote  bool
	NoteContent string // content to write into the note; empty means empty note
	NoteFile    string // path to an existing file to copy into the note (copy, not move)
	DryRun      bool   // preview what would be written without persisting

	// Note disposition flags. Kind "work-order" stamps a disposable marker;
	// Synopsis/Source stamp a todo-native record (the default when no
	// disposition flags are given); Category additionally stamps a park-shaped
	// record for interop. These are ignored when CreateNote is false.
	Kind     string
	Category string
	Synopsis string
	Source   string
}

// MaxSummaryLen caps the on-disk task summary in runes. A longer summary is
// truncated on the line and spilled into the companion note.
const MaxSummaryLen = 120

// truncateSummary shortens s to at most max runes, appending an ASCII ellipsis
// when it overflows. The bool reports whether truncation occurred.
func truncateSummary(s string, max int) (string, bool) {
	runes := []rune(s)
	if len(runes) <= max {
		return s, false
	}
	if max <= 3 {
		return string(runes[:max]), true
	}
	return string(runes[:max-3]) + "...", true
}

// spillBody prepends the full summary to a work-order note body, separating
// the two with a blank line when the body is non-empty.
func spillBody(summary, body string) string {
	if body == "" {
		return summary
	}
	return summary + "\n\n" + body
}

func Add(opts AddOptions) (AddResult, error) {
	priority, err := parsePriority(opts.Priority)
	if err != nil {
		return AddResult{}, err
	}

	if strings.ContainsAny(opts.Summary, "\n\r") {
		return AddResult{}, errors.New("task summary must be a single line")
	}

	tasks, header, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return AddResult{}, fmt.Errorf("read todo file: %w", err)
	}

	nextID := nextTaskID(tasks, header)

	fullSummary := opts.Summary
	lineSummary, spilled := truncateSummary(fullSummary, MaxSummaryLen)

	// An over-long summary spills into a disposable work-order note so the
	// full text survives truncation on the line.
	if spilled && !opts.CreateNote {
		opts.CreateNote = true
		opts.Kind = "work-order"
		opts.NoteContent = ""
		opts.NoteFile = ""
	}

	task := Task{
		ID:       nextID,
		Priority: priority,
		Opened:   now().Format("2006-01-02"),
		Status:   StatusOpen,
		Summary:  lineSummary,
	}

	result := AddResult{
		ID:       nextID,
		Priority: priority,
		Line:     task.String(),
	}

	// Resolve the note content once up front so dry-run can preview it.
	notePath := ""
	if opts.CreateNote {
		notePath = filepath.Join(opts.NotesDir, nextID+".md")
		result.NotePath = notePath

		content := opts.NoteContent
		if opts.NoteFile != "" {
			data, err := os.ReadFile(opts.NoteFile)
			if err != nil {
				return AddResult{}, fmt.Errorf("read note file: %w", err)
			}
			content = string(data)
		}

		if spilled && opts.Kind == "work-order" {
			content = spillBody(fullSummary, content)
		}

		content, err = buildNoteContent(content, opts.Kind, opts.Category, opts.Synopsis, opts.Source, fullSummary, task.Opened)
		if err != nil {
			return AddResult{}, err
		}
		result.NoteContent = content
	}

	if opts.DryRun {
		return result, nil
	}

	header.lastUpdated = now().Format("2006-01-02T15:04")
	if err := writeTodoFile(opts.TodoPath, header, append(tasks, task)); err != nil {
		return AddResult{}, fmt.Errorf("write todo file: %w", err)
	}

	if opts.CreateNote {
		if err := os.MkdirAll(opts.NotesDir, 0o755); err != nil {
			return AddResult{}, fmt.Errorf("create notes dir: %w", err)
		}
		if err := os.WriteFile(notePath, []byte(result.NoteContent), 0o644); err != nil {
			return AddResult{}, fmt.Errorf("write note: %w", err)
		}
	}

	return result, nil
}

func Init(todoPath string) error {
	exists, err := fs.VerifyExists(todoPath)
	if err != nil {
		return fmt.Errorf("stat todo file: %w", err)
	}
	if exists {
		return errors.New("todo already initialized - no action taken")
	}

	dir := filepath.Dir(todoPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create todo dir: %w", err)
	}

	repo := git.RemoteURL("origin")

	current := now()
	h := header{
		// The repo root is the grandparent of the todo file; derive the
		// project name from it so a missing remote yields a real name
		// instead of ".".
		project:      filepath.Base(filepath.Dir(filepath.Dir(todoPath))),
		repo:         repo,
		configured:   current.Format("2006-01-02"),
		lastUpdated:  current.Format("2006-01-02T15:04"),
		legacySource: "none",
	}

	if err := writeTodoFile(todoPath, h, nil); err != nil {
		return fmt.Errorf("write todo file: %w", err)
	}
	return nil
}

// Pickup marks the referenced task as in progress and returns its updated line
// plus the full path of its companion note, if any.
func Pickup(opts RefOptions) (Result, error) {
	tasks, h, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return Result{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return Result{}, err
	}

	if tasks[idx].Status != StatusOpen {
		return Result{}, fmt.Errorf("cannot pickup %s: task is %s", tasks[idx].ID, describeStatus(tasks[idx].Status))
	}

	tasks[idx].Status = StatusInProgress
	tasks[idx].Claimed = now().Format("2006-01-02")
	return writeUpdated(opts.TodoPath, h, tasks, idx, opts.NotesDir)
}

// Complete marks the referenced task done (or removes its line when clear is
// true) and returns the resulting line plus the full path of its companion
// note, if any.
func Complete(opts CompleteOptions) (Result, error) {
	tasks, h, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return Result{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return Result{}, err
	}

	if tasks[idx].Status != StatusInProgress {
		return Result{}, fmt.Errorf("cannot complete %s: task is %s", tasks[idx].ID, describeStatus(tasks[idx].Status))
	}

	note, exists := NotePath(opts.NotesDir, tasks[idx].ID)
	if !exists {
		note = ""
	}
	if opts.Clear {
		line := tasks[idx].String()
		tasks = append(tasks[:idx], tasks[idx+1:]...)
		if err := writeTodoFile(opts.TodoPath, h, tasks); err != nil {
			return Result{}, fmt.Errorf("write todo file: %w", err)
		}
		return Result{Line: line, Note: note}, nil
	}

	tasks[idx].Status = StatusDone
	tasks[idx].Claimed = ""
	return writeUpdated(opts.TodoPath, h, tasks, idx, opts.NotesDir)
}

// Release returns a picked-up task to open and drops its claimed date. It only
// accepts an in-progress task and returns the resulting line plus the full
// path of its companion note, if any.
func Release(opts RefOptions) (Result, error) {
	tasks, h, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return Result{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return Result{}, err
	}

	if tasks[idx].Status != StatusInProgress {
		return Result{}, fmt.Errorf("cannot release %s: task is %s", tasks[idx].ID, describeStatus(tasks[idx].Status))
	}

	tasks[idx].Status = StatusOpen
	tasks[idx].Claimed = ""
	return writeUpdated(opts.TodoPath, h, tasks, idx, opts.NotesDir)
}

// ReopenResult describes the outcome of Reopen. NoOp is true when the task
// was already open and no write was performed.
type ReopenResult struct {
	Result
	NoOp bool
}

// Reopen restores a completed task to open status. It is a no-op when the task
// is already open and an error when the task is not done (except for the
// already-open no-op case).
func Reopen(opts RefOptions) (ReopenResult, error) {
	tasks, h, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return ReopenResult{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return ReopenResult{}, err
	}

	note, exists := NotePath(opts.NotesDir, tasks[idx].ID)
	if !exists {
		note = ""
	}

	if tasks[idx].Status == StatusOpen {
		return ReopenResult{Result: Result{Line: tasks[idx].String(), Note: note}, NoOp: true}, nil
	}
	if tasks[idx].Status != StatusDone {
		return ReopenResult{}, fmt.Errorf("cannot reopen %s: task is %s", tasks[idx].ID, describeStatus(tasks[idx].Status))
	}

	tasks[idx].Status = StatusOpen
	tasks[idx].Claimed = ""
	res, err := writeUpdated(opts.TodoPath, h, tasks, idx, opts.NotesDir)
	if err != nil {
		return ReopenResult{}, err
	}
	return ReopenResult{Result: res}, nil
}

// Remove deletes the referenced task line regardless of its status and returns
// the removed line (unchanged) plus the full path of its companion note, if any.
func Remove(opts RefOptions) (Result, error) {
	tasks, h, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return Result{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return Result{}, err
	}

	note, exists := NotePath(opts.NotesDir, tasks[idx].ID)
	if !exists {
		note = ""
	}

	line := tasks[idx].String()
	tasks = append(tasks[:idx], tasks[idx+1:]...)
	h.lastUpdated = now().Format("2006-01-02T15:04")
	if err := writeTodoFile(opts.TodoPath, h, tasks); err != nil {
		return Result{}, fmt.Errorf("write todo file: %w", err)
	}
	return Result{Line: line, Note: note}, nil
}

// ArchiveOptions configures Archive.
type ArchiveOptions struct {
	TodoPath   string
	NotesDir   string
	ArchiveDir string
	Ref        string
	Name       string // archived note basename; default "<ID>.md"
	Synopsis   string // asserted relevance why; empty means the note's existing synopsis
}

// ArchiveResult describes the outcome of Archive.
type ArchiveResult struct {
	ID          string
	Line        string // the removed task line, unchanged
	NotePath    string // the source companion note path
	ArchivePath string
	Synopsis    string
}

// Archive retires a completed task: it moves the task's companion note into
// .todo/archive and removes the task line. The task must be done [x] and must
// have a companion note. The archived synopsis is asserted here: it defaults to
// the note's existing synopsis and is overridden by opts.Synopsis; an empty
// result is an error, never a silently unlabeled archive entry. Creation of the
// archive directory and its first file happen in one pass so the directory is
// never left empty between writes.
func Archive(opts ArchiveOptions) (ArchiveResult, error) {
	tasks, h, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return ArchiveResult{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return ArchiveResult{}, err
	}
	task := tasks[idx]
	if task.Status != StatusDone {
		return ArchiveResult{}, fmt.Errorf("cannot archive %s: task is %s (only done tasks can be archived)", task.ID, describeStatus(task.Status))
	}

	notePath, exists := NotePath(opts.NotesDir, task.ID)
	if !exists {
		return ArchiveResult{}, fmt.Errorf("cannot archive %s: no companion note to retire", task.ID)
	}
	content, err := readNoteFull(notePath)
	if err != nil {
		return ArchiveResult{}, fmt.Errorf("read note: %w", err)
	}

	fields := parseNoteFields(content)
	synopsis := opts.Synopsis
	if strings.TrimSpace(synopsis) == "" {
		synopsis = fields.synopsis
	}
	if strings.TrimSpace(synopsis) == "" {
		return ArchiveResult{}, fmt.Errorf("cannot archive %s: a synopsis is required (pass --synopsis)", task.ID)
	}
	created := fields.created
	if created == "" {
		created = now().Format("2006-01-02")
	}
	source := fields.source
	if source == "" {
		source = defaultRecordSource
	}
	if err := validateFrontmatterScalar("synopsis", synopsis); err != nil {
		return ArchiveResult{}, err
	}
	if err := validateFrontmatterScalar("source", source); err != nil {
		return ArchiveResult{}, err
	}

	name := opts.Name
	if name == "" {
		name = task.ID
	}
	name = filepath.Base(name)
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	archivePath := filepath.Join(opts.ArchiveDir, name)
	if _, err := os.Stat(archivePath); err == nil {
		return ArchiveResult{}, fmt.Errorf("archive note already exists: %s", archivePath)
	}

	archived := renderRecord(created, source, synopsis, strings.TrimSuffix(fields.body, "\n"))

	if err := os.MkdirAll(opts.ArchiveDir, 0o755); err != nil {
		return ArchiveResult{}, fmt.Errorf("create archive dir: %w", err)
	}
	if err := os.WriteFile(archivePath, []byte(archived), 0o644); err != nil {
		return ArchiveResult{}, fmt.Errorf("write archive note: %w", err)
	}

	line := task.String()
	h.lastUpdated = now().Format("2006-01-02T15:04")
	remaining := append(tasks[:idx], tasks[idx+1:]...)
	if err := writeTodoFile(opts.TodoPath, h, remaining); err != nil {
		// Best-effort rollback: don't leave an archive file when the task
		// line could not be removed.
		_ = os.Remove(archivePath)
		return ArchiveResult{}, fmt.Errorf("write todo file: %w", err)
	}

	if err := os.Remove(notePath); err != nil {
		return ArchiveResult{}, fmt.Errorf("remove source note: %w", err)
	}

	return ArchiveResult{
		ID:          task.ID,
		Line:        line,
		NotePath:    notePath,
		ArchivePath: archivePath,
		Synopsis:    synopsis,
	}, nil
}

// ArchivedNote is one entry in a repo's .todo/archive.
type ArchivedNote struct {
	Name     string // file basename
	Path     string
	Synopsis string
}

// ListArchived returns the archived notes in archiveDir, sorted by filename. A
// missing directory is not an error: it means nothing has been archived yet.
func ListArchived(archiveDir string) ([]ArchivedNote, error) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read archive dir: %w", err)
	}

	notes := make([]ArchivedNote, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(archiveDir, e.Name())
		content, err := readNoteFull(path)
		if err != nil {
			return nil, fmt.Errorf("read archived note %s: %w", e.Name(), err)
		}
		notes = append(notes, ArchivedNote{
			Name:     e.Name(),
			Path:     path,
			Synopsis: parseNoteFields(content).synopsis,
		})
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, nil
}

// ClearResult describes the outcome of clearing completed tasks from a single
// repo based on their companion-note disposition.
type ClearResult struct {
	RemovedClear     []string // completed IDs removed because no companion note existed
	RemovedWorkOrder []string // IDs removed and whose notes were deleted
	Parked           []string // IDs removed but whose park notes were kept
	Recorded         []string // IDs removed but whose todo-native record notes were kept
	Float            []string // completed IDs left in the file for review
}

// BumpResult describes the outcome of Bump. NoOp is true when the task was
// already at the boundary (high when bumping up, low when bumping down) and
// no write was performed.
type BumpResult struct {
	Result
	NoOp bool
}

// Bump changes a task's priority up or down by one level. Bumping up when
// already high, or down when already low, is a no-op that returns the current
// line without writing.
func Bump(todoPath, notesDir, ref string, down bool) (BumpResult, error) {
	tasks, h, err := parseTodoFile(todoPath)
	if err != nil {
		return BumpResult{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, ref)
	if err != nil {
		return BumpResult{}, err
	}

	note, exists := NotePath(notesDir, tasks[idx].ID)
	if !exists {
		note = ""
	}

	next := bumpPriority(tasks[idx].Priority, down)
	if next == tasks[idx].Priority {
		return BumpResult{Result: Result{Line: tasks[idx].String(), Note: note}, NoOp: true}, nil
	}

	tasks[idx].Priority = next
	res, err := writeUpdated(todoPath, h, tasks, idx, notesDir)
	if err != nil {
		return BumpResult{}, err
	}
	return BumpResult{Result: res}, nil
}

// bumpPriority returns the next priority level: up goes low→med→high→high,
// down goes high→med→low→low.
func bumpPriority(p Priority, down bool) Priority {
	if down {
		switch p {
		case PriorityHigh:
			return PriorityMed
		case PriorityMed:
			return PriorityLow
		default:
			return PriorityLow
		}
	}
	switch p {
	case PriorityLow:
		return PriorityMed
	case PriorityMed:
		return PriorityHigh
	default:
		return PriorityHigh
	}
}

// writeUpdated persists tasks after a status change and returns the updated
// line and note for the task at idx.
func writeUpdated(todoPath string, h header, tasks []Task, idx int, notesDir string) (Result, error) {
	h.lastUpdated = now().Format("2006-01-02T15:04")
	if err := writeTodoFile(todoPath, h, tasks); err != nil {
		return Result{}, fmt.Errorf("write todo file: %w", err)
	}
	note, exists := NotePath(notesDir, tasks[idx].ID)
	if !exists {
		note = ""
	}
	return Result{Line: tasks[idx].String(), Note: note}, nil
}

func renderHeader(h header) string {
	var b strings.Builder

	b.WriteString("---\n")
	if h.project != "" {
		fmt.Fprintf(&b, "project: %s\n", h.project)
	}
	if h.repo != "" {
		fmt.Fprintf(&b, "repo: %s\n", h.repo)
	}
	fmt.Fprintf(&b, "last_updated: %s\n", h.lastUpdated)
	if h.configured != "" {
		fmt.Fprintf(&b, "configured: %s\n", h.configured)
	}
	if h.legacySource != "" {
		fmt.Fprintf(&b, "legacy_source: %s\n", h.legacySource)
	}
	if h.nextID != "" {
		fmt.Fprintf(&b, "next_id: %s\n", h.nextID)
	}
	for _, ol := range h.otherLines {
		b.WriteString(ol)
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")

	return b.String()
}

func writeTodoFile(path string, h header, tasks []Task) error {
	var b strings.Builder

	// Lazily backfill recognized header fields before writing so every
	// mutation converges toward a valid, self-describing header.
	normalizeHeader(&h, tasks)

	b.WriteString(renderHeader(h))

	for _, t := range tasks {
		b.WriteString(t.String())
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// normalizeHeader ensures recognized header fields are valid before a write,
// lazily backfilling missing, malformed, or stale values. It never decrements
// next_id: missing/invalid/too-low next_id is rewritten to max(id)+1, while a
// valid high mark is preserved so removed tasks are never re-issued.
func normalizeHeader(h *header, tasks []Task) {
	if n, ok := parseIDNum(h.nextID); !ok || n <= maxTaskNum(tasks) {
		h.nextID = formatID(maxTaskNum(tasks) + 1)
	}
	if h.lastUpdated == "" {
		h.lastUpdated = now().Format("2006-01-02T15:04")
	}
}

// SortField selects the task attribute used to order List results.
type SortField string

const (
	SortFieldPriority SortField = "priority"
	SortFieldOpened   SortField = "opened"
	SortFieldClaimed  SortField = "claimed"
	SortFieldAge      SortField = "age"
)

// ListFilter filters and sorts tasks returned by List. A nil State matches all
// states; StaleDays <= 0 disables stale filtering. An empty SortField preserves
// file order.
type ListFilter struct {
	State       *Status
	StaleDays   int
	SortField   SortField
	SortReverse bool
}

// List returns the tasks in a todo file, filtered by f and sorted according to
// f.SortField. File order is preserved when no sort field is requested.
func List(todoPath string, f ListFilter) ([]Task, error) {
	tasks, _, err := parseTodoFile(todoPath)
	if err != nil {
		return nil, fmt.Errorf("read todo file: %w", err)
	}

	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		age := t.AgeDays()
		if f.State != nil && t.Status != *f.State {
			continue
		}
		if f.StaleDays > 0 && (age < 0 || age < f.StaleDays) {
			continue
		}
		out = append(out, t)
	}

	if f.SortField != "" {
		sortTasks(out, f.SortField, f.SortReverse)
	}
	return out, nil
}

// sortTasks reorders tasks in-place using a stable sort. Missing claimed/age
// values always sort to the end, regardless of direction.
func sortTasks(tasks []Task, field SortField, reverse bool) {
	sort.SliceStable(tasks, func(i, j int) bool {
		return taskCompare(tasks[i], tasks[j], field, reverse) < 0
	})
}

func taskCompare(a, b Task, field SortField, reverse bool) int {
	switch field {
	case SortFieldPriority:
		return applyReverse(cmp.Compare(priorityRank(a.Priority), priorityRank(b.Priority)), reverse)
	case SortFieldOpened:
		return applyReverse(strings.Compare(a.Opened, b.Opened), reverse)
	case SortFieldClaimed:
		return compareNullableString(a.Claimed, b.Claimed, reverse)
	case SortFieldAge:
		return compareNullableInt(a.AgeDays(), b.AgeDays(), -1, reverse)
	}
	return 0
}

func applyReverse(cmp int, reverse bool) int {
	if reverse && cmp != 0 {
		return -cmp
	}
	return cmp
}

func priorityRank(p Priority) int {
	switch p {
	case PriorityHigh:
		return 3
	case PriorityMed:
		return 2
	case PriorityLow:
		return 1
	}
	return 0
}

func compareNullableString(a, b string, reverse bool) int {
	aMissing := a == ""
	bMissing := b == ""
	if aMissing && bMissing {
		return 0
	}
	if aMissing {
		return 1
	}
	if bMissing {
		return -1
	}
	return applyReverse(strings.Compare(a, b), reverse)
}

func compareNullableInt(a, b, missing int, reverse bool) int {
	aMissing := a == missing
	bMissing := b == missing
	if aMissing && bMissing {
		return 0
	}
	if aMissing {
		return 1
	}
	if bMissing {
		return -1
	}
	return applyReverse(cmp.Compare(a, b), reverse)
}

// AgeDays returns the number of full days since the task was claimed, or -1
// when the task has no claimed date (i.e. is not in progress).
func (t Task) AgeDays() int {
	if t.Claimed == "" {
		return -1
	}
	claimed, err := time.Parse("2006-01-02", t.Claimed)
	if err != nil {
		return -1
	}
	now := now().Truncate(24 * time.Hour)
	return int(now.Sub(claimed).Hours() / 24)
}

// OpenedDays returns the number of full days since the task was opened.
func (t Task) OpenedDays() int {
	opened, err := time.Parse("2006-01-02", t.Opened)
	if err != nil {
		return -1
	}
	now := now().Truncate(24 * time.Hour)
	return int(now.Sub(opened).Hours() / 24)
}

func parseTodoFile(path string) ([]Task, header, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, header{}, err
	}
	//nolint:errcheck // read-only file; close error not meaningful here
	defer f.Close()

	var tasks []Task
	var h header
	inHeader := false
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "---" {
			inHeader = !inHeader
			continue
		}

		if inHeader {
			if after, ok := strings.CutPrefix(line, "project:"); ok {
				h.project = strings.TrimSpace(after)
			} else if after, ok := strings.CutPrefix(line, "repo:"); ok {
				h.repo = strings.TrimSpace(after)
			} else if after0, ok0 := strings.CutPrefix(line, "configured:"); ok0 {
				h.configured = strings.TrimSpace(after0)
			} else if after1, ok1 := strings.CutPrefix(line, "legacy_source:"); ok1 {
				h.legacySource = strings.TrimSpace(after1)
			} else if after2, ok2 := strings.CutPrefix(line, "last_updated:"); ok2 {
				h.lastUpdated = strings.TrimSpace(after2)
			} else if after3, ok3 := strings.CutPrefix(line, "next_id:"); ok3 {
				h.nextID = strings.TrimSpace(after3)
			} else {
				h.otherLines = append(h.otherLines, line)
			}
			continue
		}

		if strings.TrimSpace(line) == "" {
			continue
		}

		if t, ok := parseTask(line); ok {
			tasks = append(tasks, t)
		}
	}

	return tasks, h, scanner.Err()
}

// maxTaskNum returns the highest numeric part of a task ID, or 0 when absent.
func maxTaskNum(tasks []Task) int {
	max := 0
	for _, t := range tasks {
		if n, ok := parseIDNum(t.ID); ok && n > max {
			max = n
		}
	}
	return max
}

// parseIDNum parses a TSK-NNN (or bare number) identifier into its numeric
// part. ok is false for empty, malformed, or out-of-range values.
func parseIDNum(s string) (int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToUpper(s), "TSK-")
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 999999 {
		return 0, false
	}
	return n, true
}

// formatID renders a numeric task id as its zero-padded TSK-NNN form.
func formatID(n int) string {
	return fmt.Sprintf("TSK-%03d", n)
}

// nextTaskID returns the ID to issue for the next task: the larger of the
// header's next_id high-water mark (when valid) and max(current ids)+1.
func nextTaskID(tasks []Task, h header) string {
	candidate := maxTaskNum(tasks) + 1
	if n, ok := parseIDNum(h.nextID); ok && n > candidate {
		candidate = n
	}
	return formatID(candidate)
}

func normalizeTaskRef(ref string) (string, error) {
	s := strings.TrimSpace(ref)
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimPrefix(strings.ToUpper(s), "TSK-")
	if s == "" {
		return "", fmt.Errorf("invalid task reference %q", ref)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 999999 {
		return "", fmt.Errorf("invalid task reference %q: want TSK-NNN, NNN, N, or #N", ref)
	}
	return fmt.Sprintf("TSK-%03d", n), nil
}

func findTaskIndex(tasks []Task, ref string) (int, error) {
	id, err := normalizeTaskRef(ref)
	if err != nil {
		return -1, err
	}
	for i, t := range tasks {
		if t.ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("task %s not found", id)
}

// NotePath returns the full path of a task's companion note in .todo/notes and
// whether it currently exists on disk. It is empty when absent, so callers that
// only need the path can discard the existence flag. Stat errors other than
// "not exist" are treated as absent as well, matching the existing callers.
func NotePath(notesDir, id string) (string, bool) {
	path := filepath.Join(notesDir, id+".md")
	if _, err := os.Stat(path); err != nil {
		return path, false
	}
	return path, true
}

// DetailOptions configures Detail.
type DetailOptions struct {
	TodoPath string
	NotesDir string
	Ref      string
	Lines    int  // max note lines to preview; <=0 means unlimited
	NoNote   bool // skip note preview
	Full     bool // return the complete note body instead of a preview
}

// DetailResult is the read-only result of Detail.
type DetailResult struct {
	Task          Task
	NotePath      string
	NoteExists    bool
	Disposition   Disposition // park, record, work-order, clear (no note), or float
	NotePreview   string      // first Lines lines of the note, if any
	NoteTruncated bool        // true when more lines exist beyond the preview
	NoteBody      string      // full note body, when Full is requested
}

// Detail returns full information about a single task plus a preview of its
// companion note. It never mutates the todo file or header.
func Detail(opts DetailOptions) (DetailResult, error) {
	tasks, _, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return DetailResult{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return DetailResult{}, err
	}

	task := tasks[idx]
	notePath, exists := NotePath(opts.NotesDir, task.ID)
	if !exists {
		notePath = filepath.Join(opts.NotesDir, task.ID+".md")
	}

	res := DetailResult{
		Task:        task,
		NotePath:    notePath,
		NoteExists:  exists,
		Disposition: DispositionClear,
	}

	if exists {
		disp, err := NoteDisposition(notePath)
		if err != nil {
			return DetailResult{}, fmt.Errorf("read note disposition: %w", err)
		}
		res.Disposition = disp
	}

	if !opts.NoNote && exists {
		if opts.Full {
			body, err := readNoteFull(notePath)
			if err != nil {
				return DetailResult{}, fmt.Errorf("read note: %w", err)
			}
			res.NoteBody = body
		} else {
			preview, truncated, err := readNotePreview(notePath, opts.Lines)
			if err != nil {
				return DetailResult{}, fmt.Errorf("read note: %w", err)
			}
			res.NotePreview = preview
			res.NoteTruncated = truncated
		}
	}

	return res, nil
}

// readNotePreview reads up to lines from path. A lines value <= 0 means no
// limit. It returns the preview text and whether more content follows.
func readNotePreview(path string, lines int) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	//nolint:errcheck // read-only file; close error not meaningful here
	defer f.Close()

	r := bufio.NewReader(f)
	var out []string
	for i := 0; ; i++ {
		if lines > 0 && i >= lines {
			// Read one extra line to detect whether content was truncated.
			_, err := r.ReadString('\n')
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", false, err
			}
			return strings.Join(out, "\n"), true, nil
		}

		line, err := r.ReadString('\n')
		if line != "" {
			out = append(out, strings.TrimSuffix(line, "\n"))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", false, err
		}
	}
	return strings.Join(out, "\n"), false, nil
}

// readNoteFull returns the entire contents of the note file. It follows
// symlinks so lnk-managed notes are read transparently.
func readNoteFull(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
