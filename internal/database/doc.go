package database

import (
	"strings"

	"github.com/ZenNotes/tui/internal/vault"
)

const (
	// FormDirSuffix marks a database folder.
	FormDirSuffix = ".base"
	// FormDataFile and FormSchemaFile are the fixed names inside it.
	FormDataFile   = "data.csv"
	FormSchemaFile = "schema.json"
	// EmptyGroup is the board column for rows whose group-by cell is empty.
	EmptyGroup = "__empty__"
)

// Field is one typed column.
type Field struct {
	ID      string
	Name    string
	Type    string
	Options []SelectOption
	Hidden  bool
	// OptionsSource, when set, discovers select options from notes instead
	// of the explicit list: every note, a folder's notes, or a tag's.
	OptionsSource *OptionsSource
	raw           *Object
}

// OptionsSource names where a select field's options come from.
type OptionsSource struct {
	Kind string // notes, folder, tag
	Path string // folder: vault-relative directory
	Tag  string // tag: without the #
}

// Describe is the source's display text.
func (s *OptionsSource) Describe() string {
	if s == nil {
		return "manual"
	}
	switch s.Kind {
	case "folder":
		return "notes in " + s.Path
	case "tag":
		return "notes tagged #" + s.Tag
	}
	return "all notes"
}

// SelectOption is one pickable value of a select field.
type SelectOption struct {
	ID    string
	Value string
	Label string
	Color string
}

// Row is one record; cells are raw CSV strings keyed by field id.
type Row struct {
	ID    string
	Cells map[string]string
}

// FilterRule is one view filter.
type FilterRule struct {
	FieldID string
	Op      string
	Value   string
}

// SortRule is one view sort.
type SortRule struct {
	FieldID   string
	Direction string
}

// View is a saved table or board configuration.
type View struct {
	ID                string
	Name              string
	Type              string
	Filters           []FilterRule
	FilterConjunction string
	Sorts             []SortRule
	ColumnOrder       []string
	HiddenFieldIDs    []string
	GroupByFieldID    string
	BoardColumnOrder  []string
	// CardFieldIDs lists the fields a board card shows under its title;
	// empty means every field.
	CardFieldIDs []string
}

// Doc is a fully hydrated database.
type Doc struct {
	// Path is the vault-relative `data.csv` path: the database's identity.
	Path  string
	Title string
	// Sidecar is the normalized schema.json with pages made vault-relative.
	Sidecar      *Object
	IDFieldID    string
	Fields       []Field
	Views        []View
	ActiveViewID string
	// Pages maps row id to the record page's vault-relative path.
	Pages map[string]string
	Rows  []Row
	// PageHasContent says whether a row's page has body beyond its heading.
	PageHasContent map[string]bool
}

// Summary is a listing entry.
type Summary struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

func toPosix(p string) string { return strings.ReplaceAll(p, "\\", "/") }

func lastSegment(p string) string {
	s := toPosix(p)
	return s[strings.LastIndex(s, "/")+1:]
}

// IsFormDirName is true for a `<Name>.base` folder name or path.
func IsFormDirName(nameOrPath string) bool {
	return strings.HasSuffix(strings.ToLower(lastSegment(nameOrPath)), FormDirSuffix)
}

// FormDirFromCSVPath is the database folder for a `data.csv` path, or "".
func FormDirFromCSVPath(csvPath string) string {
	p := toPosix(csvPath)
	slash := strings.LastIndex(p, "/")
	if slash < 0 {
		return ""
	}
	dir, file := p[:slash], p[slash+1:]
	if strings.ToLower(file) != FormDataFile || !IsFormDirName(dir) {
		return ""
	}
	return dir
}

// CSVPathForFormDir is `<dir>/data.csv`.
func CSVPathForFormDir(formDir string) string {
	return toPosix(formDir) + "/" + FormDataFile
}

// SidecarSuffix follows a loose `.csv` to name its schema file:
// `Books.csv` keeps its schema in `Books.csv.base.json`.
const SidecarSuffix = ".base.json"

// SchemaPathFor is `<dir>/schema.json` for a `.base` folder's data.csv,
// `<file>.csv.base.json` for a loose CSV, or "" for anything else.
func SchemaPathFor(csvPath string) string {
	if dir := FormDirFromCSVPath(csvPath); dir != "" {
		return dir + "/" + FormSchemaFile
	}
	if IsLooseCSVPath(csvPath) {
		return toPosix(csvPath) + SidecarSuffix
	}
	return ""
}

// IsSidecarPath is true for a loose CSV's schema file.
func IsSidecarPath(rel string) bool {
	return strings.HasSuffix(strings.ToLower(toPosix(rel)), ".csv"+SidecarSuffix)
}

// IsLooseCSVPath is true for a `.csv` outside any `.base` folder.
func IsLooseCSVPath(rel string) bool {
	p := strings.ToLower(toPosix(rel))
	if !strings.HasSuffix(p, ".csv") || IsSidecarPath(p) {
		return false
	}
	return FormDirContaining(rel) == ""
}

// HasRecordPages is true when a database can own record pages: only
// `.base` folders have a pages directory.
func HasRecordPages(csvPath string) bool {
	return FormDirFromCSVPath(csvPath) != ""
}

// FormDirContaining is the `.base` folder a path lives in, or "".
func FormDirContaining(relPath string) string {
	parts := strings.Split(toPosix(relPath), "/")
	for i, part := range parts {
		if strings.HasSuffix(strings.ToLower(part), FormDirSuffix) {
			return strings.Join(parts[:i+1], "/")
		}
	}
	return ""
}

// TitleFromDir is the display title of a database folder.
func TitleFromDir(nameOrPath string) string {
	name := lastSegment(nameOrPath)
	if strings.HasSuffix(strings.ToLower(name), FormDirSuffix) {
		return name[:len(name)-len(FormDirSuffix)]
	}
	return name
}

// TitleFromCSVPath is the display title from a data.csv path.
func TitleFromCSVPath(csvPath string) string {
	if dir := FormDirFromCSVPath(csvPath); dir != "" {
		return TitleFromDir(dir)
	}
	base := lastSegment(csvPath)
	return strings.TrimSuffix(strings.TrimSuffix(base, ".csv"), ".CSV")
}

// FieldByID finds a field.
func (d *Doc) FieldByID(id string) *Field {
	for i := range d.Fields {
		if d.Fields[i].ID == id {
			return &d.Fields[i]
		}
	}
	return nil
}

// TitleFieldID is the first non-id field: the record title column.
func (d *Doc) TitleFieldID() string {
	for _, f := range d.Fields {
		if f.ID != d.IDFieldID {
			return f.ID
		}
	}
	return ""
}

// RecordTitle is a row's display title: the title field's value or Untitled.
func (d *Doc) RecordTitle(row Row) string {
	if id := d.TitleFieldID(); id != "" {
		if v := strings.TrimSpace(row.Cells[id]); v != "" {
			return v
		}
	}
	return "Untitled"
}

// ComposePageBody composes a record page: the row's properties as flat YAML
// frontmatter (id and title fields omitted) followed by body.
func (d *Doc) ComposePageBody(row Row, body string) string {
	titleID := d.TitleFieldID()
	lines := []string{"---"}
	for _, f := range d.Fields {
		if f.ID == d.IDFieldID || f.ID == titleID {
			continue
		}
		v := row.Cells[f.ID]
		if v != "" {
			lines = append(lines, f.Name+": "+vault.YAMLValue(v))
		} else {
			lines = append(lines, f.Name+":")
		}
	}
	lines = append(lines, "---")
	return strings.Join(lines, "\n") + "\n" + strings.TrimLeft(body, "\n")
}

// SetCell writes one cell.
func (d *Doc) SetCell(rowID, fieldID, value string) {
	for i := range d.Rows {
		if d.Rows[i].ID == rowID {
			if d.Rows[i].Cells == nil {
				d.Rows[i].Cells = map[string]string{}
			}
			d.Rows[i].Cells[fieldID] = value
		}
	}
}

// AddRow appends an empty row and returns it.
func (d *Doc) AddRow() Row {
	id := GenID()
	cells := map[string]string{}
	for _, f := range d.Fields {
		cells[f.ID] = ""
	}
	cells[d.IDFieldID] = id
	row := Row{ID: id, Cells: cells}
	d.Rows = append(d.Rows, row)
	return row
}

// DeleteRow removes a row.
func (d *Doc) DeleteRow(rowID string) {
	out := d.Rows[:0]
	for _, r := range d.Rows {
		if r.ID != rowID {
			out = append(out, r)
		}
	}
	d.Rows = out
}

// RowByID finds a row.
func (d *Doc) RowByID(id string) *Row {
	for i := range d.Rows {
		if d.Rows[i].ID == id {
			return &d.Rows[i]
		}
	}
	return nil
}

// EnsureSelectOption mints an option for a select field when the value is
// new. Option values may not contain commas (the multiSelect separator).
// Returns true when the schema changed.
func (d *Doc) EnsureSelectOption(fieldID, rawValue string) bool {
	value := strings.ReplaceAll(strings.TrimSpace(rawValue), ",", " ")
	if value == "" {
		return false
	}
	field := d.FieldByID(fieldID)
	if field == nil {
		return false
	}
	for _, o := range field.Options {
		if o.Value == value {
			return false
		}
	}
	opt := SelectOption{ID: GenID(), Value: value}
	field.Options = append(field.Options, opt)
	if field.raw != nil {
		options := field.raw.Array("options")
		entry := NewObject()
		entry.Set("id", opt.ID)
		entry.Set("value", opt.Value)
		field.raw.Set("options", append(options, entry))
	}
	return true
}

// SplitMultiSelect splits a multiSelect cell ("a, b") into values.
func SplitMultiSelect(cell string) []string {
	out := []string{}
	for _, s := range strings.Split(cell, ",") {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// JoinMultiSelect composes a multiSelect cell.
func JoinMultiSelect(values []string) string {
	return strings.Join(values, ", ")
}

// IsCheckboxTrue reads a checkbox cell.
func IsCheckboxTrue(cell string) bool {
	return boolTrue[strings.ToLower(strings.TrimSpace(cell))]
}
