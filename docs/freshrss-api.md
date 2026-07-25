# FreshRSS `greader.php` 互換 API

hayari が実装するのは、FreshRSS の `greader.php` を基準にした Google Reader API
の互換サブセットである。完全な FreshRSS API または完全な Google Reader API の実装
ではない。

実装済みの範囲は、ReadKit と NetNewsWire が実際の同期で要求した操作を基準にして
いる。`rename-tag` と `disable-tag` など、確認済みのクライアントワークフローが
要求しないエンドポイントは未実装である。対応状況はこの文書のエンドポイント一覧を
正とする。

## API の入口

hayari は同じ互換サブセットを、クライアント設定に合わせた2つの URL 形式で公開する。

| API 形式 | ログイン URL | API のベース URL |
|---|---|---|
| Google Reader 形式 | `/accounts/ClientLogin` | `/reader/api/0/...` |
| FreshRSS `greader.php` 形式 | `/api/greader.php/accounts/ClientLogin` | `/api/greader.php/reader/api/0/...` |

2つの入口は同じハンドラーへ接続されるため、hayari が提供する機能は同一である。
Web UI の認証は `/login` の Cookie セッションであり、このトークンAPIとは別系統。
ただしフィード・記事・状態は同じデータベースを共有する。

## 実機確認（2026-07-24）

- **ReadKit**: ログイン、フォルダ／フィード、未読・スター・既読の記事同期、本文取得、`edit-tag` 更新を確認。
- **NetNewsWire**: ログイン、フォルダ／フィード、記事一覧・本文の同期、`edit-tag` 更新を確認。

記事の初回同期では、ReadKit は `stream/items/ids` を1000件単位でページングし、続けて `stream/items/contents` を複数回POSTする。IDと継続トークンの形式を変えないこと。

---

## 認証

### POST /accounts/ClientLogin

クライアントが最初に呼び出すエンドポイント。

**リクエスト**
- Content-Type: `application/x-www-form-urlencoded`
- パラメータ:
  - `Email` — ユーザー名またはメールアドレス
  - `Passwd` — パスワード

**レスポンス** (`text/plain`)
```
SID=<token>
LSID=<token>
Auth=<token>
```

3つは同じトークン値でよい。

**注意点**
- 通常は POST を使う。GET は `--allow-greader-login-get` 指定時だけ許可する。
- トークンはメモリ保持で、サーバー再起動時に失効する。ReadKit と NetNewsWire は 401 後に再ログインする。

---

### GET /reader/api/0/token

CSRF トークン取得。POST 系エンドポイントで `T=` パラメータとして送る。

**認証:** Authorization ヘッダー必須

**レスポンス** (`text/plain`)
```
<token>
```

**注意点**
- Reeder / FeedMe 等は `T=` に空文字や `x` を送ることがある → 受け入れる（検証しない）
- 実装上は token エンドポイントは `OK` や任意の文字列を返すだけでも動く

---

### Authorization ヘッダー形式

```
Authorization: GoogleLogin auth=<token>
```

---

## Stream ID

アイテムのコレクションを識別するキー。

### システム定義 Stream

| Stream ID | 意味 |
|---|---|
| `user/-/state/com.google/reading-list` | 全アイテム（メインリスト） |
| `user/-/state/com.google/read` | 既読アイテム |
| `user/-/state/com.google/unread` | 未読アイテム |
| `user/-/state/com.google/starred` | スター付き（お気に入り） |
| `user/-/state/com.google/kept-unread` | 明示的に未読保持 |

### フィード Stream

```
feed/<feed_id>          # フィード ID で指定（DB の ID）
feed/<feed_url>         # URL で指定（まれに使われる）
```

### ラベル / フォルダ Stream

```
user/-/label/<label_name>
```

例:
```
user/-/label/Technology
user/-/label/Work
```

---

## Item ID

### エントリ表現のフォーマット

```
tag:google.com,2005:reader/item/<hex_id>
```

`<hex_id>` は 64bit 整数を 16桁の小文字 hex にゼロパディングしたもの。

```
tag:google.com,2005:reader/item/00000000000000a1
```

### 変換

```go
// DB integer → API ID
fmt.Sprintf("tag:google.com,2005:reader/item/%016x", itemID)

// API ID → DB integer
hex := strings.TrimPrefix(fullID, "tag:google.com,2005:reader/item/")
id, err := strconv.ParseInt(hex, 16, 64)
```

### 同期用IDとページネーション

`stream/items/ids` は FreshRSS に合わせ、DB IDを **10進数文字列** で返す。`continuation` も最後に返したDB IDの10進数文字列にする。

```json
{
  "itemRefs": [{"id": "3285"}],
  "continuation": "1173"
}
```

NetNewsWire はこの10進IDを受け、本文取得時に `tag:google.com,2005:reader/item/<16桁hex>` へ変換する。`stream/items/contents` は、10進IDとこのタグ付き16進IDの両方を受け入れる。

`stream/contents` の内部ページングには日時とIDを含む不透明なカーソルを使うため、`stream/items/ids` の10進継続トークンとは別物である。

---

## エンドポイント一覧

### GET /reader/api/0/user-info

**レスポンス (JSON)**
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

---

### GET /reader/api/0/subscription/list

**レスポンス (JSON)**
```json
{
  "subscriptions": [
    {
      "id": "feed/12345",
      "title": "Example Blog",
      "url": "http://example.com/feed.xml",
      "htmlUrl": "http://example.com",
      "categories": [
        {
          "id": "user/-/label/Technology",
          "label": "Technology"
        }
      ]
    }
  ]
}
```

**フィールド**
- `id` — `feed/` + feed_id
- `title` — 表示名
- `url` — フィード URL
- `htmlUrl` — サイト URL
- `categories` — フォルダ/ラベルの配列

---

### POST /reader/api/0/subscription/edit

**パラメータ (form data)**
- `ac` — アクション: `subscribe`, `unsubscribe`, `edit`
- `s` — Stream ID（繰り返し可）
- `t` — タイトル（`s` と1対1で繰り返し可）
- `a` — カテゴリ追加: `user/-/label/CategoryName`
- `r` — カテゴリ削除: `user/-/label/CategoryName`
- `T` — CSRF トークン

**アクション例**

*購読追加*
```
ac=subscribe&s=feed/http://example.com/feed.xml&t=Title&a=user/-/label/Work
```

*購読削除*
```
ac=unsubscribe&s=feed/12345
```

*タイトル・フォルダ変更*
```
ac=edit&s=feed/12345&t=New+Title&a=user/-/label/NewFolder&r=user/-/label/OldFolder
```

**レスポンス** (`text/plain`): `OK`

---

### GET /reader/api/0/tag/list

**レスポンス (JSON)**
```json
{
  "tags": [
    {
      "id": "user/-/state/com.google/starred",
      "type": ""
    },
    {
      "id": "user/-/state/com.google/reading-list",
      "type": ""
    },
    {
      "id": "user/-/label/Technology",
      "type": "folder"
    }
  ]
}
```

**フィールド**
- `id` — Stream ID
- `type` — `"folder"`, `"tag"`, または空文字

---

### GET /reader/api/0/unread-count

**レスポンス (JSON)**
```json
{
  "max": 42,
  "unreadcounts": [
    {
      "id": "feed/12345",
      "count": 5,
      "newestItemTimestampUsec": "1623456789000000"
    },
    {
      "id": "user/-/label/Technology",
      "count": 10,
      "newestItemTimestampUsec": "1623456789000000"
    },
    {
      "id": "user/-/state/com.google/reading-list",
      "count": 42,
      "newestItemTimestampUsec": "1623456789000000"
    }
  ]
}
```

**フィールド**
- `max` — 全未読数合計
- `count` — 各 Stream の未読数
- `newestItemTimestampUsec` — マイクロ秒タイムスタンプ（Unix秒 × 1,000,000）を **文字列** で

**実装注意**
- フィードごと・ラベルごと・reading-list 全体の3種類を返す

---

### GET /reader/api/0/stream/contents/\<stream_id\>

**クエリパラメータ**
| パラメータ | 型 | デフォルト | 説明 |
|---|---|---|---|
| `n` | int | 20 | 取得件数（最大 1000 程度） |
| `c` | string | - | Continuation トークン（ページネーション） |
| `ot` | int | - | この Unix 秒以降のアイテムのみ |
| `nt` | int | - | この Unix 秒以前のアイテムのみ |
| `it` | string | - | このタグを持つアイテムのみ |
| `xt` | string | - | このタグを持つアイテムを除外 |
| `r` | string | - | ソート: `d`/`n`=新着順, `o`=古い順 |
| `output` | string | json | 出力形式 |

**レスポンス (JSON)**
```json
{
  "id": "user/-/state/com.google/reading-list",
  "updated": 1623456789,
  "items": [
    {
      "id": "tag:google.com,2005:reader/item/00000000000000a1",
      "title": "Article Title",
      "author": "Author Name",
      "published": 1623456789,
      "updated": 1623456789,
      "categories": [
        "user/-/state/com.google/reading-list",
        "user/-/state/com.google/read",
        "user/-/state/com.google/starred",
        "user/-/label/Technology"
      ],
      "summary": {
        "direction": "ltr",
        "content": "<p>抜粋テキスト</p>"
      },
      "content": {
        "direction": "ltr",
        "content": "<p>本文 HTML</p>"
      },
      "canonical": [
        {"href": "http://example.com/article"}
      ],
      "alternate": [
        {
          "href": "http://example.com/article",
          "type": "text/html"
        }
      ],
      "origin": {
        "streamId": "feed/12345",
        "title": "Example Blog"
      }
    }
  ],
  "continuation": "161"
}
```

**categories フィールドのルール**
- 常に `user/-/state/com.google/reading-list` を含む
- 既読なら `user/-/state/com.google/read` を追加
- スター付きなら `user/-/state/com.google/starred` を追加
- 所属フォルダがあれば `user/-/label/<name>` を追加

**continuation**
- 返したアイテム数がリクエストした `n` 以上の場合のみ含める
- 値は最後のアイテムの ID（10進数文字列）

---

### GET /reader/api/0/stream/items/ids

全文ではなく ID だけを取得する軽量エンドポイント。

**クエリパラメータ**
- `s` — Stream ID（必須）
- `n`, `c`, `ot`, `nt`, `it`, `xt`, `r` — stream/contents と同じ

**レスポンス (JSON)**
```json
{
  "itemRefs": [
    {"id": "161"},
    {"id": "162"}
  ],
  "continuation": "1000"
}
```

**注意**
- `id` はタグ形式ではない **10進数文字列**。
- `continuation` も10進数文字列。

---

### POST /reader/api/0/stream/items/contents

指定した ID のアイテム全文を取得。

**リクエスト (form data、繰り返し可)**
```
i=161
i=tag:google.com,2005:reader/item/00000000000000a1
```

両形式を受け付ける。FreshRSS 形式のクライアントは10進IDを、NetNewsWire はタグ付き16進IDを送る。

**クエリパラメータ**
- `r` — ソート順

**レスポンス**: stream/contents と同じ形式

---

### POST /reader/api/0/edit-tag

アイテムの既読/スター状態を変更する。

**リクエスト (form data)**
- `T` — CSRF トークン
- `i` — アイテム ID（繰り返し可）
- `a` — 追加するタグ（繰り返し可）
- `r` — 削除するタグ（繰り返し可）

**追加可能なタグ (`a`)**
| タグ | 動作 |
|---|---|
| `user/-/state/com.google/read` | 既読にする |
| `user/-/state/com.google/starred` | スター付きにする |
| `user/-/state/com.google/kept-unread` | 未読保持 |

**削除可能なタグ (`r`)**
| タグ | 動作 |
|---|---|
| `user/-/state/com.google/read` | 未読に戻す |
| `user/-/state/com.google/starred` | スター解除 |

**レスポンス** (`text/plain`): `OK`

---

### POST /reader/api/0/mark-all-as-read

Stream 内の全アイテムを一括既読。

**リクエスト (form data)**
- `T` — CSRF トークン
- `s` — Stream ID（必須）
- `ts` — この **ナノ秒** タイムスタンプ以前のアイテムのみ対象（省略時は全件）

```
s=feed/12345&ts=1623456789000000000
```

**レスポンス** (`text/plain`): `OK`

**注意**
- `ts` はナノ秒（Unix秒 × 1,000,000,000）
- 対応 Stream: feed/\<id\>, user/-/label/\<name\>, user/-/state/com.google/reading-list 等

---

### 補助エンドポイント

#### 未実装

以下は FreshRSS にはあるが、hayari では現時点で未実装。確認済みのクライアント
ワークフローでは要求されていないため、クライアント互換の根拠として扱わない。

**POST /reader/api/0/rename-tag** — ラベル名変更
```
T=<token>&s=user/-/label/OldName&dest=user/-/label/NewName
```
レスポンス: `OK`

**POST /reader/api/0/disable-tag** — ラベル削除
```
T=<token>&s=user/-/label/LabelName
```
レスポンス: `OK`

#### 実装済み: POST /reader/api/0/subscription/quickadd

フィードURLから素早く購読する。
```
quickadd=http://example.com/feed.xml
```
レスポンス (JSON):
```json
{
  "numResults": 1,
  "query": "http://example.com/feed.xml",
  "streamId": "feed/12345",
  "streamName": "Example Feed"
}
```

**GET /reader/api/0/subscription/export** — OPML エクスポート

**POST /reader/api/0/subscription/import** — OPML インポート

---

## 実装上の重要ポイント

### タイムスタンプ単位

| フィールド | 単位 |
|---|---|
| `published`, `updated` | Unix 秒 |
| `newestItemTimestampUsec` | マイクロ秒（×1,000,000） |
| `ts` パラメータ（mark-all-as-read） | ナノ秒（×1,000,000,000） |

### CSRF トークン検証

- 厳密な検証は実装しなくてよい
- Reeder 等は `T=x` や `T=` など無効な値を送る
- 受け入れる（検証スキップ）のが実用的

### クライアントの癖

| クライアント | 注意 |
|---|---|
| Reeder | GET で ClientLogin、空の CSRF トークン |
| FeedMe | 空の CSRF トークン |
| News+ | 空リスト時に `[{"id": 0}]` を期待するバグあり |
| NetNewsWire | 標準的な実装 |

### エラーレスポンス

- 401: 認証失敗 → `Google-Bad-Token: true` ヘッダーも付けると良い
- 400: パラメータ不正
- 500: 内部エラー

### CORS ヘッダー（ブラウザクライアント対応が必要な場合）

```
Access-Control-Allow-Headers: Authorization
Access-Control-Allow-Methods: GET, POST
Access-Control-Allow-Origin: *
Access-Control-Max-Age: 600
```

---

## 実装済みエンドポイント

| エンドポイント | 用途 |
|---|---|
| `POST /accounts/ClientLogin` | 認証 |
| `GET /reader/api/0/token` | CSRF トークン |
| `GET /reader/api/0/user-info` | ユーザー情報 |
| `GET /reader/api/0/subscription/list` | フィード一覧 |
| `POST /reader/api/0/subscription/edit` | フィード追加/削除/編集 |
| `GET /reader/api/0/tag/list` | ラベル/フォルダ一覧 |
| `GET /reader/api/0/unread-count` | 未読数 |
| `GET /reader/api/0/stream/contents/{stream-id}` | アイテム取得 |
| `GET /reader/api/0/stream/items/ids` | アイテム ID 取得 |
| `POST /reader/api/0/stream/items/contents` | アイテム全文取得 |
| `POST /reader/api/0/edit-tag` | 既読/スター操作 |
| `POST /reader/api/0/mark-all-as-read` | 一括既読 |

上の各パスは `/api/greader.php` を前置した FreshRSS 入口でも利用できる。

---

## 参考実装

- [FreshRSS greader.php](https://github.com/FreshRSS/FreshRSS/blob/edge/p/api/greader.php) — 最も完全な実装（約1300行）
- [Miniflux google_reader.go](https://github.com/miniflux/v2/blob/main/internal/api/google_reader.go) — Go 実装の参考
