package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ZenNotes/tui/internal/backend"
	"github.com/ZenNotes/tui/internal/database"
	"github.com/ZenNotes/tui/internal/vault"
)

// resolveDatabase finds a database by title (case-insensitive) or path
// (`inbox/X.base`, with or without `/data.csv`).
func resolveDatabase(ops *database.Ops, ref string) (database.Summary, error) {
	if ref == "" {
		return database.Summary{}, errors.New(`Name the database: a title like "Meetings" or a path like inbox/Meetings.base`)
	}
	all, err := ops.ListDatabases()
	if err != nil {
		return database.Summary{}, err
	}
	norm := strings.Trim(strings.ReplaceAll(ref, "\\", "/"), "/")
	for _, d := range all {
		if d.Path == norm || database.FormDirFromCSVPath(d.Path) == norm || strings.EqualFold(d.Title, norm) {
			return d, nil
		}
	}
	titles := make([]string, 0, len(all))
	for _, d := range all {
		titles = append(titles, d.Title)
	}
	available := strings.Join(titles, ", ")
	if available == "" {
		available = "(none)"
	}
	return database.Summary{}, fmt.Errorf("No database matches %q. Databases in this vault: %s", ref, available)
}

// resolveRow finds a row by id, or by its title value when unambiguous.
func resolveRow(doc *database.Doc, ref string) (database.Row, error) {
	if ref == "" {
		return database.Row{}, errors.New("Name the row: an id from `zn base rows`, or its title value.")
	}
	if row := doc.RowByID(ref); row != nil {
		return *row, nil
	}
	needle := strings.ToLower(strings.TrimSpace(ref))
	matches := []database.Row{}
	for _, r := range doc.Rows {
		if strings.ToLower(strings.TrimSpace(doc.RecordTitle(r))) == needle {
			matches = append(matches, r)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for i, r := range matches {
			ids[i] = r.ID
		}
		return database.Row{}, fmt.Errorf("%q matches %d rows; use an id: %s", ref, len(matches), strings.Join(ids, ", "))
	}
	return database.Row{}, fmt.Errorf("No row matches %q. List them with: zn base rows %q", ref, doc.Title)
}

func fieldByName(doc *database.Doc, name string) (database.Field, error) {
	needle := strings.ToLower(strings.TrimSpace(name))
	for _, f := range doc.Fields {
		if strings.ToLower(f.Name) == needle {
			return f, nil
		}
	}
	names := []string{}
	for _, f := range doc.Fields {
		if f.ID != doc.IDFieldID {
			names = append(names, f.Name)
		}
	}
	return database.Field{}, fmt.Errorf("No field named %q. Fields: %s", name, strings.Join(names, ", "))
}

type assignment struct {
	name, value string
}

func parseAssignments(args Args) ([]assignment, error) {
	out := []assignment{}
	for _, raw := range args.Many("set") {
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("--set takes Field=Value (got %q)", raw)
		}
		out = append(out, assignment{name: strings.TrimSpace(raw[:eq]), value: raw[eq+1:]})
	}
	return out, nil
}

// applyAssignments writes cells with the grid's semantics: select values
// mint options (a schema write), everything else is a plain cell write.
func applyAssignments(doc *database.Doc, rowID string, assignments []assignment) (bool, error) {
	schemaChanged := false
	for _, a := range assignments {
		field, err := fieldByName(doc, a.name)
		if err != nil {
			return false, err
		}
		if field.ID == doc.IDFieldID {
			return false, errors.New("The id field cannot be set.")
		}
		if field.Type == "select" || field.Type == "multiSelect" {
			values := []string{}
			if field.Type == "multiSelect" {
				values = database.SplitMultiSelect(a.value)
			} else if a.value != "" {
				values = []string{a.value}
			}
			for _, v := range values {
				if doc.EnsureSelectOption(field.ID, v) {
					schemaChanged = true
				}
			}
		}
		doc.SetCell(rowID, field.ID, a.value)
	}
	return schemaChanged, nil
}

func persist(ops *database.Ops, csvPath string, doc *database.Doc, schemaChanged bool) (*database.Doc, error) {
	if schemaChanged {
		return ops.WriteSchema(csvPath, doc)
	}
	return ops.WriteRows(csvPath, doc.Rows)
}

// remirrorPage re-mirrors a row's properties into its page's frontmatter,
// keeping the body, exactly what the app does when it opens a record page.
func remirrorPage(ctx context.Context, b backend.Backend, doc *database.Doc, row database.Row) string {
	pagePath := doc.Pages[row.ID]
	if pagePath == "" {
		return ""
	}
	note, err := b.ReadNote(ctx, pagePath)
	if err != nil {
		return ""
	}
	_, body, _ := vault.Frontmatter(note.Body)
	if _, err := b.WriteNote(ctx, pagePath, doc.ComposePageBody(row, body)); err != nil {
		return ""
	}
	return pagePath
}

type rowView struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Cells map[string]string `json:"cells"`
	Page  string            `json:"page,omitempty"`
}

func viewOf(doc *database.Doc, row database.Row) rowView {
	cells := map[string]string{}
	for _, f := range doc.Fields {
		if f.ID == doc.IDFieldID {
			continue
		}
		cells[f.Name] = row.Cells[f.ID]
	}
	return rowView{ID: row.ID, Title: doc.RecordTitle(row), Cells: cells, Page: doc.Pages[row.ID]}
}

func cmdBaseList(_ context.Context, b backend.Backend, args Args) error {
	all, err := b.DatabaseOps().ListDatabases()
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(all)
		return nil
	}
	if len(all) == 0 {
		emitLine("No databases in this vault.")
		return nil
	}
	for _, d := range all {
		emitLine(d.Title + "\t" + d.Path)
	}
	return nil
}

func cmdBaseCreate(_ context.Context, b backend.Backend, args Args) error {
	title := args.Positional(0)
	if title == "" {
		title = args.Str("title")
	}
	if title == "" {
		return errors.New("zn base create requires a title.")
	}
	folderSpec := strings.Trim(args.Str("folder"), "/")
	if folderSpec == "" {
		folderSpec = "inbox"
	}
	parts := strings.Split(folderSpec, "/")
	var folder vault.NoteFolder
	for _, f := range topFolders {
		if string(f) == parts[0] {
			folder = f
		}
	}
	if folder == "" {
		return fmt.Errorf("--folder must start with one of inbox, quick, archive (got %q).", parts[0])
	}
	doc, err := b.DatabaseOps().CreateDatabase(folder, strings.Join(parts[1:], "/"), title)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "title": doc.Title, "path": doc.Path})
		return nil
	}
	emitOK(fmt.Sprintf("Created database %s at %s", doc.Title, doc.Path))
	return nil
}

func cmdBaseRows(_ context.Context, b backend.Backend, args Args) error {
	ops := b.DatabaseOps()
	summary, err := resolveDatabase(ops, args.Positional(0))
	if err != nil {
		return err
	}
	doc, err := ops.OpenDatabase(summary.Path)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		views := make([]rowView, 0, len(doc.Rows))
		for _, r := range doc.Rows {
			views = append(views, viewOf(doc, r))
		}
		emitJSON(views)
		return nil
	}
	if len(doc.Rows) == 0 {
		emitLine("No rows.")
		return nil
	}
	for _, row := range doc.Rows {
		extras := []string{}
		skippedTitle := false
		for _, f := range doc.Fields {
			if f.ID == doc.IDFieldID {
				continue
			}
			if !skippedTitle {
				skippedTitle = true
				continue
			}
			if v := strings.TrimSpace(row.Cells[f.ID]); v != "" {
				extras = append(extras, f.Name+"="+v)
			}
		}
		line := row.ID + "\t" + doc.RecordTitle(row)
		if len(extras) > 0 {
			line += "\t" + truncate(strings.Join(extras, "  "), 100)
		}
		emitLine(line)
	}
	return nil
}

func cmdBaseGet(_ context.Context, b backend.Backend, args Args) error {
	ops := b.DatabaseOps()
	summary, err := resolveDatabase(ops, args.Positional(0))
	if err != nil {
		return err
	}
	doc, err := ops.OpenDatabase(summary.Path)
	if err != nil {
		return err
	}
	row, err := resolveRow(doc, args.Positional(1))
	if err != nil {
		return err
	}
	view := viewOf(doc, row)
	if args.Bool("json") {
		emitJSON(view)
		return nil
	}
	emitLine("id: " + row.ID)
	for _, f := range doc.Fields {
		if f.ID == doc.IDFieldID {
			continue
		}
		emitLine(f.Name + ": " + row.Cells[f.ID])
	}
	if view.Page != "" {
		emitLine("page: " + view.Page)
	}
	return nil
}

func cmdBaseAdd(ctx context.Context, b backend.Backend, args Args) error {
	ops := b.DatabaseOps()
	summary, err := resolveDatabase(ops, args.Positional(0))
	if err != nil {
		return err
	}
	assignments, err := parseAssignments(args)
	if err != nil {
		return err
	}
	if len(assignments) == 0 {
		return errors.New("zn base add requires at least one --set Field=Value (set the title field).")
	}
	// `--body -` reads stdin EXPLICITLY. Auto-draining a non-TTY stdin hangs
	// forever when a spawning process leaves its pipe open, which is how
	// agent tooling runs CLIs.
	var body *string
	if flag, ok := args.String("body"); ok {
		if flag == "-" {
			if s := strings.TrimSpace(ReadStdin()); s != "" {
				body = &s
			}
		} else {
			body = &flag
		}
	}
	wantPage := body != nil || args.Bool("page")
	if wantPage && !database.HasRecordPages(summary.Path) {
		return fmt.Errorf("%s is a loose .csv, which has no record pages. Run `zn base convert %q` first, or add the row without --page/--body.", summary.Path, summary.Title)
	}

	doc, err := ops.OpenDatabase(summary.Path)
	if err != nil {
		return err
	}
	row := doc.AddRow()
	schemaChanged, err := applyAssignments(doc, row.ID, assignments)
	if err != nil {
		return err
	}
	pagePath := ""
	if wantPage {
		current := *doc.RowByID(row.ID)
		title := doc.RecordTitle(current)
		pageBody := "# " + title + "\n\n"
		if body != nil {
			pageBody += *body + "\n"
		}
		pagePath, err = ops.CreateRecordPage(summary.Path, title, doc.ComposePageBody(current, pageBody))
		if err != nil {
			return err
		}
		if doc.Pages == nil {
			doc.Pages = map[string]string{}
		}
		doc.Pages[row.ID] = pagePath
		schemaChanged = true
	}
	doc, err = persist(ops, summary.Path, doc, schemaChanged)
	if err != nil {
		return err
	}
	finalRow := *doc.RowByID(row.ID)
	if args.Bool("json") {
		view := viewOf(doc, finalRow)
		if pagePath != "" {
			view.Page = pagePath
		}
		emitJSON(struct {
			OK bool `json:"ok"`
			rowView
		}{true, view})
		return nil
	}
	emitOK(fmt.Sprintf("Added %s (%s)", doc.RecordTitle(finalRow), row.ID))
	if pagePath != "" {
		emitLine("  page: " + pagePath)
	}
	return nil
}

func cmdBaseSet(ctx context.Context, b backend.Backend, args Args) error {
	ops := b.DatabaseOps()
	summary, err := resolveDatabase(ops, args.Positional(0))
	if err != nil {
		return err
	}
	doc, err := ops.OpenDatabase(summary.Path)
	if err != nil {
		return err
	}
	row, err := resolveRow(doc, args.Positional(1))
	if err != nil {
		return err
	}
	assignments, err := parseAssignments(args)
	if err != nil {
		return err
	}
	if len(assignments) == 0 {
		return errors.New("zn base set requires --set Field=Value.")
	}
	schemaChanged, err := applyAssignments(doc, row.ID, assignments)
	if err != nil {
		return err
	}
	doc, err = persist(ops, summary.Path, doc, schemaChanged)
	if err != nil {
		return err
	}
	finalRow := *doc.RowByID(row.ID)
	page := remirrorPage(ctx, b, doc, finalRow)
	if args.Bool("json") {
		view := viewOf(doc, finalRow)
		if page != "" {
			view.Page = page
		}
		emitJSON(struct {
			OK bool `json:"ok"`
			rowView
		}{true, view})
		return nil
	}
	emitOK(fmt.Sprintf("Updated %s (%s)", doc.RecordTitle(finalRow), finalRow.ID))
	for _, a := range assignments {
		field, _ := fieldByName(doc, a.name)
		emitLine(fmt.Sprintf("  %s: %s", field.Name, finalRow.Cells[field.ID]))
	}
	if page != "" {
		emitLine("  page re-mirrored: " + page)
	}
	return nil
}

// cmdBaseConvert turns a loose .csv into a `<Name>.base` folder, keeping
// the schema sidecar, so its rows can have record pages.
func cmdBaseConvert(_ context.Context, b backend.Backend, args Args) error {
	ops := b.DatabaseOps()
	summary, err := resolveDatabase(ops, args.Positional(0))
	if err != nil {
		return err
	}
	if !database.IsLooseCSVPath(summary.Path) {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "path": summary.Path, "title": summary.Title, "changed": false})
			return nil
		}
		emitOK(fmt.Sprintf("%s is already a folder database (%s)", summary.Title, summary.Path))
		return nil
	}
	next, err := ops.ConvertToFolder(summary.Path)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "path": next, "title": database.TitleFromCSVPath(next), "changed": true})
		return nil
	}
	emitOK(fmt.Sprintf("Converted %s to %s", summary.Path, database.FormDirFromCSVPath(next)))
	return nil
}
