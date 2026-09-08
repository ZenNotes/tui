// Package database reads and writes ZenNotes databases: a `<Name>.base/`
// folder holding `data.csv`, a `schema.json` sidecar and record-page notes.
// Everything is composed from generic vault-file IO, the same composition
// the web and desktop remote clients use, so an edit made here is
// byte-identical to one made in the app's grid.
package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// Object is a JSON object that remembers key order. schema.json is
// user-visible and user-editable, and the app re-serializes it as parsed, so
// a round trip through here must keep every key where it was, known or not.
type Object struct {
	keys   []string
	values map[string]any
}

// NewObject makes an empty ordered object.
func NewObject() *Object {
	return &Object{values: map[string]any{}}
}

// Keys lists the keys in order.
func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.keys...)
}

// Get reads a value.
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

// Set writes a value, appending a new key at the end.
func (o *Object) Set(key string, value any) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Delete removes a key.
func (o *Object) Delete(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// String reads a string value or "".
func (o *Object) String(key string) string {
	v, _ := o.Get(key)
	s, _ := v.(string)
	return s
}

// Bool reads a boolean value or false.
func (o *Object) Bool(key string) bool {
	v, _ := o.Get(key)
	b, _ := v.(bool)
	return b
}

// Array reads an array value or nil.
func (o *Object) Array(key string) []any {
	v, _ := o.Get(key)
	a, _ := v.([]any)
	return a
}

// Object reads a nested object or nil.
func (o *Object) Object(key string) *Object {
	v, _ := o.Get(key)
	n, _ := v.(*Object)
	return n
}

// Clone copies the object deeply.
func (o *Object) Clone() *Object {
	if o == nil {
		return nil
	}
	out := NewObject()
	for _, k := range o.keys {
		out.Set(k, cloneValue(o.values[k]))
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case *Object:
		return t.Clone()
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	}
	return v
}

// UnmarshalJSON decodes an object keeping key order; numbers stay as
// json.Number so their spelling survives a round trip.
func (o *Object) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return err
	}
	obj, ok := v.(*Object)
	if !ok {
		return errors.New("expected a JSON object")
	}
	*o = *obj
	return nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, errors.New("expected an object key")
				}
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return tok, nil
	}
}

// MarshalJSON encodes the object in key order without HTML escaping, the
// way JSON.stringify does.
func (o *Object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, o); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeValue(w io.Writer, v any) error {
	switch t := v.(type) {
	case *Object:
		if t == nil {
			_, err := io.WriteString(w, "null")
			return err
		}
		if _, err := io.WriteString(w, "{"); err != nil {
			return err
		}
		for i, k := range t.keys {
			if i > 0 {
				if _, err := io.WriteString(w, ","); err != nil {
					return err
				}
			}
			if err := writeString(w, k); err != nil {
				return err
			}
			if _, err := io.WriteString(w, ":"); err != nil {
				return err
			}
			if err := writeValue(w, t.values[k]); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "}")
		return err
	case []any:
		if _, err := io.WriteString(w, "["); err != nil {
			return err
		}
		for i, e := range t {
			if i > 0 {
				if _, err := io.WriteString(w, ","); err != nil {
					return err
				}
			}
			if err := writeValue(w, e); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "]")
		return err
	case string:
		return writeString(w, t)
	case json.Number:
		_, err := io.WriteString(w, t.String())
		return err
	case nil:
		_, err := io.WriteString(w, "null")
		return err
	default:
		data, err := json.Marshal(t)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
}

func writeString(w io.Writer, s string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	_, err := w.Write(bytes.TrimRight(buf.Bytes(), "\n"))
	return err
}

// Stringify renders like `JSON.stringify(value, null, 2)`.
func Stringify(o *Object) (string, error) {
	compact, err := o.MarshalJSON()
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return "", err
	}
	return out.String(), nil
}

// ParseObject decodes JSON text into an ordered object.
func ParseObject(text string) (*Object, error) {
	o := NewObject()
	if err := o.UnmarshalJSON([]byte(text)); err != nil {
		return nil, err
	}
	return o, nil
}

// objectFromMap builds an object with keys in sorted order, for the rare
// value this process authors from scratch.
func objectFromMap(m map[string]any) *Object {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	o := NewObject()
	for _, k := range keys {
		o.Set(k, m[k])
	}
	return o
}
