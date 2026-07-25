# FreshRSS `greader.php`-compatible API

[日本語版](freshrss-api.ja.md)

Hayari implements a Google Reader API compatibility subset based on FreshRSS's
`greader.php`. It is not a complete implementation of either the FreshRSS API
or the historical Google Reader API.

The implemented scope follows the operations required by the verified ReadKit
and NetNewsWire synchronization workflows. Endpoints such as `rename-tag` and
`disable-tag`, which are outside those workflows, are not implemented. The
endpoint list in this document is authoritative.

## API entry points

Hayari exposes the same compatibility subset through two URL forms.

| API form | Login URL | API base URL |
|---|---|---|
| Google Reader form | `/accounts/ClientLogin` | `/reader/api/0/...` |
| FreshRSS `greader.php` form | `/api/greader.php/accounts/ClientLogin` | `/api/greader.php/reader/api/0/...` |

Both forms reach the same handlers and provide the same functionality. Web UI
authentication uses the separate cookie-based `/login` endpoint, while sharing
the same feeds, items, and state.

## Verified clients (2026-07-24)

- **ReadKit:** login; folder and feed synchronization; unread, read, and starred
  state; article retrieval; and `edit-tag` updates.
- **NetNewsWire:** login; folder and feed synchronization; article list and
  content retrieval; and `edit-tag` updates.

During its initial sync, ReadKit requests `stream/items/ids` in pages of up to
1000 entries, then sends multiple POST requests to `stream/items/contents`.
Keep the item-ID and continuation formats stable.

---

## Authentication

### `POST /accounts/ClientLogin`

The initial client login endpoint.

**Request** (`application/x-www-form-urlencoded`)

- `Email` — user name or email address
- `Passwd` — password

**Response** (`text/plain`)

```text
SID=<token>
LSID=<token>
Auth=<token>
```

All three values are the same token.

Use POST by default. GET is enabled only with `--allow-greader-login-get`,
because query credentials may be recorded in proxy logs. Tokens are held in
memory and expire on server restart; verified clients reauthenticate after a
401 response.

### `GET /reader/api/0/token`

Returns a CSRF token for the `T=` parameter used by POST endpoints.

- Authentication: `Authorization` header required
- Response: `text/plain`

```text
<token>
```

Some clients send an empty `T=` or `T=x`. Hayari accepts those values; it does
not strictly validate the CSRF token.

### Authorization header

```text
Authorization: GoogleLogin auth=<token>
```

---

## Stream IDs

### System streams

| Stream ID | Meaning |
|---|---|
| `user/-/state/com.google/reading-list` | all items |
| `user/-/state/com.google/read` | read items |
| `user/-/state/com.google/unread` | unread items |
| `user/-/state/com.google/starred` | starred items |
| `user/-/state/com.google/kept-unread` | explicitly kept unread |

### Feed streams

```text
feed/<feed_id>          # database feed ID
feed/<feed_url>         # feed URL; used less often
```

### Label and folder streams

```text
user/-/label/<label_name>
```

For example, `user/-/label/Technology` or `user/-/label/Work`.

## Item IDs and pagination

An item entry uses the tagged hexadecimal representation:

```text
tag:google.com,2005:reader/item/<hex_id>
```

`<hex_id>` is a zero-padded, 16-character lowercase hexadecimal 64-bit integer.

```text
tag:google.com,2005:reader/item/00000000000000a1
```

For synchronization, `stream/items/ids` follows FreshRSS and returns database
IDs as decimal strings. Its `continuation` is also the decimal ID of the last
item returned.

```json
{
  "itemRefs": [{"id": "3285"}],
  "continuation": "1173"
}
```

`stream/items/contents` accepts both decimal IDs and tagged hexadecimal IDs.
The opaque cursor used internally by `stream/contents` includes a timestamp and
an ID, and is separate from the decimal `stream/items/ids` continuation.

---

## Endpoints

### `GET /reader/api/0/user-info`

Returns the authenticated user:

```json
{
  "userId": "username",
  "userName": "username",
  "userProfileId": "username",
  "userEmail": "user@example.com",
  "isBloggerUser": false,
  "signupTimeSec": 0,
  "isMultiLoginEnabled": false
}
```

### `GET /reader/api/0/subscription/list`

Returns subscriptions and their optional folder category.

```json
{
  "subscriptions": [
    {
      "id": "feed/12345",
      "title": "Example Blog",
      "url": "http://example.com/feed.xml",
      "htmlUrl": "http://example.com",
      "categories": [{
        "id": "user/-/label/Technology",
        "label": "Technology"
      }]
    }
  ]
}
```

### `POST /reader/api/0/subscription/edit`

Creates, removes, or updates subscriptions.

| Form parameter | Meaning |
|---|---|
| `ac` | `subscribe`, `unsubscribe`, or `edit` |
| `s` | stream ID; repeatable |
| `t` | title; repeatable, paired with `s` |
| `a` | add category, such as `user/-/label/Work` |
| `r` | remove category, such as `user/-/label/Work` |
| `T` | CSRF token |

Examples:

```text
ac=subscribe&s=feed/http://example.com/feed.xml&t=Title&a=user/-/label/Work
ac=unsubscribe&s=feed/12345
ac=edit&s=feed/12345&t=New+Title&a=user/-/label/NewFolder&r=user/-/label/OldFolder
```

Response: `OK` (`text/plain`). A feed supports one folder; an `edit` operation
therefore maps to that single folder assignment.

### `POST /reader/api/0/subscription/quickadd`

Subscribes using a feed URL.

```text
quickadd=http://example.com/feed.xml
```

```json
{
  "numResults": 1,
  "query": "http://example.com/feed.xml",
  "streamId": "feed/12345",
  "streamName": "Example Feed"
}
```

### `GET /reader/api/0/tag/list`

Returns system states and folders.

```json
{
  "tags": [
    {"id": "user/-/state/com.google/starred", "type": ""},
    {"id": "user/-/state/com.google/reading-list", "type": ""},
    {"id": "user/-/label/Technology", "type": "folder"}
  ]
}
```

`type` is `"folder"`, `"tag"`, or an empty string.

### `GET /reader/api/0/unread-count`

Returns unread counts for feeds, folders, and the reading list.

```json
{
  "max": 42,
  "unreadcounts": [
    {
      "id": "feed/12345",
      "count": 5,
      "newestItemTimestampUsec": "1623456789000000"
    }
  ]
}
```

- `max` is the total unread count.
- `count` is the unread count for a stream.
- `newestItemTimestampUsec` is a microsecond Unix timestamp encoded as a string.

### `GET /reader/api/0/stream/contents/<stream_id>`

Returns items in a stream.

| Query parameter | Type | Default | Meaning |
|---|---:|---:|---|
| `n` | integer | 20 | number of items; roughly 1000 maximum |
| `c` | string | — | pagination continuation |
| `ot` | integer | — | include items from this Unix second |
| `nt` | integer | — | include items through this Unix second |
| `it` | string | — | include items with this tag |
| `xt` | string | — | exclude items with this tag |
| `r` | string | — | `d`/`n` newest first; `o` oldest first |
| `output` | string | `json` | response format |

The JSON response contains `id`, `updated`, `items`, and, when more items are
available, `continuation`. Each item contains its tagged ID, title, author,
publication timestamps, categories, HTML summary and content, canonical and
alternate links, and origin feed metadata.

Item categories always include `user/-/state/com.google/reading-list`; they add
`read`, `starred`, and a folder label when applicable. The continuation is the
decimal ID of the last returned item and is sent only when the result reaches
the requested item count.

### `GET /reader/api/0/stream/items/ids`

Returns only item IDs for efficient synchronization.

- `s` is the required stream ID.
- `n`, `c`, `ot`, `nt`, `it`, `xt`, and `r` have the same meanings as for
  `stream/contents`.
- IDs and `continuation` are decimal strings, not tagged IDs.

```json
{
  "itemRefs": [{"id": "161"}, {"id": "162"}],
  "continuation": "1000"
}
```

### `POST /reader/api/0/stream/items/contents`

Returns full entries for the specified IDs. Repeat `i` in form data; both ID
forms are accepted.

```text
i=161
i=tag:google.com,2005:reader/item/00000000000000a1
```

The response has the same shape as `stream/contents`. The optional `r`
parameter controls sort order.

### `POST /reader/api/0/edit-tag`

Updates read and starred state for one or more items.

| Form parameter | Meaning |
|---|---|
| `T` | CSRF token |
| `i` | item ID; repeatable |
| `a` | tag to add; repeatable |
| `r` | tag to remove; repeatable |

Supported added tags:

| Tag | Effect |
|---|---|
| `user/-/state/com.google/read` | mark as read |
| `user/-/state/com.google/starred` | star |
| `user/-/state/com.google/kept-unread` | keep unread |

Supported removed tags:

| Tag | Effect |
|---|---|
| `user/-/state/com.google/read` | mark as unread |
| `user/-/state/com.google/starred` | remove star |

Response: `OK` (`text/plain`).

### `POST /reader/api/0/mark-all-as-read`

Marks all items in a stream as read.

| Form parameter | Meaning |
|---|---|
| `T` | CSRF token |
| `s` | required stream ID |
| `ts` | optional inclusive upper bound as a nanosecond Unix timestamp |

```text
s=feed/12345&ts=1623456789000000000
```

Response: `OK` (`text/plain`). Supported streams include feeds, labels, and the
reading list.

### Not implemented

The following FreshRSS endpoints are not implemented. They are not part of the
verified client workflows and must not be treated as compatibility guarantees.

- `POST /reader/api/0/rename-tag` — rename a label
- `POST /reader/api/0/disable-tag` — remove a label
- `GET /reader/api/0/subscription/export` — export OPML
- `POST /reader/api/0/subscription/import` — import OPML

Hayari provides OPML import and export for the Web UI through its own
authenticated `/opml/import` and `/opml/export` endpoints instead.

---

## Implementation notes

### Timestamp units

| Field | Unit |
|---|---|
| `published`, `updated` | Unix seconds |
| `newestItemTimestampUsec` | microseconds |
| `ts` for `mark-all-as-read` | nanoseconds |

### Client behaviour

Hayari intentionally accepts empty or placeholder CSRF tokens because some
clients send `T=` or `T=x`. The Google Reader login GET endpoint remains
disabled by default; enable it only with `--allow-greader-login-get` when a
client requires it.

### Errors

- `401` for authentication failure, with `Google-Bad-Token: true`.
- `400` for malformed parameters.
- `500` for internal errors.

## Implemented endpoint summary

| Endpoint | Purpose |
|---|---|
| `POST /accounts/ClientLogin` | authentication |
| `GET /reader/api/0/token` | CSRF token |
| `GET /reader/api/0/user-info` | user information |
| `GET /reader/api/0/subscription/list` | feed list |
| `POST /reader/api/0/subscription/edit` | add, remove, or edit feeds |
| `POST /reader/api/0/subscription/quickadd` | subscribe by URL |
| `GET /reader/api/0/tag/list` | labels and folders |
| `GET /reader/api/0/unread-count` | unread counts |
| `GET /reader/api/0/stream/contents/{stream-id}` | retrieve items |
| `GET /reader/api/0/stream/items/ids` | retrieve item IDs |
| `POST /reader/api/0/stream/items/contents` | retrieve full item content |
| `POST /reader/api/0/edit-tag` | update read and starred state |
| `POST /reader/api/0/mark-all-as-read` | mark a stream as read |

Prefix any of the paths above with `/api/greader.php` to use the FreshRSS URL
form.

## Reference implementations

- [FreshRSS greader.php](https://github.com/FreshRSS/FreshRSS/blob/edge/p/api/greader.php)
  — the more complete reference implementation.
- [Miniflux google_reader.go](https://github.com/miniflux/v2/blob/main/internal/api/google_reader.go)
  — a Go implementation reference.
