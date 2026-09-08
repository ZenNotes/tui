// Package templates is the note-template system: the built-ins the app
// ships, custom templates parsed from `.zennotes/templates/*.md`, and the
// `{{variable}}` substitution applied when a note is created from one.
package templates

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ZenNotes/tui/internal/periodic"
	"github.com/ZenNotes/tui/internal/vault"
)

// Template is a note template, built-in or custom.
type Template struct {
	// ID is `builtin.<name>` for a built-in, `custom:<slug>` for a custom one.
	ID          string
	Name        string
	Description string
	Category    string
	// Body is markdown with `{{title}}`, `{{date}}`, `{{cursor}}` tokens.
	Body string
	// TitleTemplate is an optional title pattern.
	TitleTemplate string
	// TargetFolder is the default destination, never trash.
	TargetFolder  vault.NoteFolder
	TargetSubpath string
	Builtin       bool
	SourcePath    string
	// BuiltinID marks a custom template that is an edited copy of a
	// built-in; it shadows the built-in everywhere.
	BuiltinID string
}

func builtin(id, name, description, category, titleTemplate, body string) Template {
	return Template{ID: id, Name: name, Description: description, Category: category, TitleTemplate: titleTemplate, Body: body, Builtin: true}
}

// Builtins are the templates the app ships, in its order.
func Builtins() []Template {
	return []Template{
		builtin("builtin.adr", "ADR", "Architecture Decision Record", "Engineering", "",
			"# {{title}}\n\n- **Status:** Proposed\n- **Date:** {{date}}\n- **Deciders:**\n\n## Context\n\n{{cursor}}\n\n## Decision\n\n## Consequences\n\n### Positive\n\n### Negative\n\n## Related\n\n"),
		builtin("builtin.rfc", "RFC / Design Doc", "Proposal with motivation, design, and alternatives", "Engineering", "",
			"# {{title}}\n\n- **Author:**\n- **Status:** Draft\n- **Date:** {{date}}\n\n## Summary\n\n{{cursor}}\n\n## Motivation\n\n## Proposal\n\n## Alternatives considered\n\n## Rollout & risks\n\n## Related\n\n"),
		builtin("builtin.bug", "Bug Report", "Reproducible bug with expected vs actual behavior", "Engineering", "",
			"# {{title}}\n\n- **Date:** {{date}}\n- **Severity:**\n- **Environment:**\n\n## Steps to reproduce\n\n1. {{cursor}}\n\n## Expected\n\n## Actual\n\n## Notes\n\n"),
		builtin("builtin.postmortem", "Postmortem", "Incident review: timeline, root cause, action items", "Engineering", "",
			"# {{title}}\n\n- **Date:** {{date}}\n- **Authors:**\n- **Impact:**\n\n## Summary\n\n{{cursor}}\n\n## Timeline\n\n## Root cause\n\n## Resolution\n\n## Action items\n\n- [ ]\n\n## Related\n\n"),
		builtin("builtin.meeting", "Meeting Notes", "Agenda, notes, decisions, and action items", "Engineering", "Meeting — {{date:YYYY-MM-DD}}",
			"# {{title}}\n\n- **Date:** {{date}}\n- **Attendees:**\n\n## Agenda\n\n- {{cursor}}\n\n## Notes\n\n## Decisions\n\n## Action items\n\n- [ ]\n\n"),
		builtin("builtin.oneonone", "1:1", "One-on-one: wins, blockers, growth, follow-ups", "Engineering", "1-1 — {{date:YYYY-MM-DD}}",
			"# {{title}}\n\n- **Date:** {{date}}\n\n## Wins\n\n{{cursor}}\n\n## Challenges & blockers\n\n## Growth & feedback\n\n## Follow-ups\n\n- [ ]\n\n"),
		builtin("builtin.daily", "Daily Note", "A dated daily log with focus, schedule, and tasks", "Personal", "{{date:YYYY-MM-DD}}",
			"# {{date:dddd, MMMM D, YYYY}}\n\n## Focus\n\n- {{cursor}}\n\n## Schedule\n\n## Notes\n\n## Tasks\n\n- [ ]\n\n## Log\n\n"),
		builtin("builtin.weekly", "Weekly Review", "Review last week and plan the next", "Personal", "{{date:YYYY}}-W{{week}}",
			"# Week {{week}}, {{date:YYYY}}\n\n## Last week — review\n\n- {{cursor}}\n\n## Wins\n\n## This week — plan\n\n- [ ]\n\n## Carry-overs\n\n## Notes\n\n## Related\n\n"),
		builtin("builtin.reading", "Reading Notes", "Notes on a book or article: ideas, quotes, takeaways", "Personal", "",
			"# {{title}}\n\n- **Author:**\n- **Started:** {{date}}\n- **Status:** Reading\n\n## Key ideas\n\n- {{cursor}}\n\n## Quotes\n\n>\n\n## Takeaways\n\n## Related\n\n"),
		builtin("builtin.journal", "Journal", "A free-form, first-person dated entry", "Personal", "{{date:YYYY-MM-DD}}",
			"# {{date:dddd, MMMM D, YYYY}}\n\n{{cursor}}\n\n"),
		builtin("builtin.kickoff", "Project Kickoff", "Goals, scope, milestones, and stakeholders", "Personal", "",
			"# {{title}}\n\n- **Date:** {{date}}\n- **Owner:**\n\n## Goals\n\n- {{cursor}}\n\n## Scope\n\n### In scope\n\n### Out of scope\n\n## Milestones\n\n## Stakeholders\n\n## Risks\n\n## Related\n\n"),
		builtin("builtin.todo", "To-do", "A simple checklist scaffold", "Personal", "",
			"# {{title}}\n\n- [ ] {{cursor}}\n- [ ]\n- [ ]\n\n"),
	}
}

func filenameStem(sourcePath string) string {
	file := sourcePath[strings.LastIndex(sourcePath, "/")+1:]
	if strings.HasSuffix(strings.ToLower(file), ".md") {
		return file[:len(file)-3]
	}
	return file
}

// SlugifyName turns a display name into a safe, lowercase filename stem.
func SlugifyName(name string) string {
	slug := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "template"
	}
	return slug
}

// ParseCustom reads a custom template file: optional frontmatter (name,
// description, category, titleTemplate, targetFolder, targetSubpath,
// builtinId) plus a body. Never fails.
func ParseCustom(file vault.CustomTemplateFile) Template {
	data, body := vault.ParseFrontmatterScalars(file.Raw)
	stem := filenameStem(file.SourcePath)
	name := strings.TrimSpace(data["name"])
	if name == "" {
		name = stem
	}
	category := data["category"]
	if category != "Engineering" && category != "Personal" && category != "Custom" {
		category = "Custom"
	}
	var folder vault.NoteFolder
	switch data["targetFolder"] {
	case "inbox", "quick", "archive":
		folder = vault.NoteFolder(data["targetFolder"])
	}
	return Template{
		ID:            "custom:" + stem,
		Name:          name,
		Description:   strings.TrimSpace(data["description"]),
		Category:      category,
		Body:          body,
		TitleTemplate: strings.TrimSpace(data["titleTemplate"]),
		TargetFolder:  folder,
		TargetSubpath: strings.TrimSpace(data["targetSubpath"]),
		SourcePath:    file.SourcePath,
		BuiltinID:     strings.TrimSpace(data["builtinId"]),
	}
}

func yamlScalar(value string) string {
	if strings.ContainsAny(value, ":#\"'\n") || strings.TrimSpace(value) != value || value == "" {
		return vault.YAMLValue(value)
	}
	return value
}

// ComposeInput describes a template file to write.
type ComposeInput struct {
	Name          string
	Description   string
	Category      string
	TitleTemplate string
	TargetFolder  vault.NoteFolder
	TargetSubpath string
	BuiltinID     string
	Body          string
}

// ComposeFile builds a raw template `.md` from structured fields.
func ComposeFile(in ComposeInput) string {
	lines := []string{"---", "name: " + yamlScalar(in.Name)}
	if in.Description != "" {
		lines = append(lines, "description: "+yamlScalar(in.Description))
	}
	category := in.Category
	if category == "" {
		category = "Custom"
	}
	lines = append(lines, "category: "+category)
	if in.TitleTemplate != "" {
		lines = append(lines, "titleTemplate: "+yamlScalar(in.TitleTemplate))
	}
	if in.TargetFolder != "" {
		lines = append(lines, "targetFolder: "+string(in.TargetFolder))
	}
	if in.TargetSubpath != "" {
		lines = append(lines, "targetSubpath: "+yamlScalar(in.TargetSubpath))
	}
	if in.BuiltinID != "" {
		lines = append(lines, "builtinId: "+in.BuiltinID)
	}
	lines = append(lines, "---")
	return strings.Join(lines, "\n") + "\n" + strings.TrimLeft(in.Body, "\n")
}

func categoryRank(c string) int {
	switch c {
	case "Engineering":
		return 0
	case "Personal":
		return 1
	case "Custom":
		return 2
	}
	return 3
}

// Merge combines built-ins and custom templates into one display list:
// grouped by category, built-ins (and customized built-ins) before custom
// within a group, then alphabetical. A custom template carrying builtinId
// replaces that built-in.
func Merge(builtins, custom []Template) []Template {
	overridden := map[string]bool{}
	for _, c := range custom {
		if c.BuiltinID != "" {
			overridden[c.BuiltinID] = true
		}
	}
	out := []Template{}
	for _, b := range builtins {
		if !overridden[b.ID] {
			out = append(out, b)
		}
	}
	out = append(out, custom...)
	isPrimary := func(t Template) bool { return t.Builtin || t.BuiltinID != "" }
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Category != b.Category {
			return categoryRank(a.Category) < categoryRank(b.Category)
		}
		if isPrimary(a) != isPrimary(b) {
			return isPrimary(a)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// All is Merge over the built-ins (unless hidden) and the vault's custom
// templates.
func All(custom []vault.CustomTemplateFile, hideBuiltins bool) []Template {
	parsed := make([]Template, 0, len(custom))
	for _, file := range custom {
		parsed = append(parsed, ParseCustom(file))
	}
	var builtins []Template
	if !hideBuiltins {
		builtins = Builtins()
	}
	return Merge(builtins, parsed)
}

const cursorToken = "{{cursor}}"

var tokenRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

func substitute(input, title string, now time.Time) string {
	return tokenRe.ReplaceAllStringFunc(input, func(full string) string {
		m := tokenRe.FindStringSubmatch(full)
		token := strings.TrimSpace(m[1])
		_, week := now.ISOWeek()
		switch {
		case token == "cursor":
			return cursorToken
		case token == "title":
			return title
		case token == "date":
			return now.Format("2006-01-02")
		case token == "time":
			return now.Format("15:04")
		case token == "week":
			return padTwo(week)
		case strings.HasPrefix(token, "date:"):
			return periodic.FormatTemplateDate(now, token[len("date:"):])
		}
		return full
	})
}

func padTwo(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// Rendered is a template body with its tokens expanded; CursorOffset is the
// rune offset of `{{cursor}}` in the body, or -1 when absent.
type Rendered struct {
	Body         string
	CursorOffset int
}

// Render expands a template body.
func Render(body, title string, now time.Time) Rendered {
	substituted := substitute(body, title, now)
	offset := -1
	if idx := strings.Index(substituted, cursorToken); idx >= 0 {
		offset = len([]rune(substituted[:idx]))
	}
	return Rendered{Body: strings.ReplaceAll(substituted, cursorToken, ""), CursorOffset: offset}
}

// RenderTitle expands a title pattern; cursor tokens drop.
func RenderTitle(titleTemplate, title string, now time.Time) string {
	return strings.TrimSpace(strings.ReplaceAll(substitute(titleTemplate, title, now), cursorToken, ""))
}
