package todo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// handoffFixture writes a todo file with the given tasks plus a companion note
// for every task in notes. It returns the todo path and the notes directory.
func handoffFixture(t *testing.T, tasks []Task, notes map[string]string) (string, string) {
	t.Helper()
	todoPath, notesDir := writeTestTodo(t, tasks)
	if len(notes) == 0 {
		return todoPath, notesDir
	}
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, content := range notes {
		if err := os.WriteFile(filepath.Join(notesDir, id+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return todoPath, notesDir
}

func readNoteFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// recordNote is a task note carrying frontmatter and one other section, so the
// handoff has to slot itself in without disturbing either.
func recordNote(synopsis, problem string) string {
	return "---\nkind: record\ncreated: 2026-01-01\nsource: repo\nsynopsis: " + synopsis +
		"\n---\n\n## Problem\n\n" + problem + "\n"
}

func TestHandoffWritesReplaceableSection(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "the task"},
	}, map[string]string{"TSK-001": recordNote("the task", "the original brief")})

	res, err := Handoff(HandoffOptions{
		TodoPath: todoPath,
		NotesDir: notesDir,
		Ref:      "TSK-001",
		Body:     "## State\n\nparser half done\n\n### Verify\n\nmake test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Date != "2026-08-18" || res.Replaced || res.NoteCreated {
		t.Errorf("result = %+v, want a fresh 2026-08-18 handoff", res)
	}

	note := readNoteFile(t, res.NotePath)
	want := "---\nkind: record\ncreated: 2026-01-01\nsource: repo\nsynopsis: the task\n---\n\n" +
		"## Handoff (2026-08-18)\n\n## State\n\nparser half done\n\n### Verify\n\nmake test\n\n" +
		handoffEndMarker + "\n\n## Problem\n\nthe original brief\n"
	if note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	if n := countFrontmatterDelims(note); n != 2 {
		t.Errorf("frontmatter delimiters = %d, want 2", n)
	}

	// A second handoff replaces the first: one heading, one marker, new text.
	res, err = Handoff(HandoffOptions{
		TodoPath: todoPath,
		NotesDir: notesDir,
		Ref:      "TSK-001",
		Body:     "## State\n\nfinal shape\n\n### Verify\n\nmake check",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced {
		t.Error("second handoff did not report a replacement")
	}

	note = readNoteFile(t, res.NotePath)
	if got := strings.Count(note, handoffHeading); got != 1 {
		t.Errorf("handoff headings = %d, want 1 in %q", got, note)
	}
	if got := strings.Count(note, handoffEndMarker); got != 1 {
		t.Errorf("end markers = %d, want 1 in %q", got, note)
	}
	if strings.Contains(note, "parser half done") {
		t.Errorf("note still carries the replaced handoff: %q", note)
	}
	for _, want := range []string{"final shape", "make check", "## Problem", "the original brief"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q: %q", want, note)
		}
	}
	if !strings.HasSuffix(note, "\n") || strings.HasSuffix(note, "\n\n") {
		t.Errorf("note does not end with exactly one newline: %q", note)
	}
}

func TestHandoffRefusesTasksItDoesNotHold(t *testing.T) {
	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusOpen, Summary: "open"},
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "done"},
	}, nil)

	for _, ref := range []string{"TSK-001", "TSK-002"} {
		_, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: ref, Body: "state"})
		if err == nil {
			t.Fatalf("handoff %s: expected a guard error", ref)
		}
		if !strings.Contains(err.Error(), "cannot handoff "+ref) {
			t.Errorf("handoff %s error = %q, want the guard to be named", ref, err.Error())
		}
		if _, err := os.Stat(filepath.Join(notesDir, ref+".md")); !os.IsNotExist(err) {
			t.Errorf("handoff %s wrote a note: %v", ref, err)
		}
	}
}

func TestHandoffCreatesNoteWithOneBlock(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "no note yet"},
	}, nil)

	res, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001", Body: "## State\n\nfresh"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoteCreated {
		t.Error("handoff on a note-less task did not report creating the note")
	}

	note := readNoteFile(t, res.NotePath)
	want := "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: no note yet\n---\n\n" +
		"## Handoff (2026-08-18)\n\n## State\n\nfresh\n\n" + handoffEndMarker + "\n"
	if note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	if n := countFrontmatterDelims(note); n != 2 {
		t.Errorf("frontmatter delimiters = %d, want 2", n)
	}
	if disp, err := NoteDisposition(res.NotePath); err != nil || disp != DispositionRecord {
		t.Errorf("disposition = %q (err %v), want record", disp, err)
	}
}

func TestHandoffBodyFileDropsDocFrontmatter(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "from doc"},
	}, nil)

	doc := filepath.Join(t.TempDir(), "session.md")
	content := "---\ntitle: Session doc\nstatus: draft\n---\n\n## State\n\nfrom a doc\n\n### Verify\n\nmake all\n"
	if err := os.WriteFile(doc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001", BodyFile: doc})
	if err != nil {
		t.Fatal(err)
	}

	note := readNoteFile(t, res.NotePath)
	if strings.Contains(note, "title: Session doc") || strings.Contains(note, "status: draft") {
		t.Errorf("doc frontmatter crossed into the note: %q", note)
	}
	if n := countFrontmatterDelims(note); n != 2 {
		t.Errorf("frontmatter delimiters = %d, want 2 in %q", n, note)
	}
	if !strings.Contains(note, "## State\n\nfrom a doc\n\n### Verify\n\nmake all") {
		t.Errorf("note does not carry the doc body: %q", note)
	}
}

func TestHandoffNeedsText(t *testing.T) {
	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "task"},
	}, nil)

	_, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001", Body: "   \n  "})
	if err == nil {
		t.Fatal("handoff without text: expected an error")
	}
	if !strings.Contains(err.Error(), "needs text") {
		t.Errorf("error = %q, want it to ask for text", err.Error())
	}
	if _, err := os.Stat(filepath.Join(notesDir, "TSK-001.md")); !os.IsNotExist(err) {
		t.Errorf("empty handoff wrote a note: %v", err)
	}
}

func TestHandoffLeavesTodoFileUntouched(t *testing.T) {
	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "task"},
	}, map[string]string{"TSK-001": recordNote("task", "brief")})

	before := readNoteFile(t, todoPath)
	if _, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001", Body: "state"}); err != nil {
		t.Fatal(err)
	}
	if after := readNoteFile(t, todoPath); after != before {
		t.Errorf("handoff rewrote the todo file:\nbefore %q\nafter  %q", before, after)
	}
}

func TestResumeReadsHandoffAndVerify(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "task"},
	}, nil)

	body := "## State\n\nparser half done\n\n### Verify\n\nmake test\nmake lint"
	if _, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001", Body: body}); err != nil {
		t.Fatal(err)
	}

	entries, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "tsk-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.Date != "2026-08-18" {
		t.Errorf("date = %q, want 2026-08-18", got.Date)
	}
	if got.Handoff != body {
		t.Errorf("handoff = %q, want %q", got.Handoff, body)
	}
	if got.Verify != "make test\nmake lint" {
		t.Errorf("verify = %q, want the commands alone", got.Verify)
	}
}

func TestResumeRefWithoutHandoffErrors(t *testing.T) {
	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "in flight"},
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "noted"},
	}, map[string]string{"TSK-002": recordNote("noted", "brief")})

	_, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001"})
	if err == nil {
		t.Fatal("resume of a task with no handoff: expected an error")
	}
	if !strings.Contains(err.Error(), "no handoff recorded for TSK-001") {
		t.Errorf("error = %q, want the missing handoff to be named", err.Error())
	}

	if _, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-002"}); err == nil {
		t.Error("resume of a note without a handoff section: expected an error")
	}
}

func TestResumeEnumeratesInProgressNewestFirst(t *testing.T) {
	tasks := []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-04", Summary: "january handoff"},
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-03", Summary: "march handoff"},
		{ID: "TSK-003", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "february handoff"},
		{ID: "TSK-004", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-05", Summary: "no handoff"},
		{ID: "TSK-005", Priority: "med", Opened: "2026-08-01", Status: StatusOpen, Summary: "open, skipped"},
		{ID: "TSK-006", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "done, skipped"},
	}
	todoPath, notesDir := handoffFixture(t, tasks, map[string]string{
		"TSK-004": recordNote("no handoff", "brief"),
		"TSK-005": recordNote("open, skipped", "brief"),
		"TSK-006": recordNote("done, skipped", "brief"),
	})

	for _, tc := range []struct{ ref, date string }{
		{"TSK-001", "2026-01-01"},
		{"TSK-002", "2026-03-01"},
		{"TSK-003", "2026-02-01"},
	} {
		date, err := time.Parse("2006-01-02", tc.date)
		if err != nil {
			t.Fatal(err)
		}
		setNow(func() time.Time { return date })
		if _, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: tc.ref, Body: "state " + tc.date}); err != nil {
			t.Fatal(err)
		}
	}
	defer setNow(time.Now)

	// A hand-written section without the closing marker still reads back, and
	// the following section stays out of it.
	handwritten := "---\nkind: record\ncreated: 2026-01-01\nsource: repo\nsynopsis: handwritten\n---\n\n" +
		"## Handoff (2025-12-31)\n\nby hand\n\n## Problem\n\nnot part of the handoff\n"
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-005.md"), []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir})
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for _, e := range entries {
		ids = append(ids, e.Task.ID)
	}
	want := []string{"TSK-002", "TSK-003", "TSK-001"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("ids = %v, want %v (in-progress with a handoff, newest first)", ids, want)
	}

	// The hand-written section reads back by ref, marker or not, and the
	// section that follows it stays out of the handoff.
	handwrittenEntries, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-005"})
	if err != nil {
		t.Fatal(err)
	}
	if got := handwrittenEntries[0].Handoff; got != "by hand" {
		t.Errorf("hand-written handoff = %q, want just the body", got)
	}
}

func TestHandoffRewritesHandWrittenSection(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "hand written"},
	}, map[string]string{
		"TSK-001": "---\nkind: record\ncreated: 2026-01-01\nsource: repo\nsynopsis: hand written\n---\n\n" +
			"## Handoff (2025-12-31)\n\nby hand\n\n## Problem\n\nkeep me\n",
	})

	res, err := Handoff(HandoffOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001", Body: "by hand again"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced {
		t.Error("rewriting a hand-written section did not report a replacement")
	}

	// The rewrite adopts the canonical marker, so the boundary stops depending
	// on whatever section happens to follow it.
	note := readNoteFile(t, res.NotePath)
	want := "---\nkind: record\ncreated: 2026-01-01\nsource: repo\nsynopsis: hand written\n---\n\n" +
		"## Handoff (2026-08-18)\n\nby hand again\n\n" + handoffEndMarker + "\n\n## Problem\n\nkeep me\n"
	if note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

func TestResumeRefAcceptsAnyStatus(t *testing.T) {
	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "finished"},
	}, map[string]string{
		"TSK-001": "---\nkind: record\ncreated: 2026-01-01\nsource: repo\nsynopsis: finished\n---\n\n" +
			"## Handoff (2026-01-05)\n\nlast state\n\n" + handoffEndMarker + "\n",
	})

	entries, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Handoff != "last state" {
		t.Errorf("entries = %+v, want the done task's handoff", entries)
	}

	all, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("bare resume = %+v, want a done task excluded", all)
	}
}

func TestResumeSurfacesUnreadableNote(t *testing.T) {
	todoPath, notesDir := handoffFixture(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Claimed: "2026-08-02", Summary: "task"},
	}, nil)

	// A directory where the note should be is an error, not a missing note:
	// only a genuinely absent file may be read as "no handoff".
	if err := os.MkdirAll(filepath.Join(notesDir, "TSK-001.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Resume(ResumeOptions{TodoPath: todoPath, NotesDir: notesDir}); err == nil {
		t.Fatal("resume with an unreadable note: expected an error")
	}
}
