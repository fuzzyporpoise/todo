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

// noteBuild describes a companion note to create: its body plus the
// disposition inputs. An empty flag field defers to the body's own frontmatter
// block, and only then to the fallback.
type noteBuild struct {
	Body             string
	Kind             string
	Category         string
	Synopsis         string
	Source           string
	FallbackSynopsis string
	Created          string
	// SpillSummary is the full summary to prepend to the body when the task
	// line truncated it. Callers set it only for a work-order note.
	SpillSummary string
}

// buildNoteContent renders the note's disposition frontmatter over its body.
// kind "work-order" stamps the disposable marker; a category stamps a
// park-shaped record for interop; otherwise a todo-native record is stamped,
// defaulting synopsis to the task summary and source to the repo. created is
// always the note's write date.
//
// A body that already opens with a frontmatter block is reconciled, never
// stacked: the block is stripped, its disposition is adopted when no flag
// named one, and its synopsis/source fill the fields a flag left silent. A body
// declaring a disposition the flags contradict is an error, not a second block.
func buildNoteContent(nb noteBuild) (string, error) {
	if nb.Kind != "" && nb.Kind != "work-order" {
		return "", fmt.Errorf("invalid kind %q: want work-order", nb.Kind)
	}

	declared, body := extractFrontmatter(nb.Body)
	if nb.SpillSummary != "" {
		body = spillBody(nb.SpillSummary, body)
	}

	disp, err := nb.resolveDisposition(declared)
	if err != nil {
		return "", err
	}

	if disp == DispositionWorkOrder {
		return "---\nkind: work-order\n---\n\n" + body, nil
	}

	synopsis := nb.Synopsis
	if synopsis == "" {
		synopsis = declared.synopsis
	}
	if synopsis == "" {
		synopsis = nb.FallbackSynopsis
	}
	source := nb.Source
	if source == "" {
		source = declared.source
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

	// The record renderers append the closing newline, so the body is stored
	// without its trailing one: a note ends with exactly one newline whatever
	// its source was.
	body = strings.TrimSuffix(body, "\n")

	if disp == DispositionPark {
		category := nb.Category
		if category == "" {
			category = declared.category
		}
		if !isParkCategory(category) {
			return "", fmt.Errorf("invalid category %q: want one of %s", category, strings.Join(parkCategories, ", "))
		}
		return renderParkRecord(category, nb.Created, source, synopsis, body), nil
	}

	return renderRecord(nb.Created, source, synopsis, body), nil
}

// resolveDisposition reports the disposition to stamp: an explicit flag wins, a
// conflicting declaration in the body is an error, and a declaration stands on
// its own when the flags named no disposition.
func (nb noteBuild) resolveDisposition(declared declaredBlock) (Disposition, error) {
	requested := DispositionRecord
	explicitFlag := ""
	switch {
	case nb.Kind != "":
		requested, explicitFlag = DispositionWorkOrder, "kind"
	case nb.Category != "":
		requested, explicitFlag = DispositionPark, "category"
	}

	selfDeclared, ok := declared.disposition()
	if !ok {
		return requested, nil
	}
	if explicitFlag == "" {
		return selfDeclared, nil
	}
	if selfDeclared != requested {
		return "", fmt.Errorf("note body declares a %s disposition but --%s requests %s: drop the flag or the body's frontmatter", selfDeclared, explicitFlag, requested)
	}
	return requested, nil
}

// declaredBlock holds the fields a supplied note body declares in its own
// leading frontmatter block. Unrecognized keys are dropped with the block: they
// belong to the document the body came from, not to the note.
type declaredBlock struct {
	kind     string
	category string
	source   string
	synopsis string
}

// disposition reports the note disposition the block declares, using the same
// precedence as NoteDisposition. A kind that is not a todo marker (a doc's own
// `kind: design-doc`) declares nothing when it carries no category either.
func (b declaredBlock) disposition() (Disposition, bool) {
	switch {
	case b.kind == string(DispositionWorkOrder):
		return DispositionWorkOrder, true
	case b.category != "":
		return DispositionPark, true
	case b.kind == string(DispositionRecord):
		return DispositionRecord, true
	}
	return "", false
}

// fillFrom takes the values the receiver has not declared yet, so an earlier
// block outranks a later one.
func (b *declaredBlock) fillFrom(other declaredBlock) {
	fill := func(dst *string, val string) {
		if *dst == "" {
			*dst = val
		}
	}
	fill(&b.kind, other.kind)
	fill(&b.category, other.category)
	fill(&b.source, other.source)
	fill(&b.synopsis, other.synopsis)
}

// stripFrontmatter discards any leading frontmatter blocks from body and
// returns what is left. It backs --note-file, whose source is usually a
// free-standing doc: its frontmatter describes the doc, not the note, so it is
// dropped rather than reconciled. The note's own block is then written by
// buildNoteContent alone.
func stripFrontmatter(body string) string {
	_, stripped := extractFrontmatter(body)
	return stripped
}

// extractFrontmatter splits the leading frontmatter blocks off body, returning
// what they declare and the remaining body. Stacked blocks are consumed in
// order, which is how an already-double-blocked note heals into one.
func extractFrontmatter(body string) (declaredBlock, string) {
	var declared declaredBlock
	for {
		block, rest, ok := leadingBlock(body)
		if !ok {
			return declared, body
		}
		declared.fillFrom(block)
		body = rest
	}
}

// leadingBlock reads one leading frontmatter block. A block counts only when
// body opens with a --- line, closes with a --- line, and every non-blank line
// inside reads as `key: value`. A body that merely opens with a horizontal rule
// is left alone, so prose is never eaten by mistake.
func leadingBlock(body string) (declaredBlock, string, bool) {
	rest, ok := strings.CutPrefix(body, "---\n")
	if !ok {
		return declaredBlock{}, body, false
	}

	var declared declaredBlock
	for {
		line, tail, hasNewline := strings.Cut(rest, "\n")
		if strings.TrimSpace(line) == "---" {
			if !hasNewline {
				return declared, "", true
			}
			return declared, strings.TrimPrefix(tail, "\n"), true
		}
		if !hasNewline {
			return declaredBlock{}, body, false
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			key, val, isField := strings.Cut(trimmed, ":")
			if !isField {
				return declaredBlock{}, body, false
			}
			declared.set(strings.TrimSpace(key), strings.TrimSpace(val))
		}
		rest = tail
	}
}

// set records a recognized frontmatter key. Unknown keys are ignored.
func (b *declaredBlock) set(key, value string) {
	switch key {
	case "kind":
		b.kind = value
	case "category":
		b.category = value
	case "source":
		b.source = value
	case "synopsis":
		b.synopsis = value
	}
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
