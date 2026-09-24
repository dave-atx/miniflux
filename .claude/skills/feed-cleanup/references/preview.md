# Previewing rules

## Contents
- How the preview works
- Fidelity: matching the server
- Flags
- Reading the output
- Server support

## How the preview works

`mf preview ID` runs `cmd/rewrite-preview` from a Miniflux source checkout. The tool reads the feed's
saved settings and the user's global filters through the API, fetches the feed itself, and runs each
entry through the same code the server uses (filters, URL cleanup and rewrite, scraper, rewrite rules,
sanitizer) twice: once with the saved settings ("before") and once with the proposal ("after"). It
never writes to the server.

It validates the proposal with the server's own validators first, plus a lint for selectors that are
valid but can't match, and exits with status 2 if anything fails.

`mf` builds the binary once per checkout commit and caches it under `${XDG_CACHE_HOME:-~/.cache}/feed-cleanup/`.
A checkout with uncommitted changes under `cmd/`, `internal/` or `go.mod` is rebuilt on every run.

## Fidelity: matching the server

The preview runs the checkout's code, not the server's. The header prints the server's version and
commit; compare it with the checkout (`git -C "$MINIFLUX_SRC" log -1 --format=%h`). When they differ,
rule syntax and predefined rules may differ too. Say so when presenting results, and prefer a
checkout at the server's commit or release tag.

Other differences that are expected:

- The preview fetches the feed from the local machine; the server fetches from where it's hosted.
  Sites that block one of them give different results (watch for `SCRAPE ERROR`).
- The preview scrapes every entry when `crawler` is on, as a forced refresh does. A normal refresh
  only scrapes new entries.

## Flags

Pass these after the feed ID: `mf preview ID [flags]`.

| Flag | Meaning |
|---|---|
| `-proposal DIR` | Proposed values, one file per API field (`rewrite_rules`, `scraper_rules`, `urlrewrite_rules`, `block_filter_entry_rules`, `keep_filter_entry_rules`, `crawler`). Fields without a file keep their saved value. |
| `-rewrite`, `-scraper`, `-url-rewrite`, `-block`, `-keep`, `-crawler` | Override one field directly; `@path` reads the value from a file, `''` clears it. These win over `-proposal`. |
| `-limit N` (10), `-match RE` | Which entries to process. The regex matches title or URL, case-insensitively. |
| `-boilerplate`, `-min-share F` (0.3) | Instead of the per-entry diff, list text blocks repeated across entries after the proposed rules, with selector hints. |
| `-out DIR` | Write `NN-raw.html` (pre-rewrite, with classes and ids), `NN-before.html` and `NN-after.html` per entry. |
| `-dump`, `-dump-max N` (6000) | Print each entry's raw pre-rewrite HTML. Prefer `-out` and grep for anything large. |
| `-all-blocks` | Also list unchanged text blocks. |
| `-feed-url URL` | Fetch a different URL than the saved feed URL. |

## Reading the output

Header: feed, server version, feed and site URLs, entry count, **predefined rules** for the domain,
each setting that differs between before and after, and user-level filters.

Per entry:

```
[3] Entry title
    https://example.com/post
    text: 4210 -> 3876 chars (-7.9%), images 3 -> 2
    - Subscribe to our newsletter Sign up
    + ...
```

- `-` lines are text blocks the proposal removes, `+` lines are blocks it adds.
- `title:` and `url:` lines appear when the proposal changes them.
- `blocked: before=kept after=BLOCKED(before_scrape)` shows filter changes; `after_scrape` means the
  block matched only on scraped content.
- Warnings: `SCRAPE ERROR`, `content is EMPTY after rules`, `more than half of the text was removed`.

The last line totals text size before and after across all previewed entries.

## Server support

Two server features make the workflow safer; both are newer than some Miniflux releases.

- **Forced refresh in the API** (`PUT /v1/feeds/{id}/refresh?force=true`). Without it the parameter is
  ignored and a refresh only processes new entries, so stored entries keep their old content.
- **Selector validation at save time.** Without it, an invalid selector in `remove()`,
  `base64_decode()` or scraper rules saves fine and does nothing. The preview catches these anyway, so
  this only matters for rules saved some other way.

Check the server version with `mf version`, and when a feature is missing, tell the user what they
won't get rather than failing.

A server-side preview endpoint would remove the need for a source checkout; until one exists, the
preview depends on `cmd/rewrite-preview`.
