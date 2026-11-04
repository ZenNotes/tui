package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/templates"
	"github.com/ZenNotes/tui/internal/vault"
)

// Tools for the jobs an assistant is asked to do most and could only do badly
// before: changing a task without hand-editing its line, finding the daily note
// without guessing a path, and working on part of a long note without reading
// or rewriting all of it. Each is composed from the backend's own reads and
// writes, so a scope and a server apply to them as to any other tool.

func optionalBoolPtr(a args, key string) (*bool, error) {
	v, present := a[key]
	if !present || v == nil {
		return nil, nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil, fmt.Errorf("%s must be a boolean", key)
	}
	return &b, nil
}

func optionalStringPtr(a args, key string) (*string, error) {
	s, present, err := a.optionalString(key)
	if err != nil || !present {
		return nil, err
	}
	return &s, nil
}

// optionalLine is a zero-based line number, -1 when the client gave none.
func optionalLine(a args, key string) (int, error) {
	n, present, err := a.optionalNumber(key)
	if err != nil {
		return -1, err
	}
	if !present {
		return -1, nil
	}
	return int(n), nil
}

// resolveDate reads `today`, `yesterday`, `tomorrow` or YYYY-MM-DD, in the time
// zone of the machine running the server, as the app does.
func resolveDate(raw string, now time.Time) (time.Time, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "today":
		return now, nil
	case "yesterday":
		return now.AddDate(0, 0, -1), nil
	case "tomorrow":
		return now.AddDate(0, 0, 1), nil
	}
	if !vault.IsValidISODate(raw) {
		return time.Time{}, errors.New("date must be today, yesterday, tomorrow or YYYY-MM-DD")
	}
	return time.ParseInLocation("2006-01-02", raw, now.Location())
}

// periodicBody is what the app starts the note with: the vault's template for
// this kind when the client may read it, else a heading. The app's rollover of
// unfinished tasks is left out: moving a user's tasks is not what finding a note
// should do. A scope that hides
// part of the vault gets the heading, because a template can hold anything.
func periodicBody(ctx context.Context, b Backend, kind periodic.Kind, settings vault.VaultSettings, title string, date time.Time) string {
	p := settings.DailyNotes
	switch kind {
	case periodic.Weekly:
		p = settings.WeeklyNotes
	case periodic.Monthly:
		p = settings.MonthlyNotes
	}
	if p.TemplateID != "" {
		if custom, err := b.ListTemplates(ctx); err == nil {
			for _, t := range templates.All(custom, false) {
				if t.ID == p.TemplateID || t.BuiltinID == p.TemplateID {
					return templates.RenderLocale(t.Body, title, date, p.Locale).Body
				}
			}
		}
	}
	return "# " + title + "\n\n"
}

func workTools() []toolDef {
	return []toolDef{
		{
			name:        "update_task",
			description: "Change a task by its id from list_tasks: due date, priority, @waiting, state, status (Kanban column) or text. Only what you pass changes; pass \"\" to clear due, priority or status. The task stays where it is, and the rest of the note keeps its bytes. Returns the updated task, or null if the id no longer matches one.",
			schema: `{"type":"object","properties":{
"id":{"type":"string","description":"Task id from list_tasks: sourcePath#taskIndex."},
"due":{"type":"string","description":"YYYY-MM-DD, or \"\" to clear."},
"priority":{"type":"string","enum":["high","med","low",""],"description":"\"\" clears it."},
"waiting":{"type":"boolean"},
"state":{"type":"string","enum":["open","done","in-progress","cancelled"]},
"status":{"type":"string","description":"The column a Kanban board groups it under, one word, or \"\" to clear."},
"text":{"type":"string","description":"Replaces everything after the checkbox, one line, due and priority tokens included. Not for a task note: use rename_note."}},"required":["id"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				in := backend.UpdateTaskInput{}
				var err error
				if in.ID, err = a.requireString("id"); err != nil {
					return nil, err
				}
				for key, target := range map[string]**string{"due": &in.Due, "priority": &in.Priority, "state": &in.State, "status": &in.Status, "text": &in.Text} {
					if *target, err = optionalStringPtr(a, key); err != nil {
						return nil, err
					}
				}
				if in.Waiting, err = optionalBoolPtr(a, "waiting"); err != nil {
					return nil, err
				}
				task, err := backend.UpdateTask(ctx, b, in)
				if err != nil {
					return nil, err
				}
				if task == nil {
					return nil, nil
				}
				return task, nil
			},
		},
		{
			name:        "daily_note",
			description: "Find the daily note for a date (or the weekly or monthly note holding it) under this vault's own folder and naming settings, which you cannot guess. Changes nothing. If `exists`, use its `path`. If not, pass `createWith` to create_note as it is, which starts the note from the vault's template, and use the `path` create_note returns. `enabled: false` means the user has this kind of note turned off, so ask before creating one.",
			schema: `{"type":"object","properties":{
"kind":{"type":"string","enum":["daily","weekly","monthly"],"description":"Default: daily."},
"date":{"type":"string","description":"today, yesterday, tomorrow or YYYY-MM-DD. Default: today."}}}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				kindName, _, err := a.optionalString("kind")
				if err != nil {
					return nil, err
				}
				kind := periodic.Daily
				switch kindName {
				case "", "daily":
				case "weekly":
					kind = periodic.Weekly
				case "monthly":
					kind = periodic.Monthly
				default:
					return nil, errors.New("kind must be daily, weekly or monthly")
				}
				rawDate, _, err := a.optionalString("date")
				if err != nil {
					return nil, err
				}
				date, err := resolveDate(rawDate, time.Now())
				if err != nil {
					return nil, err
				}
				settings, err := b.VaultSettings(ctx)
				if err != nil {
					return nil, err
				}
				desc, err := b.Describe(ctx)
				if err != nil {
					return nil, err
				}
				notes, err := b.ListNotes(ctx)
				if err != nil {
					return nil, err
				}
				atRoot := desc.PrimaryNotesLocation == vault.PrimaryNotesRoot
				enabled := settings.DailyNotes.Enabled
				switch kind {
				case periodic.Weekly:
					enabled = settings.WeeklyNotes.Enabled
				case periodic.Monthly:
					enabled = settings.MonthlyNotes.Enabled
				}
				loc := periodic.LocationFor(kind, date, settings, atRoot)
				rel := periodic.RelPathFor(kind, date, settings, atRoot, settings.SystemFolderPaths)
				result := orderedResult{{"kind", string(kind)}, {"date", date.Format("2006-01-02")}, {"enabled", enabled}}
				var found *vault.NoteMeta
				for i := range notes {
					if notes[i].Path == rel {
						found = &notes[i]
						break
					}
				}
				// A note made under an older naming pattern still counts.
				if found == nil {
					want := periodicAnchor(kind, date)
					for i := range notes {
						n := &notes[i]
						if n.Folder != vault.FolderInbox {
							continue
						}
						sub := vault.FolderSubpathOf(n.Path, n.Folder, atRoot, settings.SystemFolderPaths)
						if d, ok := periodic.DateOf(kind, sub, n.Title, settings, atRoot); ok && periodicAnchor(kind, d) == want {
							found = n
							break
						}
					}
				}
				if found != nil {
					return append(result, orderedResult{{"exists", true}, {"path", found.Path}, {"note", *found}}...), nil
				}
				return append(result, orderedResult{
					{"exists", false},
					{"createWith", orderedResult{
						{"folder", string(vault.FolderInbox)},
						{"subpath", loc.Subpath},
						{"title", loc.Title},
						{"body", periodicBody(ctx, b, kind, settings, loc.Title, date)},
					}},
				}...), nil
			},
		},
		{
			name:        "note_sections",
			description: "Read a note by its headings. Without `heading`: its outline, every heading with its level and lines, for long notes and for finding where to edit. With `heading`: the text under that heading, subheadings included, so you need not read the whole note. Lines are zero-based, as in insert_at_line; endLineNumber is exclusive.",
			schema: `{"type":"object","properties":{
"path":{"type":"string"},
"heading":{"type":"string","description":"Heading text, any case, without the #s."},
"lineNumber":{"type":"number","description":"The heading's line, from the outline, when its text appears more than once."}},"required":["path"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				heading, _, err := a.optionalString("heading")
				if err != nil {
					return nil, err
				}
				line, err := optionalLine(a, "lineNumber")
				if err != nil {
					return nil, err
				}
				note, err := b.ReadNote(ctx, rel)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(heading) == "" {
					return orderedResult{{"path", note.Path}, {"headings", vault.NoteSections(note.Body)}}, nil
				}
				section, err := vault.FindSection(note.Body, heading, line)
				if err != nil {
					return nil, err
				}
				return orderedResult{
					{"path", note.Path},
					{"heading", section.Text},
					{"level", section.Level},
					{"lineNumber", section.Line},
					{"endLineNumber", section.End},
					{"body", vault.SectionBody(note.Body, section)},
				}, nil
			},
		},
		{
			name:        "edit_section",
			description: "Edit the part of a note under one heading: `append` (the usual choice, such as a new item under Action Items; it goes above any subheading), `prepend`, or `replace`, which rewrites everything under the heading, subheadings included. The heading and the rest of the note keep their bytes. Fails with the note's headings if there is no such heading.",
			schema: `{"type":"object","properties":{
"path":{"type":"string"},
"heading":{"type":"string","description":"Heading text, any case, without the #s."},
"mode":{"type":"string","enum":["append","prepend","replace"]},
"text":{"type":"string","description":"Markdown. Empty only with replace, to clear the section."},
"lineNumber":{"type":"number","description":"The heading's line, from note_sections, when its text appears more than once."}},"required":["path","heading","mode","text"]}`,
			handler: func(ctx context.Context, a args, b Backend) (any, error) {
				rel, err := a.requireString("path")
				if err != nil {
					return nil, err
				}
				heading, err := a.requireString("heading")
				if err != nil {
					return nil, err
				}
				mode, err := a.requireString("mode")
				if err != nil {
					return nil, err
				}
				text, ok := a["text"].(string)
				if !ok {
					return nil, errors.New("text must be a string")
				}
				editMode := vault.SectionEditMode(mode)
				switch editMode {
				case vault.SectionAppend, vault.SectionPrepend:
					if strings.TrimSpace(text) == "" {
						return nil, errors.New("text is empty; only replace can clear a section")
					}
				case vault.SectionReplace:
				default:
					return nil, errors.New("mode must be append, prepend or replace")
				}
				line, err := optionalLine(a, "lineNumber")
				if err != nil {
					return nil, err
				}
				note, err := b.ReadNote(ctx, rel)
				if err != nil {
					return nil, err
				}
				section, err := vault.FindSection(note.Body, heading, line)
				if err != nil {
					return nil, err
				}
				return b.WriteNote(ctx, note.Path, vault.EditSectionInBody(note.Body, section, editMode, text))
			},
		},
	}
}

// periodicAnchor is the day that names a date's note of this kind, so two dates
// in one week or month compare equal.
func periodicAnchor(kind periodic.Kind, date time.Time) string {
	switch kind {
	case periodic.Weekly:
		y, w := date.ISOWeek()
		return periodic.MondayOfISOWeek(y, w).Format("2006-01-02")
	case periodic.Monthly:
		return date.Format("2006-01")
	}
	return date.Format("2006-01-02")
}
