// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseFieldSet(t *testing.T) {
	testCases := []struct {
		name      string
		value     string
		allowed   FieldSet
		expected  FieldSet
		expectErr bool
	}{
		{
			name:     "empty string returns nil",
			value:    "",
			allowed:  EntryFields,
			expected: nil,
		},
		{
			name:     "blank string returns nil",
			value:    "   ",
			allowed:  EntryFields,
			expected: nil,
		},
		{
			name:     "simple list",
			value:    "id,title",
			allowed:  EntryFields,
			expected: FieldSet{"id": nil, "title": nil},
		},
		{
			name:     "whitespace is trimmed",
			value:    " id , title ",
			allowed:  EntryFields,
			expected: FieldSet{"id": nil, "title": nil},
		},
		{
			name:     "empty elements are skipped",
			value:    "id,,title,",
			allowed:  EntryFields,
			expected: FieldSet{"id": nil, "title": nil},
		},
		{
			name:     "dotted field",
			value:    "id,feed.title",
			allowed:  EntryFields,
			expected: FieldSet{"id": nil, "feed": {"title": nil}},
		},
		{
			name:     "bare parent supersedes dotted entry, dotted first",
			value:    "feed.title,feed",
			allowed:  EntryFields,
			expected: FieldSet{"feed": nil},
		},
		{
			name:     "bare parent supersedes dotted entry, bare first",
			value:    "feed,feed.title",
			allowed:  EntryFields,
			expected: FieldSet{"feed": nil},
		},
		{
			name:     "only empty elements returns nil",
			value:    ",,",
			allowed:  EntryFields,
			expected: nil,
		},
		{
			name:      "sub-field on a scalar field is invalid",
			value:     "title.bogus",
			allowed:   EntryFields,
			expectErr: true,
		},
		{
			name:      "invalid top-level field name",
			value:     "bogus",
			allowed:   EntryFields,
			expectErr: true,
		},
		{
			name:      "invalid sub-field name",
			value:     "feed.bogus",
			allowed:   EntryFields,
			expectErr: true,
		},
		{
			name:      "unknown parent for dotted field",
			value:     "bogus.title",
			allowed:   EntryFields,
			expectErr: true,
		},
		{
			name:      "two dots is invalid",
			value:     "feed.category.title",
			allowed:   EntryFields,
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseFieldSet(tc.value, tc.allowed)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("ParseFieldSet(%q) expected an error, got none", tc.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFieldSet(%q) unexpected error: %v", tc.value, err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("ParseFieldSet(%q) = %#v, want %#v", tc.value, got, tc.expected)
			}
		})
	}
}

func TestFieldSet_Has(t *testing.T) {
	testCases := []struct {
		name     string
		fs       FieldSet
		path     string
		expected bool
	}{
		{
			name:     "nil fieldset selects everything",
			fs:       nil,
			path:     "anything.at.all",
			expected: true,
		},
		{
			name:     "top-level field present",
			fs:       FieldSet{"title": nil},
			path:     "title",
			expected: true,
		},
		{
			name:     "top-level field absent",
			fs:       FieldSet{"title": nil},
			path:     "id",
			expected: false,
		},
		{
			name:     "dotted path with nil sub-fieldset selects whole object",
			fs:       FieldSet{"feed": nil},
			path:     "feed.title",
			expected: true,
		},
		{
			name:     "dotted path with explicit sub-fieldset containing the child",
			fs:       FieldSet{"feed": {"title": nil}},
			path:     "feed.title",
			expected: true,
		},
		{
			name:     "dotted path with explicit sub-fieldset missing the child",
			fs:       FieldSet{"feed": {"title": nil}},
			path:     "feed.site_url",
			expected: false,
		},
		{
			name:     "dotted path with unknown parent",
			fs:       FieldSet{"title": nil},
			path:     "feed.title",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fs.Has(tc.path); got != tc.expected {
				t.Errorf("FieldSet(%#v).Has(%q) = %v, want %v", tc.fs, tc.path, got, tc.expected)
			}
		})
	}
}

func TestFieldSet_FilterJSON_NilPassthrough(t *testing.T) {
	entry := &Entry{ID: 42, Title: "hello"}

	var fs FieldSet
	got, err := fs.FilterJSON(entry)
	if err != nil {
		t.Fatalf("FilterJSON returned an error: %v", err)
	}

	if !reflect.DeepEqual(got, entry) {
		t.Errorf("FilterJSON(nil fieldset) = %#v, want the same value back: %#v", got, entry)
	}
}

func TestFieldSet_FilterJSON_Entry(t *testing.T) {
	entry := &Entry{
		ID:    9007199254740993, // 2^53+1: not exactly representable as float64
		Title: "hello",
		URL:   "https://example.com/article",
		Hash:  "abcd",
		Feed:  &Feed{ID: 1, Title: "My Feed", SiteURL: "https://example.com"},
		Tags:  []string{"news"},
		Enclosures: EnclosureList{
			{ID: 1, URL: "https://example.com/file.mp3", MimeType: "audio/mpeg"},
		},
	}

	fs := FieldSet{
		"id":         nil,
		"title":      nil,
		"feed":       {"title": nil},
		"enclosures": {"url": nil},
	}

	got, err := fs.FilterJSON(entry)
	if err != nil {
		t.Fatalf("FilterJSON returned an error: %v", err)
	}

	obj, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("FilterJSON did not return an object: %#v", got)
	}

	if len(obj) != 4 {
		t.Fatalf("expected 4 keys in filtered entry, got %d: %#v", len(obj), obj)
	}

	idNumber, ok := obj["id"].(json.Number)
	if !ok {
		t.Fatalf("id is not a json.Number: %#v", obj["id"])
	}
	if idNumber.String() != "9007199254740993" {
		t.Errorf("id = %s, want 9007199254740993 (large int64 precision must survive)", idNumber.String())
	}

	if obj["title"] != "hello" {
		t.Errorf("title = %#v, want %q", obj["title"], "hello")
	}

	feedObj, ok := obj["feed"].(map[string]any)
	if !ok {
		t.Fatalf("feed is not an object: %#v", obj["feed"])
	}
	if len(feedObj) != 1 || feedObj["title"] != "My Feed" {
		t.Errorf("feed = %#v, want only {title: My Feed}", feedObj)
	}

	enclosures, ok := obj["enclosures"].([]any)
	if !ok || len(enclosures) != 1 {
		t.Fatalf("enclosures = %#v, want a single-element array", obj["enclosures"])
	}
	enclosureObj, ok := enclosures[0].(map[string]any)
	if !ok {
		t.Fatalf("enclosure element is not an object: %#v", enclosures[0])
	}
	if len(enclosureObj) != 1 || enclosureObj["url"] != "https://example.com/file.mp3" {
		t.Errorf("enclosure = %#v, want only {url: ...}", enclosureObj)
	}
}

func TestFieldSet_FilterJSON_EntrySlice(t *testing.T) {
	entries := Entries{
		{ID: 1, Title: "first"},
		{ID: 2, Title: "second"},
	}

	fs := FieldSet{"id": nil}

	got, err := fs.FilterJSON(entries)
	if err != nil {
		t.Fatalf("FilterJSON returned an error: %v", err)
	}

	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("FilterJSON did not return an array: %#v", got)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr))
	}

	for i, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("element %d is not an object: %#v", i, item)
		}
		if len(obj) != 1 {
			t.Errorf("element %d has %d keys, want 1: %#v", i, len(obj), obj)
		}
		if _, ok := obj["title"]; ok {
			t.Errorf("element %d unexpectedly contains 'title': %#v", i, obj)
		}
	}
}

func TestFieldSet_FilterJSON_MarshalOutput(t *testing.T) {
	entry := &Entry{ID: 42, Title: "hello", Feed: &Feed{Title: "My Feed"}}
	fs := FieldSet{"id": nil, "title": nil, "feed": {"title": nil}}

	got, err := fs.FilterJSON(entry)
	if err != nil {
		t.Fatalf("FilterJSON returned an error: %v", err)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal returned an error: %v", err)
	}

	expected := `{"feed":{"title":"My Feed"},"id":42,"title":"hello"}`
	if string(data) != expected {
		t.Errorf("marshaled output = %s, want %s", data, expected)
	}
}

func TestFieldSet_FilterJSON_OmitEmpty(t *testing.T) {
	// Entry.Feed is tagged omitempty: a nil feed must be omitted even when
	// requested, matching how the unfiltered entry serializes.
	entry := &Entry{ID: 42}
	got, err := FieldSet{"id": nil, "feed": nil}.FilterJSON(entry)
	if err != nil {
		t.Fatalf("FilterJSON returned an error: %v", err)
	}
	obj := got.(map[string]any)
	if _, ok := obj["feed"]; ok {
		t.Errorf(`expected nil "feed" to be omitted (omitempty), got %#v`, obj)
	}

	// Feed.Icon is not tagged omitempty: a nil icon must serialize as null,
	// including when selected with a dotted sub-field.
	feed := &Feed{ID: 1}
	got, err = FieldSet{"icon": {"icon_id": nil}}.FilterJSON(feed)
	if err != nil {
		t.Fatalf("FilterJSON returned an error: %v", err)
	}
	obj = got.(map[string]any)
	if icon, ok := obj["icon"]; !ok || icon != nil {
		t.Errorf(`expected "icon" to be present and null, got %#v`, obj)
	}
}

func TestEntryFields(t *testing.T) {
	if !EntryFields.Has("content") {
		t.Error(`EntryFields should allow "content"`)
	}
	if !EntryFields.Has("hash") {
		t.Error(`EntryFields should allow "hash"`)
	}
	if !EntryFields.Has("feed.title") {
		t.Error(`EntryFields should allow "feed.title"`)
	}
	if EntryFields.Has("bogus") {
		t.Error(`EntryFields should not allow "bogus"`)
	}

	// Paths nested deeper than one level are rejected by ParseFieldSet's
	// validation lookups, not by Has.
	if _, err := ParseFieldSet("feed.category.title", EntryFields); err == nil {
		t.Error(`ParseFieldSet should reject "feed.category.title"`)
	}
}

func TestFeedFields(t *testing.T) {
	if !FeedFields.Has("title") {
		t.Error(`FeedFields should allow "title"`)
	}
	if !FeedFields.Has("feed_url") {
		t.Error(`FeedFields should allow "feed_url"`)
	}
	if !FeedFields.Has("category.title") {
		t.Error(`FeedFields should allow "category.title"`)
	}

	if _, err := ParseFieldSet("category.title.extra", FeedFields); err == nil {
		t.Error(`ParseFieldSet should reject "category.title.extra"`)
	}
}
