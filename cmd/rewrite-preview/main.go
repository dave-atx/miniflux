// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command rewrite-preview fetches a feed and runs it through the same
// entry-processing pipeline as the server (filters, URL rewrite, scraper,
// content rewrite, sanitizer) twice: once with the feed's saved settings
// ("before") and once with proposed overrides ("after"). It prints a per-entry
// report of what changed so rules can be checked before they are saved.
//
// Usage:
//
//	go run ./cmd/rewrite-preview -feed-id 42 -rewrite 'remove(".newsletter")'
//	go run ./cmd/rewrite-preview -feed-url https://example.org/feed -dump -out /tmp/x
//
// With -feed-id, the feed's settings and the user's global filter rules are
// read from the API using MINIFLUX_URL (default https://miniflux.marquard.org)
// and MINIFLUX_API_KEY.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	miniflux "miniflux.app/v2/client"
	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/reader/fetcher"
	"miniflux.app/v2/internal/reader/filter"
	"miniflux.app/v2/internal/reader/parser"
	"miniflux.app/v2/internal/reader/processor"
	"miniflux.app/v2/internal/reader/rewrite"
	"miniflux.app/v2/internal/reader/sanitizer"
	"miniflux.app/v2/internal/reader/scraper"
	"miniflux.app/v2/internal/reader/urlcleaner"
	"miniflux.app/v2/internal/validator"
)

// optString is a flag that records whether it was set, so an explicit empty
// value ("clear this setting") differs from "keep the saved value".
type optString struct {
	set   bool
	value string
}

func (o *optString) String() string { return o.value }

// Set accepts "@path" to read the value from a file, which sidesteps shell
// quoting for rules that contain both kinds of quotes.
func (o *optString) Set(v string) error {
	if path, ok := strings.CutPrefix(v, "@"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		v = strings.TrimRight(string(data), "\n")
	}
	o.set, o.value = true, v
	return nil
}

type settings struct {
	feed      model.Feed
	userBlock string
	userKeep  string
}

type result struct {
	blocked      bool
	blockedStage string
	title        string
	url          string
	raw          string // content before rewrite rules (after scraping, if any)
	rewritten    string // content after rewrite rules, before the sanitizer
	content      string // final sanitized content
	scrapeErr    error
}

func main() {
	var (
		feedID                                                               = flag.Int64("feed-id", 0, "Miniflux feed ID (reads settings via the API)")
		feedURL                                                              = flag.String("feed-url", "", "feed URL to fetch (overrides the saved one; required without -feed-id)")
		limit                                                                = flag.Int("limit", 10, "maximum number of entries to process")
		match                                                                = flag.String("match", "", "only entries whose title or URL match this case-insensitive regex")
		outDir                                                               = flag.String("out", "", "directory to write per-entry raw/before/after HTML files")
		dump                                                                 = flag.Bool("dump", false, "print each entry's raw pre-rewrite HTML (what selectors match against)")
		dumpMax                                                              = flag.Int("dump-max", 6000, "truncate each -dump entry to this many bytes (0 = no limit)")
		showBlocks                                                           = flag.Bool("all-blocks", false, "list unchanged text blocks too, not just removed/added ones")
		boiler                                                               = flag.Bool("boilerplate", false, "report text blocks repeated across entries (after proposed rules), with selector hints")
		minShare                                                             = flag.Float64("min-share", 0.3, "with -boilerplate: minimum fraction of entries a block must appear in")
		rewriteOpt, scraperOpt, urlRewriteOpt, blockOpt, keepOpt, crawlerOpt optString
	)
	flag.Var(&rewriteOpt, "rewrite", "proposed content rewrite rules (@file reads them from a file; same for the flags below)")
	flag.Var(&scraperOpt, "scraper", "proposed scraper rules (CSS selector)")
	flag.Var(&urlRewriteOpt, "url-rewrite", "proposed URL rewrite rule")
	flag.Var(&blockOpt, "block", "proposed feed block filter rules (newline separated)")
	flag.Var(&keepOpt, "keep", "proposed feed keep filter rules (newline separated)")
	flag.Var(&crawlerOpt, "crawler", "proposed crawler (fetch original content) setting: true or false")
	flag.Parse()

	// Keep the processing packages quiet unless something is really wrong.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	opts, err := config.NewConfigParser().ParseEnvironmentVariables()
	if err != nil {
		fatalf("config: %v", err)
	}
	config.Opts = opts

	before, err := loadSettings(*feedID, *feedURL)
	if err != nil {
		fatalf("%v", err)
	}

	after := before
	after.feed.RewriteRules = pick(rewriteOpt, before.feed.RewriteRules)
	after.feed.ScraperRules = pick(scraperOpt, before.feed.ScraperRules)
	after.feed.UrlRewriteRules = pick(urlRewriteOpt, before.feed.UrlRewriteRules)
	after.feed.BlockFilterEntryRules = pick(blockOpt, before.feed.BlockFilterEntryRules)
	after.feed.KeepFilterEntryRules = pick(keepOpt, before.feed.KeepFilterEntryRules)
	if crawlerOpt.set {
		after.feed.Crawler = crawlerOpt.value == "true" || crawlerOpt.value == "1"
	}

	if problems := validate(after.feed, rewriteOpt, scraperOpt, urlRewriteOpt, blockOpt, keepOpt); len(problems) > 0 {
		fmt.Println("INVALID proposed settings (fix before saving):")
		for _, p := range problems {
			fmt.Println("  -", p)
		}
		os.Exit(2)
	}

	entries, err := fetchEntries(&before.feed)
	if err != nil {
		fatalf("fetch feed: %v", err)
	}

	var matchRe *regexp.Regexp
	if *match != "" {
		matchRe = regexp.MustCompile("(?i)" + *match)
	}
	var selected model.Entries
	for _, e := range entries {
		if matchRe != nil && !matchRe.MatchString(e.Title) && !matchRe.MatchString(e.URL) {
			continue
		}
		selected = append(selected, e)
		if len(selected) >= *limit {
			break
		}
	}

	printHeader(before, after, entries, len(selected))

	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			fatalf("%v", err)
		}
	}

	cache := map[string]scrapeResult{}
	var totalBefore, totalAfter int
	var afterResults []result
	for i, orig := range selected {
		b := process(before, orig, cache)
		a := process(after, orig, cache)
		afterResults = append(afterResults, a)
		if !*boiler {
			report(i+1, orig, b, a, *showBlocks)
		}
		totalBefore += textLen(b)
		totalAfter += textLen(a)

		if *dump {
			raw := a.raw
			if *dumpMax > 0 && len(raw) > *dumpMax {
				raw = raw[:*dumpMax] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(a.raw))
			}
			fmt.Printf("    --- raw HTML (pre-rewrite, as selectors see it) ---\n%s\n    --- end raw ---\n", raw)
		}
		if *outDir != "" {
			prefix := filepath.Join(*outDir, fmt.Sprintf("%02d", i+1))
			writeFile(prefix+"-raw.html", a.raw)
			writeFile(prefix+"-before.html", b.content)
			writeFile(prefix+"-after.html", a.content)
		}
	}

	if *boiler {
		reportBoilerplate(afterResults, *minShare)
		return
	}

	fmt.Printf("\nTOTAL text: before %d chars, after %d chars (%s)\n", totalBefore, totalAfter, pct(totalBefore, totalAfter))
	if *outDir != "" {
		fmt.Printf("HTML written to %s (NN-raw/before/after.html)\n", *outDir)
	}
}

func pick(o optString, saved string) string {
	if o.set {
		return o.value
	}
	return saved
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "rewrite-preview: "+format+"\n", args...)
	os.Exit(1)
}

func loadSettings(feedID int64, feedURL string) (settings, error) {
	var s settings
	if feedID == 0 {
		if feedURL == "" {
			return s, fmt.Errorf("either -feed-id or -feed-url is required")
		}
		s.feed.FeedURL = feedURL
		s.feed.SiteURL = feedURL
		return s, nil
	}

	baseURL := os.Getenv("MINIFLUX_URL")
	if baseURL == "" {
		baseURL = "https://miniflux.marquard.org"
	}
	apiKey := os.Getenv("MINIFLUX_API_KEY")
	if apiKey == "" {
		return s, fmt.Errorf("MINIFLUX_API_KEY is not set")
	}
	client := miniflux.NewClient(baseURL, apiKey)

	f, err := client.Feed(feedID)
	if err != nil {
		return s, fmt.Errorf("get feed %d: %w", feedID, err)
	}
	me, err := client.Me()
	if err != nil {
		return s, fmt.Errorf("get user: %w", err)
	}

	s.feed = model.Feed{
		ID:                          f.ID,
		Title:                       f.Title,
		FeedURL:                     f.FeedURL,
		SiteURL:                     f.SiteURL,
		ScraperRules:                f.ScraperRules,
		RewriteRules:                f.RewriteRules,
		UrlRewriteRules:             f.UrlRewriteRules,
		BlocklistRules:              f.BlocklistRules,
		KeeplistRules:               f.KeeplistRules,
		BlockFilterEntryRules:       f.BlockFilterEntryRules,
		KeepFilterEntryRules:        f.KeepFilterEntryRules,
		Crawler:                     f.Crawler,
		UserAgent:                   f.UserAgent,
		Cookie:                      f.Cookie,
		ProxyURL:                    f.ProxyURL,
		AllowSelfSignedCertificates: f.AllowSelfSignedCertificates,
		DisableHTTP2:                f.DisableHTTP2,
	}
	if feedURL != "" {
		s.feed.FeedURL = feedURL
	}
	s.userBlock = me.BlockFilterEntryRules
	s.userKeep = me.KeepFilterEntryRules
	return s, nil
}

func validate(f model.Feed, rw, sc, urw, block, keep optString) []string {
	var problems []string
	if rw.set {
		for _, e := range rewrite.ValidateRules(f.RewriteRules) {
			msg := fmt.Sprintf("rewrite: %s", e.Message)
			if e.Rule != "" {
				msg += fmt.Sprintf(" (rule %q)", e.Rule)
			}
			if e.Token != "" {
				msg += fmt.Sprintf(" (token %s at column %d)", e.Token, e.Pos.Column)
			}
			problems = append(problems, msg)
		}
	}
	if rw.set {
		problems = append(problems, checkRuleArgs(f.RewriteRules)...)
	}
	if sc.set && f.ScraperRules != "" {
		if _, err := cascadia.ParseGroup(f.ScraperRules); err != nil {
			problems = append(problems, fmt.Sprintf("scraper: selector %q is invalid: %v", f.ScraperRules, err))
		}
	}
	if urw.set && !validator.IsValidRegex(f.UrlRewriteRules) {
		problems = append(problems, "url-rewrite: invalid rule")
	}
	if block.set && f.BlockFilterEntryRules != "" {
		if err := validator.IsValidFilterRules(f.BlockFilterEntryRules, "block"); err != nil {
			problems = append(problems, "block: "+err.Translate("en_US"))
		}
	}
	if keep.set && f.KeepFilterEntryRules != "" {
		if err := validator.IsValidFilterRules(f.KeepFilterEntryRules, "keep"); err != nil {
			problems = append(problems, "keep: "+err.Translate("en_US"))
		}
	}
	return problems
}

func requestBuilder(f *model.Feed) *fetcher.RequestBuilder {
	return fetcher.NewRequestBuilder().
		WithUserAgent(f.UserAgent, config.Opts.HTTPClientUserAgent()).
		WithCookie(f.Cookie).
		WithTimeout(config.Opts.HTTPClientTimeout()).
		WithCustomFeedProxyURL(f.ProxyURL).
		IgnoreTLSErrors(f.AllowSelfSignedCertificates).
		DisableHTTP2(f.DisableHTTP2)
}

func fetchEntries(f *model.Feed) (model.Entries, error) {
	rh := fetcher.NewResponseHandler(requestBuilder(f).ExecuteRequest(f.FeedURL))
	defer rh.Close()
	if lerr := rh.LocalizedError(); lerr != nil {
		return nil, lerr.Error()
	}
	body, lerr := rh.ReadBody(config.Opts.HTTPClientMaxBodySize())
	if lerr != nil {
		return nil, lerr.Error()
	}
	parsed, err := parser.ParseFeed(rh.EffectiveURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if f.SiteURL == f.FeedURL || f.SiteURL == "" {
		f.SiteURL = parsed.SiteURL
	}
	if f.Title == "" {
		f.Title = parsed.Title
	}
	return parsed.Entries, nil
}

type scrapeResult struct {
	baseURL string
	content string
	err     error
}

// process mirrors processor.ProcessFeedEntries for a single entry. Scraping is
// always attempted when the crawler is on, as on a forced refresh.
func process(s settings, orig *model.Entry, cache map[string]scrapeResult) result {
	entry := *orig
	feed := s.feed
	blockRules := filter.ParseRules(s.userBlock, feed.BlockFilterEntryRules)
	allowRules := filter.ParseRules(s.userKeep, feed.KeepFilterEntryRules)

	if filter.IsBlockedEntry(blockRules, allowRules, &feed, &entry) {
		return result{blocked: true, blockedStage: "before_scrape", title: entry.Title, url: entry.URL}
	}

	parsedFeedURL, _ := url.Parse(feed.FeedURL)
	parsedSiteURL, _ := url.Parse(feed.SiteURL)
	if u, err := url.Parse(entry.URL); err == nil {
		if cleaned, err := urlcleaner.RemoveTrackingParameters(parsedFeedURL, parsedSiteURL, u); err == nil {
			entry.URL = cleaned
		}
	}
	entry.URL = rewrite.RewriteEntryURL(&feed, &entry)

	var res result
	webpageBaseURL := ""
	scraped := false
	if feed.Crawler {
		key := entry.URL + "\x00" + feed.ScraperRules
		sr, ok := cache[key]
		if !ok {
			sr.baseURL, sr.content, sr.err = scraper.ScrapeWebsite(requestBuilder(&feed), entry.URL, feed.ScraperRules)
			cache[key] = sr
		}
		if sr.baseURL != "" {
			webpageBaseURL = sr.baseURL
		}
		if sr.err != nil {
			res.scrapeErr = sr.err
		} else if sr.content != "" {
			entry.Content = processor.MinifyContent(sr.content)
			scraped = true
		}
	}

	res.raw = entry.Content
	rewrite.ApplyContentRewriteRules(&entry, feed.RewriteRules)
	res.rewritten = entry.Content

	if scraped && filter.IsBlockedEntry(blockRules, allowRules, &feed, &entry) {
		res.blocked, res.blockedStage = true, "after_scrape"
	}
	if webpageBaseURL == "" {
		webpageBaseURL = entry.URL
	}
	res.content = sanitizer.SanitizeHTML(webpageBaseURL, entry.Content, &sanitizer.SanitizerOptions{})
	res.title = entry.Title
	res.url = entry.URL
	return res
}

func printHeader(before, after settings, entries model.Entries, n int) {
	f := before.feed
	if f.ID != 0 {
		fmt.Printf("Feed %d: %s\n", f.ID, f.Title)
	} else {
		fmt.Printf("Feed: %s\n", f.Title)
	}
	fmt.Printf("  feed_url: %s\n  site_url: %s\n", f.FeedURL, f.SiteURL)
	fmt.Printf("  entries in feed: %d, previewing: %d\n", len(entries), n)

	sample := f.SiteURL
	if len(entries) > 0 {
		sample = entries[0].URL
	}
	if p := rewrite.PredefinedRewriteRules(sample); p != "" {
		fmt.Printf("  PREDEFINED rewrite rules for this domain: %s\n", p)
		fmt.Println("    (custom rewrite rules REPLACE these; include them if still wanted)")
	}
	if p := scraper.PredefinedScraperRules(sample); p != "" {
		fmt.Printf("  PREDEFINED scraper rules for this domain: %s\n", p)
	}

	type field struct{ name, b, a string }
	fields := []field{
		{"crawler", fmt.Sprint(before.feed.Crawler), fmt.Sprint(after.feed.Crawler)},
		{"rewrite_rules", before.feed.RewriteRules, after.feed.RewriteRules},
		{"scraper_rules", before.feed.ScraperRules, after.feed.ScraperRules},
		{"urlrewrite_rules", before.feed.UrlRewriteRules, after.feed.UrlRewriteRules},
		{"block_filter_entry_rules", before.feed.BlockFilterEntryRules, after.feed.BlockFilterEntryRules},
		{"keep_filter_entry_rules", before.feed.KeepFilterEntryRules, after.feed.KeepFilterEntryRules},
		{"blocklist_rules (legacy)", before.feed.BlocklistRules, after.feed.BlocklistRules},
		{"keeplist_rules (legacy)", before.feed.KeeplistRules, after.feed.KeeplistRules},
	}
	fmt.Println("Settings (before = saved, after = proposed):")
	for _, fl := range fields {
		switch {
		case fl.b == fl.a && fl.b != "" && fl.b != "false":
			fmt.Printf("  %s: %q (unchanged)\n", fl.name, fl.b)
		case fl.b != fl.a:
			fmt.Printf("  %s:\n    before: %q\n    after:  %q\n", fl.name, fl.b, fl.a)
		}
	}
	if before.userBlock != "" || before.userKeep != "" {
		fmt.Printf("  user-level filters also apply: block=%q keep=%q\n", before.userBlock, before.userKeep)
	}
}

func report(n int, orig *model.Entry, b, a result, showAll bool) {
	fmt.Printf("\n[%d] %s\n    %s\n", n, orig.Title, orig.URL)
	if b.title != a.title && !a.blocked && !b.blocked {
		fmt.Printf("    title: %q -> %q\n", b.title, a.title)
	}
	if b.url != a.url && !a.blocked && !b.blocked {
		fmt.Printf("    url: %s -> %s\n", b.url, a.url)
	}
	if b.blocked || a.blocked {
		fmt.Printf("    blocked: before=%s after=%s\n", blockedLabel(b), blockedLabel(a))
		return
	}
	if a.scrapeErr != nil {
		fmt.Printf("    SCRAPE ERROR: %v\n", a.scrapeErr)
	}

	bl, al := textLen(b), textLen(a)
	fmt.Printf("    text: %d -> %d chars (%s)", bl, al, pct(bl, al))
	bi, ai := countTags(b.content, "img"), countTags(a.content, "img")
	if bi != ai {
		fmt.Printf(", images %d -> %d", bi, ai)
	}
	fmt.Println()
	switch {
	case al == 0 && bl > 0:
		fmt.Println("    WARNING: content is EMPTY after rules")
	case bl > 0 && al*2 < bl:
		fmt.Println("    WARNING: more than half of the text was removed; check the article body survived")
	}

	removed, added, same := diffBlocks(textBlocks(b.content), textBlocks(a.content))
	for _, s := range removed {
		fmt.Printf("    - %s\n", clip(s, 160))
	}
	for _, s := range added {
		fmt.Printf("    + %s\n", clip(s, 160))
	}
	if showAll {
		for _, s := range same {
			fmt.Printf("      %s\n", clip(s, 160))
		}
	}
	if len(removed) == 0 && len(added) == 0 {
		fmt.Println("    (no text changes)")
	}
}

func blockedLabel(r result) string {
	if r.blocked {
		return "BLOCKED(" + r.blockedStage + ")"
	}
	return "kept"
}

var blockTags = map[string]bool{
	"p": true, "div": true, "li": true, "ul": true, "ol": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "blockquote": true, "figure": true, "figcaption": true,
	"pre": true, "tr": true, "td": true, "th": true, "table": true, "br": true, "hr": true,
	"section": true, "article": true, "aside": true, "header": true, "footer": true, "dl": true,
	"dt": true, "dd": true, "details": true, "summary": true, "img": true, "iframe": true,
}

// textBlocks splits HTML into normalized text chunks at block boundaries.
// Images and iframes become "[img src]" / "[iframe src]" markers.
func textBlocks(content string) []string {
	var blocks []string
	var cur strings.Builder
	flush := func() {
		t := strings.Join(strings.Fields(cur.String()), " ")
		if t != "" {
			blocks = append(blocks, t)
		}
		cur.Reset()
	}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				flush()
				return blocks
			}
			flush()
			return blocks
		case html.TextToken:
			cur.Write(z.Text())
			cur.WriteByte(' ')
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			if !blockTags[tok.Data] {
				continue
			}
			flush()
			if tt != html.EndTagToken && (tok.Data == "img" || tok.Data == "iframe") {
				for _, attr := range tok.Attr {
					if attr.Key == "src" {
						blocks = append(blocks, "["+tok.Data+" "+attr.Val+"]")
					}
				}
			}
		}
	}
}

// diffBlocks does a multiset comparison, preserving order of appearance.
func diffBlocks(before, after []string) (removed, added, same []string) {
	count := map[string]int{}
	for _, s := range after {
		count[s]++
	}
	for _, s := range before {
		if count[s] > 0 {
			count[s]--
			same = append(same, s)
		} else {
			removed = append(removed, s)
		}
	}
	count = map[string]int{}
	for _, s := range before {
		count[s]++
	}
	for _, s := range after {
		if count[s] > 0 {
			count[s]--
		} else {
			added = append(added, s)
		}
	}
	return removed, added, same
}

func textLen(r result) int {
	if r.blocked {
		return 0
	}
	n := 0
	for _, b := range textBlocks(r.content) {
		if !strings.HasPrefix(b, "[img ") && !strings.HasPrefix(b, "[iframe ") {
			n += utf8.RuneCountInString(b)
		}
	}
	return n
}

func countTags(content, tag string) int {
	n := 0
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return n
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			if name, _ := z.TagName(); string(name) == tag {
				n++
			}
		}
	}
}

func pct(before, after int) string {
	if before == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+.1f%%", float64(after-before)*100/float64(before))
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func writeFile(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fatalf("%v", err)
	}
}
