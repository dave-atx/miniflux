// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/timezone"
)

// feedQueryBuilder builds a SQL query to fetch feeds.
type feedQueryBuilder struct {
	db                *sql.DB
	args              []any
	conditions        []string
	sortExpressions   []string
	limit             int
	offset            int
	withCounters      bool
	counterJoinFeeds  bool
	counterArgs       []any
	counterConditions []string
	fieldSet          model.FieldSet
}

// WithFields restricts which columns are fetched; see model.FieldSet.
func (f *feedQueryBuilder) WithFields(fields model.FieldSet) *feedQueryBuilder {
	f.fieldSet = fields
	return f
}

// NewFeedQueryBuilder returns a new FeedQueryBuilder.
func (s *Storage) NewFeedQueryBuilder(userID int64) *feedQueryBuilder {
	return &feedQueryBuilder{
		db:                s.db,
		args:              []any{userID},
		conditions:        []string{"f.user_id = $1"},
		counterArgs:       []any{userID, model.EntryStatusRead, model.EntryStatusUnread},
		counterConditions: []string{"e.user_id = $1", "e.status IN ($2, $3)"},
	}
}

// WithCategoryID filter by category ID.
func (f *feedQueryBuilder) WithCategoryID(categoryID int64) *feedQueryBuilder {
	if categoryID > 0 {
		f.conditions = append(f.conditions, "f.category_id = $"+strconv.Itoa(len(f.args)+1))
		f.args = append(f.args, categoryID)
		f.counterConditions = append(f.counterConditions, "f.category_id = $"+strconv.Itoa(len(f.counterArgs)+1))
		f.counterArgs = append(f.counterArgs, categoryID)
		f.counterJoinFeeds = true
	}
	return f
}

// WithFeedID filter by feed ID.
func (f *feedQueryBuilder) WithFeedID(feedID int64) *feedQueryBuilder {
	if feedID > 0 {
		f.conditions = append(f.conditions, "f.id = $"+strconv.Itoa(len(f.args)+1))
		f.args = append(f.args, feedID)
	}
	return f
}

// WithCounters let the builder return feeds with counters of statuses of entries.
func (f *feedQueryBuilder) WithCounters() *feedQueryBuilder {
	f.withCounters = true
	return f
}

// WithSorting add a sort expression.
func (f *feedQueryBuilder) WithSorting(column, direction string) *feedQueryBuilder {
	switch {
	case strings.EqualFold(direction, "ASC"):
		f.sortExpressions = append(f.sortExpressions, pq.QuoteIdentifier(column)+" ASC")
	case strings.EqualFold(direction, "DESC"):
		f.sortExpressions = append(f.sortExpressions, pq.QuoteIdentifier(column)+" DESC")
	}

	return f
}

// WithLimit set the limit.
func (f *feedQueryBuilder) WithLimit(limit int) *feedQueryBuilder {
	f.limit = limit
	return f
}

// WithOffset set the offset.
func (f *feedQueryBuilder) WithOffset(offset int) *feedQueryBuilder {
	f.offset = offset
	return f
}

func (f *feedQueryBuilder) buildCondition() string {
	return strings.Join(f.conditions, " AND ")
}

func (f *feedQueryBuilder) buildCounterCondition() string {
	return strings.Join(f.counterConditions, " AND ")
}

func (f *feedQueryBuilder) buildSorting() string {
	var parts string

	if len(f.sortExpressions) > 0 {
		parts += " ORDER BY " + strings.Join(f.sortExpressions, ", ")
	}

	if len(parts) > 0 {
		parts += ", lower(f.title) ASC"
	}

	if f.limit > 0 {
		parts += " LIMIT " + strconv.Itoa(f.limit)
	}

	if f.offset > 0 {
		parts += " OFFSET " + strconv.Itoa(f.offset)
	}

	return parts
}

// GetFeed returns a single feed that match the condition.
func (f *feedQueryBuilder) GetFeed() (*model.Feed, error) {
	f.limit = 1
	feeds, err := f.GetFeeds()
	if err != nil {
		return nil, err
	}

	if len(feeds) != 1 {
		return nil, nil
	}

	return feeds[0], nil
}

// GetFeeds returns a list of feeds that match the condition.
func (f *feedQueryBuilder) GetFeeds() (model.Feeds, error) {
	// Columns not going through fieldColumn must always be fetched: they are
	// either used by post-scan code, ORDER BY targets, or sort keys used by
	// byStateAndName (disabled, parsing_error_count, title).
	fs := f.fieldSet
	query := `
		SELECT
			f.id,
			` + fieldColumn(fs, "feed_url", "f.feed_url", "''") + `,
			` + fieldColumn(fs, "site_url", "f.site_url", "''") + `,
			f.title,
			` + fieldColumn(fs, "description", "f.description", "''") + `,
			` + fieldColumn(fs, "language", "f.language", "''") + `,
			` + fieldColumn(fs, "etag_header", "f.etag_header", "''") + `,
			` + fieldColumn(fs, "last_modified_header", "f.last_modified_header", "''") + `,
			f.user_id,
			` + fieldColumn(fs, "checked_at", "f.checked_at at time zone u.timezone", "'0001-01-01'::timestamp") + `,
			` + fieldColumn(fs, "next_check_at", "f.next_check_at at time zone u.timezone", "'0001-01-01'::timestamp") + `,
			f.parsing_error_count,
			` + fieldColumn(fs, "parsing_error_message", "f.parsing_error_msg", "''") + `,
			` + fieldColumn(fs, "scraper_rules", "f.scraper_rules", "''") + `,
			` + fieldColumn(fs, "rewrite_rules", "f.rewrite_rules", "''") + `,
			` + fieldColumn(fs, "urlrewrite_rules", "f.url_rewrite_rules", "''") + `,
			` + fieldColumn(fs, "blocklist_rules", "f.blocklist_rules", "''") + `,
			` + fieldColumn(fs, "keeplist_rules", "f.keeplist_rules", "''") + `,
			` + fieldColumn(fs, "block_filter_entry_rules", "f.block_filter_entry_rules", "''") + `,
			` + fieldColumn(fs, "keep_filter_entry_rules", "f.keep_filter_entry_rules", "''") + `,
			` + fieldColumn(fs, "crawler", "f.crawler", "false") + `,
			` + fieldColumn(fs, "user_agent", "f.user_agent", "''") + `,
			` + fieldColumn(fs, "cookie", "f.cookie", "''") + `,
			` + fieldColumn(fs, "username", "f.username", "''") + `,
			` + fieldColumn(fs, "password", "f.password", "''") + `,
			` + fieldColumn(fs, "ignore_http_cache", "f.ignore_http_cache", "false") + `,
			` + fieldColumn(fs, "allow_self_signed_certificates", "f.allow_self_signed_certificates", "false") + `,
			` + fieldColumn(fs, "fetch_via_proxy", "f.fetch_via_proxy", "false") + `,
			f.disabled,
			` + fieldColumn(fs, "no_media_player", "f.no_media_player", "false") + `,
			` + fieldColumn(fs, "hide_globally", "f.hide_globally", "false") + `,
			f.category_id,
			c.title as category_title,
			` + fieldColumn(fs, "category", "c.hide_globally as category_hidden", "false") + `,
			` + fieldColumn(fs, "icon", "fi.icon_id", "NULL::bigint") + `,
			` + fieldColumn(fs, "icon", "i.external_id", "NULL::text") + `,
			u.timezone,
			` + fieldColumn(fs, "apprise_service_urls", "f.apprise_service_urls", "''") + `,
			` + fieldColumn(fs, "webhook_url", "f.webhook_url", "''") + `,
			` + fieldColumn(fs, "disable_http2", "f.disable_http2", "false") + `,
			` + fieldColumn(fs, "ntfy_enabled", "f.ntfy_enabled", "false") + `,
			` + fieldColumn(fs, "ntfy_priority", "f.ntfy_priority", "0") + `,
			` + fieldColumn(fs, "ntfy_topic", "f.ntfy_topic", "''") + `,
			` + fieldColumn(fs, "pushover_enabled", "f.pushover_enabled", "false") + `,
			` + fieldColumn(fs, "pushover_priority", "f.pushover_priority", "0") + `,
			` + fieldColumn(fs, "proxy_url", "f.proxy_url", "''") + `,
			` + fieldColumn(fs, "ignore_entry_updates", "f.ignore_entry_updates", "false") + `
		FROM
			feeds f
		LEFT JOIN
			categories c ON c.id=f.category_id
		LEFT JOIN
			feed_icons fi ON fi.feed_id=f.id
		LEFT JOIN
			icons i ON i.id=fi.icon_id
		LEFT JOIN
			users u ON u.id=f.user_id
		WHERE %s
		%s
	`

	query = fmt.Sprintf(query, f.buildCondition(), f.buildSorting())

	readCounters, unreadCounters, err := f.fetchFeedCounter()
	if err != nil {
		return nil, err
	}

	rows, err := f.db.Query(query, f.args...)
	if err != nil {
		return nil, fmt.Errorf(`store: unable to fetch feeds: %w`, err)
	}
	defer rows.Close()

	feeds := make(model.Feeds, 0)
	for rows.Next() {
		var feed model.Feed
		var iconID sql.NullInt64
		var externalIconID sql.NullString
		var tz string
		feed.Category = &model.Category{}

		err := rows.Scan(
			&feed.ID,
			&feed.FeedURL,
			&feed.SiteURL,
			&feed.Title,
			&feed.Description,
			&feed.Language,
			&feed.EtagHeader,
			&feed.LastModifiedHeader,
			&feed.UserID,
			&feed.CheckedAt,
			&feed.NextCheckAt,
			&feed.ParsingErrorCount,
			&feed.ParsingErrorMsg,
			&feed.ScraperRules,
			&feed.RewriteRules,
			&feed.UrlRewriteRules,
			&feed.BlocklistRules,
			&feed.KeeplistRules,
			&feed.BlockFilterEntryRules,
			&feed.KeepFilterEntryRules,
			&feed.Crawler,
			&feed.UserAgent,
			&feed.Cookie,
			&feed.Username,
			&feed.Password,
			&feed.IgnoreHTTPCache,
			&feed.AllowSelfSignedCertificates,
			&feed.FetchViaProxy,
			&feed.Disabled,
			&feed.NoMediaPlayer,
			&feed.HideGlobally,
			&feed.Category.ID,
			&feed.Category.Title,
			&feed.Category.HideGlobally,
			&iconID,
			&externalIconID,
			&tz,
			&feed.AppriseServiceURLs,
			&feed.WebhookURL,
			&feed.DisableHTTP2,
			&feed.NtfyEnabled,
			&feed.NtfyPriority,
			&feed.NtfyTopic,
			&feed.PushoverEnabled,
			&feed.PushoverPriority,
			&feed.ProxyURL,
			&feed.IgnoreEntryUpdates,
		)
		if err != nil {
			return nil, fmt.Errorf(`store: unable to fetch feeds row: %w`, err)
		}

		if iconID.Valid && externalIconID.Valid {
			feed.Icon = &model.FeedIcon{FeedID: feed.ID, IconID: iconID.Int64, ExternalIconID: externalIconID.String}
		} else {
			feed.Icon = &model.FeedIcon{FeedID: feed.ID, IconID: 0, ExternalIconID: ""}
		}

		if readCounters != nil {
			if count, found := readCounters[feed.ID]; found {
				feed.ReadCount = count
			}
		}
		if unreadCounters != nil {
			if count, found := unreadCounters[feed.ID]; found {
				feed.UnreadCount = count
			}
		}

		feed.NumberOfVisibleEntries = feed.ReadCount + feed.UnreadCount
		feed.CheckedAt = timezone.Convert(tz, feed.CheckedAt)
		feed.NextCheckAt = timezone.Convert(tz, feed.NextCheckAt)
		feed.Category.UserID = feed.UserID
		feeds = append(feeds, &feed)
	}

	return feeds, nil
}

func (f *feedQueryBuilder) fetchFeedCounter() (unreadCounters map[int64]int, readCounters map[int64]int, err error) {
	if !f.withCounters {
		return nil, nil, nil
	}
	query := `
		SELECT
			e.feed_id,
			e.status,
			count(*)
		FROM
			entries e
		%s
		WHERE
			%s
		GROUP BY
			e.feed_id, e.status
	`
	join := ""
	if f.counterJoinFeeds {
		join = "INNER JOIN feeds f ON f.id=e.feed_id"
	}
	query = fmt.Sprintf(query, join, f.buildCounterCondition())

	rows, err := f.db.Query(query, f.counterArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf(`store: unable to fetch feed counts: %w`, err)
	}
	defer rows.Close()

	readCounters = make(map[int64]int)
	unreadCounters = make(map[int64]int)
	for rows.Next() {
		var feedID int64
		var status string
		var count int
		if err := rows.Scan(&feedID, &status, &count); err != nil {
			return nil, nil, fmt.Errorf(`store: unable to fetch feed counter row: %w`, err)
		}

		switch status {
		case model.EntryStatusRead:
			readCounters[feedID] = count
		case model.EntryStatusUnread:
			unreadCounters[feedID] = count
		}
	}

	return readCounters, unreadCounters, nil
}
