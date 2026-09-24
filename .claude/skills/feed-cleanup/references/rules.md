# Miniflux cleanup rules reference

## Contents
- Processing order
- Content rewrite rules: syntax, rule list, patterns
- Scraper rules
- Filters
- URL rewrite

Source of truth, in the Miniflux source: `internal/reader/rewrite/content_rewrite.go` (the `knownRules`
table), `internal/reader/filter/filter.go`, `internal/reader/scraper/scraper.go` and
`internal/reader/processor/processor.go`. If this file and the code disagree, the code wins.

## Processing order (per new entry, on each refresh)

1. **Block/keep filters**, on the entry as parsed from the feed. Blocked entries are never stored.
2. URL cleanup (tracking params), then the **URL rewrite** rule.
3. **Scraper**, only if `crawler` is true. The fetched page replaces the content. It runs for new entries, or
   for all entries in the XML on a forced refresh.
4. **Content rewrite rules**. `add_pdf_download_link` is always appended.
5. Filters run again, but only if the scraper replaced the content.
6. **Sanitizer**: strips `class`/`id`/`style`, scripts, and unknown tags. Rewrite selectors run *before*
   this, so they can use classes and ids. The stored entry has none left, which is why selectors are
   found in the preview's raw HTML and not in the API's entry content.

## Content rewrite rules (`rewrite_rules`)

### Syntax
- Parsed with Go's `text/scanner`: identifiers are rule names, and **double-quoted Go strings** are
  arguments attached to the preceding rule. Everything else (commas, parentheses, `|`, newlines) is
  ignored as a separator. So `remove(".a"), replace("x"|"y")` and the same thing split over lines are
  equivalent.
- **Backtick strings are not supported.** Arguments use Go escapes, so a regex `\s` is written `"\\s"`,
  and a literal `"` inside an argument is `\"`. For example: `remove("table[border=\"0\"]")`.
- Rules the UI saved may contain `\r\n` between rules. That's harmless.
- Unknown rule names, missing arguments and invalid regexes are rejected at save time (HTTP 400).
  Newer servers also reject invalid CSS selectors in `remove()`, `base64_decode()` and scraper rules;
  older ones save them and the rule silently does nothing. The preview flags all of these either way.
- **Custom rules completely replace the predefined rules for that domain**
  (`content_rewrite_rules.go`, matched on the entry URL's domain without `www.`).

### Rules
| Rule | Args | What it does |
|---|---|---|
| `remove("sel")` | CSS selector group | Deletes matching elements (goquery/cascadia, supports `:has()`, `:not()`, attribute selectors). |
| `replace("re"\|"repl")` | Go RE2 regex, replacement (`$1` groups) | Regex-replaces over the content's raw HTML string. |
| `replace_title("re"\|"repl")` | same | Regex-replaces over the title. |
| `remove_clickbait` | – | Title-cases every word of the title. (It doesn't remove anything, despite the name.) |
| `remove_tables` | – | Unwraps table markup and keeps the cell contents. |
| `add_dynamic_image` | – | Promotes lazy-load attributes (`data-src`, `data-srcset`, …) to `src`/`srcset`. |
| `add_dynamic_iframe` | – | Same, for iframes. |
| `use_noscript_figure_images` | – | Uses the `<noscript>` image inside `<figure>` when the visible one is a placeholder. |
| `remove_img_blur_params` | – | Strips blur/low-quality params from image URLs. |
| `fix_medium_images` | – | Medium image markup fix. |
| `fix_ghost_cards` | – | Cleans up Ghost `figure.kg-card` bookmark cards. |
| `add_image_title` | – | Shows each `img` title attribute as a caption (for comics). |
| `add_mailto_subject` | – | Shows mailto subject text (qwantz). |
| `add_youtube_video`, `add_youtube_video_from_id`, `add_invidious_video`, `add_youtube_video_using_invidious_player` | – | Embed video players. |
| `add_castopod_episode`, `add_enclosure_links` | – | Podcast/enclosure helpers. |
| `add_hn_links_using_hack`, `add_hn_links_using_opener` | – | Rewrites HN links to open in the Hack or Opener apps. |
| `nl2br`, `convert_text_link(s)` | – | Plain-text feeds: newlines to `<br>`, bare URLs to links. |
| `base64_decode("sel")` | optional selector (default `body`) | Decodes base64 text content. |
| `add_pdf_download_link` | – | Always applied automatically. |

### Patterns
- Substack: `remove("p.button-wrapper, div.subscription-widget-wrap, div.subscription-widget-wrap-editor, div.captioned-button-wrap")`.
  Verify the class names against `-boilerplate` output, because they drift.
- WordPress "The post X appeared first on Y": `replace("<p>The post <a[^>]*>[^<]*</a> appeared first on <a[^>]*>[^<]*</a>\\.</p>"|"")`.
- Remove a container by the text inside it: `remove("p:has(a[href*='/subscribe'])")` or
  `remove("div:has(h3:contains('Related'))")`. Relative selectors like `:has(> x)` are **not supported** by cascadia (the selector is invalid). `:contains()` is case-insensitive; `:containsOwn()` also exists. `:matches(regex)` takes the regex **unquoted**, e.g. `p:matches(^\\s*—\\s*$)`; a quoted regex silently never matches.
- Truncating everything after a footer marker (`replace("<hr[^>]*>\\s*<p>Support[\\s\\S]*$"|"")`) is
  powerful but risky. Always check the preview's text-size change across many entries.

## Scraper rules (`scraper_rules`)
- Only used when `crawler` is true. The value is a CSS selector group, and the outer HTML of **every**
  match is concatenated in document order. Empty means predefined scraper rules for the domain, if any,
  otherwise readability's automatic extraction.
- Pick the article body container (`article .entry-content`, `div.post-body`). Add header images with
  a comma group if needed (`.post-hero img, .entry-content`). Then use rewrite rules to remove what
  survives inside.
- The server scrapes from wherever it's hosted, but the preview scrapes from the local machine. A site
  that blocks one and not the other gives different results, so watch for `SCRAPE ERROR`.

## Filters (`block_filter_entry_rules`, `keep_filter_entry_rules`)
- One rule per line: `Field=RE2 regex`. The fields are `EntryTitle`, `EntryURL`, `EntryCommentsURL`,
  `EntryContent`, `EntryAuthor`, `EntryTag` (matches any tag) and `EntryDate`.
- `EntryDate` takes `future`, `before:YYYY-MM-DD`, `after:YYYY-MM-DD`,
  `between:YYYY-MM-DD,YYYY-MM-DD` or `max-age:<duration>` (e.g. `max-age:30d`).
- Use `(?i)` for case-insensitive matching: `EntryTitle=(?i)^sponsored`.
- Block wins. If any keep rule exists, entries matching **no** keep rule are blocked.
- User-level rules (Settings) are combined with feed rules. The preview header shows them.
- Filters see the feed's content *before* rewriting, so match on the original text.
- `blocklist_rules`/`keeplist_rules` are legacy single-regex fields. Prefer the `*_filter_entry_rules`
  fields, and mention it if a feed still uses the legacy ones.

## URL rewrite (`urlrewrite_rules`)
A single `rewrite("regex"|"replacement")` applied to entry URLs (for example, to go from AMP to canonical URLs, or to swap in a
mirror domain). It changes which page the scraper fetches.
