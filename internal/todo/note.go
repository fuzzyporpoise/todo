// Package todo handles note disposition: write-time frontmatter classification
// for clear-time.
package todo

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Disposition classifies a task's companion note by its frontmatter so
// clear-time tooling can decide whether to preserve, delete, or float it.
type Disposition string

const (
	// DispositionPark marks a record note carrying park's native frontmatter.
	DispositionPark Disposition = "park"
	// DispositionWorkOrder marks a disposable note stamped kind: work-order.
	DispositionWorkOrder Disposition = "work-order"
	// DispositionRecord marks a durable todo-native note stamped kind: record.
	DispositionRecord Disposition = "record"
	// DispositionFloat marks a note with no recognized disposition.
	DispositionFloat Disposition = "float"
	// DispositionClear marks a task with no companion note - the task line
	// should be removed on clear with no note to preserve.
	DispositionClear Disposition = "clear"
)

// defaultRecordSource is stamped into a record's source field when none is
// supplied. A todo note originates from the repo's task list.
const defaultRecordSource = "repo"

// parkCategories is the canonical parked-note category enum. It is duplicated
// here rather than imported from park/schema so todo's note contract stands on
// its own; a park-shaped record is emitted only for interop.
var parkCategories = []string{"inbox", "projects", "areas", "archive"}

// ParkCategories returns the canonical park category values, for CLI validation
// and interop only. Todo's own durable notes are todo-native records.
func ParkCategories() []string {
	return slices.Clone(parkCategories)
}

// NoteDisposition reads the leading frontmatter block of a note and classifies
// it: work-order when a kind: work-order marker is present, park when a category
// field is present, record when kind: record is present, and float when none is
// found. A note without any frontmatter block is float, never an error.
func NoteDisposition(path string) (Disposition, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	//nolint:errcheck // read-only file; close error not meaningful here
	defer f.Close()

	var hasCategory, hasWorkOrder, hasRecord bool
	scanner := bufio.NewScanner(f)
	if scanner.Scan() && strings.TrimSpace(scanner.Text()) == "---" {
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "---" {
				break
			}
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "category":
				hasCategory = true
			case "kind":
				switch strings.TrimSpace(val) {
				case "work-order":
					hasWorkOrder = true
				case "record":
					hasRecord = true
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	if hasWorkOrder {
		return DispositionWorkOrder, nil
	}
	if hasCategory {
		return DispositionPark, nil
	}
	if hasRecord {
		return DispositionRecord, nil
	}
	return DispositionFloat, nil
}

// buildNoteContent prefixes body with the note's disposition frontmatter.
// kind "work-order" stamps the disposable marker; a category stamps a
// park-shaped record for interop; otherwise a todo-native record is stamped,
// defaulting synopsis to the task summary and source to the repo. created is
// the note's write date.
func buildNoteContent(body, kind, category, synopsis, source, fallbackSynopsis, created string) (string, error) {
	if kind != "" {
		if kind != "work-order" {
			return "", fmt.Errorf("invalid kind %q: want work-order", kind)
		}
		return "---\nkind: work-order\n---\n\n" + body, nil
	}

	if synopsis == "" {
		synopsis = fallbackSynopsis
	}
	if source == "" {
		source = defaultRecordSource
	}

	if err := validateFrontmatterScalar("synopsis", synopsis); err != nil {
		return "", err
	}
	if err := validateFrontmatterScalar("source", source); err != nil {
		return "", err
	}

	if category != "" {
		if !isParkCategory(category) {
			return "", fmt.Errorf("invalid category %q: want one of %s", category, strings.Join(parkCategories, ", "))
		}
		return renderParkRecord(category, created, source, synopsis, body), nil
	}

	return renderRecord(created, source, synopsis, body), nil
}

// validateFrontmatterScalar rejects values that would break the simple
// single-line YAML frontmatter format used by todo and park records.
func validateFrontmatterScalar(name, value string) error {
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("frontmatter field %q must be a single line", name)
	}
	return nil
}

// renderParkRecord emits park's four-key frontmatter block for interop. It
// matches park/schema's WriteTemplate byte for byte.
func renderParkRecord(category, created, source, synopsis, body string) string {
	return "---\ncategory: " + category + "\ncreated: " + created +
		"\nsource: " + source + "\nsynopsis: " + synopsis + "\n---\n\n" + body + "\n"
}

// renderRecord emits the todo-native durable-note frontmatter block.
func renderRecord(created, source, synopsis, body string) string {
	return "---\nkind: record\ncreated: " + created +
		"\nsource: " + source + "\nsynopsis: " + synopsis + "\n---\n\n" + body + "\n"
}

// isParkCategory reports whether s is a canonical park category.
func isParkCategory(s string) bool {
	return slices.Contains(parkCategories, s)
}

// noteFields holds the frontmatter values the archive path needs from an
// existing note, plus its body.
type noteFields struct {
	created  string
	source   string
	synopsis string
	body     string
}

// parseNoteFields splits a note into its recognized frontmatter values and its
// body. A note without a frontmatter block yields only a body.
func parseNoteFields(content string) noteFields {
	nf := noteFields{body: content}
	if !strings.HasPrefix(content, "---\n") {
		return nf
	}
	rest := content[len("---\n"):]
	before, after, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		nf.body = ""
		return nf
	}
	for line := range strings.SplitSeq(before, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "created":
			nf.created = strings.TrimSpace(val)
		case "source":
			nf.source = strings.TrimSpace(val)
		case "synopsis":
			nf.synopsis = strings.TrimSpace(val)
		}
	}
	nf.body = strings.TrimPrefix(after, "\n")
	return nf
}
