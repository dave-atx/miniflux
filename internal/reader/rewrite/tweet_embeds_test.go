// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package rewrite // import "miniflux.app/v2/internal/reader/rewrite"

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"miniflux.app/v2/internal/model"
)

const tweetVideoJSON = `{
	"mediaDetails": [{
		"type": "video",
		"url": "https://t.co/pXdUR21Gev",
		"media_url_https": "https://pbs.twimg.com/amplify_video_thumb/1/img/poster.jpg",
		"original_info": {"width": 896, "height": 1000},
		"video_info": {"variants": [
			{"content_type": "application/x-mpegURL", "url": "https://video.twimg.com/pl/playlist.m3u8"},
			{"bitrate": 632000, "content_type": "video/mp4", "url": "https://video.twimg.com/vid/320x356/low.mp4"},
			{"bitrate": 2176000, "content_type": "video/mp4", "url": "https://video.twimg.com/vid/720x802/high.mp4"},
			{"bitrate": 950000, "content_type": "video/mp4", "url": "https://video.twimg.com/vid/480x534/mid.mp4"}
		]}
	}]
}`

const tweetLinksJSON = `{
	"__typename": "Tweet",
	"entities": {"urls": [
		{"url": "https://t.co/luC5CdcWDy", "expanded_url": "https://alzresearch.org/giving/", "display_url": "alzresearch.org/giving/"},
		{"url": "https://t.co/zvNptfyuNd", "expanded_url": "https://alzfdn.org/donate/?a=1&b=2", "display_url": "alzfdn.org/donate/?a=1&…"},
		{"url": "https://t.co/unsafe1234", "expanded_url": "javascript:alert(1)", "display_url": "unsafe"}
	]}
}`

const tweetPhotosJSON = `{
	"mediaDetails": [
		{"type": "photo", "url": "https://t.co/yV1a1lbJcr", "media_url_https": "https://pbs.twimg.com/media/one.png", "ext_alt_text": "A \"quoted\" alt", "original_info": {"width": 160, "height": 160}},
		{"type": "photo", "url": "https://t.co/yV1a1lbJcr", "media_url_https": "https://pbs.twimg.com/media/two.jpg"}
	]
}`

func mockFetchTweet(t *testing.T, responses map[string]string) *int {
	t.Helper()

	calls := 0
	originalFetchTweet := fetchTweet
	fetchTweet = func(tweetID string) (*syndicationTweet, error) {
		calls++
		body, ok := responses[tweetID]
		if !ok {
			return nil, errors.New("not found")
		}
		var tweet syndicationTweet
		if err := json.Unmarshal([]byte(body), &tweet); err != nil {
			t.Fatalf("invalid test JSON: %v", err)
		}
		return &tweet, nil
	}

	clear(tweetCache.entries)
	t.Cleanup(func() {
		fetchTweet = originalFetchTweet
		clear(tweetCache.entries)
	})

	return &calls
}

func TestExpandTweetEmbedsVideo(t *testing.T) {
	mockFetchTweet(t, map[string]string{"2116296999297548615": tweetVideoJSON})

	input := `<p>Intro</p><blockquote class="twitter-tweet"><p lang="en" dir="ltr">Hello! <a href="https://t.co/pXdUR21Gev">pic.twitter.com/pXdUR21Gev</a></p>— Guilherme Rambo (@_inside) <a href="https://twitter.com/_inside/status/2116296999297548615?ref_src=twsrc%5Etfw">October 30, 2026</a></blockquote><script async src="https://platform.twitter.com/widgets.js"></script>`
	output := expandTweetEmbeds(input)

	expectedVideo := `<figure><video src="https://video.twimg.com/vid/720x802/high.mp4" poster="https://pbs.twimg.com/amplify_video_thumb/1/img/poster.jpg"></video></figure></blockquote>`
	if !strings.Contains(output, expectedVideo) {
		t.Errorf(`Expected the highest bitrate MP4 at the end of the blockquote, got: %s`, output)
	}
	if strings.Contains(output, "pic.twitter.com") {
		t.Errorf(`Expected the media placeholder link to be removed, got: %s`, output)
	}
	if !strings.Contains(output, `October 30, 2026</a>`) {
		t.Errorf(`Expected the tweet link to be kept, got: %s`, output)
	}
}

func TestExpandTweetEmbedsPhotosWithBareMediaURL(t *testing.T) {
	mockFetchTweet(t, map[string]string{"2116296960600928345": tweetPhotosJSON})

	input := `<blockquote><p>HomeView Software contains this image https://t.co/yV1a1lbJcr</p>— Aaron (@aaronp613) <a href="https://x.com/aaronp613/status/2116296960600928345">October 30, 2026</a></blockquote>`
	output := expandTweetEmbeds(input)

	expected := `<blockquote><p>HomeView Software contains this image </p>— Aaron (@aaronp613) <a href="https://x.com/aaronp613/status/2116296960600928345">October 30, 2026</a>` +
		`<figure><img src="https://pbs.twimg.com/media/one.png" alt="A &#34;quoted&#34; alt" width="160" height="160"/></figure>` +
		`<figure><img src="https://pbs.twimg.com/media/two.jpg" alt=""/></figure></blockquote>`
	if output != expected {
		t.Errorf(`Unexpected output:\ngot:  %s\nwant: %s`, output, expected)
	}
}

func TestExpandTweetEmbedsIgnoresOtherBlockquotes(t *testing.T) {
	calls := mockFetchTweet(t, nil)

	inputs := []string{
		`<blockquote><p>A regular quote.</p></blockquote>`,
		`<blockquote><p>Quote with a link to <a href="https://example.org/status/123">a status page</a>.</p></blockquote>`,
		`<blockquote><p>See <a href="https://x.com/user/status/123">this tweet</a> and <a href="https://example.org/">this site</a>.</p></blockquote>`,
		`<p>A link to a tweet outside a blockquote: <a href="https://x.com/user/status/123">tweet</a></p>`,
	}

	for _, input := range inputs {
		if output := expandTweetEmbeds(input); output != input {
			t.Errorf(`Expected content to be unchanged:\ngot:  %s\nwant: %s`, output, input)
		}
	}
	if *calls != 0 {
		t.Errorf(`Expected no fetches, got %d`, *calls)
	}
}

func TestExpandTweetEmbedsKeepsContentWhenFetchFails(t *testing.T) {
	mockFetchTweet(t, nil)

	input := `<blockquote><p>Text https://t.co/abc</p>— User <a href="https://twitter.com/user/status/42">Date</a></blockquote>`
	if output := expandTweetEmbeds(input); output != input {
		t.Errorf(`Expected content to be unchanged:\ngot:  %s\nwant: %s`, output, input)
	}
}

func TestExpandTweetEmbedsKeepsContentWithoutMedia(t *testing.T) {
	mockFetchTweet(t, map[string]string{"42": `{"mediaDetails": []}`})

	input := `<blockquote><p>Just text</p>— User <a href="https://twitter.com/user/status/42">Date</a></blockquote>`
	if output := expandTweetEmbeds(input); output != input {
		t.Errorf(`Expected content to be unchanged:\ngot:  %s\nwant: %s`, output, input)
	}
}

func TestExpandTweetEmbedsSkipsUnsafeMediaURLs(t *testing.T) {
	mockFetchTweet(t, map[string]string{"42": `{"mediaDetails": [
		{"type": "photo", "media_url_https": "javascript:alert(1)"},
		{"type": "video", "media_url_https": "https://pbs.twimg.com/poster.jpg", "video_info": {"variants": [{"bitrate": 1, "content_type": "video/mp4", "url": "http://video.twimg.com/insecure.mp4"}]}}
	]}`})

	input := `<blockquote><p>Text</p>— User <a href="https://twitter.com/user/status/42">Date</a></blockquote>`
	if output := expandTweetEmbeds(input); output != input {
		t.Errorf(`Expected content to be unchanged:\ngot:  %s\nwant: %s`, output, input)
	}
}

func TestExpandTweetEmbedsCachesResults(t *testing.T) {
	calls := mockFetchTweet(t, map[string]string{"2116296999297548615": tweetVideoJSON})

	input := `<blockquote><p>Hello!</p><a href="https://x.com/_inside/status/2116296999297548615">Date</a></blockquote>`
	expandTweetEmbeds(input)
	expandTweetEmbeds(input)
	expandTweetEmbeds(`<blockquote><p>Missing</p><a href="https://x.com/user/status/1">Date</a></blockquote>`)
	expandTweetEmbeds(`<blockquote><p>Missing</p><a href="https://x.com/user/status/1">Date</a></blockquote>`)

	if *calls != 2 {
		t.Errorf(`Expected 2 fetches (one success, one failure), got %d`, *calls)
	}
}

func TestExpandTweetEmbedsRewriteRule(t *testing.T) {
	mockFetchTweet(t, map[string]string{"2116296999297548615": tweetVideoJSON})

	entry := &model.Entry{
		URL:     "https://example.org/article",
		Content: `<blockquote><p>Hello!</p><a href="https://mobile.twitter.com/_inside/status/2116296999297548615">Date</a></blockquote>`,
	}
	ApplyContentRewriteRules(entry, `expand_tweet_embeds`)

	if !strings.Contains(entry.Content, `<video src="https://video.twimg.com/vid/720x802/high.mp4"`) {
		t.Errorf(`Expected the expand_tweet_embeds rule to add the video, got: %s`, entry.Content)
	}
}

func TestExpandTweetEmbedsExpandsShortLinks(t *testing.T) {
	mockFetchTweet(t, map[string]string{"42": tweetLinksJSON})

	input := `<blockquote><p>Give to <a href="https://t.co/luC5CdcWDy">https://t.co/luC5CdcWDy</a> &amp; https://t.co/zvNptfyuNd or <a href="https://example.org/">https://t.co/luC5CdcWDy</a> https://t.co/unsafe1234</p>— User <a href="https://twitter.com/user/status/42">Date</a></blockquote>`
	expected := `<blockquote><p>Give to <a href="https://alzresearch.org/giving/">alzresearch.org/giving/</a> &amp; <a href="https://alzfdn.org/donate/?a=1&amp;b=2">alzfdn.org/donate/?a=1&amp;…</a> or <a href="https://example.org/">https://t.co/luC5CdcWDy</a> https://t.co/unsafe1234</p>— User <a href="https://twitter.com/user/status/42">Date</a></blockquote>`

	if output := expandTweetEmbeds(input); output != expected {
		t.Errorf(`Unexpected output:\ngot:  %s\nwant: %s`, output, expected)
	}
}
