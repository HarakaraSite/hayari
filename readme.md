# yarr2

A self-hosted RSS aggregator written in Go with a vanilla JS frontend.

## Features

- Single binary with embedded frontend assets
- SQLite database (no external DB required)
- Desktop tray icon support (build with `gui` tag)
- FreshRSS / Google Reader API compatible
- Article filter rules (keyword, author, content)
- Pico CSS based lightweight UI

## Building

```sh
# Server-only build
make build

# Desktop tray build
make build-gui
```

## Running

```sh
./yarr2 --addr :7070 --db path/to/yarr.db
```

## API

yarr2 implements the Google Reader / FreshRSS API:

- `POST /accounts/ClientLogin` - Authentication
- `GET  /reader/api/0/subscription/list` - Subscription list
- `GET  /reader/api/0/unread-count` - Unread counts
- `GET  /reader/api/0/stream/contents/{stream-id}` - Item stream
- `POST /reader/api/0/edit-tag` - Tag editing (read/starred)
- `POST /reader/api/0/subscription/edit` - Subscription management
- `GET  /reader/api/0/tag/list` - Label/folder list

## License

MIT
