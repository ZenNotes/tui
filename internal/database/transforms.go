package database

import (
	"sort"
	"strconv"
	"strings"
)

func cell(row Row, fieldID string) string {
	return row.Cells[fieldID]
}

func matchesRule(row Row, rule FilterRule, field *Field) bool {
	if field == nil {
		return true
	}
	raw := cell(row, field.ID)
	v := strings.TrimSpace(raw)
	target := strings.TrimSpace(rule.Value)
	contains := func(list []string, s string) bool {
		for _, e := range list {
			if e == s {
				return true
			}
		}
		return false
	}
	switch rule.Op {
	case "is":
		if field.Type == "multiSelect" {
			return contains(SplitMultiSelect(raw), target)
		}
		return strings.EqualFold(v, target)
	case "isNot":
		if field.Type == "multiSelect" {
			return !contains(SplitMultiSelect(raw), target)
		}
		return !strings.EqualFold(v, target)
	case "contains":
		return strings.Contains(strings.ToLower(v), strings.ToLower(target))
	case "notContains":
		return !strings.Contains(strings.ToLower(v), strings.ToLower(target))
	case "isEmpty":
		return v == ""
	case "isNotEmpty":
		return v != ""
	case "gt":
		a, err1 := strconv.ParseFloat(v, 64)
		b, err2 := strconv.ParseFloat(target, 64)
		return err1 == nil && err2 == nil && a > b
	case "lt":
		a, err1 := strconv.ParseFloat(v, 64)
		b, err2 := strconv.ParseFloat(target, 64)
		return err1 == nil && err2 == nil && a < b
	case "before":
		return v != "" && v < target
	case "after":
		return v != "" && v > target
	case "checked":
		return IsCheckboxTrue(raw)
	case "unchecked":
		return !IsCheckboxTrue(raw)
	}
	return true
}

// FilterRows applies a view's filters; `or` keeps rows matching any rule.
func FilterRows(rows []Row, filters []FilterRule, doc *Doc, conjunction string) []Row {
	if len(filters) == 0 {
		return rows
	}
	out := []Row{}
	for _, row := range rows {
		matched := conjunction != "or"
		for _, rule := range filters {
			ok := matchesRule(row, rule, doc.FieldByID(rule.FieldID))
			if conjunction == "or" && ok {
				matched = true
				break
			}
			if conjunction != "or" && !ok {
				matched = false
				break
			}
		}
		if matched {
			out = append(out, row)
		}
	}
	return out
}

func compareByField(a, b Row, field Field) int {
	av := strings.TrimSpace(cell(a, field.ID))
	bv := strings.TrimSpace(cell(b, field.ID))
	switch {
	case av == "" && bv == "":
		return 0
	case av == "":
		return 1
	case bv == "":
		return -1
	}
	switch field.Type {
	case "number":
		na, err1 := strconv.ParseFloat(av, 64)
		nb, err2 := strconv.ParseFloat(bv, 64)
		if err1 != nil || err2 != nil {
			return strings.Compare(av, bv)
		}
		switch {
		case na < nb:
			return -1
		case na > nb:
			return 1
		}
		return 0
	case "checkbox":
		ca, cb := 0, 0
		if IsCheckboxTrue(av) {
			ca = 1
		}
		if IsCheckboxTrue(bv) {
			cb = 1
		}
		return ca - cb
	case "date":
		return strings.Compare(av, bv)
	}
	return strings.Compare(strings.ToLower(av), strings.ToLower(bv))
}

// SortRows applies a view's sorts, stable.
func SortRows(rows []Row, sorts []SortRule, doc *Doc) []Row {
	if len(sorts) == 0 {
		return rows
	}
	out := append([]Row(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		for _, s := range sorts {
			field := doc.FieldByID(s.FieldID)
			if field == nil {
				continue
			}
			cmp := compareByField(out[i], out[j], *field)
			if cmp == 0 {
				continue
			}
			if s.Direction == "desc" {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
	return out
}

// BoardColumn is one column of a board view.
type BoardColumn struct {
	Key  string
	Rows []Row
}

// BoardColumns groups rows by a select field's value, an EmptyGroup column
// appended for rows whose cell is empty or references a removed option.
func BoardColumns(rows []Row, groupField Field, optionOrder []string) []BoardColumn {
	buckets := map[string][]Row{}
	known := map[string]bool{}
	for _, v := range optionOrder {
		known[v] = true
		buckets[v] = []Row{}
	}
	buckets[EmptyGroup] = []Row{}
	for _, row := range rows {
		v := strings.TrimSpace(cell(row, groupField.ID))
		key := EmptyGroup
		if v != "" && known[v] {
			key = v
		}
		buckets[key] = append(buckets[key], row)
	}
	columns := make([]BoardColumn, 0, len(optionOrder)+1)
	for _, v := range optionOrder {
		columns = append(columns, BoardColumn{Key: v, Rows: buckets[v]})
	}
	columns = append(columns, BoardColumn{Key: EmptyGroup, Rows: buckets[EmptyGroup]})
	return columns
}
