// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package api // import "miniflux.app/v2/internal/api"

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	miniflux "miniflux.app/v2/client"
)

const (
	benchmarkEntryCount   = 100
	benchmarkContentSize  = 5 * 1024
	benchmarkEntriesQuery = "/v1/entries?limit=100&order=id&direction=asc"
)

// BenchmarkGetEntriesFullObjects measures the entries list endpoint without a
// fieldset, returning full entry objects. Compare with
// BenchmarkGetEntriesFieldset: ns/op shows latency, bytes/resp shows the
// transferred response size.
func BenchmarkGetEntriesFullObjects(b *testing.B) {
	benchmarkGetEntries(b, benchmarkEntriesQuery)
}

// BenchmarkGetEntriesFieldset measures the same request restricted to a small
// fieldset, which prunes the JSON response and lets PostgreSQL skip reading
// the unselected columns.
func BenchmarkGetEntriesFieldset(b *testing.B) {
	benchmarkGetEntries(b, benchmarkEntriesQuery+"&fields=id,title,feed.title")
}

func benchmarkGetEntries(b *testing.B, path string) {
	testConfig := newIntegrationTestConfig()
	if !testConfig.isConfigured() {
		b.Skip(skipIntegrationTestsMessage)
	}

	adminClient := miniflux.NewClient(testConfig.testBaseURL, testConfig.testAdminUsername, testConfig.testAdminPassword)

	regularTestUser, err := adminClient.CreateUser(testConfig.genRandomUsername(), testConfig.testRegularPassword, false)
	if err != nil {
		b.Fatal(err)
	}
	defer adminClient.DeleteUser(regularTestUser.ID)

	regularUserClient := miniflux.NewClient(testConfig.testBaseURL, regularTestUser.Username, testConfig.testRegularPassword)

	feedID, err := regularUserClient.CreateFeed(&miniflux.FeedCreationRequest{
		FeedURL: testConfig.testFeedURL,
	})
	if err != nil {
		b.Fatal(err)
	}

	content := strings.Repeat("<p>Benchmark entry content padding.</p>", benchmarkContentSize/40)
	for i := range benchmarkEntryCount {
		if _, err := regularUserClient.ImportFeedEntry(feedID, map[string]any{
			"url":          fmt.Sprintf("https://example.org/benchmark/%d", i),
			"title":        fmt.Sprintf("Benchmark entry %d", i),
			"content":      content,
			"author":       "Benchmark Author",
			"published_at": time.Now().Unix(),
		}); err != nil {
			b.Fatal(err)
		}
	}

	// Authenticate with an API key: basic auth verifies a bcrypt hash on
	// every request, which would dominate the timings.
	apiKey, err := regularUserClient.CreateAPIKey("benchmark")
	if err != nil {
		b.Fatal(err)
	}

	httpClient := &http.Client{}
	requestURL := apiBaseURL(testConfig) + path
	var totalBytes int64

	b.ResetTimer()
	for b.Loop() {
		req, err := http.NewRequest(http.MethodGet, requestURL, nil)
		if err != nil {
			b.Fatal(err)
		}
		req.Header.Set("X-Auth-Token", apiKey.Token)

		resp, err := httpClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}

		n, err := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			b.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		totalBytes += n
	}
	b.StopTimer()

	b.ReportMetric(float64(totalBytes)/float64(b.N), "bytes/resp")
}
