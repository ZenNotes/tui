package database

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

// Layout is the vault layout facts path composition depends on.
type Layout struct {
	PrimaryNotesAtRoot bool
	SystemFolderPaths  map[string]string
}

// FileOps is the generic vault-file IO a transport provides. ReadFileTextOrNull
// must return nil for an ABSENT file and an error for anything else: nil is
// read as "no schema yet" and a schema is then inferred and written.
type FileOps interface {
	ReadFileTextOrNull(rel string) (*string, error)
	WriteFile(rel, text string) error
	CreateFolder(folder vault.NoteFolder, subpath string) error
	RenameFolder(folder vault.NoteFolder, oldSubpath, newSubpath string) (string, error)
	// ListFolders must include `.base` folders.
	ListFolders() ([]vault.FolderEntry, error)
	VaultLayout() (Layout, error)
}

// CSVLister is implemented by backends that can find loose `.csv` files;
// without it only `.base` folders are databases.
type CSVLister interface {
	ListCSVFiles() ([]string, error)
}

// FileMover is implemented by backends that can rename a file in place,
// which loose-CSV renames and conversions need.
type FileMover interface {
	RenameFile(oldRel, newRel string) error
}

// Ops composes database operations over FileOps.
type Ops struct {
	io FileOps
}

// NewOps binds the composition to a transport.
func NewOps(io FileOps) *Ops {
	return &Ops{io: io}
}

const schemaSampleRows = 50

var dbTitleBadRe = regexp.MustCompile(`[\\/:*?"<>|]`)

func joinSub(parts ...string) string {
	out := []string{}
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// vaultRelDir mirrors the server's folderRoot, remap included.
func vaultRelDir(folder vault.NoteFolder, subpath string, layout Layout) string {
	sub := strings.Trim(subpath, "/")
	if folder == vault.FolderInbox && layout.PrimaryNotesAtRoot {
		return sub
	}
	return joinSub(vault.ResolveFolderPath(folder, layout.SystemFolderPaths), sub)
}

func splitVaultPath(rel string, layout Layout) (vault.NoteFolder, string) {
	parts := []string{}
	for _, p := range strings.Split(toPosix(rel), "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	first := ""
	if len(parts) > 0 {
		first = parts[0]
	}
	if system, ok := vault.SystemFolderForDirName(first, layout.SystemFolderPaths); ok {
		if system != vault.FolderInbox || !layout.PrimaryNotesAtRoot {
			return system, strings.Join(parts[1:], "/")
		}
	}
	return vault.FolderInbox, strings.Join(parts, "/")
}

// noteHasBody is true when a page has content beyond frontmatter and a
// single title heading.
var leadingHeadingRe = regexp.MustCompile(`^\s*#[^\n]*\r?\n?`)

func noteHasBody(text string) bool {
	_, body, _ := vault.Frontmatter(text)
	body = leadingHeadingRe.ReplaceAllString(body, "")
	return strings.TrimSpace(body) != ""
}

func fieldFromObject(o *Object) (Field, bool) {
	if o == nil {
		return Field{}, false
	}
	id, idOK := o.values["id"].(string)
	name, nameOK := o.values["name"].(string)
	if !idOK || !nameOK {
		return Field{}, false
	}
	f := Field{ID: id, Name: name, Type: o.String("type"), Hidden: o.Bool("hidden"), raw: o}
	if f.Type == "" {
		f.Type = "text"
	}
	if src := o.Object("optionsSource"); src != nil {
		switch src.String("kind") {
		case "notes":
			f.OptionsSource = &OptionsSource{Kind: "notes"}
		case "folder":
			f.OptionsSource = &OptionsSource{Kind: "folder", Path: src.String("path")}
		case "tag":
			f.OptionsSource = &OptionsSource{Kind: "tag", Tag: src.String("tag")}
		}
	}
	for _, entry := range o.Array("options") {
		opt, ok := entry.(*Object)
		if !ok {
			continue
		}
		f.Options = append(f.Options, SelectOption{
			ID:    opt.String("id"),
			Value: opt.String("value"),
			Label: opt.String("label"),
			Color: opt.String("color"),
		})
	}
	return f, true
}

func stringsFromArray(arr []any) []string {
	out := []string{}
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func viewFromObject(o *Object) (View, bool) {
	if o == nil {
		return View{}, false
	}
	id, ok := o.values["id"].(string)
	if !ok {
		return View{}, false
	}
	kind := o.String("type")
	if kind != "table" && kind != "board" {
		return View{}, false
	}
	v := View{
		ID:                id,
		Name:              o.String("name"),
		Type:              kind,
		FilterConjunction: o.String("filterConjunction"),
		ColumnOrder:       stringsFromArray(o.Array("columnOrder")),
		HiddenFieldIDs:    stringsFromArray(o.Array("hiddenFieldIds")),
		GroupByFieldID:    o.String("groupByFieldId"),
		BoardColumnOrder:  stringsFromArray(o.Array("boardColumnOrder")),
		CardFieldIDs:      stringsFromArray(o.Array("cardFieldIds")),
	}
	for _, entry := range o.Array("filters") {
		f, ok := entry.(*Object)
		if !ok {
			continue
		}
		v.Filters = append(v.Filters, FilterRule{FieldID: f.String("fieldId"), Op: f.String("op"), Value: f.String("value")})
	}
	for _, entry := range o.Array("sorts") {
		s, ok := entry.(*Object)
		if !ok {
			continue
		}
		v.Sorts = append(v.Sorts, SortRule{FieldID: s.String("fieldId"), Direction: s.String("direction")})
	}
	return v, true
}

// normalizeSidecar is the defensive parse of a user-editable schema.json.
// It returns the ordered sidecar to write back plus the typed view of it.
func normalizeSidecar(raw *Object) (*Object, string, []Field, []View, string, map[string]string, bool) {
	if raw == nil {
		return nil, "", nil, nil, "", nil, false
	}
	fieldObjs := raw.Array("fields")
	if len(fieldObjs) == 0 {
		return nil, "", nil, nil, "", nil, false
	}
	fields := []Field{}
	for _, entry := range fieldObjs {
		o, ok := entry.(*Object)
		if !ok {
			return nil, "", nil, nil, "", nil, false
		}
		f, ok := fieldFromObject(o)
		if !ok {
			return nil, "", nil, nil, "", nil, false
		}
		fields = append(fields, f)
	}
	idFieldID := raw.String("idFieldId")
	valid := false
	for _, f := range fields {
		if f.ID == idFieldID {
			valid = true
			break
		}
	}
	if !valid {
		idFieldID = fields[0].ID
	}
	viewObjs := []any{}
	views := []View{}
	for _, entry := range raw.Array("views") {
		o, ok := entry.(*Object)
		if !ok {
			continue
		}
		if v, ok := viewFromObject(o); ok {
			views = append(views, v)
			viewObjs = append(viewObjs, o)
		}
	}
	if len(views) == 0 {
		view, id := DefaultView(fields)
		v, _ := viewFromObject(view)
		views = []View{v}
		viewObjs = []any{view}
		_ = id
	}
	activeViewID := raw.String("activeViewId")
	valid = false
	for _, v := range views {
		if v.ID == activeViewID {
			valid = true
			break
		}
	}
	if !valid {
		activeViewID = views[0].ID
	}
	var pages map[string]string
	pagesObj := NewObject()
	if p := raw.Object("pages"); p != nil {
		pages = map[string]string{}
		for _, k := range p.keys {
			if s, ok := p.values[k].(string); ok {
				pages[k] = s
				pagesObj.Set(k, s)
			}
		}
	}
	out := NewObject()
	out.Set("version", json.Number("1"))
	out.Set("idFieldId", idFieldID)
	out.Set("fields", fieldObjs)
	out.Set("views", viewObjs)
	out.Set("activeViewId", activeViewID)
	if pages != nil {
		out.Set("pages", pagesObj)
	}
	return out, idFieldID, fields, views, activeViewID, pages, true
}

func pagesToFull(csvRel string, pages map[string]string) map[string]string {
	formDir := FormDirFromCSVPath(csvRel)
	if formDir == "" || pages == nil {
		return pages
	}
	prefix := formDir + "/"
	out := map[string]string{}
	for id, p := range pages {
		if strings.HasPrefix(p, prefix) {
			out[id] = p
		} else {
			out[id] = prefix + p
		}
	}
	return out
}

func pagesToRelative(csvRel string, pages *Object) *Object {
	formDir := FormDirFromCSVPath(csvRel)
	if formDir == "" || pages == nil {
		return pages
	}
	prefix := formDir + "/"
	out := NewObject()
	for _, k := range pages.keys {
		p, _ := pages.values[k].(string)
		out.Set(k, strings.TrimPrefix(p, prefix))
	}
	return out
}

func (o *Ops) readSidecar(csvPath string) (*Doc, bool, error) {
	schemaPath := SchemaPathFor(csvPath)
	if schemaPath == "" {
		return nil, false, nil
	}
	raw, err := o.io.ReadFileTextOrNull(schemaPath)
	if err != nil {
		return nil, false, err
	}
	if raw == nil {
		return nil, false, nil
	}
	parsed, err := ParseObject(*raw)
	if err != nil {
		return nil, false, nil
	}
	sidecar, idFieldID, fields, views, activeViewID, pages, ok := normalizeSidecar(parsed)
	if !ok {
		return nil, false, nil
	}
	doc := &Doc{
		Path:         toPosix(csvPath),
		Title:        TitleFromCSVPath(csvPath),
		Sidecar:      sidecar,
		IDFieldID:    idFieldID,
		Fields:       fields,
		Views:        views,
		ActiveViewID: activeViewID,
		Pages:        pagesToFull(toPosix(csvPath), pages),
	}
	return doc, true, nil
}

func (o *Ops) persistSidecar(csvPath string, sidecar *Object) error {
	schemaPath := SchemaPathFor(csvPath)
	if schemaPath == "" {
		return fmt.Errorf("Not a database folder: %s", csvPath)
	}
	onDisk := sidecar.Clone()
	if pages := onDisk.Object("pages"); pages != nil {
		onDisk.Set("pages", pagesToRelative(toPosix(csvPath), pages))
	}
	text, err := Stringify(onDisk)
	if err != nil {
		return err
	}
	return o.io.WriteFile(schemaPath, text+"\n")
}

func (o *Ops) readPageFlags(pages map[string]string) map[string]bool {
	if len(pages) == 0 {
		return nil
	}
	flags := map[string]bool{}
	for rowID, notePath := range pages {
		text, err := o.io.ReadFileTextOrNull(notePath)
		if err == nil && text != nil {
			flags[rowID] = noteHasBody(*text)
		}
	}
	return flags
}

// OpenDatabase hydrates a database. A CSV without a sidecar is adopted: its
// schema is inferred and materialized, and its rows re-written with ids.
func (o *Ops) OpenDatabase(csvPath string) (*Doc, error) {
	rel := toPosix(csvPath)
	csvText, err := o.io.ReadFileTextOrNull(rel)
	if err != nil {
		return nil, err
	}
	if csvText == nil {
		return nil, fmt.Errorf("Database not found: %s", rel)
	}
	existing, ok, err := o.readSidecar(rel)
	if err != nil {
		return nil, err
	}
	if ok {
		existing.Rows = ParseRows(*csvText, existing.Fields, existing.IDFieldID)
		existing.PageHasContent = o.readPageFlags(existing.Pages)
		return existing, nil
	}
	grid := ParseCSV(*csvText)
	headers := []string{}
	if len(grid) > 0 {
		headers = grid[0]
	}
	samples := [][]string{}
	if len(grid) > 1 {
		samples = grid[1:min(len(grid), 1+schemaSampleRows)]
	}
	idFieldID, fields := InferFields(headers, samples)
	view, activeViewID := DefaultView(fields)
	sidecar := NewObject()
	sidecar.Set("version", json.Number("1"))
	sidecar.Set("idFieldId", idFieldID)
	fieldObjs := make([]any, len(fields))
	for i, f := range fields {
		fieldObjs[i] = f.raw
	}
	sidecar.Set("fields", fieldObjs)
	sidecar.Set("views", []any{view})
	sidecar.Set("activeViewId", activeViewID)
	rows := ParseRows(*csvText, fields, idFieldID)
	if err := o.persistSidecar(rel, sidecar); err != nil {
		return nil, err
	}
	if err := o.io.WriteFile(rel, SerializeRows(rows, fields)); err != nil {
		return nil, err
	}
	v, _ := viewFromObject(view)
	return &Doc{
		Path:         rel,
		Title:        TitleFromCSVPath(rel),
		Sidecar:      sidecar,
		IDFieldID:    idFieldID,
		Fields:       fields,
		Views:        []View{v},
		ActiveViewID: activeViewID,
		Rows:         rows,
	}, nil
}

// WriteRows persists rows through the cheaper rows-only route.
func (o *Ops) WriteRows(csvPath string, rows []Row) (*Doc, error) {
	rel := toPosix(csvPath)
	doc, ok, err := o.readSidecar(rel)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("Database sidecar missing: %s", rel)
	}
	if err := o.io.WriteFile(rel, SerializeRows(rows, doc.Fields)); err != nil {
		return nil, err
	}
	doc.Rows = rows
	return doc, nil
}

// WriteSchema persists a sidecar and rows together, for edits that mint
// options or change the pages map.
func (o *Ops) WriteSchema(csvPath string, doc *Doc) (*Doc, error) {
	rel := toPosix(csvPath)
	sidecar := doc.Sidecar.Clone()
	if doc.Pages != nil {
		pages := NewObject()
		ids := make([]string, 0, len(doc.Pages))
		for id := range doc.Pages {
			ids = append(ids, id)
		}
		if existing := doc.Sidecar.Object("pages"); existing != nil {
			ordered := []string{}
			seen := map[string]bool{}
			for _, k := range existing.keys {
				if _, ok := doc.Pages[k]; ok {
					ordered = append(ordered, k)
					seen[k] = true
				}
			}
			sort.Strings(ids)
			for _, id := range ids {
				if !seen[id] {
					ordered = append(ordered, id)
				}
			}
			ids = ordered
		} else {
			sort.Strings(ids)
		}
		for _, id := range ids {
			pages.Set(id, doc.Pages[id])
		}
		sidecar.Set("pages", pages)
	}
	normalized, idFieldID, fields, views, activeViewID, pages, ok := normalizeSidecar(sidecar)
	if !ok {
		return nil, fmt.Errorf("Invalid database schema: %s", rel)
	}
	if err := o.persistSidecar(rel, normalized); err != nil {
		return nil, err
	}
	if err := o.io.WriteFile(rel, SerializeRows(doc.Rows, fields)); err != nil {
		return nil, err
	}
	return &Doc{
		Path:         rel,
		Title:        TitleFromCSVPath(rel),
		Sidecar:      normalized,
		IDFieldID:    idFieldID,
		Fields:       fields,
		Views:        views,
		ActiveViewID: activeViewID,
		Pages:        pagesToFull(rel, pages),
		Rows:         doc.Rows,
	}, nil
}

// CreateDatabase creates an empty database with an id and a Name field.
func (o *Ops) CreateDatabase(folder vault.NoteFolder, subpath, title string) (*Doc, error) {
	layout, err := o.io.VaultLayout()
	if err != nil {
		return nil, err
	}
	baseTitle := strings.TrimSpace(title)
	if baseTitle == "" {
		baseTitle = "Untitled Database"
	}
	baseName := dbTitleBadRe.ReplaceAllString(baseTitle, "-")
	dirRel := vaultRelDir(folder, subpath, layout)
	csvFor := func(name string) string {
		return CSVPathForFormDir(joinSub(dirRel, name+FormDirSuffix))
	}
	name := baseName
	for n := 2; ; n++ {
		existing, err := o.io.ReadFileTextOrNull(csvFor(name))
		if err != nil {
			return nil, err
		}
		if existing == nil {
			break
		}
		name = baseName + " " + strconv.Itoa(n)
	}
	csvPath := csvFor(name)
	folderSub := joinSub(subpath, name+FormDirSuffix)

	idField := Field{ID: GenID(), Name: "id", Type: "text", Hidden: true}
	nameField := Field{ID: GenID(), Name: "Name", Type: "text"}
	idField.raw = fieldObject(idField)
	nameField.raw = fieldObject(nameField)
	fields := []Field{idField, nameField}
	view, activeViewID := DefaultView(fields)
	sidecar := NewObject()
	sidecar.Set("version", json.Number("1"))
	sidecar.Set("idFieldId", idField.ID)
	sidecar.Set("fields", []any{idField.raw, nameField.raw})
	sidecar.Set("views", []any{view})
	sidecar.Set("activeViewId", activeViewID)

	if err := o.io.CreateFolder(folder, folderSub); err != nil {
		return nil, err
	}
	if err := o.persistSidecar(csvPath, sidecar); err != nil {
		return nil, err
	}
	if err := o.io.WriteFile(csvPath, SerializeRows(nil, fields)); err != nil {
		return nil, err
	}
	v, _ := viewFromObject(view)
	return &Doc{
		Path:         csvPath,
		Title:        TitleFromCSVPath(csvPath),
		Sidecar:      sidecar,
		IDFieldID:    idField.ID,
		Fields:       fields,
		Views:        []View{v},
		ActiveViewID: activeViewID,
		Rows:         []Row{},
	}, nil
}

// renameLooseCSV renames a loose CSV and its sidecar together.
func (o *Ops) renameLooseCSV(rel, newTitle string) (string, error) {
	mover, ok := o.io.(FileMover)
	if !ok {
		return "", fmt.Errorf("renaming a loose .csv needs a local vault")
	}
	title := strings.TrimSpace(strings.TrimSuffix(newTitle, ".csv"))
	if title == "" || strings.ContainsAny(title, "/\\") {
		return "", fmt.Errorf("invalid database name")
	}
	dir := ""
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dir = rel[:i+1]
	}
	next := dir + title + ".csv"
	if next == rel {
		return rel, nil
	}
	if existing, _ := o.io.ReadFileTextOrNull(next); existing != nil {
		return "", fmt.Errorf("%s already exists", next)
	}
	if err := mover.RenameFile(rel, next); err != nil {
		return "", err
	}
	if sidecar, _ := o.io.ReadFileTextOrNull(rel + SidecarSuffix); sidecar != nil {
		if err := mover.RenameFile(rel+SidecarSuffix, next+SidecarSuffix); err != nil {
			return next, err
		}
	}
	return next, nil
}

// ConvertToFolder turns a loose CSV into a `<Name>.base` folder database,
// which is what record pages need. It returns the new data.csv path.
func (o *Ops) ConvertToFolder(csvPath string) (string, error) {
	rel := toPosix(csvPath)
	if !IsLooseCSVPath(rel) {
		return rel, nil
	}
	mover, ok := o.io.(FileMover)
	if !ok {
		return "", fmt.Errorf("converting a loose .csv needs a local vault")
	}
	// The folder goes exactly where the file was; the rename creates it.
	dir := ""
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dir = rel[:i]
	}
	formDir := TitleFromCSVPath(rel) + FormDirSuffix
	if dir != "" {
		formDir = dir + "/" + formDir
	}
	if existing, _ := o.io.ReadFileTextOrNull(CSVPathForFormDir(formDir)); existing != nil {
		return "", fmt.Errorf("%s already exists", formDir)
	}
	next := CSVPathForFormDir(formDir)
	if err := mover.RenameFile(rel, next); err != nil {
		return "", err
	}
	if sidecar, _ := o.io.ReadFileTextOrNull(rel + SidecarSuffix); sidecar != nil {
		if err := mover.RenameFile(rel+SidecarSuffix, SchemaPathFor(next)); err != nil {
			return next, err
		}
	}
	return next, nil
}

// CreateRecordPage writes a record page note inside the database folder and
// returns its vault-relative path.
func (o *Ops) CreateRecordPage(csvPath, title, body string) (string, error) {
	formDir := FormDirFromCSVPath(toPosix(csvPath))
	if formDir == "" {
		return "", fmt.Errorf("Not a database folder: %s", csvPath)
	}
	safe := strings.TrimSpace(title)
	if safe == "" {
		safe = "Untitled"
	}
	safe = strings.NewReplacer("\\", "-", "/", "-").Replace(safe)
	finalTitle := safe
	for n := 2; ; n++ {
		existing, err := o.io.ReadFileTextOrNull(formDir + "/" + finalTitle + ".md")
		if err != nil {
			return "", err
		}
		if existing == nil {
			break
		}
		finalTitle = safe + " " + strconv.Itoa(n)
	}
	noteRel := formDir + "/" + finalTitle + ".md"
	if err := o.io.WriteFile(noteRel, body); err != nil {
		return "", err
	}
	return noteRel, nil
}

// RenameDatabase renames the `.base` folder and returns the new csv path.
func (o *Ops) RenameDatabase(csvPath, newTitle string) (string, error) {
	oldFormDir := FormDirFromCSVPath(toPosix(csvPath))
	if oldFormDir == "" && IsLooseCSVPath(csvPath) {
		return o.renameLooseCSV(toPosix(csvPath), newTitle)
	}
	if oldFormDir == "" {
		return "", fmt.Errorf("Not a database folder: %s", csvPath)
	}
	parentRel := ""
	if i := strings.LastIndex(oldFormDir, "/"); i >= 0 {
		parentRel = oldFormDir[:i]
	}
	safeName := strings.TrimSpace(newTitle)
	if safeName == "" {
		safeName = "Untitled Database"
	}
	safeName = dbTitleBadRe.ReplaceAllString(safeName, "-")
	makeFormDir := func(name string) string {
		if parentRel != "" {
			return parentRel + "/" + name + FormDirSuffix
		}
		return name + FormDirSuffix
	}
	target := makeFormDir(safeName)
	if target == oldFormDir {
		return csvPath, nil
	}
	for n := 2; ; n++ {
		existing, err := o.io.ReadFileTextOrNull(CSVPathForFormDir(target))
		if err != nil {
			return "", err
		}
		if existing == nil {
			break
		}
		target = makeFormDir(safeName + " " + strconv.Itoa(n))
	}
	layout, err := o.io.VaultLayout()
	if err != nil {
		return "", err
	}
	folder, oldSub := splitVaultPath(oldFormDir, layout)
	_, newSub := splitVaultPath(target, layout)
	if _, err := o.io.RenameFolder(folder, oldSub, newSub); err != nil {
		return "", err
	}
	return CSVPathForFormDir(target), nil
}

// ListDatabases finds every `.base` folder.
func (o *Ops) ListDatabases() ([]Summary, error) {
	folders, err := o.io.ListFolders()
	if err != nil {
		return nil, err
	}
	layout, err := o.io.VaultLayout()
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, f := range folders {
		if !IsFormDirName(f.Subpath) {
			continue
		}
		csv := CSVPathForFormDir(vaultRelDir(f.Folder, f.Subpath, layout))
		out = append(out, Summary{Path: csv, Title: TitleFromCSVPath(csv)})
	}
	if lister, ok := o.io.(CSVLister); ok {
		files, err := lister.ListCSVFiles()
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, s := range out {
			seen[s.Path] = true
		}
		for _, rel := range files {
			rel = toPosix(rel)
			if seen[rel] {
				continue
			}
			if IsLooseCSVPath(rel) || FormDirFromCSVPath(rel) != "" {
				seen[rel] = true
				out = append(out, Summary{Path: rel, Title: TitleFromCSVPath(rel)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, nil
}
