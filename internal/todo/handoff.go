package todo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// handoffHeading is the level-2 heading that marks a task's replaceable handoff
// section. It is rendered as "## Handoff (YYYY-MM-DD)".
const handoffHeading = "## Handoff"

// handoffEndMarker closes a handoff section. The section body carries headings
// of its own (a "## State", a "### Verify"), so the boundary cannot be inferred
// from them; the marker makes the end explicit and renders as nothing.
const handoffEndMarker = "<!-- /handoff -->"

// verifyHeading is the level-3 heading that marks the runnable verify commands
// inside a handoff section, so resume can report them on their own.
const verifyHeading = "### Verify"

// HandoffOptions configures Handoff.
type HandoffOptions struct {
	TodoPath string
	NotesDir string
	Ref      string
	Body     string // the handoff text: state, verify commands, open/next
	BodyFile string // a file to read the handoff text from instead of Body
}

// HandoffResult reports what Handoff wrote to the companion note.
type HandoffResult struct {
	ID          string
	NotePath    string
	Date        string
	Replaced    bool // an earlier handoff section was replaced
	NoteCreated bool // the companion note did not exist and was created
}

// Handoff writes the referenced task's handoff section: a "## Handoff (date)"
// block holding the supplied text, placed directly below the note's
// frontmatter. An earlier handoff section is replaced, so a note keeps exactly
// one handoff however many times its work is passed on, and the note's other
// sections survive untouched.
//
// It only accepts an in-progress task - you can only hand off work you hold.
// The todo file is read but never written, so a handoff does not move
// last_updated. A task with no companion note gets one, stamped through the
// same single-block path add uses.
func Handoff(opts HandoffOptions) (HandoffResult, error) {
	tasks, _, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return HandoffResult{}, fmt.Errorf("read todo file: %w", err)
	}

	idx, err := findTaskIndex(tasks, opts.Ref)
	if err != nil {
		return HandoffResult{}, err
	}
	task := tasks[idx]
	if task.Status != StatusInProgress {
		return HandoffResult{}, fmt.Errorf("cannot handoff %s: task is %s", task.ID, describeStatus(task.Status))
	}

	body := strings.TrimSpace(opts.Body)
	if opts.BodyFile != "" {
		data, err := os.ReadFile(opts.BodyFile)
		if err != nil {
			return HandoffResult{}, fmt.Errorf("read handoff file: %w", err)
		}
		// The source is a free-standing doc: its frontmatter describes the doc,
		// so only its body lands in the note.
		body = strings.TrimSpace(stripFrontmatter(string(data)))
	}
	if body == "" {
		return HandoffResult{}, fmt.Errorf("handoff for %s needs text: pass --note-content, --note-file, or stdin", task.ID)
	}

	date := now().Format("2006-01-02")
	notePath := filepath.Join(opts.NotesDir, task.ID+".md")
	result := HandoffResult{ID: task.ID, NotePath: notePath, Date: date}

	var note string
	data, err := os.ReadFile(notePath)
	switch {
	case err == nil:
		note = string(data)
	case errors.Is(err, os.ErrNotExist):
		result.NoteCreated = true
		created, err := buildNoteContent(noteBuild{
			FallbackSynopsis: task.Summary,
			Created:          date,
		})
		if err != nil {
			return HandoffResult{}, err
		}
		note = created
	default:
		return HandoffResult{}, fmt.Errorf("read note: %w", err)
	}

	updated, replaced := upsertHandoffSection(note, date, body)
	result.Replaced = replaced

	if err := os.MkdirAll(opts.NotesDir, 0o755); err != nil {
		return HandoffResult{}, fmt.Errorf("create notes dir: %w", err)
	}
	if err := os.WriteFile(notePath, []byte(updated), 0o644); err != nil {
		return HandoffResult{}, fmt.Errorf("write note: %w", err)
	}
	return result, nil
}

// ResumeOptions configures Resume.
type ResumeOptions struct {
	TodoPath string
	NotesDir string
	Ref      string // empty enumerates every in-progress task carrying a handoff
}

// HandoffEntry is one task's recorded handoff, as read back by Resume.
type HandoffEntry struct {
	Task     Task
	NotePath string
	Date     string
	Handoff  string // the handoff section body, its heading excluded
	Verify   string // the ### Verify subsection body, empty when absent
}

// Resume reads back recorded handoffs. With a ref it returns that task's
// handoff whatever its status, and errors when none is recorded. With no ref it
// enumerates every in-progress task whose companion note carries a handoff,
// newest first: the re-entry point pickup refuses, since pickup rejects a task
// that is already claimed.
func Resume(opts ResumeOptions) ([]HandoffEntry, error) {
	tasks, _, err := parseTodoFile(opts.TodoPath)
	if err != nil {
		return nil, fmt.Errorf("read todo file: %w", err)
	}

	if opts.Ref != "" {
		idx, err := findTaskIndex(tasks, opts.Ref)
		if err != nil {
			return nil, err
		}
		entry, ok, err := handoffEntryFor(tasks[idx], opts.NotesDir)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("no handoff recorded for %s", tasks[idx].ID)
		}
		return []HandoffEntry{entry}, nil
	}

	var entries []HandoffEntry
	for _, task := range tasks {
		if task.Status != StatusInProgress {
			continue
		}
		entry, ok, err := handoffEntryFor(task, opts.NotesDir)
		if err != nil {
			return nil, err
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Date != entries[j].Date {
			return entries[i].Date > entries[j].Date
		}
		return entries[i].Task.ID < entries[j].Task.ID
	})
	return entries, nil
}

// handoffEntryFor reads one task's handoff. A task with no note, or a note with
// no handoff section, reports false. Any other read failure is returned, so an
// unreadable note is never mistaken for "no handoff".
func handoffEntryFor(task Task, notesDir string) (HandoffEntry, bool, error) {
	notePath := filepath.Join(notesDir, task.ID+".md")
	data, err := os.ReadFile(notePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return HandoffEntry{}, false, nil
		}
		return HandoffEntry{}, false, fmt.Errorf("read note %s: %w", notePath, err)
	}
	section, ok := parseHandoffSection(string(data))
	if !ok {
		return HandoffEntry{}, false, nil
	}
	return HandoffEntry{
		Task:     task,
		NotePath: notePath,
		Date:     section.Date,
		Handoff:  section.Body,
		Verify:   section.Verify,
	}, true, nil
}

// HandoffSection is a handoff read back off a companion note.
type HandoffSection struct {
	Date   string // the date the heading carries, empty when it carries none
	Body   string // the section body, its heading excluded
	Verify string // the ### Verify subsection body, empty when absent
}

// upsertHandoffSection writes the handoff section into note, replacing any
// handoff section already there. A fresh handoff leads the note body, directly
// below the frontmatter, so a resuming reader meets it first; an existing one
// keeps its place. The result ends with exactly one newline.
func upsertHandoffSection(note, date, body string) (string, bool) {
	prefix, rest := splitLeadingFrontmatter(note)
	updated, replaced := replaceHandoffSection(rest, renderHandoff(date, body))
	return strings.TrimRight(prefix+updated, "\n") + "\n", replaced
}

// renderHandoff renders the canonical handoff section. Its closing marker keeps
// the headings the body carries from ending the section early.
func renderHandoff(date, body string) string {
	return handoffHeading + " (" + date + ")" + "\n\n" + strings.Trim(body, "\n") +
		"\n\n" + handoffEndMarker + "\n"
}

// replaceHandoffSection swaps the handoff section in body for section. With no
// handoff present the section leads the body; with one present it takes that
// section's place and the surrounding blank lines are normalized to one.
func replaceHandoffSection(body, section string) (string, bool) {
	lines := strings.Split(body, "\n")

	start, end, replaced := handoffBounds(lines)
	if !replaced {
		start, end = 0, 0
	}

	head := ""
	if start > 0 {
		head = trimBlankLines(lines[:start])
	}
	tail := trimBlankLines(lines[end:])

	var b strings.Builder
	if head != "" {
		b.WriteString(head)
		b.WriteString("\n\n")
	}
	b.WriteString(section)
	if tail != "" {
		b.WriteString("\n")
		b.WriteString(tail)
	}
	return b.String(), replaced
}

// parseHandoffSection finds the note's handoff section and splits the optional
// ### Verify subsection out of it, so resume can report the commands that prove
// the work alongside the narrative.
func parseHandoffSection(note string) (HandoffSection, bool) {
	lines := strings.Split(note, "\n")

	start, end, ok := handoffBounds(lines)
	if !ok {
		return HandoffSection{}, false
	}

	section := dropEndMarker(lines[start+1 : end])
	return HandoffSection{
		Date:   handoffDate(lines[start]),
		Body:   trimBlankLines(section),
		Verify: verifySubsection(section),
	}, true
}

// handoffBounds locates the note's handoff section: the index of its heading
// and the index one past its last line. The closing marker counts as part of
// the extent; a section hand-written without one ends at the next heading at or
// above its own level.
func handoffBounds(lines []string) (start, end int, ok bool) {
	start = -1
	for i, line := range lines {
		if isHandoffHeading(line) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false
	}

	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == handoffEndMarker {
			return start, i + 1, true
		}
	}
	for i := start + 1; i < len(lines); i++ {
		if lvl := headingLevel(lines[i]); lvl == 1 || lvl == 2 {
			return start, i, true
		}
	}
	return start, len(lines), true
}

// dropEndMarker removes the closing marker line, which belongs to the section's
// extent but not to its body.
func dropEndMarker(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end > 0 && strings.TrimSpace(lines[end-1]) == handoffEndMarker {
		return lines[:end-1]
	}
	return lines
}

// verifySubsection returns the body of the ### Verify subsection, empty when
// the section carries none. A later heading at or above its own level belongs
// to the section, not to verify.
func verifySubsection(lines []string) string {
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), verifyHeading) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if lvl := headingLevel(lines[i]); lvl >= 1 && lvl <= 3 {
			end = i
			break
		}
	}
	return trimBlankLines(lines[start+1 : end])
}

// isHandoffHeading reports whether line opens a handoff section, bare or with a
// parenthesized date.
func isHandoffHeading(line string) bool {
	t := strings.TrimSpace(line)
	return t == handoffHeading || strings.HasPrefix(t, handoffHeading+" (")
}

// handoffDate extracts the date from a handoff heading. It is empty when the
// heading carries none.
func handoffDate(line string) string {
	t := strings.TrimSpace(line)
	open := strings.LastIndex(t, "(")
	closing := strings.LastIndex(t, ")")
	if open < 0 || closing <= open {
		return ""
	}
	return strings.TrimSpace(t[open+1 : closing])
}

// headingLevel reports the markdown heading level of line, and 0 when it is not
// a heading, so a "#tag" or a rendered "#hashtag" stays ordinary text.
func headingLevel(line string) int {
	t := strings.TrimSpace(line)
	hashes := 0
	for hashes < len(t) && t[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes > 6 || (hashes < len(t) && t[hashes] != ' ') {
		return 0
	}
	return hashes
}

// trimBlankLines joins lines, dropping the blank ones at either end so a
// rewritten section never accumulates empty lines.
func trimBlankLines(lines []string) string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}
