package todo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var sampleTasks = []Task{
	{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusOpen, Summary: "fix the thing"},
	{ID: "TSK-002", Priority: "med", Opened: "2026-08-02", Status: StatusInProgress, Summary: "refactor parser"},
	{ID: "TSK-003", Priority: "low", Opened: "2026-08-03", Status: StatusDone, Summary: "write docs"},
}

// parseTaskFields returns the display fields of a task line, using placeholders
// when the line is not a recognized task.
func parseTaskFields(line string) (id, priority, opened, status, summary string) {
	t, ok := parseTask(line)
	if !ok {
		return "-", "-", "-", "-", line
	}
	return t.ID, string(t.Priority), t.Opened, string(t.Status), t.Summary
}

func writeTestTodo(t *testing.T, tasks []Task) (string, string) {
	t.Helper()
	dir := t.TempDir()
	todoPath := filepath.Join(dir, ".todo", "todo.md")
	notesDir := filepath.Join(dir, ".todo", "notes")
	h := header{
		project:      "test",
		repo:         "http://example.com/repo",
		lastUpdated:  "2026-01-01T00:00",
		configured:   "2026-01-01",
		legacySource: "none",
	}
	if err := os.MkdirAll(filepath.Dir(todoPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTodoFile(todoPath, h, tasks); err != nil {
		t.Fatal(err)
	}
	return todoPath, notesDir
}

func readTasks(t *testing.T, todoPath string) []Task {
	t.Helper()
	tasks, _, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

// countFrontmatterDelims counts the --- delimiter lines in a note, which is how
// the tests assert a note carries exactly one frontmatter block.
func countFrontmatterDelims(content string) int {
	count := 0
	for line := range strings.SplitSeq(content, "\n") {
		if strings.TrimSpace(line) == "---" {
			count++
		}
	}
	return count
}

func TestNormalizeTaskRef(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		want string
	}{
		{"TSK-001", "TSK-001"},
		{"tsk-001", "TSK-001"},
		{"001", "TSK-001"},
		{"1", "TSK-001"},
		{"#1", "TSK-001"},
		{"TSK-042", "TSK-042"},
	} {
		got, err := normalizeTaskRef(tc.ref)
		if err != nil {
			t.Fatalf("normalizeTaskRef(%q): %v", tc.ref, err)
		}
		if got != tc.want {
			t.Errorf("normalizeTaskRef(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}

	for _, bad := range []string{"", "abc", "TSK-abc", "-1", "TSK-"} {
		if _, err := normalizeTaskRef(bad); err == nil {
			t.Errorf("normalizeTaskRef(%q): expected error", bad)
		}
	}
}

func TestParseStatus(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Status
	}{
		{" ", StatusOpen},
		{"o", StatusInProgress},
		{"x", StatusDone},
		{"O", StatusInProgress},
		{"X", StatusDone},
		{"unknown", StatusOpen},
	} {
		if got := parseStatus(tc.in); got != tc.want {
			t.Errorf("parseStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParsePriority(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Priority
	}{
		{"low", PriorityLow},
		{"med", PriorityMed},
		{"high", PriorityHigh},
	} {
		got, err := parsePriority(tc.in)
		if err != nil {
			t.Fatalf("parsePriority(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parsePriority(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{"urgent", "", "HIGH"} {
		if _, err := parsePriority(bad); err == nil {
			t.Errorf("parsePriority(%q): expected error", bad)
		}
	}
}

func TestTaskRoundTrip(t *testing.T) {
	for _, tt := range sampleTasks {
		line := tt.String()
		got, ok := parseTask(line)
		if !ok {
			t.Fatalf("parseTask(%q): not recognized", line)
		}
		if got != tt {
			t.Errorf("round trip = %+v, want %+v", got, tt)
		}
	}
}

func TestTaskRoundTripLargeID(t *testing.T) {
	for _, tt := range []Task{
		{ID: "TSK-1000", Priority: "med", Opened: "2026-08-01", Status: StatusOpen, Summary: "four digit"},
		{ID: "TSK-12345", Priority: "low", Opened: "2026-08-01", Status: StatusDone, Summary: "five digit"},
	} {
		line := tt.String()
		got, ok := parseTask(line)
		if !ok {
			t.Fatalf("parseTask(%q): not recognized", line)
		}
		if got != tt {
			t.Errorf("round trip = %+v, want %+v", got, tt)
		}
	}
}

func TestParseTaskFieldsAndNextID(t *testing.T) {
	id, pri, opened, status, sum := parseTaskFields(sampleTasks[0].String())
	if id != "TSK-001" || pri != "high" || opened != "2026-08-01" || status != " " || sum != "fix the thing" {
		t.Errorf("parseTaskFields got %q %q %q %q %q", id, pri, opened, status, sum)
	}
	if got := nextTaskID(sampleTasks, header{}); got != "TSK-004" {
		t.Errorf("nextTaskID = %q, want TSK-004", got)
	}
	if got := nextTaskID(sampleTasks, header{nextID: "TSK-010"}); got != "TSK-010" {
		t.Errorf("nextTaskID with high next_id = %q, want TSK-010", got)
	}
	if got := nextTaskID(nil, header{nextID: "TSK-004"}); got != "TSK-004" {
		t.Errorf("nextTaskID empty-with-next = %q, want TSK-004", got)
	}
	if got := nextTaskID(nil, header{nextID: "garbage"}); got != "TSK-001" {
		t.Errorf("nextTaskID malformed = %q, want TSK-001", got)
	}
}

func TestAdd(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)

	result, err := Add(AddOptions{
		TodoPath: todoPath,
		NotesDir: notesDir,
		Priority: "high",
		Summary:  "new task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "TSK-001" {
		t.Errorf("result.ID = %q, want TSK-001", result.ID)
	}
	if result.Priority != "high" {
		t.Errorf("result.Priority = %q, want high", result.Priority)
	}
	if result.NotePath != "" {
		t.Errorf("result.NotePath = %q, want empty", result.NotePath)
	}

	tasks := readTasks(t, todoPath)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if tasks[0].Opened != "2026-08-18" {
		t.Errorf("opened = %q, want 2026-08-18", tasks[0].Opened)
	}
	if tasks[0].Summary != "new task" {
		t.Errorf("summary = %q, want new task", tasks[0].Summary)
	}
}

func TestAddInvalidPriority(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, nil)

	_, err := Add(AddOptions{
		TodoPath: todoPath,
		NotesDir: notesDir,
		Priority: "urgent",
		Summary:  "new task",
	})
	if err == nil {
		t.Fatal("Add with invalid priority: expected error")
	}

	tasks := readTasks(t, todoPath)
	if len(tasks) != 0 {
		t.Errorf("invalid add wrote %d task(s), want 0", len(tasks))
	}
}

func TestPickup(t *testing.T) {
	claimed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return claimed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	res, err := Pickup(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "#1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Line, "- [o] [TSK-001]") {
		t.Errorf("pickup line = %q, want [o] TSK-001", res.Line)
	}
	if !strings.Contains(res.Line, "[claimed:2026-08-18]") {
		t.Errorf("pickup line = %q, want claimed date", res.Line)
	}
	if res.Note != "" {
		t.Errorf("note = %q, want empty (no notes dir)", res.Note)
	}

	tasks := readTasks(t, todoPath)
	if !strings.HasPrefix(tasks[0].String(), "- [o] [TSK-001]") {
		t.Errorf("task after pickup = %q", tasks[0].String())
	}
	if tasks[0].Claimed != "2026-08-18" {
		t.Errorf("task claimed = %q, want 2026-08-18", tasks[0].Claimed)
	}
	if tasks[1] != sampleTasks[1] {
		t.Errorf("unrelated task changed: %q", tasks[1].String())
	}

	_, hdr, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.lastUpdated == "2026-01-01T00:00" {
		t.Error("last_updated not updated by pickup")
	}
}

func TestNotePath(t *testing.T) {
	dir := t.TempDir()
	notesDir := filepath.Join(dir, ".todo", "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	path, exists := NotePath(notesDir, "TSK-001")
	if want := filepath.Join(notesDir, "TSK-001.md"); path != want {
		t.Errorf("NotePath path = %q, want %q", path, want)
	}
	if exists {
		t.Error("NotePath exists = true before note created")
	}

	if err := os.WriteFile(path, []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, exists := NotePath(notesDir, "TSK-001"); !exists {
		t.Error("NotePath exists = false after note created")
	}
}

func TestPickupWithNote(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-001.md"), []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Pickup(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(notesDir, "TSK-001.md"); res.Note != want {
		t.Errorf("note = %q, want %q", res.Note, want)
	}
}

func TestPickupNotFound(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Pickup(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "999"}); err == nil {
		t.Error("pickup of missing task: expected error")
	}
}

func TestPickupAlreadyClaimed(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Pickup(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "2"}); err == nil {
		t.Error("pickup of in-progress task: expected error")
	}
	if _, err := Pickup(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "3"}); err == nil {
		t.Error("pickup of done task: expected error")
	}

	tasks := readTasks(t, todoPath)
	if tasks[1].Status != StatusInProgress || tasks[2].Status != StatusDone {
		t.Error("failed pickup mutated task state")
	}
}

func TestComplete(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	res, err := Complete(CompleteOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-002", Clear: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Line, "- [x] [TSK-002]") {
		t.Errorf("complete line = %q, want [x] TSK-002", res.Line)
	}
	if res.Note != "" {
		t.Errorf("note = %q, want empty", res.Note)
	}

	tasks := readTasks(t, todoPath)
	if !strings.HasPrefix(tasks[1].String(), "- [x] [TSK-002]") {
		t.Errorf("task after complete = %q", tasks[1].String())
	}

	_, hdr, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.lastUpdated == "2026-01-01T00:00" {
		t.Error("last_updated not updated by complete")
	}
}

func TestCompleteNotPickedUp(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Complete(CompleteOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1", Clear: false}); err == nil {
		t.Error("complete of open task: expected error")
	}
	if _, err := Complete(CompleteOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "3", Clear: false}); err == nil {
		t.Error("complete of done task: expected error")
	}

	tasks := readTasks(t, todoPath)
	if tasks[0].Status != StatusOpen || tasks[2].Status != StatusDone {
		t.Error("failed complete mutated task state")
	}
}

func TestCompleteClear(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	res, err := Complete(CompleteOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "2", Clear: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Line, "- [o] [TSK-002]") {
		t.Errorf("cleared line = %q, want original TSK-002 line", res.Line)
	}
	if res.Note != "" {
		t.Errorf("note = %q, want empty", res.Note)
	}

	tasks := readTasks(t, todoPath)
	if len(tasks) != 2 {
		t.Fatalf("tasks after clear = %d, want 2", len(tasks))
	}
	if strings.Contains(tasks[0].String(), "TSK-002") || strings.Contains(tasks[1].String(), "TSK-002") {
		t.Errorf("TSK-002 still present after clear: %v", tasks)
	}
}

func TestCompleteParkNote(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-002.md"), []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Complete(CompleteOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-002", Clear: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Line, "- [o] [TSK-002]") {
		t.Errorf("cleared line = %q, want original TSK-002 line", res.Line)
	}
	if want := filepath.Join(notesDir, "TSK-002.md"); res.Note != want {
		t.Errorf("note = %q, want %q", res.Note, want)
	}
}

func TestCompleteDropsClaimed(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	tasks := readTasks(t, todoPath)
	tasks[1].Claimed = "2026-08-18"
	_ = writeTodoFile(todoPath, header{project: "test", repo: "http://example.com/repo", lastUpdated: "2026-01-01T00:00", configured: "2026-01-01", legacySource: "none"}, tasks)

	res, err := Complete(CompleteOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-002", Clear: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Line, "claimed") {
		t.Errorf("completed line still has claimed: %q", res.Line)
	}
	after := readTasks(t, todoPath)
	if after[1].Claimed != "" {
		t.Errorf("claimed not dropped on complete: %q", after[1].String())
	}
}

func TestClearByDisposition(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusDone, Summary: "work order task"},
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-02", Status: StatusDone, Summary: "park task"},
		{ID: "TSK-003", Priority: "low", Opened: "2026-08-03", Status: StatusDone, Summary: "float task"},
		{ID: "TSK-004", Priority: "low", Opened: "2026-08-04", Status: StatusOpen, Summary: "open task"},
	})
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-001.md"), []byte("---\nkind: work-order\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-002.md"), []byte("---\ncategory: areas\ncreated: 2026-08-02\nsource: repo\nsynopsis: park\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	// TSK-003 has no note -> clear (removed, no note to delete).

	res, err := Clear(todoPath, notesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.RemovedWorkOrder) != 1 || res.RemovedWorkOrder[0] != "TSK-001" {
		t.Errorf("removed work-order = %v, want [TSK-001]", res.RemovedWorkOrder)
	}
	if len(res.Parked) != 1 || res.Parked[0] != "TSK-002" {
		t.Errorf("parked = %v, want [TSK-002]", res.Parked)
	}
	if len(res.RemovedClear) != 1 || res.RemovedClear[0] != "TSK-003" {
		t.Errorf("removed clear = %v, want [TSK-003]", res.RemovedClear)
	}
	if len(res.Float) != 0 {
		t.Errorf("float = %v, want none", res.Float)
	}

	tasks := readTasks(t, todoPath)
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	want := []string{"TSK-004"}
	if len(ids) != len(want) {
		t.Fatalf("remaining tasks = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("remaining task %d = %q, want %q", i, ids[i], want[i])
		}
	}

	if _, err := os.Stat(filepath.Join(notesDir, "TSK-001.md")); !os.IsNotExist(err) {
		t.Errorf("work-order note not deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(notesDir, "TSK-002.md")); err != nil {
		t.Errorf("park note deleted: %v", err)
	}
}

func TestClearUnreadableNoteFloats(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "unreadable note"},
	})
	// A directory where the note file is expected makes the read fail with
	// something other than "not exist".
	if err := os.MkdirAll(filepath.Join(notesDir, "TSK-001.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Clear(todoPath, notesDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Float) != 1 || res.Float[0] != "TSK-001" {
		t.Errorf("float = %v, want [TSK-001]", res.Float)
	}
	if len(res.RemovedClear) != 0 {
		t.Errorf("removed clear = %v, want none", res.RemovedClear)
	}

	tasks := readTasks(t, todoPath)
	if len(tasks) != 1 || tasks[0].ID != "TSK-001" {
		t.Errorf("remaining tasks = %v, want [TSK-001]", tasks)
	}
}

func TestAddDryRun(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)

	result, err := Add(AddOptions{
		TodoPath:    todoPath,
		NotesDir:    notesDir,
		Priority:    "med",
		Summary:     "x",
		CreateNote:  true,
		NoteContent: "note body",
		DryRun:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "TSK-001" {
		t.Errorf("dry run ID = %q, want TSK-001", result.ID)
	}
	if want := "- [ ] [TSK-001][priority:med][opened:2026-08-18] x"; result.Line != want {
		t.Errorf("dry run line = %q, want %q", result.Line, want)
	}
	if want := filepath.Join(notesDir, "TSK-001.md"); result.NotePath != want {
		t.Errorf("dry run note path = %q, want %q", result.NotePath, want)
	}
	wantContent := "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: x\n---\n\nnote body\n"
	if result.NoteContent != wantContent {
		t.Errorf("dry run note content = %q, want %q", result.NoteContent, wantContent)
	}

	// Nothing persisted: no todo file change and no note file on disk.
	if _, err := os.Stat(filepath.Join(notesDir, "TSK-001.md")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote a note file: %v", err)
	}
	if len(readTasks(t, todoPath)) != 0 {
		t.Error("dry run wrote a task")
	}
}

func TestAddNoteContent(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)

	result, err := Add(AddOptions{
		TodoPath:    todoPath,
		NotesDir:    notesDir,
		Priority:    "med",
		Summary:     "with note",
		CreateNote:  true,
		NoteContent: "hello\nworld",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(notesDir, "TSK-001.md"); result.NotePath != want {
		t.Errorf("note path = %q, want %q", result.NotePath, want)
	}
	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: with note\n---\n\nhello\nworld\n"
	if string(data) != want {
		t.Errorf("note content = %q, want %q", string(data), want)
	}
}

func TestAddNoteFileCopy(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)
	src := filepath.Join(t.TempDir(), "src.md")
	if err := os.WriteFile(src, []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Add(AddOptions{
		TodoPath:   todoPath,
		NotesDir:   notesDir,
		Priority:   "med",
		Summary:    "copy note",
		CreateNote: true,
		NoteFile:   src,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: copy note\n---\n\nfrom file\n"
	if string(data) != want {
		t.Errorf("note content = %q, want %q", string(data), want)
	}
	// Copy, not move: source still present.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source file was moved: %v", err)
	}
}

func TestListFilterState(t *testing.T) {
	todoPath, _ := writeTestTodo(t, sampleTasks)

	open := StatusOpen
	tasks, err := List(todoPath, ListFilter{State: &open})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "TSK-001" {
		t.Errorf("open filter = %+v, want only TSK-001", tasks)
	}

	// No filter returns everything in file order.
	all, err := List(todoPath, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("unfiltered = %d, want 3", len(all))
	}
}

func TestListFilterStale(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	staleTask := Task{
		ID: "TSK-010", Priority: "med", Opened: "2026-08-01",
		Status: StatusInProgress, Summary: "old", Claimed: "2026-08-10", // 8 days ago
	}
	freshTask := Task{
		ID: "TSK-011", Priority: "med", Opened: "2026-08-01",
		Status: StatusInProgress, Summary: "fresh", Claimed: "2026-08-17", // 1 day ago
	}
	todoPath, _ := writeTestTodo(t, []Task{staleTask, freshTask})

	// stale threshold that includes the 8-day task only.
	tasks, err := List(todoPath, ListFilter{StaleDays: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "TSK-010" {
		t.Errorf("stale filter = %+v, want only TSK-010", tasks)
	}
}

func TestListSortPriority(t *testing.T) {
	todoPath, _ := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusOpen, Summary: "a"},
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-02", Status: StatusOpen, Summary: "b"},
		{ID: "TSK-003", Priority: "low", Opened: "2026-08-03", Status: StatusOpen, Summary: "c"},
	})

	tasks, err := List(todoPath, ListFilter{SortField: SortFieldPriority})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-003", "TSK-002", "TSK-001"}) {
		t.Errorf("priority asc = %v, want low->med->high", got)
	}

	tasks, err = List(todoPath, ListFilter{SortField: SortFieldPriority, SortReverse: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-001", "TSK-002", "TSK-003"}) {
		t.Errorf("priority desc = %v, want high->med->low", got)
	}
}

func TestListSortOpened(t *testing.T) {
	todoPath, _ := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-03", Status: StatusOpen, Summary: "a"},
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-01", Status: StatusOpen, Summary: "b"},
		{ID: "TSK-003", Priority: "low", Opened: "2026-08-02", Status: StatusOpen, Summary: "c"},
	})

	tasks, err := List(todoPath, ListFilter{SortField: SortFieldOpened})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-002", "TSK-003", "TSK-001"}) {
		t.Errorf("opened asc = %v, want oldest->newest", got)
	}

	tasks, err = List(todoPath, ListFilter{SortField: SortFieldOpened, SortReverse: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-001", "TSK-003", "TSK-002"}) {
		t.Errorf("opened desc = %v, want newest->oldest", got)
	}
}

func TestListSortClaimed(t *testing.T) {
	todoPath, _ := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusInProgress, Summary: "a", Claimed: "2026-08-10"},
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-02", Status: StatusOpen, Summary: "b"},
		{ID: "TSK-003", Priority: "low", Opened: "2026-08-03", Status: StatusInProgress, Summary: "c", Claimed: "2026-08-05"},
	})

	tasks, err := List(todoPath, ListFilter{SortField: SortFieldClaimed})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-003", "TSK-001", "TSK-002"}) {
		t.Errorf("claimed asc = %v, want oldest claimed->newest claimed->unclaimed", got)
	}

	tasks, err = List(todoPath, ListFilter{SortField: SortFieldClaimed, SortReverse: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-001", "TSK-003", "TSK-002"}) {
		t.Errorf("claimed desc = %v, want newest claimed->oldest claimed->unclaimed", got)
	}
}

func TestListSortAge(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, _ := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusInProgress, Summary: "a", Claimed: "2026-08-10"}, // 8 days
		{ID: "TSK-002", Priority: "med", Opened: "2026-08-02", Status: StatusOpen, Summary: "b"},                               // unclaimed
		{ID: "TSK-003", Priority: "low", Opened: "2026-08-03", Status: StatusInProgress, Summary: "c", Claimed: "2026-08-15"},  // 3 days
	})

	tasks, err := List(todoPath, ListFilter{SortField: SortFieldAge})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-003", "TSK-001", "TSK-002"}) {
		t.Errorf("age asc = %v, want youngest->oldest->unclaimed", got)
	}

	tasks, err = List(todoPath, ListFilter{SortField: SortFieldAge, SortReverse: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(tasks); !slices.Equal(got, []string{"TSK-001", "TSK-003", "TSK-002"}) {
		t.Errorf("age desc = %v, want oldest->youngest->unclaimed", got)
	}
}

func taskIDs(tasks []Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}

func TestAgeDays(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	if got := (Task{}).AgeDays(); got != -1 {
		t.Errorf("unclaimed AgeDays = %d, want -1", got)
	}
	if got := (Task{Claimed: "2026-08-10"}).AgeDays(); got != 8 {
		t.Errorf("AgeDays = %d, want 8", got)
	}
	if got := (Task{Claimed: "2026-08-18"}).AgeDays(); got != 0 {
		t.Errorf("AgeDays today = %d, want 0", got)
	}
}

func TestRoundTripWithClaimed(t *testing.T) {
	tt := Task{
		ID: "TSK-001", Priority: "high", Opened: "2026-08-01",
		Status: StatusInProgress, Summary: "claimed task", Claimed: "2026-08-10",
	}
	got, ok := parseTask(tt.String())
	if !ok {
		t.Fatalf("parseTask(%q): not recognized", tt.String())
	}
	if got != tt {
		t.Errorf("round trip = %+v, want %+v", got, tt)
	}
}

func TestRelease(t *testing.T) {
	claimed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return claimed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	// Give TSK-002 an in-progress claim to release.
	tasks := readTasks(t, todoPath)
	tasks[1].Claimed = "2026-08-10"
	_ = writeTodoFile(todoPath, header{project: "test", repo: "http://example.com/repo", lastUpdated: "2026-01-01T00:00", configured: "2026-01-01", legacySource: "none"}, tasks)

	res, err := Release(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-002"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Line, "- [ ] [TSK-002]") {
		t.Errorf("release line = %q, want open [ ] TSK-002", res.Line)
	}
	if strings.Contains(res.Line, "claimed") {
		t.Errorf("release line still has claimed: %q", res.Line)
	}

	after := readTasks(t, todoPath)
	if after[1].Status != StatusOpen || after[1].Claimed != "" {
		t.Errorf("task after release = %q, want open with no claim", after[1].String())
	}
}

func TestReleaseInvalid(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	// Open and done tasks cannot be released.
	if _, err := Release(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1"}); err == nil {
		t.Error("release of open task: expected error")
	}
	if _, err := Release(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "3"}); err == nil {
		t.Error("release of done task: expected error")
	}
	tasks := readTasks(t, todoPath)
	if tasks[0].Status != StatusOpen || tasks[2].Status != StatusDone {
		t.Error("failed release mutated state")
	}
}

func TestRemove(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	// Remove a done task line - the [x] -> removed path.
	res, err := Remove(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-003"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Line, "- [x] [TSK-003]") {
		t.Errorf("removed line = %q, want original TSK-003 line", res.Line)
	}

	tasks := readTasks(t, todoPath)
	if len(tasks) != 2 {
		t.Fatalf("tasks after remove = %d, want 2", len(tasks))
	}
	for _, tt := range tasks {
		if tt.ID == "TSK-003" {
			t.Errorf("TSK-003 still present after remove: %v", tasks)
		}
	}
}

func TestRemoveAnyStatus(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	// Remove an open and an in-progress task; all statuses should work.
	for _, ref := range []string{"TSK-001", "TSK-002"} {
		if _, err := Remove(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: ref}); err != nil {
			t.Fatalf("remove %s: %v", ref, err)
		}
	}
	tasks := readTasks(t, todoPath)
	if len(tasks) != 1 || tasks[0].ID != "TSK-003" {
		t.Errorf("after multi-remove = %+v, want only TSK-003", tasks)
	}
}

func TestRemoveNotFound(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Remove(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "999"}); err == nil {
		t.Error("remove of missing task: expected error")
	}
}

func TestAddBackfillsNextIDOnOldFile(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	// Write a legacy file with no next_id field and three tasks.
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	h := &header{}
	h.project = "test"
	h.repo = "http://example.com/repo"
	h.configured = "2026-01-01"
	h.lastUpdated = "2026-01-01T00:00"
	h.legacySource = "none"
	_ = writeTodoFile(todoPath, *h, sampleTasks)

	result, err := Add(AddOptions{TodoPath: todoPath, NotesDir: notesDir, Priority: "med", Summary: "next"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "TSK-004" {
		t.Errorf("first add after migration = %q, want TSK-004", result.ID)
	}

	// Header should now carry next_id: TSK-005.
	_, hdr, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.nextID != "TSK-005" {
		t.Errorf("next_id after add = %q, want TSK-005", hdr.nextID)
	}
}

func TestAddResumesAfterEmptying(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	// Backfill a next_id high-water mark (as a prior add would have written).
	h := &header{}
	h.project = "test"
	h.repo = "http://example.com/repo"
	h.configured = "2026-01-01"
	h.lastUpdated = "2026-01-01T00:00"
	h.legacySource = "none"
	h.nextID = "TSK-004"
	_ = writeTodoFile(todoPath, *h, sampleTasks)

	// Remove every task.
	for _, ref := range []string{"TSK-001", "TSK-002", "TSK-003"} {
		if _, err := Remove(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: ref}); err != nil {
			t.Fatal(err)
		}
	}

	// next_id must NOT be decremented by removals.
	_, hdr, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.nextID != "TSK-004" {
		t.Errorf("next_id after removals = %q, want TSK-004 preserved", hdr.nextID)
	}

	result, err := Add(AddOptions{TodoPath: todoPath, NotesDir: notesDir, Priority: "med", Summary: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "TSK-004" {
		t.Errorf("add after emptying = %q, want TSK-004 (resume high-water mark)", result.ID)
	}
}

func TestBackfillInvalidNextID(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	// Malformed next_id is treated as absent and backfilled on the next write.
	h := &header{}
	h.project = "test"
	h.repo = "http://example.com/repo"
	h.configured = "2026-01-01"
	h.lastUpdated = "2026-01-01T00:00"
	h.legacySource = "none"
	h.nextID = "not-a-task"
	_ = writeTodoFile(todoPath, *h, sampleTasks)

	result, err := Add(AddOptions{TodoPath: todoPath, NotesDir: notesDir, Priority: "med", Summary: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "TSK-004" {
		t.Errorf("add with malformed next_id = %q, want TSK-004", result.ID)
	}

	// A next_id <= max(current) is likewise backfilled upward.
	h2 := &header{}
	h2.project = "test"
	h2.repo = "http://example.com/repo"
	h2.configured = "2026-01-01"
	h2.lastUpdated = "2026-01-01T00:00"
	h2.legacySource = "none"
	h2.nextID = "TSK-002" // stale: below current max of 3
	_ = writeTodoFile(todoPath, *h2, sampleTasks)

	r2, err := Add(AddOptions{TodoPath: todoPath, NotesDir: notesDir, Priority: "med", Summary: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.ID != "TSK-004" {
		t.Errorf("add with stale next_id = %q, want TSK-004", r2.ID)
	}
}

func TestNormalizeHeaderPreservesHighMark(t *testing.T) {
	tasks := []Task{
		{ID: "TSK-001", Status: StatusOpen},
	}
	var h header
	h.nextID = "TSK-050" // valid high mark well above current max
	normalizeHeader(&h, tasks)
	if h.nextID != "TSK-050" {
		t.Errorf("normalizeHeader lowered high mark = %q, want TSK-050", h.nextID)
	}

	h2 := header{nextID: "TSK-002"} // stale: <= max (3) -> backfilled
	highTasks := []Task{
		{ID: "TSK-001", Status: StatusOpen},
		{ID: "TSK-002", Status: StatusOpen},
		{ID: "TSK-003", Status: StatusOpen},
	}
	normalizeHeader(&h2, highTasks)
	if h2.nextID != "TSK-004" {
		t.Errorf("normalizeHeader stale next_id = %q, want TSK-004", h2.nextID)
	}
}

func TestDetailNoNote(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Task.ID != "TSK-001" {
		t.Errorf("ID = %q, want TSK-001", res.Task.ID)
	}
	if res.Task.Status != StatusOpen {
		t.Errorf("status = %q, want open", res.Task.Status)
	}
	if res.Task.OpenedDays() != 17 {
		t.Errorf("opened_days = %d, want 17", res.Task.OpenedDays())
	}
	if res.NoteExists {
		t.Error("NoteExists = true, want false")
	}
	if res.NotePreview != "" {
		t.Errorf("NotePreview = %q, want empty", res.NotePreview)
	}
}

func TestDetailWithNote(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(notesDir, "TSK-001.md")
	if err := os.WriteFile(notePath, []byte("line 1\nline 2\nline 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1", Lines: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoteExists {
		t.Error("NoteExists = false, want true")
	}
	want := "line 1\nline 2\nline 3"
	if res.NotePreview != want {
		t.Errorf("NotePreview = %q, want %q", res.NotePreview, want)
	}
	if res.NoteTruncated {
		t.Error("NoteTruncated = true, want false")
	}
}

func TestDetailNoteTruncation(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(notesDir, "TSK-001.md")
	if err := os.WriteFile(notePath, []byte("line 1\nline 2\nline 3\nline 4\nline 5"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1", Lines: 3})
	if err != nil {
		t.Fatal(err)
	}
	want := "line 1\nline 2\nline 3"
	if res.NotePreview != want {
		t.Errorf("NotePreview = %q, want %q", res.NotePreview, want)
	}
	if !res.NoteTruncated {
		t.Error("NoteTruncated = false, want true")
	}
}

func TestDetailNoNoteFlag(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-001.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1", NoNote: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoteExists {
		t.Error("NoteExists = false, want true")
	}
	if res.NotePreview != "" {
		t.Errorf("NotePreview = %q, want empty", res.NotePreview)
	}
}

func TestDetailAnyStatus(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	for _, ref := range []string{"1", "2", "3"} {
		if _, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: ref}); err != nil {
			t.Errorf("detail %s: %v", ref, err)
		}
	}
}

func TestDetailNotFound(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "999"}); err == nil {
		t.Error("detail of missing task: expected error")
	}
}

func TestDetailDoesNotMutate(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	_, hdrBefore, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1"}); err != nil {
		t.Fatal(err)
	}
	_, hdrAfter, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdrBefore.lastUpdated != hdrAfter.lastUpdated {
		t.Errorf("last_updated changed from %q to %q", hdrBefore.lastUpdated, hdrAfter.lastUpdated)
	}
}

func TestDetailFullNote(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "line 1\nline 2\nline 3\nline 4\nline 5"
	notePath := filepath.Join(notesDir, "TSK-001.md")
	if err := os.WriteFile(notePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.NotePreview != "" {
		t.Errorf("NotePreview = %q, want empty", res.NotePreview)
	}
	if res.NoteTruncated {
		t.Error("NoteTruncated = true, want false")
	}
	if res.NoteBody != body {
		t.Errorf("NoteBody = %q, want %q", res.NoteBody, body)
	}
}

func TestDetailFullNoteTruncationStillWorks(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "line 1\nline 2\nline 3\nline 4\nline 5"
	notePath := filepath.Join(notesDir, "TSK-001.md")
	if err := os.WriteFile(notePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1", Lines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoteTruncated {
		t.Error("NoteTruncated = false, want true")
	}
	if res.NotePreview != "line 1\nline 2\nline 3" {
		t.Errorf("NotePreview = %q, want truncated preview", res.NotePreview)
	}
}

func TestDetailFullNoNote(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1", Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteBody != "" {
		t.Errorf("NoteBody = %q, want empty", res.NoteBody)
	}
	if res.NoteExists {
		t.Error("NoteExists = true, want false")
	}
}

func TestRemoveWithNoteDelete(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(notesDir, "TSK-003.md")
	if err := os.WriteFile(notePath, []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Remove(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-003"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(notesDir, "TSK-003.md"); res.Note != want {
		t.Errorf("note = %q, want %q", res.Note, want)
	}

	// Remove itself does not delete the note; it only reports the path.
	if _, err := os.Stat(notePath); err != nil {
		t.Errorf("Remove deleted the note: %v", err)
	}
}

func TestReopen(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	res, err := Reopen(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-003"})
	if err != nil {
		t.Fatal(err)
	}
	if res.NoOp {
		t.Error("reopen of done task reported no-op")
	}
	if !strings.HasPrefix(res.Line, "- [ ] [TSK-003]") {
		t.Errorf("reopen line = %q, want open [ ] TSK-003", res.Line)
	}
	if strings.Contains(res.Line, "claimed") {
		t.Errorf("reopen line still has claimed: %q", res.Line)
	}

	after := readTasks(t, todoPath)
	if after[2].Status != StatusOpen || after[2].Claimed != "" {
		t.Errorf("task after reopen = %q, want open with no claim", after[2].String())
	}

	_, hdr, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.lastUpdated == "2026-01-01T00:00" {
		t.Error("last_updated not updated by reopen")
	}
}

func TestReopenNoOp(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	res, err := Reopen(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-001"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoOp {
		t.Error("reopen of open task did not report no-op")
	}
	if !strings.HasPrefix(res.Line, "- [ ] [TSK-001]") {
		t.Errorf("no-op reopen line = %q, want original open line", res.Line)
	}

	_, hdr, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.lastUpdated != "2026-01-01T00:00" {
		t.Error("no-op reopen mutated last_updated")
	}
}

func TestReopenNotFound(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Reopen(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "999"}); err == nil {
		t.Error("reopen of missing task: expected error")
	}
}

func TestReopenInProgress(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Reopen(RefOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "TSK-002"}); err == nil {
		t.Error("reopen of in-progress task: expected error")
	}
	tasks := readTasks(t, todoPath)
	if tasks[1].Status != StatusInProgress {
		t.Error("failed reopen mutated state")
	}
}

func TestBumpPriority(t *testing.T) {
	for _, tc := range []struct {
		in   Priority
		down bool
		want Priority
	}{
		{PriorityLow, false, PriorityMed},
		{PriorityMed, false, PriorityHigh},
		{PriorityHigh, false, PriorityHigh},
		{PriorityHigh, true, PriorityMed},
		{PriorityMed, true, PriorityLow},
		{PriorityLow, true, PriorityLow},
	} {
		got := bumpPriority(tc.in, tc.down)
		if got != tc.want {
			dir := "up"
			if tc.down {
				dir = "down"
			}
			t.Errorf("bumpPriority(%q, %s) = %q, want %q", tc.in, dir, got, tc.want)
		}
	}
}

func TestBumpUp(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	// Bump TSK-003: low -> med
	res, err := Bump(todoPath, notesDir, "TSK-003", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoOp {
		t.Error("bump low task reported no-op")
	}
	if !strings.Contains(res.Line, "[priority:med]") {
		t.Errorf("bump line = %q, want priority:med", res.Line)
	}

	tasks := readTasks(t, todoPath)
	if tasks[2].Priority != PriorityMed {
		t.Errorf("TSK-003 priority = %q, want med", tasks[2].Priority)
	}

	// Bump TSK-003 again: med -> high
	res, err = Bump(todoPath, notesDir, "TSK-003", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoOp {
		t.Error("bump med task reported no-op")
	}
	if !strings.Contains(res.Line, "[priority:high]") {
		t.Errorf("bump line = %q, want priority:high", res.Line)
	}

	// Bump TSK-003 again: already high -> no-op
	res, err = Bump(todoPath, notesDir, "TSK-003", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoOp {
		t.Error("bump high task did not report no-op")
	}
	if !strings.Contains(res.Line, "[priority:high]") {
		t.Errorf("no-op bump line = %q, still want priority:high", res.Line)
	}

	_, hdr, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.lastUpdated == "2026-01-01T00:00" {
		t.Error("last_updated not updated by bump")
	}
}

func TestBumpDown(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	// Bump TSK-001: high -> med
	res, err := Bump(todoPath, notesDir, "TSK-001", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoOp {
		t.Error("bump high down reported no-op")
	}
	if !strings.Contains(res.Line, "[priority:med]") {
		t.Errorf("bump line = %q, want priority:med", res.Line)
	}

	// Bump TSK-001 again: med -> low
	res, err = Bump(todoPath, notesDir, "TSK-001", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoOp {
		t.Error("bump med down reported no-op")
	}
	if !strings.Contains(res.Line, "[priority:low]") {
		t.Errorf("bump line = %q, want priority:low", res.Line)
	}

	// Bump TSK-001 again: already low -> no-op
	res, err = Bump(todoPath, notesDir, "TSK-001", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoOp {
		t.Error("bump low down did not report no-op")
	}
	if !strings.Contains(res.Line, "[priority:low]") {
		t.Errorf("no-op bump line = %q, still want priority:low", res.Line)
	}
}

func TestBumpNoOpDoesNotWrite(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	_, hdrBefore, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}

	// TSK-001 is already high; bump up should be no-op without writing.
	res, err := Bump(todoPath, notesDir, "TSK-001", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoOp {
		t.Error("bump high up did not report no-op")
	}

	_, hdrAfter, err := parseTodoFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if hdrBefore.lastUpdated != hdrAfter.lastUpdated {
		t.Error("no-op bump mutated last_updated")
	}
}

func TestBumpNotFound(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if _, err := Bump(todoPath, notesDir, "999", false); err == nil {
		t.Error("bump of missing task: expected error")
	}
}

func TestBumpAnyStatus(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, sampleTasks)

	// Bump should work on open, in-progress, and done tasks.
	for _, ref := range []string{"TSK-001", "TSK-002", "TSK-003"} {
		if _, err := Bump(todoPath, notesDir, ref, false); err != nil {
			t.Errorf("bump %s: %v", ref, err)
		}
	}
}

func TestBumpWithNote(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-003.md"), []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Bump(todoPath, notesDir, "TSK-003", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoOp {
		t.Error("bump low up reported no-op")
	}
	if want := filepath.Join(notesDir, "TSK-003.md"); res.Note != want {
		t.Errorf("note = %q, want %q", res.Note, want)
	}
}

func TestAddNoteWorkOrder(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)

	_, err := Add(AddOptions{
		TodoPath:    todoPath,
		NotesDir:    notesDir,
		Priority:    "med",
		Summary:     "work order task",
		CreateNote:  true,
		NoteContent: "body text",
		Kind:        "work-order",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nkind: work-order\n---\n\nbody text"
	if string(data) != want {
		t.Errorf("note content = %q, want %q", string(data), want)
	}
}

func TestAddNoteRecordFrontmatter(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)

	_, err := Add(AddOptions{
		TodoPath:    todoPath,
		NotesDir:    notesDir,
		Priority:    "med",
		Summary:     "record task",
		CreateNote:  true,
		NoteContent: "body text",
		Category:    "areas",
		Synopsis:    "a synopsis",
		Source:      "repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ncategory: areas\ncreated: 2026-08-18\nsource: repo\nsynopsis: a synopsis\n---\n\nbody text\n"
	if string(data) != want {
		t.Errorf("note content = %q, want %q", string(data), want)
	}
}

func TestTruncateSummary(t *testing.T) {
	cases := []struct {
		in      string
		max     int
		want    string
		spilled bool
	}{
		{"short", 10, "short", false},
		{"exactly10", 10, "exactly10", false},
		{"elevenchars", 10, "elevenc...", true},
		{"abc", 3, "abc", false},
		{"abcd", 3, "abc", true},   // max <= 3: hard cut, no room for ellipsis
		{"héllo", 4, "h...", true}, // multibyte-safe
	}
	for _, tc := range cases {
		got, spilled := truncateSummary(tc.in, tc.max)
		if got != tc.want || spilled != tc.spilled {
			t.Errorf("truncateSummary(%q, %d) = (%q, %v), want (%q, %v)", tc.in, tc.max, got, spilled, tc.want, tc.spilled)
		}
		if len([]rune(got)) > tc.max {
			t.Errorf("truncateSummary(%q, %d) produced %q (%d runes) > max", tc.in, tc.max, got, len([]rune(got)))
		}
	}
}

func TestAddTruncatesLongSummary(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)
	fullSummary := strings.Repeat("a", 150)

	result, err := Add(AddOptions{
		TodoPath: todoPath,
		NotesDir: notesDir,
		Priority: "med",
		Summary:  fullSummary,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The line summary is capped at MaxSummaryLen runes and flagged with "...".
	tasks := readTasks(t, todoPath)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	got := tasks[0].Summary
	if len([]rune(got)) != MaxSummaryLen {
		t.Errorf("summary runes = %d, want %d", len([]rune(got)), MaxSummaryLen)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("summary = %q, want trailing ellipsis", got)
	}
	if strings.Contains(got, strings.Repeat("a", 150)) {
		t.Error("summary was not truncated")
	}

	// The full summary spills into an auto-created work-order note.
	want := filepath.Join(notesDir, "TSK-001.md")
	if result.NotePath != want {
		t.Errorf("note path = %q, want %q", result.NotePath, want)
	}
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	wantContent := "---\nkind: work-order\n---\n\n" + fullSummary
	if string(data) != wantContent {
		t.Errorf("note content = %q, want %q", string(data), wantContent)
	}
	if disp, err := NoteDisposition(want); err != nil || disp != DispositionWorkOrder {
		t.Errorf("disposition = %q (err %v), want work-order", disp, err)
	}
}

func TestAddLongSummarySpillsIntoWorkOrderNote(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)
	fullSummary := strings.Repeat("b", 150)

	_, err := Add(AddOptions{
		TodoPath:    todoPath,
		NotesDir:    notesDir,
		Priority:    "med",
		Summary:     fullSummary,
		CreateNote:  true,
		NoteContent: "body text",
		Kind:        "work-order",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nkind: work-order\n---\n\n" + fullSummary + "\n\nbody text"
	if string(data) != want {
		t.Errorf("note content = %q, want %q", string(data), want)
	}
}

func TestAddLongSummaryParkRecordKeepsSynopsis(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)
	fullSummary := strings.Repeat("c", 150)

	_, err := Add(AddOptions{
		TodoPath:   todoPath,
		NotesDir:   notesDir,
		Priority:   "med",
		Summary:    fullSummary,
		CreateNote: true,
		Category:   "areas",
	})
	if err != nil {
		t.Fatal(err)
	}

	tasks := readTasks(t, todoPath)
	if strings.Contains(tasks[0].Summary, fullSummary) {
		t.Error("line summary was not truncated for a park record")
	}

	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "synopsis: "+fullSummary) {
		t.Errorf("note synopsis missing full summary: %q", string(data))
	}
	if disp, err := NoteDisposition(filepath.Join(notesDir, "TSK-001.md")); err != nil || disp != DispositionPark {
		t.Errorf("disposition = %q (err %v), want park", disp, err)
	}
}

func TestAddLongSummaryDryRun(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)
	fullSummary := strings.Repeat("d", 150)

	result, err := Add(AddOptions{
		TodoPath: todoPath,
		NotesDir: notesDir,
		Priority: "med",
		Summary:  fullSummary,
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(notesDir, "TSK-001.md"); result.NotePath != want {
		t.Errorf("dry run note path = %q, want %q", result.NotePath, want)
	}
	if result.NoteContent != "---\nkind: work-order\n---\n\n"+fullSummary {
		t.Errorf("dry run note content = %q", result.NoteContent)
	}
	if strings.Contains(result.Line, fullSummary) {
		t.Error("dry run line not truncated")
	}

	// Nothing persisted.
	if _, err := os.Stat(filepath.Join(notesDir, "TSK-001.md")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote a note file: %v", err)
	}
	if len(readTasks(t, todoPath)) != 0 {
		t.Error("dry run wrote a task")
	}
}

func TestAddNoteInvalidCategory(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, nil)

	_, err := Add(AddOptions{
		TodoPath:   todoPath,
		NotesDir:   notesDir,
		Priority:   "med",
		Summary:    "bad category",
		CreateNote: true,
		Category:   "nonsense",
	})
	if err == nil {
		t.Fatal("Add with invalid category: expected error")
	}
	if len(readTasks(t, todoPath)) != 0 {
		t.Error("invalid add wrote a task")
	}
}

func TestAddRejectsMultilineSummary(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, nil)

	_, err := Add(AddOptions{
		TodoPath: todoPath,
		NotesDir: notesDir,
		Priority: "med",
		Summary:  "line one\nline two",
	})
	if err == nil {
		t.Fatal("Add with multiline summary: expected error")
	}
	if len(readTasks(t, todoPath)) != 0 {
		t.Error("invalid add wrote a task")
	}
}

func TestBuildNoteContentRejectsMultilineFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		synopsis string
		source   string
	}{
		{"multiline synopsis", "a\nb", "repo"},
		{"multiline source", "syn", "a\nb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildNoteContent(noteBuild{
				Body:             "body",
				Synopsis:         tc.synopsis,
				Source:           tc.source,
				FallbackSynopsis: "fallback",
				Created:          "2026-09-26",
			})
			if err == nil {
				t.Fatal("expected error for multiline frontmatter value")
			}
		})
	}
}

func TestBuildNoteContentReconcilesSuppliedFrontmatter(t *testing.T) {
	base := noteBuild{FallbackSynopsis: "task summary", Created: "2026-08-18"}

	cases := []struct {
		name     string
		body     string
		kind     string
		category string
		synopsis string
		source   string
		want     string
		wantErr  bool
	}{
		{
			name: "record block fields become fallbacks",
			body: "---\nkind: record\ncreated: 2020-01-01\nsource: notes\nsynopsis: from the block\n---\n\nbody prose",
			want: "---\nkind: record\ncreated: 2026-08-18\nsource: notes\nsynopsis: from the block\n---\n\nbody prose\n",
		},
		{
			name: "park block is adopted when no flag names a disposition",
			body: "---\ncategory: areas\nsource: notes\n---\n\nbody prose",
			want: "---\ncategory: areas\ncreated: 2026-08-18\nsource: notes\nsynopsis: task summary\n---\n\nbody prose\n",
		},
		{
			name:     "flag category overrides the declared one",
			body:     "---\ncategory: areas\n---\n\nbody prose",
			category: "projects",
			want:     "---\ncategory: projects\ncreated: 2026-08-18\nsource: repo\nsynopsis: task summary\n---\n\nbody prose\n",
		},
		{
			name:     "flag synopsis wins over the block",
			body:     "---\nkind: record\nsynopsis: from the block\n---\n\nbody prose",
			synopsis: "given synopsis",
			want:     "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: given synopsis\n---\n\nbody prose\n",
		},
		{
			name: "doc metadata block is dropped with the block",
			body: "---\ntitle: Plan\ntags: draft\n---\n\nbody prose",
			want: "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: task summary\n---\n\nbody prose\n",
		},
		{
			name: "stacked blocks heal into one",
			body: "---\nkind: record\nsynopsis: outer\n---\n\n---\ncategory: areas\n---\n\ninner prose",
			want: "---\ncategory: areas\ncreated: 2026-08-18\nsource: repo\nsynopsis: outer\n---\n\ninner prose\n",
		},
		{
			name:    "work-order flag over a declared record errors",
			body:    "---\nkind: record\n---\n\nbody prose",
			kind:    "work-order",
			wantErr: true,
		},
		{
			name:     "category flag over a declared work-order errors",
			body:     "---\nkind: work-order\n---\n\nbody prose",
			category: "areas",
			wantErr:  true,
		},
		{
			name: "declared work-order matches the flag and re-renders once",
			body: "---\nkind: work-order\n---\n\nbody prose",
			kind: "work-order",
			want: "---\nkind: work-order\n---\n\nbody prose",
		},
		{
			name: "a body opening with a horizontal rule is not a block",
			body: "---\nnot a frontmatter block\n---\n\nbody prose",
			want: "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: task summary\n---\n\n---\nnot a frontmatter block\n---\n\nbody prose\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nb := base
			nb.Body = tc.body
			nb.Kind = tc.kind
			nb.Category = tc.category
			nb.Synopsis = tc.synopsis
			nb.Source = tc.source

			got, err := buildNoteContent(nb)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got content %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("content = %q, want %q", got, tc.want)
			}
			if !strings.Contains(tc.body, "not a frontmatter block") {
				if n := countFrontmatterDelims(got); n != 2 {
					t.Errorf("frontmatter delimiters = %d, want 2 in %q", n, got)
				}
			}
		})
	}
}

func TestAddNoteFileStripsDocFrontmatter(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)
	src := filepath.Join(t.TempDir(), "plan.md")
	doc := "---\ntitle: Phase 3\ncategory: areas\nsource: notes\nsynopsis: doc synopsis\n---\n\nplan body prose\n"
	if err := os.WriteFile(src, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Add(AddOptions{
		TodoPath:   todoPath,
		NotesDir:   notesDir,
		Priority:   "med",
		Summary:    "copy doc",
		CreateNote: true,
		NoteFile:   src,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	// The doc's frontmatter belongs to the doc: the note carries todo's block.
	want := "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: copy doc\n---\n\nplan body prose\n"
	if got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
	if n := countFrontmatterDelims(got); n != 2 {
		t.Errorf("frontmatter delimiters = %d, want 2 in %q", n, got)
	}
}

func TestAddNoteFileDryRunPreviewsOneBlock(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)
	src := filepath.Join(t.TempDir(), "plan.md")
	doc := "---\ntitle: Phase 3\n---\n\nplan body prose\n"
	if err := os.WriteFile(src, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Add(AddOptions{
		TodoPath:   todoPath,
		NotesDir:   notesDir,
		Priority:   "med",
		Summary:    "copy doc",
		CreateNote: true,
		NoteFile:   src,
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := countFrontmatterDelims(result.NoteContent); n != 2 {
		t.Errorf("dry run frontmatter delimiters = %d, want 2 in %q", n, result.NoteContent)
	}
	if _, err := os.Stat(filepath.Join(notesDir, "TSK-001.md")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote a note file: %v", err)
	}
}

func TestAddNoteContentReconcilesDeclaredPark(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)

	_, err := Add(AddOptions{
		TodoPath:    todoPath,
		NotesDir:    notesDir,
		Priority:    "med",
		Summary:     "declared park",
		CreateNote:  true,
		NoteContent: "---\ncategory: areas\n---\n\nbody prose",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(notesDir, "TSK-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := "---\ncategory: areas\ncreated: 2026-08-18\nsource: repo\nsynopsis: declared park\n---\n\nbody prose\n"
	if got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
	if disp, err := NoteDisposition(filepath.Join(notesDir, "TSK-001.md")); err != nil || disp != DispositionPark {
		t.Errorf("disposition = %q (err %v), want park", disp, err)
	}
}

func TestAddNoteContentDispositionConflictWritesNothing(t *testing.T) {
	fixed := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, nil)

	_, err := Add(AddOptions{
		TodoPath:    todoPath,
		NotesDir:    notesDir,
		Priority:    "med",
		Summary:     "conflict",
		CreateNote:  true,
		NoteContent: "---\nkind: record\n---\n\nbody prose",
		Kind:        "work-order",
	})
	if err == nil {
		t.Fatal("Add with a conflicting body declaration: expected error")
	}
	if len(readTasks(t, todoPath)) != 0 {
		t.Error("conflicting add wrote a task")
	}
	if _, err := os.Stat(filepath.Join(notesDir, "TSK-001.md")); !os.IsNotExist(err) {
		t.Errorf("conflicting add wrote a note file: %v", err)
	}
}

func TestNoteDisposition(t *testing.T) {
	dir := t.TempDir()
	notesDir := filepath.Join(dir, ".todo", "notes")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		content string
		want    Disposition
	}{
		{
			name:    "park record",
			content: "---\ncategory: areas\ncreated: 2026-08-18\nsource: repo\nsynopsis: x\n---\n\nbody\n",
			want:    DispositionPark,
		},
		{
			name:    "work order",
			content: "---\nkind: work-order\n---\n\nbody\n",
			want:    DispositionWorkOrder,
		},
		{
			name:    "no frontmatter",
			content: "# Heading\n\nbody\n",
			want:    DispositionFloat,
		},
		{
			name:    "unknown kind",
			content: "---\nkind: other\n---\n\nbody\n",
			want:    DispositionFloat,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(notesDir, tc.name+".md")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := NoteDisposition(path)
			if err != nil {
				t.Fatalf("NoteDisposition(%q): %v", path, err)
			}
			if got != tc.want {
				t.Errorf("NoteDisposition(%q) = %q, want %q", path, got, tc.want)
			}
		})
	}
}

func TestDetailDisposition(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, sampleTasks)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A record note yields park; a work-order note yields work-order.
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-001.md"), []byte("---\ncategory: areas\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-002.md"), []byte("---\nkind: work-order\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Disposition != DispositionPark {
		t.Errorf("TSK-001 disposition = %q, want park", res.Disposition)
	}

	res, err = Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Disposition != DispositionWorkOrder {
		t.Errorf("TSK-002 disposition = %q, want work-order", res.Disposition)
	}

	// No note defaults to clear.
	res, err = Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Disposition != DispositionClear {
		t.Errorf("TSK-003 disposition = %q, want clear", res.Disposition)
	}

	// A note with no recognized markers still floats.
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-003.md"), []byte("plain note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = Detail(DetailOptions{TodoPath: todoPath, NotesDir: notesDir, Ref: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Disposition != DispositionFloat {
		t.Errorf("TSK-003 disposition = %q, want float", res.Disposition)
	}
}

func TestNoteDispositionRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TSK-001.md")
	content := "---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: why\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NoteDisposition(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != DispositionRecord {
		t.Errorf("disposition = %q, want record", got)
	}
}

func TestClearRecordKeepsNote(t *testing.T) {
	todoPath, notesDir := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "record task"},
	})
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(notesDir, "TSK-001.md")
	if err := os.WriteFile(notePath, []byte("---\nkind: record\ncreated: 2026-08-18\nsource: repo\nsynopsis: why\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Clear(todoPath, notesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Recorded) != 1 || res.Recorded[0] != "TSK-001" {
		t.Errorf("recorded = %v, want [TSK-001]", res.Recorded)
	}
	if len(res.Parked) != 0 || len(res.Float) != 0 {
		t.Errorf("parked = %v, float = %v, want none", res.Parked, res.Float)
	}
	if len(readTasks(t, todoPath)) != 0 {
		t.Error("record task line not removed")
	}
	if _, err := os.Stat(notePath); err != nil {
		t.Errorf("record note deleted: %v", err)
	}
}

func TestArchive(t *testing.T) {
	fixed := time.Date(2026, 9, 26, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "high", Opened: "2026-08-01", Status: StatusDone, Summary: "retire me"},
		{ID: "TSK-002", Priority: "low", Opened: "2026-08-02", Status: StatusOpen, Summary: "keep me"},
	})
	archiveDir := filepath.Join(filepath.Dir(notesDir), "archive")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(notesDir, "TSK-001.md")
	note := "---\nkind: record\ncreated: 2026-08-01\nsource: repo\nsynopsis: the original why\n---\n\n# body\ndetails\n"
	if err := os.WriteFile(notePath, []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Archive(ArchiveOptions{
		TodoPath:   todoPath,
		NotesDir:   notesDir,
		ArchiveDir: archiveDir,
		Ref:        "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "TSK-001" {
		t.Errorf("id = %q, want TSK-001", res.ID)
	}
	if res.Synopsis != "the original why" {
		t.Errorf("synopsis = %q, want the note's own", res.Synopsis)
	}
	wantPath := filepath.Join(archiveDir, "TSK-001.md")
	if res.ArchivePath != wantPath {
		t.Errorf("archive path = %q, want %q", res.ArchivePath, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nkind: record\ncreated: 2026-08-01\nsource: repo\nsynopsis: the original why\n---\n\n# body\ndetails\n"
	if string(data) != want {
		t.Errorf("archived content = %q, want %q", string(data), want)
	}

	// The line and the source note entry are gone; the other task survives.
	tasks := readTasks(t, todoPath)
	if len(tasks) != 1 || tasks[0].ID != "TSK-002" {
		t.Errorf("remaining tasks = %v, want [TSK-002]", tasks)
	}
	if _, err := os.Stat(notePath); !os.IsNotExist(err) {
		t.Errorf("source note still present: %v", err)
	}
}

func TestArchiveSynopsisOverrideAndName(t *testing.T) {
	fixed := time.Date(2026, 9, 26, 10, 30, 0, 0, time.UTC)
	setNow(func() time.Time { return fixed })
	defer setNow(time.Now)

	todoPath, notesDir := writeTestTodo(t, []Task{
		{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "retire me"},
	})
	archiveDir := filepath.Join(filepath.Dir(notesDir), "archive")
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notesDir, "TSK-001.md"), []byte("plain body"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Archive(ArchiveOptions{
		TodoPath:   todoPath,
		NotesDir:   notesDir,
		ArchiveDir: archiveDir,
		Ref:        "TSK-001",
		Name:       "retired-why",
		Synopsis:   "asserted why",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Synopsis != "asserted why" {
		t.Errorf("synopsis = %q, want asserted why", res.Synopsis)
	}
	wantPath := filepath.Join(archiveDir, "retired-why.md")
	if res.ArchivePath != wantPath {
		t.Errorf("archive path = %q, want %q", res.ArchivePath, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nkind: record\ncreated: 2026-09-26\nsource: repo\nsynopsis: asserted why\n---\n\nplain body\n"
	if string(data) != want {
		t.Errorf("archived content = %q, want %q", string(data), want)
	}
}

func TestArchiveGuards(t *testing.T) {
	archiveDirOf := func(notesDir string) string {
		return filepath.Join(filepath.Dir(notesDir), "archive")
	}

	t.Run("not done", func(t *testing.T) {
		todoPath, notesDir := writeTestTodo(t, []Task{
			{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusInProgress, Summary: "open"},
		})
		if _, err := Archive(ArchiveOptions{TodoPath: todoPath, NotesDir: notesDir, ArchiveDir: archiveDirOf(notesDir), Ref: "1"}); err == nil {
			t.Fatal("archive of a non-done task: expected error")
		}
		if len(readTasks(t, todoPath)) != 1 {
			t.Error("guard mutated the task list")
		}
	})

	t.Run("no note", func(t *testing.T) {
		todoPath, notesDir := writeTestTodo(t, []Task{
			{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "done"},
		})
		if _, err := Archive(ArchiveOptions{TodoPath: todoPath, NotesDir: notesDir, ArchiveDir: archiveDirOf(notesDir), Ref: "1"}); err == nil {
			t.Fatal("archive without a note: expected error")
		}
		if len(readTasks(t, todoPath)) != 1 {
			t.Error("guard mutated the task list")
		}
	})

	t.Run("synopsis required", func(t *testing.T) {
		fixed := time.Date(2026, 9, 26, 10, 30, 0, 0, time.UTC)
		setNow(func() time.Time { return fixed })
		defer setNow(time.Now)

		todoPath, notesDir := writeTestTodo(t, []Task{
			{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "done"},
		})
		if err := os.MkdirAll(notesDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// A work-order note carries no synopsis, so archiving it must assert one.
		if err := os.WriteFile(filepath.Join(notesDir, "TSK-001.md"), []byte("---\nkind: work-order\n---\n\nbody"), 0o644); err != nil {
			t.Fatal(err)
		}
		archiveDir := archiveDirOf(notesDir)
		if _, err := Archive(ArchiveOptions{TodoPath: todoPath, NotesDir: notesDir, ArchiveDir: archiveDir, Ref: "1"}); err == nil {
			t.Fatal("archive without a synopsis: expected error")
		}
		if len(readTasks(t, todoPath)) != 1 {
			t.Error("failed archive removed the task line")
		}
		if _, err := os.Stat(filepath.Join(notesDir, "TSK-001.md")); err != nil {
			t.Errorf("failed archive removed the note: %v", err)
		}

		res, err := Archive(ArchiveOptions{
			TodoPath:   todoPath,
			NotesDir:   notesDir,
			ArchiveDir: archiveDir,
			Ref:        "1",
			Synopsis:   "why it matters",
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Synopsis != "why it matters" {
			t.Errorf("synopsis = %q, want why it matters", res.Synopsis)
		}
	})

	t.Run("multiline synopsis", func(t *testing.T) {
		todoPath, notesDir := writeTestTodo(t, []Task{
			{ID: "TSK-001", Priority: "med", Opened: "2026-08-01", Status: StatusDone, Summary: "done"},
		})
		if err := os.MkdirAll(notesDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(notesDir, "TSK-001.md"), []byte("plain body"), 0o644); err != nil {
			t.Fatal(err)
		}
		archiveDir := archiveDirOf(notesDir)
		_, err := Archive(ArchiveOptions{
			TodoPath:   todoPath,
			NotesDir:   notesDir,
			ArchiveDir: archiveDir,
			Ref:        "1",
			Synopsis:   "line one\nline two",
		})
		if err == nil {
			t.Fatal("archive with multiline synopsis: expected error")
		}
		if len(readTasks(t, todoPath)) != 1 {
			t.Error("failed archive removed the task line")
		}
		if _, err := os.Stat(filepath.Join(archiveDir, "TSK-001.md")); !os.IsNotExist(err) {
			t.Errorf("failed archive left an archive file: %v", err)
		}
	})
}

func TestListArchived(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, ".todo", "archive")

	// A missing directory means nothing has been archived yet.
	notes, err := ListArchived(archiveDir)
	if err != nil {
		t.Fatalf("ListArchived(missing): %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, want none", notes)
	}

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"b.md":       "---\nkind: record\nsynopsis: bee\n---\n\nbody\n",
		"a.md":       "---\ncategory: areas\nsynopsis: ay\n---\n\nbody\n",
		"ignore.txt": "not a note",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(archiveDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	notes, err = ListArchived(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %v, want 2", notes)
	}
	if notes[0].Name != "a.md" || notes[0].Synopsis != "ay" {
		t.Errorf("notes[0] = %+v, want a.md/ay", notes[0])
	}
	if notes[1].Name != "b.md" || notes[1].Synopsis != "bee" {
		t.Errorf("notes[1] = %+v, want b.md/bee", notes[1])
	}
}
