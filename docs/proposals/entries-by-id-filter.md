# Proposal: fetch entries by ID via `GET /v1/entries?entry_id=…`

## Summary

Add a repeatable `entry_id` query parameter to `GET /v1/entries` so a client can
fetch a specific set of entries — with full content and `fields=` selection — in a
single request, instead of issuing one `GET /v1/entries/{entryID}` per entry.

## Motivation

API clients that keep a local mirror of read/starred state routinely end up holding
entry IDs they have *status* for but no cached *content* — e.g. after reconciling the
local store against `GET /v1/entries/ids?status=unread`. Today the only way to fetch
those bodies is one request per entry:

```
GET /v1/entries/{id}      × N
```

In NetNewsWire's Miniflux integration this is the `refreshMissingArticles` path: it
chunks the missing IDs (100 at a time) and then loops a per-entry GET *inside* each
chunk — N round-trips for N missing entries. A batch fetch turns that into one request
per chunk.

## Current state (already 90% there)

The query builder already supports filtering by an ID set; it just isn't exposed on the
collection endpoint:

- `EntryQueryBuilder.WithEntryIDs(entryIDs ...int64)` —
  `internal/storage/entry_query_builder.go:135` — already handles 0 / 1 / N IDs
  correctly (`e.id = $n` for one, `e.id = ANY($n)` for many, no-op when empty).
- It is used internally by the single-entry handlers (`getFeedEntryHandler`,
  `getCategoryEntryHandler`), but `findEntries`
  (`internal/api/entry_handlers.go:151`) never reads an ID list from the request.

`findEntries` already parses `status` (repeatable), `order`, `direction`, `limit`,
`offset`, `category_id`, `feed_id`, `tags`, and `fields`, then composes the builder.
Adding an ID filter is one more parse plus one more builder call.

## Proposed change

Accept a repeatable `entry_id` query parameter (snake_case, consistent with `feed_id`
and `category_id`):

```
GET /v1/entries?entry_id=121624&entry_id=121634&fields=id,content&limit=100
```

Semantics:

- Repeated `entry_id` values restrict the result to that set, combined with any other
  filters via `AND` — exactly like the existing filters.
- Absent → unchanged behaviour.
- Honours `fields=`, `limit`/`offset`, and sorting with no special cases.

### Implementation sketch

In `findEntries` (`internal/api/entry_handlers.go`), alongside the other param parsing:

```go
var entryIDs []int64
for _, raw := range request.QueryStringParamList(r, "entry_id") {
    id, err := strconv.ParseInt(raw, 10, 64)
    if err != nil {
        response.JSONBadRequest(w, r, fmt.Errorf("invalid entry_id: %q", raw))
        return
    }
    entryIDs = append(entryIDs, id)
}
```

and add one call to the builder chain:

```go
builder := h.store.NewEntryQueryBuilder(userID).
    WithEntryIDs(entryIDs...).   // no-op when the slice is empty
    WithFeedID(feedID).
    WithCategoryID(categoryID).
    WithStatuses(statuses...).
    ...
```

Because `WithEntryIDs` already no-ops on an empty slice, the addition is completely
inert unless a client sends `entry_id`.

Optionally also accept a comma-separated form (`entry_id=1,2,3`) to keep URLs short for
large batches; repeated params alone are sufficient and mirror how `status` works.

## Backward compatibility / capability detection

The change is purely additive and safe on the server. **It does not degrade gracefully
on the client, though:** a server *without* this patch simply ignores the unknown
`entry_id` param and returns *all* entries (subject to `limit`), which is not what the
caller wants. So a client must feature-detect before using it — e.g. gate on a minimum
server version (the same approach already used for the `fields=` extension) and fall
back to the per-entry `GET /v1/entries/{id}` loop otherwise.

## Client benefit (NetNewsWire)

`refreshMissingArticles` collapses from:

```
for id in chunk:  GET /v1/entries/{id}          # up to 100 requests per chunk
```

to:

```
GET /v1/entries?entry_id=…(× chunk)&fields=…    # 1 request per chunk
```

Combined with `fields=`, each request returns exactly the columns the client maps,
minimising both round-trips and payload. It also lightens the repeated re-fetch of
just-past-cutoff unread items that clients tend to redo on every sync.

## Upstreamability

Nothing here is fork-specific — a batch-by-ID read is generally useful and low-risk
(it reuses an existing, tested builder method). Worth proposing to `miniflux/v2`
upstream so clients can rely on it without a private server build.

## Testing

- Unit: extend the API integration tests (`internal/api/api_integration_test.go`) —
  request a known ID subset and assert the response contains exactly those entries,
  honours `fields=`, and that an absent `entry_id` is unchanged. Add an
  invalid-`entry_id` → 400 case.
- Manual: `curl -H "X-Auth-Token: …" "http://localhost:8080/v1/entries?entry_id=A&entry_id=B&fields=id,title"`.
