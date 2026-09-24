---
name: feed-cleanup
description: Create, preview and apply Miniflux per-feed cleanup rules (content rewrite rules, title rewrites, scraper rules, block/keep filters) on Dave's instance at miniflux.marquard.org. Use when Dave wants to clean up a feed ("remove the newsletter box from X", "strip 'The post appeared first on'", "block sponsored posts", "fix the scraper for Y"), points at an ugly entry, or asks to audit which feeds need cleanup.
---

# Feed cleanup for Miniflux

This repo (branch `flyio`) is the exact code running on https://miniflux.marquard.org.
Every rule is **previewed with the real pipeline** before it's saved, and nothing is
written to the instance without Dave's explicit yes.

- API: `$MINIFLUX_URL` (default `https://miniflux.marquard.org`), header `X-Auth-Token: $MINIFLUX_API_KEY`.
  If the key isn't in the environment, run commands via `fish -c '...'` (it's a fish universal var).
- Preview tool: `go run ./cmd/rewrite-preview` from the repo root (flags below).
- Rule syntax, gotchas, filter fields: see [reference.md](reference.md). Read it before drafting rules.

## Entry points

Figure out which one Dave means, then go to the per-feed loop.

1. **Named feed** ("clean up Six Colors"): `GET /v1/feeds?fields=id,title,site_url,feed_url` and
   fuzzy-match title/URL. If ambiguous, ask.
2. **Annoying entry** (entry ID or `.../entry/123` / `.../feed/5/entry/123` URL): `GET /v1/entries/{id}`
   gives `feed_id`, `url`, `title`. Preview with `-match '<distinctive slug from the entry URL>'`.
   If the entry is no longer in the feed XML, preview the whole feed; the same junk usually recurs.
3. **Describe the junk** ("kill the 'Support us' box on X"): find the feed, then locate the junk with
   `-boilerplate` or `-dump -match ...` and find the tightest selector around it.
4. **Audit all feeds**: see [Audit mode](#audit-mode). Report only; fix nothing unasked.

## Per-feed loop

### 1. Gather
```sh
curl -s -H "X-Auth-Token: $MINIFLUX_API_KEY" "$MINIFLUX_URL/v1/feeds/$ID" | jq '{id,title,site_url,feed_url,crawler,rewrite_rules,scraper_rules,urlrewrite_rules,block_filter_entry_rules,keep_filter_entry_rules,blocklist_rules,keeplist_rules}'
```
Then run the preview with no overrides. Its header shows the saved settings and **any predefined
(built-in) rules for the domain**:
```sh
go run ./cmd/rewrite-preview -feed-id $ID -limit 10 -boilerplate
```

### 2. Diagnose
- `-boilerplate` lists text blocks repeated across entries with selector hints (`p.button-wrapper`,
  `div.subscription-widget ...`). This is the main signal for promo and footer cruft.
- `-dump -match <slug> -dump-max 0` prints one entry's raw pre-rewrite HTML (with classes and ids, which
  the sanitizer later strips) so you can find exact selectors. Prefer `-out <scratchpad dir>` and grep the
  `NN-raw.html` files over dumping large HTML into context.
- Dave's priorities, in order: **promo boilerplate** (subscribe/newsletter/support/Patreon/share
  buttons), then **related/footer cruft** ("Related posts", "The post X appeared first on Y", tag lists,
  comment counts). Also check titles and whether the feed is truncated (a short excerpt plus "Read more"
  means scraper territory; see reference.md).
- Content that's the *article itself* (inline links to other posts, author asides, podcast plugs that are
  the post's point) is not junk. When unsure, ask.

### 3. Draft
Choose the right tool for each problem:

| Problem | Tool |
|---|---|
| Element with a stable class/id | `remove("css selector")` |
| Text with no wrapping element ("appeared first on") | `replace("regex"\|"")` on content |
| Title prefix/suffix noise | `replace_title("regex"\|"")` |
| Whole entries unwanted (sponsored, daily digests, deals) | block filter (`EntryTitle=`, `EntryAuthor=`, …) |
| Feed is excerpt-only | `crawler: true`, and `scraper_rules` only if readability picks the wrong thing |

Drafting rules:
- **Custom rewrite rules replace the predefined ones.** If the header shows predefined rules, include
  them in the proposal unless Dave wants them gone.
- **Keep the saved rules.** You're proposing the complete new value of each field, not a delta. Start
  from the saved string and add to it.
- Prefer the most specific stable selector: classes that name the thing (`.subscription-widget`) over
  layout classes (`.wp-block-group`), and never positional selectors (`:nth-child`) or hashed class names.
- Anchor `replace` regexes so they can't eat the article: avoid a greedy `[\s\S]*$` unless the marker
  text is truly only in the footer, and check that in the preview.

### 4. Preview (mandatory)
Write each proposed field to a file in the scratchpad (rules mix `"` and `'`, so this avoids shell-quoting
trouble in fish and bash alike), then pass it with `@`:
```sh
go run ./cmd/rewrite-preview -feed-id $ID -limit 15 -rewrite @$SCRATCH/rules.txt \
  [-scraper @file] [-block @file] [-keep @file] [-crawler true]
```
- Only flags you pass are overridden; `-flag ''` clears a field. "before" = saved settings, "after" = proposed.
- Per entry, it shows removed (`-`) and added (`+`) text blocks, the change in text size, image count
  changes, title changes, and blocked/kept status.
- **Stop and rethink** on: `INVALID proposed settings` (exit 2); `WARNING: content is EMPTY`;
  `WARNING: more than half of the text was removed`; removed blocks that are real prose; any entry that
  goes from kept to BLOCKED that Dave would want.
- Re-run `-boilerplate` with the proposed flags to confirm the targeted blocks are gone and nothing
  new showed up.
- With `crawler` on, each entry is scraped live (slow). Keep `-limit` modest.

### 5. Confirm and apply
Show Dave, in this order:
1. The final value of each changed field, as a copyable code block.
2. A short summary of the preview: which boilerplate was removed, from how many entries, and any warnings.

Only after an explicit yes, send the update. Build the JSON with `jq` from the same files you previewed,
so exactly what was tested is what gets saved:
```sh
jq -n --rawfile rw $SCRATCH/rules.txt '{rewrite_rules: ($rw | rtrimstr("\n"))}' \
  | curl -s -X PUT -H "X-Auth-Token: $MINIFLUX_API_KEY" -H 'Content-Type: application/json' \
      --data @- "$MINIFLUX_URL/v1/feeds/$ID" | jq '{id, rewrite_rules}'
```
The fields are `rewrite_rules`, `scraper_rules`, `urlrewrite_rules`, `block_filter_entry_rules`,
`keep_filter_entry_rules` and `crawler`, and only the fields you send change. A 400 response means the
server's validation rejected the rules. Show the error and fix the rules; never retry blindly.

### 6. Reprocess existing entries
Ask whether to re-run the rules on stored entries, then:
```sh
curl -s -X PUT -H "X-Auth-Token: $MINIFLUX_API_KEY" "$MINIFLUX_URL/v1/feeds/$ID/refresh?force=true" -w '%{http_code}\n'
```
`force=true` bypasses the HTTP cache and reprocesses and re-scrapes entries already stored, like the
UI's Refresh link. Tell Dave the limitation: **only entries still in the feed's XML get reprocessed.**
Older entries keep their old content.

Finally, spot-check one reprocessed entry:
`GET /v1/entries?feed_id=$ID&limit=1&order=published_at&direction=desc` and confirm the junk is gone
from `content`.

## Audit mode
Goal: a ranked report of where rules would help most. **Report only.** Dave picks which feeds to fix, and
then you run the per-feed loop for each.

1. `GET /v1/feeds?fields=id,title,site_url,crawler,rewrite_rules,scraper_rules,block_filter_entry_rules`.
2. For each feed, run `go run ./cmd/rewrite-preview -feed-id $ID -limit 8 -boilerplate` (build once to a
   scratchpad binary with `go build -o <scratchpad>/rp ./cmd/rewrite-preview` to save time). Skip feeds
   that fail to fetch and list them at the end.
3. Score each feed by its repeated promo and footer blocks (count × share of entries), ignoring blocks
   that are clearly legitimate (bylines, recurring section headings Dave might want).
4. Report a table: feed, the worst offending blocks (with selector hints), a suggested approach in a few
   words, and whether the feed already has custom rules (and whether predefined rules are active).
   Also note feeds whose `crawler` scraping looks broken (`SCRAPE ERROR`, or tiny text).
