// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model // import "miniflux.app/v2/internal/model"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// FieldSet represents the parsed value of the "fields" API query parameter.
// A nil FieldSet selects all fields. The map value is the sub-fieldset for
// nested objects: a nil value selects the whole sub-object.
type FieldSet map[string]FieldSet

// Has reports whether path is selected by fs. path is either a top-level
// field name ("title") or a one-level dotted path ("feed.title"). A nil
// receiver always returns true, since a nil FieldSet means "everything".
func (fs FieldSet) Has(path string) bool {
	if fs == nil {
		return true
	}

	parent, child, dotted := strings.Cut(path, ".")
	sub, ok := fs[parent]
	if !ok {
		return false
	}
	if !dotted {
		return true
	}

	// A nil sub-fieldset means the whole nested object is selected, which
	// includes every one of its fields.
	if sub == nil {
		return true
	}

	return sub.Has(child)
}

// ParseFieldSet parses the comma-separated list of field names in value,
// validating each element against allowed. An empty/blank value returns
// (nil, nil), meaning "select everything".
func ParseFieldSet(value string, allowed FieldSet) (FieldSet, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	fs := FieldSet{}
	for element := range strings.SplitSeq(value, ",") {
		element = strings.TrimSpace(element)
		if element == "" {
			continue
		}

		parent, child, dotted := strings.Cut(element, ".")

		// Validate with explicit lookups rather than allowed.Has: in the
		// allowed set a nil sub-fieldset marks a scalar leaf with no valid
		// children, whereas Has treats nil as "every child selected". A
		// child containing another dot is never found, which also rejects
		// paths nested deeper than one level.
		allowedSub, ok := allowed[parent]
		if !ok {
			return nil, fmt.Errorf("invalid field name: %q", element)
		}
		if dotted {
			if _, ok := allowedSub[child]; !ok {
				return nil, fmt.Errorf("invalid field name: %q", element)
			}
		}
		if !dotted {
			fs[parent] = nil
			continue
		}

		// A bare parent always supersedes dotted entries for the same
		// parent, regardless of the order they appear in: once the whole
		// object is selected, a more specific dotted entry adds nothing.
		if existing, ok := fs[parent]; ok && existing == nil {
			continue
		}

		if fs[parent] == nil {
			fs[parent] = FieldSet{}
		}
		fs[parent][child] = nil
	}

	if len(fs) == 0 {
		return nil, nil
	}

	return fs, nil
}

// FilterJSON prunes v down to the fields selected by fs. It returns v
// unchanged when fs is nil. Otherwise, v is marshaled to JSON and decoded
// back into a generic structure (using json.Number to preserve int64
// precision) that is recursively filtered and returned for the caller to
// marshal again.
//
// The round-trip may look wasteful, but the storage layer already replaced
// unrequested columns with zero values, so the intermediate document is
// small — and the pruned output is guaranteed to match the regular
// encoding/json representation of the models. Filtering during encoding
// instead requires per-call custom marshalers, which only the experimental
// encoding/json/v2 provides.
func (fs FieldSet) FilterJSON(v any) (any, error) {
	if fs == nil {
		return v, nil
	}

	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, err
	}

	return fs.filter(parsed), nil
}

func (fs FieldSet) filter(v any) any {
	switch value := v.(type) {
	case []any:
		filtered := make([]any, len(value))
		for i, item := range value {
			filtered[i] = fs.filter(item)
		}
		return filtered
	case map[string]any:
		filtered := make(map[string]any, len(fs))
		for name, sub := range fs {
			item, ok := value[name]
			if !ok {
				continue
			}
			if sub != nil {
				item = sub.filter(item)
			}
			filtered[name] = item
		}
		return filtered
	default:
		return v
	}
}

var timeType = reflect.TypeFor[time.Time]()

// EntryFields and FeedFields are the allowed field sets used to validate the
// "fields" API query parameter, derived once at startup by reflecting over
// model.Entry and model.Feed.
var (
	EntryFields = fieldSetForType(reflect.TypeFor[Entry]())
	FeedFields  = fieldSetForType(reflect.TypeFor[Feed]())
)

// fieldSetForType builds the allowed FieldSet for t by walking its exported,
// JSON-tagged fields. Struct fields (after dereferencing pointers/slices,
// excluding time.Time) get a one-level-deep sub-fieldset; deeper nesting is
// not allowed.
func fieldSetForType(t reflect.Type) FieldSet {
	return fieldSetForTypeAtDepth(t, true)
}

func fieldSetForTypeAtDepth(t reflect.Type, allowNesting bool) FieldSet {
	fs := FieldSet{}

	for field := range t.Fields() {
		if field.PkgPath != "" {
			continue
		}

		tag := field.Tag.Get("json")
		if tag == "-" || tag == "" {
			continue
		}

		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			continue
		}

		var sub FieldSet
		if allowNesting {
			fieldType := field.Type
			for fieldType.Kind() == reflect.Pointer || fieldType.Kind() == reflect.Slice {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct && fieldType != timeType {
				sub = fieldSetForTypeAtDepth(fieldType, false)
			}
		}

		fs[name] = sub
	}

	return fs
}
