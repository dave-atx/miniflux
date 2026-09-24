---
name: feed-cleanup
description: Creates, previews and applies per-feed cleanup rules on a Miniflux instance (content rewrite rules, title rewrites, scraper rules, URL rewrites, block/keep filters) through the Miniflux API, checking every change against the real processing pipeline before anything is saved. Use whenever the user wants to clean up or fix a Miniflux feed ("remove the newsletter box from X", "strip 'The post appeared first on'", "block sponsored posts", "this feed is truncated", "fix the scraper for Y"), points at an ugly Miniflux entry, or asks which of their feeds need cleanup, even if they never say "rewrite rules".
---

# Miniflux feed cleanup

Rules are drafted, previewed against the real pipeline, shown to the user, and saved only after an
explicit yes. Miniflux rules fail quietly: a bad selector or an over-eager regex saves fine and then
either does nothing or eats the article. The preview is what catches that, so don't skip it.

## Setup

Run everything through the bundled helper `scripts/mf` in this skill's directory; `mf` below means
that script, called by its full path. It needs `curl`, `jq`, and two environment variables:

- `MINIFLUX_URL`: the instance base URL, e.g. `https://reader.example.com`.
- `MINIFLUX_API_KEY`: an API key (Settings > API Keys). Never print it or put it in a file.

If either is missing, ask the user how they want to provide it, then stop. Don't guess the URL.

Previews also need a Go toolchain and a Miniflux source checkout containing `cmd/rewrite-preview`
(`MINIFLUX_SRC`, or the current git repo). See [references/preview.md](references/preview.md) for
how the preview works, its flags, and how far to trust it when the checkout and server differ.

Keep working files (proposals, HTML dumps) in a scratch directory, `$SCRATCH` below: the session
scratchpad if there is one, otherwise `mktemp -d`.

`scripts/mf` with no arguments prints its commands. The ones used below:

```sh
mf version                 # server version and commit
mf feeds                   # all feeds, with which rule fields are set
mf feed ID                 # one feed's current rule fields
mf entry ID                # an entry's feed_id, title, url
mf preview ID [flags]      # run the preview tool against feed ID
mf apply ID DIR [--dry-run]  # save the rule files in DIR
mf refresh ID --force      # reprocess stored entries with the new rules
```

## Entry points

Work out which of these the user means, then run the per-feed loop.

1. **A named feed** ("clean up Six Colors"): `mf feeds`, match on title or URL. Ask if it's ambiguous.
2. **An annoying entry** (an entry ID, or a `/entry/123` or `/feed/5/entry/123` URL): `mf entry ID`
   gives the feed and URL. Preview with `-match '<distinctive slug from the entry URL>'`. If the entry has
   dropped out of the feed, preview the whole feed; the same junk usually recurs.
3. **Junk described in words** ("kill the 'Support us' box on X"): find the feed, then find the junk
   with `-boilerplate` or `-dump -match ...` and pick the tightest selector around it.
4. **An audit of all feeds**: see [Audit mode](#audit-mode). Report only; fix nothing unasked.

## Per-feed loop

Copy this checklist and keep it current while you work:

```
- [ ] 1. Gather: saved rules, predefined rules, baseline preview
- [ ] 2. Diagnose: what's junk, what's article
- [ ] 3. Draft: proposal files
- [ ] 4. Preview until clean
- [ ] 5. Confirm with the user, then apply
- [ ] 6. Reprocess and spot-check
```

### 1. Gather

```sh
mf feed $ID
mf preview $ID -limit 10 -boilerplate
```

The preview header shows the saved settings, the server version, and any **predefined rules** Miniflux
ships for the domain. Custom rewrite rules replace the predefined ones entirely, so note them now.

### 2. Diagnose

- `-boilerplate` lists text blocks repeated across entries, with selector hints. It's the main signal
  for promo and footer cruft.
- For exact selectors, write the raw pre-rewrite HTML to files and grep them, rather than dumping large
  HTML into context: `mf preview $ID -match <slug> -out $SCRATCH/html` then look at `NN-raw.html`.
  Raw HTML still has the classes and ids that the stored entry has lost to the sanitizer.
- Default priorities, unless the user says otherwise: promo boilerplate first (subscribe, newsletter,
  support / Patreon, share buttons), then related-posts and footer cruft ("Related posts", "The post X
  appeared first on Y", tag lists, comment counts). Also check titles, and whether the feed is
  excerpt-only (a short teaser plus "Read more" means scraper territory).
- Content that is the article itself is not junk: inline links to other posts, author asides, a podcast
  plug that is the point of the post. When unsure, ask.

### 3. Draft

Read [references/rules.md](references/rules.md) before writing rules; the syntax has traps (Go string
escapes, unquoted `:matches()`, no relative `:has(> x)`).

| Problem | Tool |
|---|---|
| Element with a stable class or id | `remove("css selector")` |
| Text with no wrapping element ("appeared first on") | `replace("regex"\|"")` |
| Noise in titles | `replace_title("regex"\|"")` |
| Whole entries unwanted (sponsored, daily digests, deals) | block filter (`EntryTitle=`, `EntryAuthor=`, ...) |
| Feed is excerpt-only | `crawler` true, plus `scraper_rules` only if readability picks the wrong thing |

Write the proposal as a directory with one file per API field you're changing: `rewrite_rules`,
`scraper_rules`, `urlrewrite_rules`, `block_filter_entry_rules`, `keep_filter_entry_rules`, `crawler`
(`true` / `false`). Both the preview (`-proposal DIR`) and `mf apply` read the same files, so what gets
previewed is exactly what gets saved, and shell quoting never touches the rules.

- **Each file is the complete new value**, not a delta. Start from the saved value (`mf feed $ID`) and
  add to it, or existing cleanup is lost.
- **Carry over predefined rewrite rules** shown in the preview header unless the user wants them gone.
- Prefer selectors that name the thing (`.subscription-widget`) over layout classes (`.wp-block-group`).
  Avoid positional selectors (`:nth-child`) and hashed class names; they break on the next redesign.
- Anchor `replace` regexes so they can't run into the article. A trailing `[\s\S]*$` is only safe when
  the marker text appears nowhere but the footer, and the preview has to prove it.

### 4. Preview

```sh
mf preview $ID -limit 15 -proposal $SCRATCH/proposal
mf preview $ID -limit 15 -proposal $SCRATCH/proposal -boilerplate
```

"before" is the saved settings and "after" is the proposal. Per entry it shows removed (`-`) and added
(`+`) text blocks, the text size change, image and title changes, and blocked / kept status. See
[references/preview.md](references/preview.md) for the full output and flags.

Go back to step 3 if you see any of:

- `INVALID proposed settings` (exit status 2): the server would reject these rules, or they would never match.
- `WARNING: content is EMPTY` or `more than half of the text was removed`.
- Removed blocks that are real prose.
- An entry that changes from kept to BLOCKED that the user would want to read.
- The second, `-boilerplate` run still showing the targeted blocks, or new ones.

With `crawler` on, every entry is scraped live, so keep `-limit` modest.

### 5. Confirm and apply

Show the user:

1. The final value of each changed field, as a copyable code block.
2. A short summary of the preview: which boilerplate went, from how many entries, and any warnings.

Only after an explicit yes:

```sh
mf apply $ID $SCRATCH/proposal
```

Only the fields with a file in the directory change. If the server answers 400, it rejected the rules:
show the user its error message and fix the rules. Don't retry with tweaks you haven't previewed.

### 6. Reprocess and spot-check

Saved rules apply to new entries. Ask whether to rerun them on stored entries too:

```sh
mf refresh $ID --force
```

A forced refresh bypasses the HTTP cache and reprocesses (and, with the crawler on, re-scrapes) stored
entries that are **still in the feed's XML**. Older entries keep their old content; tell the user that.
Servers without forced-refresh support in the API ignore `--force` and do a normal refresh, which only
affects new entries (see [references/preview.md](references/preview.md#server-support)).

Then spot-check the newest entry and confirm the junk is gone from its `content`:

```sh
mf get "/v1/entries?feed_id=$ID&limit=1&order=published_at&direction=desc" | jq -r '.entries[0].content'
```

## Audit mode

Goal: a ranked report of where rules would help most. Report only; the user picks which feeds to fix,
then you run the per-feed loop for each.

1. `mf feeds`.
2. For each feed, `mf preview $ID -limit 8 -boilerplate`. The binary is built once and cached, so this
   is fast after the first feed. Skip feeds that fail to fetch and list them at the end.
3. Score each feed by its repeated promo and footer blocks (count x share of entries), ignoring
   legitimate repeats like bylines and recurring section headings.
4. Report a table: feed, the worst blocks (with selector hints), a suggested fix in a few words, and
   whether the feed already has custom rules or predefined rules. Also flag feeds whose scraping looks
   broken (`SCRAPE ERROR`, or very little text), and feeds still using the legacy `blocklist_rules` /
   `keeplist_rules` fields.
