// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package rss // import "miniflux.app/v2/internal/reader/rss"

import (
	"bytes"
	"testing"
)

func TestParseMediaContentCaptionAndCredit(t *testing.T) {
	data := `<?xml version="1.0"?>
		<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
		<channel>
			<title>Example</title>
			<link>https://example.org/</link>
			<item>
				<title>With caption</title>
				<link>https://example.org/a</link>
				<media:content url="https://example.org/a.jpg" type="image/jpeg" medium="image">
					<media:thumbnail url="https://example.org/a-small.jpg" />
					<media:credit>Darryl Pitt / Maine Mineral &amp; Gem Museum</media:credit>
					<media:text> A meteorite. </media:text>
				</media:content>
			</item>
			<item>
				<title>Without caption</title>
				<link>https://example.org/b</link>
				<media:content url="https://example.org/b.jpg" type="image/jpeg" medium="image" />
			</item>
		</channel>
		</rss>`

	feed, err := Parse("https://example.org/", bytes.NewReader([]byte(data)))
	if err != nil {
		t.Fatal(err)
	}

	enclosure := feed.Entries[0].Enclosures[0]
	if enclosure.Caption != "A meteorite." {
		t.Errorf("Incorrect caption, got: %q", enclosure.Caption)
	}
	if enclosure.Credit != "Darryl Pitt / Maine Mineral & Gem Museum" {
		t.Errorf("Incorrect credit, got: %q", enclosure.Credit)
	}

	enclosure = feed.Entries[1].Enclosures[0]
	if enclosure.Caption != "" || enclosure.Credit != "" {
		t.Errorf("Expected no caption or credit, got: %q / %q", enclosure.Caption, enclosure.Credit)
	}
}
