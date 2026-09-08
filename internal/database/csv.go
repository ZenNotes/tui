package database

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// GenID mints a lowercase v4 UUID, the row and field identity the app uses.
func GenID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "r-" + strconv.FormatInt(int64(b[0])<<8|int64(b[1]), 36)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// ParseCSV parses RFC 4180 text into a grid. Blank lines are dropped; a BOM
// is stripped.
func ParseCSV(text string) [][]string {
	rows := [][]string{}
	row := []string{}
	var field strings.Builder
	inQuotes := false
	runes := []rune(text)
	i := 0
	if len(runes) > 0 && runes[0] == 0xfeff {
		i = 1
	}
	endField := func() {
		row = append(row, field.String())
		field.Reset()
	}
	endRow := func() {
		endField()
		if !(len(row) == 1 && row[0] == "") {
			rows = append(rows, row)
		}
		row = []string{}
	}
	n := len(runes)
	for i < n {
		ch := runes[i]
		if inQuotes {
			if ch == '"' {
				if i+1 < n && runes[i+1] == '"' {
					field.WriteRune('"')
					i += 2
					continue
				}
				inQuotes = false
				i++
				continue
			}
			field.WriteRune(ch)
			i++
			continue
		}
		switch ch {
		case '"':
			inQuotes = true
			i++
		case ',':
			endField()
			i++
		case '\r':
			endRow()
			if i+1 < n && runes[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
		case '\n':
			endRow()
			i++
		default:
			field.WriteRune(ch)
			i++
		}
	}
	if field.Len() > 0 || len(row) > 0 {
		endRow()
	}
	return rows
}

var csvQuoteRe = regexp.MustCompile(`[",\r\n]`)

func serializeCell(value string) string {
	if value == "" {
		return ""
	}
	if csvQuoteRe.MatchString(value) {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return value
}

// SerializeCSV renders a grid as RFC 4180 text with LF newlines and a
// trailing newline.
func SerializeCSV(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		cells := make([]string, len(r))
		for j, c := range r {
			cells[j] = serializeCell(c)
		}
		lines[i] = strings.Join(cells, ",")
	}
	return strings.Join(lines, "\n") + "\n"
}

// ParseRows hydrates rows given the known fields. Columns match fields by
// header NAME, so external column reordering is harmless; rows without an id
// get one.
func ParseRows(csvText string, fields []Field, idFieldID string) []Row {
	grid := ParseCSV(csvText)
	if len(grid) == 0 {
		return []Row{}
	}
	headers := grid[0]
	colByName := map[string]int{}
	for idx, h := range headers {
		if _, seen := colByName[h]; !seen {
			colByName[h] = idx
		}
	}
	out := []Row{}
	for r := 1; r < len(grid); r++ {
		raw := grid[r]
		cells := map[string]string{}
		for _, field := range fields {
			col, ok := colByName[field.Name]
			if !ok || col >= len(raw) {
				cells[field.ID] = ""
			} else {
				cells[field.ID] = raw[col]
			}
		}
		id := cells[idFieldID]
		if id == "" {
			id = GenID()
			if _, has := cells[idFieldID]; has || idFieldID != "" {
				cells[idFieldID] = id
			}
		}
		out = append(out, Row{ID: id, Cells: cells})
	}
	return out
}

// SerializeRows renders rows back to CSV text, header from the field names
// in field order.
func SerializeRows(rows []Row, fields []Field) string {
	header := make([]string, len(fields))
	for i, f := range fields {
		header[i] = f.Name
	}
	grid := [][]string{header}
	for _, row := range rows {
		cells := make([]string, len(fields))
		for i, f := range fields {
			cells[i] = row.Cells[f.ID]
		}
		grid = append(grid, cells)
	}
	return SerializeCSV(grid)
}

var isoDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var boolTrue = map[string]bool{"true": true, "x": true, "1": true, "yes": true, "checked": true}
var boolFalse = map[string]bool{"false": true, "": true, "0": true, "no": true, "unchecked": true}

func inferColumnType(samples []string) string {
	nonEmpty := []string{}
	for _, s := range samples {
		if t := strings.TrimSpace(s); t != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}
	if len(nonEmpty) == 0 {
		return "text"
	}
	allBool, allNumber, allDate := true, true, true
	for _, s := range nonEmpty {
		lower := strings.ToLower(s)
		if !boolTrue[lower] && !boolFalse[lower] {
			allBool = false
		}
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			allNumber = false
		}
		if !isoDateRe.MatchString(s) {
			allDate = false
		}
	}
	switch {
	case allBool:
		return "checkbox"
	case allNumber:
		return "number"
	case allDate:
		return "date"
	}
	return "text"
}

func dedupeHeader(name string, used map[string]bool, index int) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "Column " + strconv.Itoa(index+1)
	}
	candidate := base
	for n := 2; used[candidate]; n++ {
		candidate = base + " (" + strconv.Itoa(n) + ")"
	}
	used[candidate] = true
	return candidate
}

// InferFields builds fields (and picks the id field) for a CSV that has no
// sidecar yet. A usable `id` column (all unique, non-empty) becomes the id
// field; otherwise a leading hidden `id` field is synthesized.
func InferFields(headers []string, sampleRows [][]string) (idFieldID string, fields []Field) {
	used := map[string]bool{}
	normalized := make([]string, len(headers))
	for i, h := range headers {
		normalized[i] = dedupeHeader(h, used, i)
	}
	idCol := -1
	for i, h := range normalized {
		if strings.ToLower(h) == "id" {
			values := make([]string, 0, len(sampleRows))
			allPresent := len(sampleRows) > 0
			seen := map[string]bool{}
			unique := true
			for _, r := range sampleRows {
				v := ""
				if i < len(r) {
					v = r[i]
				}
				if strings.TrimSpace(v) == "" {
					allPresent = false
				}
				if seen[v] {
					unique = false
				}
				seen[v] = true
				values = append(values, v)
			}
			if len(sampleRows) == 0 || (allPresent && unique) {
				idCol = i
			}
			break
		}
	}
	fields = make([]Field, 0, len(normalized)+1)
	for i, name := range normalized {
		f := Field{ID: GenID(), Name: name, Type: "text"}
		if i != idCol {
			samples := make([]string, 0, len(sampleRows))
			for _, r := range sampleRows {
				if i < len(r) {
					samples = append(samples, r[i])
				} else {
					samples = append(samples, "")
				}
			}
			f.Type = inferColumnType(samples)
		} else {
			f.Hidden = true
		}
		fields = append(fields, f)
	}
	if idCol >= 0 {
		idFieldID = fields[idCol].ID
	} else {
		idField := Field{ID: GenID(), Name: "id", Type: "text", Hidden: true}
		fields = append([]Field{idField}, fields...)
		idFieldID = idField.ID
	}
	for i := range fields {
		fields[i].raw = fieldObject(fields[i])
	}
	return idFieldID, fields
}

func fieldObject(f Field) *Object {
	o := NewObject()
	o.Set("id", f.ID)
	o.Set("name", f.Name)
	o.Set("type", f.Type)
	if f.Hidden {
		o.Set("hidden", true)
	}
	return o
}

// DefaultView is a single Table view (id hidden) over the fields in order.
func DefaultView(fields []Field) (*Object, string) {
	id := GenID()
	view := NewObject()
	view.Set("id", id)
	view.Set("name", "Table")
	view.Set("type", "table")
	view.Set("filters", []any{})
	view.Set("sorts", []any{})
	order := make([]any, len(fields))
	hidden := []any{}
	for i, f := range fields {
		order[i] = f.ID
		if f.Hidden {
			hidden = append(hidden, f.ID)
		}
	}
	view.Set("columnOrder", order)
	view.Set("hiddenFieldIds", hidden)
	return view, id
}
