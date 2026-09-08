package database

import (
	"fmt"
	"regexp"
	"strings"
)

// Schema and view edits. Every mutation rewrites the ordered sidecar
// object, which is what persists, then re-reads the typed view of it, so
// Doc.Fields and Doc.Views never drift from what will be written. The
// rules mirror the desktop's database-cells.ts.

// FieldTypes lists the field types in the order the desktop offers them.
var FieldTypes = []string{"text", "number", "checkbox", "date", "select", "multiSelect", "note", "noteMulti"}

// FieldTypeLabel is the display name of a field type.
func FieldTypeLabel(t string) string {
	switch t {
	case "number":
		return "Number"
	case "checkbox":
		return "Checkbox"
	case "date":
		return "Date"
	case "select":
		return "Select"
	case "multiSelect":
		return "Multi-select"
	case "note":
		return "Note link"
	case "noteMulti":
		return "Note links"
	}
	return "Text"
}

// IsSelectType is true for the two option-backed types.
func IsSelectType(t string) bool { return t == "select" || t == "multiSelect" }

// IsNoteType is true for the two wikilink-backed types.
func IsNoteType(t string) bool { return t == "note" || t == "noteMulti" }

var noteLinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)

// SplitNoteLinks extracts the wikilink targets of a note cell, in order.
func SplitNoteLinks(cell string) []string {
	out := []string{}
	for _, m := range noteLinkRe.FindAllStringSubmatch(cell, -1) {
		if t := strings.TrimSpace(m[1]); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// JoinNoteLinks composes a note cell from targets: `[[A]] [[B]]`.
func JoinNoteLinks(targets []string) string {
	parts := make([]string, 0, len(targets))
	for _, t := range targets {
		parts = append(parts, "[["+t+"]]")
	}
	return strings.Join(parts, " ")
}

// Resync refreshes the typed fields and views from the sidecar object.
func (d *Doc) Resync() error {
	normalized, idFieldID, fields, views, activeViewID, pages, ok := normalizeSidecar(d.Sidecar)
	if !ok {
		return fmt.Errorf("invalid database schema")
	}
	d.Sidecar = normalized
	d.IDFieldID = idFieldID
	d.Fields = fields
	d.Views = views
	d.ActiveViewID = activeViewID
	if pages != nil {
		d.Pages = pagesToFull(d.Path, pages)
	}
	return nil
}

func (d *Doc) fieldObject(id string) *Object {
	for _, entry := range d.Sidecar.Array("fields") {
		if o, ok := entry.(*Object); ok && o.String("id") == id {
			return o
		}
	}
	return nil
}

func (d *Doc) viewObject(id string) *Object {
	for _, entry := range d.Sidecar.Array("views") {
		if o, ok := entry.(*Object); ok && o.String("id") == id {
			return o
		}
	}
	return nil
}

// ViewByID finds a typed view.
func (d *Doc) ViewByID(id string) *View {
	for i := range d.Views {
		if d.Views[i].ID == id {
			return &d.Views[i]
		}
	}
	return nil
}

// ActiveView is the view the sidecar marks active, else the first.
func (d *Doc) ActiveView() *View {
	if v := d.ViewByID(d.ActiveViewID); v != nil {
		return v
	}
	if len(d.Views) > 0 {
		return &d.Views[0]
	}
	return nil
}

func anyStrings(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// uniqueFieldName appends " 2", " 3"… when a name is taken.
func (d *Doc) uniqueFieldName(base string, ignoreID string) string {
	taken := map[string]bool{}
	for _, f := range d.Fields {
		if f.ID != ignoreID {
			taken[f.Name] = true
		}
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s %d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// AddField appends a field and shows it in every table view.
func (d *Doc) AddField(name, typ string) (Field, error) {
	if strings.TrimSpace(name) == "" {
		name = "New field"
	}
	if !validFieldType(typ) {
		typ = "text"
	}
	f := NewObject()
	f.Set("id", GenID())
	f.Set("name", d.uniqueFieldName(strings.TrimSpace(name), ""))
	f.Set("type", typ)
	if IsSelectType(typ) {
		f.Set("options", []any{})
	}
	d.Sidecar.Set("fields", append(d.Sidecar.Array("fields"), f))
	id := f.String("id")
	for _, entry := range d.Sidecar.Array("views") {
		v, ok := entry.(*Object)
		if !ok || v.String("type") != "table" {
			continue
		}
		order := stringsFromArray(v.Array("columnOrder"))
		if len(order) == 0 {
			for _, fld := range d.Fields {
				order = append(order, fld.ID)
			}
		}
		v.Set("columnOrder", anyStrings(append(order, id)))
	}
	if err := d.Resync(); err != nil {
		return Field{}, err
	}
	for _, r := range d.Rows {
		if r.Cells != nil {
			r.Cells[id] = ""
		}
	}
	if fld := d.FieldByID(id); fld != nil {
		return *fld, nil
	}
	return Field{}, fmt.Errorf("field was not added")
}

func validFieldType(t string) bool {
	for _, ft := range FieldTypes {
		if ft == t {
			return true
		}
	}
	return false
}

// RenameField changes the CSV header of a field.
func (d *Doc) RenameField(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a field needs a name")
	}
	o := d.fieldObject(id)
	if o == nil {
		return fmt.Errorf("unknown field")
	}
	o.Set("name", d.uniqueFieldName(name, id))
	return d.Resync()
}

// RetypeField changes a field's type, keeping the raw cell values.
func (d *Doc) RetypeField(id, typ string) error {
	if !validFieldType(typ) {
		return fmt.Errorf("unknown field type %q", typ)
	}
	o := d.fieldObject(id)
	if o == nil {
		return fmt.Errorf("unknown field")
	}
	o.Set("type", typ)
	if IsSelectType(typ) {
		if _, ok := o.Get("options"); !ok {
			o.Set("options", []any{})
		}
	}
	if err := d.Resync(); err != nil {
		return err
	}
	// Becoming a select field keeps what the cells already say: every
	// distinct value turns into an option, in first-seen order.
	if IsSelectType(typ) {
		for _, r := range d.Rows {
			raw := r.Cells[id]
			values := []string{strings.TrimSpace(raw)}
			if typ == "multiSelect" {
				values = SplitMultiSelect(raw)
			}
			for _, val := range values {
				d.EnsureSelectOption(id, val)
			}
		}
	}
	return nil
}

// DeleteField removes a field everywhere it is referenced. The id field
// stays.
func (d *Doc) DeleteField(id string) error {
	if id == d.IDFieldID {
		return fmt.Errorf("the id field cannot be deleted")
	}
	kept := []any{}
	found := false
	for _, entry := range d.Sidecar.Array("fields") {
		if o, ok := entry.(*Object); ok && o.String("id") == id {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	if !found {
		return fmt.Errorf("unknown field")
	}
	d.Sidecar.Set("fields", kept)
	for _, entry := range d.Sidecar.Array("views") {
		v, ok := entry.(*Object)
		if !ok {
			continue
		}
		v.Set("columnOrder", anyStrings(without(stringsFromArray(v.Array("columnOrder")), id)))
		v.Set("hiddenFieldIds", anyStrings(without(stringsFromArray(v.Array("hiddenFieldIds")), id)))
		filtered := []any{}
		for _, fe := range v.Array("filters") {
			if fo, ok := fe.(*Object); ok && fo.String("fieldId") != id {
				filtered = append(filtered, fe)
			}
		}
		v.Set("filters", filtered)
		sorts := []any{}
		for _, se := range v.Array("sorts") {
			if so, ok := se.(*Object); ok && so.String("fieldId") != id {
				sorts = append(sorts, se)
			}
		}
		v.Set("sorts", sorts)
		if v.String("groupByFieldId") == id {
			v.Delete("groupByFieldId")
		}
	}
	for _, r := range d.Rows {
		delete(r.Cells, id)
	}
	return d.Resync()
}

func without(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

// columnOrderFor is a view's order normalized against the fields: missing
// ids appended, stale ids dropped.
func (d *Doc) columnOrderFor(v *Object) []string {
	ids := make([]string, 0, len(d.Fields))
	known := map[string]bool{}
	for _, f := range d.Fields {
		ids = append(ids, f.ID)
		known[f.ID] = true
	}
	stored := stringsFromArray(v.Array("columnOrder"))
	if len(stored) == 0 {
		stored = ids
	}
	order := []string{}
	seen := map[string]bool{}
	for _, id := range stored {
		if known[id] && !seen[id] {
			order = append(order, id)
			seen[id] = true
		}
	}
	for _, id := range ids {
		if !seen[id] {
			order = append(order, id)
		}
	}
	return order
}

// VisibleColumns lists a table view's fields in display order, minus the
// hidden ones and the id column.
func (d *Doc) VisibleColumns(viewID string) []Field {
	v := d.viewObject(viewID)
	if v == nil {
		out := []Field{}
		for _, f := range d.Fields {
			if f.ID != d.IDFieldID && !f.Hidden {
				out = append(out, f)
			}
		}
		return out
	}
	hidden := map[string]bool{}
	_, explicit := v.Get("hiddenFieldIds")
	for _, id := range stringsFromArray(v.Array("hiddenFieldIds")) {
		hidden[id] = true
	}
	out := []Field{}
	for _, id := range d.columnOrderFor(v) {
		if hidden[id] || id == d.IDFieldID {
			continue
		}
		f := d.FieldByID(id)
		if f == nil || (f.Hidden && !explicit) {
			continue
		}
		out = append(out, *f)
	}
	return out
}

// HiddenColumns lists the fields a table view hides (never the id field).
func (d *Doc) HiddenColumns(viewID string) []Field {
	v := d.viewObject(viewID)
	if v == nil {
		return nil
	}
	out := []Field{}
	for _, id := range stringsFromArray(v.Array("hiddenFieldIds")) {
		if id == d.IDFieldID {
			continue
		}
		if f := d.FieldByID(id); f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// MoveColumn shifts a column one visible step left or right in a table
// view; a no-op at the edges.
func (d *Doc) MoveColumn(viewID, fieldID, direction string) error {
	v := d.viewObject(viewID)
	if v == nil || v.String("type") != "table" {
		return nil
	}
	order := d.columnOrderFor(v)
	hidden := map[string]bool{}
	for _, id := range stringsFromArray(v.Array("hiddenFieldIds")) {
		hidden[id] = true
	}
	visible := []string{}
	for _, id := range order {
		if !hidden[id] {
			visible = append(visible, id)
		}
	}
	vi := indexOf(visible, fieldID)
	if vi < 0 {
		return nil
	}
	vj := vi + 1
	if direction == "left" {
		vj = vi - 1
	}
	if vj < 0 || vj >= len(visible) {
		return nil
	}
	oi, oj := indexOf(order, fieldID), indexOf(order, visible[vj])
	order[oi], order[oj] = order[oj], order[oi]
	v.Set("columnOrder", anyStrings(order))
	return d.Resync()
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// SetFieldHidden hides or shows a column in a table view.
func (d *Doc) SetFieldHidden(viewID, fieldID string, hidden bool) error {
	v := d.viewObject(viewID)
	if v == nil {
		return fmt.Errorf("unknown view")
	}
	ids := without(stringsFromArray(v.Array("hiddenFieldIds")), fieldID)
	if hidden {
		ids = append(ids, fieldID)
	}
	v.Set("hiddenFieldIds", anyStrings(ids))
	return d.Resync()
}

// SetViewSorts replaces a view's sort rules.
func (d *Doc) SetViewSorts(viewID string, sorts []SortRule) error {
	v := d.viewObject(viewID)
	if v == nil {
		return fmt.Errorf("unknown view")
	}
	list := []any{}
	for _, s := range sorts {
		o := NewObject()
		o.Set("fieldId", s.FieldID)
		o.Set("direction", s.Direction)
		list = append(list, o)
	}
	v.Set("sorts", list)
	return d.Resync()
}

// SetViewFilters replaces a view's filters and how they combine.
func (d *Doc) SetViewFilters(viewID string, filters []FilterRule, conjunction string) error {
	v := d.viewObject(viewID)
	if v == nil {
		return fmt.Errorf("unknown view")
	}
	list := []any{}
	for _, f := range filters {
		o := NewObject()
		o.Set("fieldId", f.FieldID)
		o.Set("op", f.Op)
		if f.Value != "" || FilterNeedsValue(f.Op) {
			o.Set("value", f.Value)
		}
		list = append(list, o)
	}
	v.Set("filters", list)
	if conjunction == "or" {
		v.Set("filterConjunction", "or")
	} else {
		v.Delete("filterConjunction")
	}
	return d.Resync()
}

// FilterOps lists the operators that make sense for a field type.
func FilterOps(fieldType string) []string {
	switch fieldType {
	case "number":
		return []string{"is", "isNot", "gt", "lt", "isEmpty", "isNotEmpty"}
	case "date":
		return []string{"is", "before", "after", "isEmpty", "isNotEmpty"}
	case "select", "multiSelect":
		return []string{"is", "isNot", "isEmpty", "isNotEmpty"}
	case "checkbox":
		return []string{"checked", "unchecked"}
	case "note", "noteMulti":
		return []string{"contains", "notContains", "isEmpty", "isNotEmpty"}
	}
	return []string{"is", "isNot", "contains", "notContains", "isEmpty", "isNotEmpty"}
}

// FilterNeedsValue is false for operators that take no value.
func FilterNeedsValue(op string) bool {
	switch op {
	case "isEmpty", "isNotEmpty", "checked", "unchecked":
		return false
	}
	return true
}

// FilterOpLabel is the operator's display text.
func FilterOpLabel(op string) string {
	switch op {
	case "isNot":
		return "is not"
	case "notContains":
		return "does not contain"
	case "isEmpty":
		return "is empty"
	case "isNotEmpty":
		return "is not empty"
	case "gt":
		return ">"
	case "lt":
		return "<"
	}
	return op
}

// RenameView changes a view's name.
func (d *Doc) RenameView(viewID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a view needs a name")
	}
	v := d.viewObject(viewID)
	if v == nil {
		return fmt.Errorf("unknown view")
	}
	v.Set("name", name)
	return d.Resync()
}

// RemoveView deletes a view, keeping at least one.
func (d *Doc) RemoveView(viewID string) error {
	views := d.Sidecar.Array("views")
	if len(views) <= 1 {
		return fmt.Errorf("a database keeps at least one view")
	}
	kept := []any{}
	for _, entry := range views {
		if o, ok := entry.(*Object); ok && o.String("id") == viewID {
			continue
		}
		kept = append(kept, entry)
	}
	d.Sidecar.Set("views", kept)
	if d.Sidecar.String("activeViewId") == viewID {
		if first, ok := kept[0].(*Object); ok {
			d.Sidecar.Set("activeViewId", first.String("id"))
		}
	}
	return d.Resync()
}

// AddView appends a table or board view and makes it active.
func (d *Doc) AddView(typ string) (View, error) {
	if typ != "board" {
		typ = "table"
	}
	o := NewObject()
	id := GenID()
	o.Set("id", id)
	if typ == "board" {
		o.Set("name", d.uniqueViewName("Board"))
	} else {
		o.Set("name", d.uniqueViewName("Table"))
	}
	o.Set("type", typ)
	o.Set("filters", []any{})
	o.Set("sorts", []any{})
	if typ == "board" {
		for _, f := range d.Fields {
			if f.Type == "select" {
				o.Set("groupByFieldId", f.ID)
				break
			}
		}
		cards := []any{}
		for _, f := range d.Fields {
			if f.ID != d.IDFieldID {
				cards = append(cards, f.ID)
			}
		}
		o.Set("cardFieldIds", cards)
	} else {
		order := []any{}
		hidden := []any{}
		for _, f := range d.Fields {
			order = append(order, f.ID)
			if f.Hidden {
				hidden = append(hidden, f.ID)
			}
		}
		o.Set("columnOrder", order)
		o.Set("hiddenFieldIds", hidden)
	}
	d.Sidecar.Set("views", append(d.Sidecar.Array("views"), o))
	d.Sidecar.Set("activeViewId", id)
	if err := d.Resync(); err != nil {
		return View{}, err
	}
	if v := d.ViewByID(id); v != nil {
		return *v, nil
	}
	return View{}, fmt.Errorf("view was not added")
}

func (d *Doc) uniqueViewName(base string) string {
	taken := map[string]bool{}
	for _, v := range d.Views {
		taken[v.Name] = true
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s %d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// SetActiveView records which view opens by default.
func (d *Doc) SetActiveView(viewID string) error {
	if d.viewObject(viewID) == nil {
		return fmt.Errorf("unknown view")
	}
	d.Sidecar.Set("activeViewId", viewID)
	return d.Resync()
}

// SetGroupBy points a board view at a select field.
func (d *Doc) SetGroupBy(viewID, fieldID string) error {
	v := d.viewObject(viewID)
	if v == nil {
		return fmt.Errorf("unknown view")
	}
	f := d.FieldByID(fieldID)
	if f == nil || f.Type != "select" {
		return fmt.Errorf("boards group by a select field")
	}
	v.Set("groupByFieldId", fieldID)
	v.Delete("boardColumnOrder")
	return d.Resync()
}

// SetBoardColumnOrder stores the column order of a board view.
func (d *Doc) SetBoardColumnOrder(viewID string, order []string) error {
	v := d.viewObject(viewID)
	if v == nil {
		return fmt.Errorf("unknown view")
	}
	v.Set("boardColumnOrder", anyStrings(order))
	return d.Resync()
}

// SetCardFields chooses which fields a board card shows.
func (d *Doc) SetCardFields(viewID string, fieldIDs []string) error {
	v := d.viewObject(viewID)
	if v == nil {
		return fmt.Errorf("unknown view")
	}
	v.Set("cardFieldIds", anyStrings(fieldIDs))
	return d.Resync()
}

// SetOptionColor records a palette token for a select option.
func (d *Doc) SetOptionColor(fieldID, value, color string) error {
	o := d.fieldObject(fieldID)
	if o == nil {
		return fmt.Errorf("unknown field")
	}
	for _, entry := range o.Array("options") {
		opt, ok := entry.(*Object)
		if !ok || opt.String("value") != value {
			continue
		}
		if color == "" {
			opt.Delete("color")
		} else {
			opt.Set("color", color)
		}
	}
	return d.Resync()
}

// RemoveSelectOption drops an option from a select field.
func (d *Doc) RemoveSelectOption(fieldID, value string) error {
	o := d.fieldObject(fieldID)
	if o == nil {
		return fmt.Errorf("unknown field")
	}
	kept := []any{}
	for _, entry := range o.Array("options") {
		if opt, ok := entry.(*Object); ok && opt.String("value") == value {
			continue
		}
		kept = append(kept, entry)
	}
	o.Set("options", kept)
	return d.Resync()
}

// SetFieldOptionsSource points a select field at notes for its options, or
// back to the manual list when source is nil.
func (d *Doc) SetFieldOptionsSource(fieldID string, source *OptionsSource) error {
	o := d.fieldObject(fieldID)
	if o == nil {
		return fmt.Errorf("unknown field")
	}
	if source == nil {
		o.Delete("optionsSource")
		return d.Resync()
	}
	src := NewObject()
	src.Set("kind", source.Kind)
	switch source.Kind {
	case "folder":
		src.Set("path", source.Path)
	case "tag":
		src.Set("tag", source.Tag)
	case "notes":
	default:
		return fmt.Errorf("unknown options source %q", source.Kind)
	}
	o.Set("optionsSource", src)
	return d.Resync()
}

// OptionColors are the palette tokens the desktop assigns to options.
var OptionColors = []string{"red", "orange", "amber", "green", "teal", "sky", "blue", "indigo", "violet", "pink"}

// DuplicateRow copies a row's cells into a new row placed after it.
func (d *Doc) DuplicateRow(rowID string) (Row, bool) {
	for i, r := range d.Rows {
		if r.ID != rowID {
			continue
		}
		id := GenID()
		cells := map[string]string{}
		for k, v := range r.Cells {
			cells[k] = v
		}
		cells[d.IDFieldID] = id
		row := Row{ID: id, Cells: cells}
		d.Rows = append(d.Rows[:i+1], append([]Row{row}, d.Rows[i+1:]...)...)
		return row, true
	}
	return Row{}, false
}
