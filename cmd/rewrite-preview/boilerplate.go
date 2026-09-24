// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

type boilerBlock struct {
	text      string
	selectors map[string]int
	entries   int
	firstSeen int
}

// leafBlockSelector matches block elements that directly hold text. Wrappers
// are found through the ancestor walk in selectorHint instead.
const leafBlockSelector = "p, li, h1, h2, h3, h4, h5, h6, blockquote, figcaption, dt, dd, td, th, pre, a, button, span, em, strong, div"

// reportBoilerplate lists text blocks that repeat across entries. The text is
// read from the content that remains after rewrite rules (but before the
// sanitizer, which strips the class and id attributes selectors need).
func reportBoilerplate(results []result, minShare float64) {
	blocks := map[string]*boilerBlock{}
	considered := 0
	for i, r := range results {
		if r.blocked {
			continue
		}
		considered++
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(r.rewritten))
		if err != nil {
			continue
		}
		doc.Find("script, style, noscript, template").Remove()
		seen := map[string]bool{}
		doc.Find(leafBlockSelector).Each(func(_ int, sel *goquery.Selection) {
			// Only the innermost element carrying a given text: skip elements
			// whose text is entirely contained in a single child element.
			if sel.Children().Length() == 1 && norm(sel.Children().Text()) == norm(sel.Text()) {
				return
			}
			text := norm(sel.Text())
			if len([]rune(text)) < 8 || seen[text] {
				return
			}
			seen[text] = true
			b, ok := blocks[text]
			if !ok {
				b = &boilerBlock{text: text, selectors: map[string]int{}, firstSeen: i}
				blocks[text] = b
			}
			b.entries++
			b.selectors[selectorHint(sel)]++
		})
	}

	threshold := max(2, int(math.Ceil(minShare*float64(considered))))
	var repeated []*boilerBlock
	for _, b := range blocks {
		if b.entries >= threshold {
			repeated = append(repeated, b)
		}
	}
	sort.Slice(repeated, func(i, j int) bool {
		if repeated[i].entries != repeated[j].entries {
			return repeated[i].entries > repeated[j].entries
		}
		return repeated[i].firstSeen < repeated[j].firstSeen
	})

	fmt.Printf("\nBOILERPLATE: text blocks in >= %d of %d non-blocked entries (after proposed rules)\n", threshold, considered)
	if len(repeated) == 0 {
		fmt.Println("  none found")
		return
	}
	for _, b := range repeated {
		fmt.Printf("  [%d/%d] %s\n", b.entries, considered, clip(b.text, 140))
		fmt.Printf("         selector: %s\n", topSelectors(b.selectors))
	}
}

func norm(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// selectorHint builds a short CSS path from the nearest ancestors that carry
// an id or class, e.g. "div.subscription-widget > p.cta".
func selectorHint(sel *goquery.Selection) string {
	var parts []string
	for n := sel.Get(0); n != nil && n.Type == html.ElementNode && len(parts) < 3; n = n.Parent {
		if n.Data == "body" || n.Data == "html" {
			break
		}
		part := n.Data
		id, cls := "", ""
		for _, a := range n.Attr {
			switch a.Key {
			case "id":
				id = a.Val
			case "class":
				cls = hintClasses(a.Val)
			}
		}
		switch {
		case id != "":
			parts = append(parts, part+"#"+id)
			// An id is specific enough; stop here.
			n = nil
		case cls != "":
			parts = append(parts, part+"."+cls)
		case len(parts) == 0:
			parts = append(parts, part)
		default:
			continue
		}
		if n == nil {
			break
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " ")
}

// hintClasses keeps at most two classes that are usable in a selector, skipping
// utility-framework classes such as "group-hover:text-white" or "w-1/2".
func hintClasses(classAttr string) string {
	var keep []string
	for _, c := range strings.Fields(classAttr) {
		if strings.ContainsAny(c, ":/[]().!@%#") {
			continue
		}
		keep = append(keep, c)
		if len(keep) == 2 {
			break
		}
	}
	return strings.Join(keep, ".")
}

func topSelectors(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	var out []string
	for i, e := range list {
		if i == 3 {
			break
		}
		out = append(out, e.k)
	}
	return strings.Join(out, "  |  ")
}
