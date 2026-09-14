package tui

import (
	"fmt"
	"github.com/ZenNotes/tui/internal/templates"
	"github.com/ZenNotes/tui/internal/vault"
	"github.com/ZenNotes/tui/internal/vim"
	"strings"
)

type templatesView struct {
	templates []templates.Template
	list      listCursor
	rows      int
	filter    string
	err       error
	rawByPath map[string]string
}

func (a *App) openTemplates()          { a.openVirtual(tabTemplates, func() view { return &templatesView{} }) }
func (v *templatesView) title() string { return "Templates" }
func (v *templatesView) hint(a *App) string {
	return "↑/↓ select · Enter actions · n new · / filter"
}
func (v *templatesView) refresh(a *App) {
	files, err := a.backend.ListTemplates(a.ctx)
	v.err = err
	v.rawByPath = map[string]string{}
	for _, f := range files {
		v.rawByPath[f.SourcePath] = f.Raw
	}
	all := templates.All(files, a.prefs.HideBuiltinTemplates)
	v.templates = nil
	for _, t := range all {
		if strings.Contains(strings.ToLower(t.Name+" "+t.Description), strings.ToLower(v.filter)) {
			v.templates = append(v.templates, t)
		}
	}
	v.list.clamp(len(v.templates))
}
func (v *templatesView) render(a *App, w, h int, focused bool) string {
	lines := []string{" Templates · " + v.filter, v.hint(a)}
	if v.err != nil {
		lines = append(lines, v.err.Error())
	}
	v.rows = max(1, h/2-2)
	v.list.ensureVisible(v.rows)
	for i := v.list.scroll; i < len(v.templates) && i < v.list.scroll+v.rows; i++ {
		t := v.templates[i]
		kind := "custom"
		if t.Builtin {
			kind = "built-in"
		}
		line := padRight(truncateCells(" "+t.Name+" · "+kind+" · "+t.Category, w), w)
		if focused && i == v.list.cursor {
			line = a.theme.SelectedFocus.Render(line)
		}
		lines = append(lines, line)
	}
	if v.list.cursor < len(v.templates) {
		t := v.templates[v.list.cursor]
		lines = append(lines, "", truncateCells("Title: "+t.TitleTemplate+" · Folder: "+string(t.TargetFolder)+"/"+t.TargetSubpath, w))
		lines = append(lines, wrapCommentText(t.Body, w)...)
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}
func (v *templatesView) handleKey(a *App, k vim.Key) bool {
	if a.listNav(&v.list, k, len(v.templates), v.rows) {
		return true
	}
	switch {
	case k.Is("enter"):
		if v.list.cursor < len(v.templates) {
			v.menu(a, v.templates[v.list.cursor])
		}
	case k.IsRune('n'):
		a.promptFor("New template name", "", "", func(a *App, name string) {
			if name = strings.TrimSpace(name); name == "" {
				return
			}
			t := templates.Template{Name: name, Category: "Custom", Body: "# {{title}}\n\n{{cursor}}\n"}
			v.save(a, t, "", true)
		})
	case k.IsRune('/'):
		a.promptFor("Filter templates", v.filter, "", func(a *App, s string) { v.filter = s; v.refresh(a) })
	case k.IsRune(':'):
		a.openLocalEx()
	default:
		return false
	}
	return true
}
func templateRaw(t templates.Template) string {
	return templates.ComposeFile(templates.ComposeInput{Name: t.Name, Description: t.Description, Category: t.Category, TitleTemplate: t.TitleTemplate, TargetFolder: t.TargetFolder, TargetSubpath: t.TargetSubpath, BuiltinID: t.BuiltinID, Body: t.Body})
}
func (v *templatesView) save(a *App, t templates.Template, previous string, edit bool, expected ...string) {
	if strings.TrimSpace(t.Name) == "" {
		a.notifyError("Template name cannot be empty")
		return
	}
	if buf := a.buffers[previous]; buf != nil && (buf.dirty() || buf.saving || buf.loading) {
		a.notifyError("Save or close the template editor before changing its metadata")
		return
	}
	if previous != "" {
		baseline, known := v.rawByPath[previous]
		if len(expected) > 0 {
			baseline = expected[0]
			known = true
		}
		current, err := readSnapshot(a.ctx, a.backend, previous)
		if err != nil {
			a.notifyError(err.Error())
			return
		}
		if !known || current.Body != baseline {
			a.notifyError("Template changed externally; refresh Templates and reopen metadata editing")
			return
		}
	}
	file, err := a.backend.WriteTemplate(a.ctx, vault.WriteTemplateInput{Slug: vault.SafeTemplateSlug(t.Name), Raw: templateRaw(t), PreviousSourcePath: previous})
	if err != nil {
		a.notifyError(err.Error())
		return
	}
	if previous != "" {
		a.repointNote(previous, file.SourcePath)
		if buf := a.buffers[file.SourcePath]; buf != nil {
			buf.ed.ReplaceText(file.Raw)
			buf.savedText = file.Raw
			buf.ed.MarkSaved()
		}
	}
	v.refresh(a)
	if edit {
		a.openTemplateFile(file)
	}
}
func (v *templatesView) menu(a *App, t templates.Template) {
	a.showMenu(t.Name, []menuItem{
		{key: "e", label: "Edit Markdown and metadata in editor", run: func(a *App) {
			if t.Builtin {
				t.BuiltinID = t.ID
				v.save(a, t, "", true)
				return
			}
			files, err := a.backend.ListTemplates(a.ctx)
			if err != nil {
				a.notifyError(err.Error())
				return
			}
			for _, file := range files {
				if file.SourcePath == t.SourcePath {
					a.openTemplateFile(file)
					return
				}
			}
			a.notifyError("Template no longer exists")
		}},
		{key: "m", label: "Edit metadata with prompts", run: func(a *App) { v.metadata(a, t) }},
		{key: "c", label: "Duplicate", run: func(a *App) {
			a.promptFor("Duplicate template", t.Name+" copy", "", func(a *App, name string) {
				if strings.TrimSpace(name) == "" {
					return
				}
				t.Name = name
				t.BuiltinID = ""
				v.save(a, t, "", true)
			})
		}},
		{key: "d", label: "Delete custom template / reset built-in override", run: func(a *App) {
			if t.Builtin {
				a.notify("Built-in templates can be hidden in Settings")
				return
			}
			a.confirm("Delete template "+t.Name+"?", func() {
				if buf := a.buffers[t.SourcePath]; buf != nil && buf.dirty() {
					a.notifyError("Save or close the template editor first")
					return
				}
				if err := a.backend.DeleteTemplate(a.ctx, t.SourcePath); err != nil {
					a.notifyError(err.Error())
					return
				}
				a.dropNote(t.SourcePath)
				v.refresh(a)
			})
		}},
		{key: "n", label: "Create a note", run: func(a *App) { a.createFromTemplate(t) }},
	})
}
func (v *templatesView) metadata(a *App, t templates.Template) {
	previous := t.SourcePath
	baseline := v.rawByPath[previous]
	if t.Builtin {
		t.BuiltinID = t.ID
		previous = ""
	}
	fields := []struct {
		name  string
		value *string
	}{{"Name", &t.Name}, {"Description", &t.Description}, {"Category", &t.Category}, {"Title pattern", &t.TitleTemplate}, {"Target subfolder", &t.TargetSubpath}}
	var step func(int)
	step = func(i int) {
		if i == len(fields) {
			items := []paletteItem{}
			for _, f := range []string{"inbox", "quick", "archive"} {
				items = append(items, paletteItem{label: f, id: f})
			}
			a.overlay = &palette{title: "Target folder", items: items, filtered: items, onSelect: func(a *App, it paletteItem) {
				t.TargetFolder = vault.NoteFolder(it.id)
				v.save(a, t, previous, false, baseline)
			}}
			return
		}
		field := fields[i]
		a.promptFor(fmt.Sprintf("Template %d/%d · %s", i+1, len(fields), field.name), *field.value, "Esc cancels all changes", func(a *App, s string) { *field.value = s; step(i + 1) })
	}
	step(0)
}
func (a *App) openTemplateFile(file vault.CustomTemplateFile) {
	if buf := a.buffers[file.SourcePath]; buf == nil {
		t := templates.ParseCustom(file)
		buf = &noteBuffer{path: file.SourcePath, meta: vault.NoteMeta{Path: file.SourcePath, Title: t.Name}, savedText: file.Raw}
		buf.ed = vim.New(file.Raw, a.editorOptions(), a.editorHooks(buf))
		buf.ed.MarkSaved()
		a.buffers[file.SourcePath] = buf
	}
	a.openNote(file.SourcePath, true)
}
