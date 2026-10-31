// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package rewrite // import "miniflux.app/v2/internal/reader/rewrite"

import (
	"testing"

	"miniflux.app/v2/internal/model"
)

func TestAddEnclosureLinksWithCaptionAndCredit(t *testing.T) {
	scenarios := []struct {
		caption, credit, expected string
	}{
		{"", "", `<li><a href="https://example.org/a.jpg">https://example.org/a.jpg</a></li>`},
		{"A <rock>.", "", `<li><a href="https://example.org/a.jpg">https://example.org/a.jpg</a><p>A &lt;rock&gt;.</p></li>`},
		{"", "Getty & Co", `<li><a href="https://example.org/a.jpg">https://example.org/a.jpg</a><p>Credit: Getty &amp; Co</p></li>`},
		{"A rock.", "Getty", `<li><a href="https://example.org/a.jpg">https://example.org/a.jpg</a><p>A rock. Credit: Getty</p></li>`},
	}

	for _, scenario := range scenarios {
		entry := &model.Entry{
			Content: "body",
			Enclosures: model.EnclosureList{
				{URL: "https://example.org/a.jpg", Caption: scenario.caption, Credit: scenario.credit},
			},
		}

		expected := "body<hr/><ul>" + scenario.expected + "</ul>"
		if got := addEnclosureLinks(entry); got != expected {
			t.Errorf("Unexpected output for %q / %q:\ngot:  %s\nwant: %s", scenario.caption, scenario.credit, got, expected)
		}
	}
}
