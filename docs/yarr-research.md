# yarr 機能調査メモ

hayari 設計の参考とするため、オリジナル [yarr](https://github.com/nkanaev/yarr) の実装を調査した結果。

---

## データベーススキーマ

### folders
| カラム | 型 | 備考 |
|---|---|---|
| id | integer PK autoincrement | |
| title | text NOT NULL | |
| is_expanded | boolean default false | サイドバーでの開閉状態 |

### feeds
| カラム | 型 | 備考 |
|---|---|---|
| id | integer PK autoincrement | |
| folder_id | integer FK → folders | ON DELETE SET NULL |
| title | text | |
| description | text | |
| link | text | サイト URL |
| feed_link | text UNIQUE | フィード URL |
| icon | blob | favicon バイナリ |

### items
| カラム | 型 | 備考 |
|---|---|---|
| id | integer PK autoincrement | |
| guid | text NOT NULL | フィード内一意識別子 |
| feed_id | integer FK → feeds | ON DELETE CASCADE |
| title | text | |
| link | text | |
| description | text | 要約・抜粋 |
| content | text | 本文 HTML |
| author | text | |
| date | datetime | 公開日時 |
| date_updated | datetime | 更新日時 |
| date_arrived | datetime | 取得日時 |
| status | integer | 0=unread, 1=read, 2=starred |
| media_links | text | JSON 配列（ポッドキャスト等） |

### feed_states
| カラム | 型 | 備考 |
|---|---|---|
| feed_id | integer FK UNIQUE | |
| last_refreshed | datetime | |
| last_error | text | |
| http_lmod | text | Last-Modified ヘッダー値（差分取得用） |
| http_etag | text | ETag ヘッダー値（差分取得用） |

### settings
| カラム | 型 | 備考 |
|---|---|---|
| key | text PK | |
| val | blob | JSON シリアライズ値 |

**設定キー一覧:**
- `filter` — フィード/フォルダフィルター状態
- `feed_list_width` — 左カラム幅
- `item_list_width` — 中カラム幅
- `newest_first` — 新着順ソート
- `theme` — テーマ（night / sepia / light）
- `font` — フォント
- `font_size` — フォントサイズ
- `refresh_rate` — 自動更新間隔（分）
- `language` — UI 言語

### search（FTS5 仮想テーブル）
- title, content を全文検索インデックス化
- SQLite の `strip_html()` カスタム関数でHTMLタグを除去してインデックス登録

---

## API エンドポイント（独自 REST API）

### Feeds
```
GET    /api/feeds              全フィード一覧（has_icon フラグ付き、icon バイナリは除外）
POST   /api/feeds              フィード追加（URL から自動検出）
PUT    /api/feeds/:id          フィード更新（title, folder_id, feed_link, icon）
DELETE /api/feeds/:id          フィード削除
POST   /api/feeds/refresh      全フィード手動更新トリガー
GET    /api/feeds/errors       フィードごとの最終エラー取得
GET    /api/feeds/:id/icon     favicon 取得（ETag キャッシュ対応）
```

### Folders
```
GET    /api/folders            全フォルダ一覧
POST   /api/folders            フォルダ作成
PUT    /api/folders/:id        フォルダ更新（title, is_expanded）
DELETE /api/folders/:id        フォルダ削除
```

### Items
```
GET    /api/items              一覧取得（クエリパラメータ: folder_id, feed_id, status, search, 日時範囲, limit, offset）
PUT    /api/items              一括既読マーク
GET    /api/items/:id          個別アイテム取得
PUT    /api/items/:id          ステータス更新（read/unread/starred）
```

### Settings / Status
```
GET    /api/settings           設定取得
PUT    /api/settings           設定更新（部分更新可）
GET    /api/status             worker 状態・フィード統計
```

### OPML
```
POST   /opml/import            OPML ファイルインポート（フォルダ・フィード一括登録）
GET    /opml/export            OPML エクスポート（フォルダ階層含む）
```

### Utility
```
GET    /page?url=...           記事本文クロール（Readability モード用）
GET    /logout                 認証クッキー削除
GET    /fever/                 Fever API v3（yarr は Fever 互換、hayari は FreshRSS 互換に変更）
```

---

## Worker 動作仕様

### 並列処理
- 4 並列ゴルーチンでフィードを処理
- チャンネル経由で分散（pool パターン）
- atomic 操作・mutex でスレッドセーフ

### HTTP クライアント設定
- 接続タイムアウト: 10 秒
- リクエストタイムアウト: 30 秒
- TLS ハンドシェイクタイムアウト: 10 秒
- Keep-alive: 無効
- User-Agent: `Yarr/{version}`
- プロキシ: 環境変数から自動取得

### 差分取得（条件付き HTTP リクエスト）
- ETag / If-None-Match 対応
- Last-Modified / If-Modified-Since 対応
- feed_states テーブルにヘッダー値を保存、次回リクエスト時に送信

### 更新サイクル
| イベント | 間隔 |
|---|---|
| 自動更新 | settings.refresh_rate（分）で設定 |
| 古いアイテム削除 | 24 時間ごと |
| 手動更新 | API 経由でいつでも |

### アイテム保持ルール
- **スター付きアイテムは削除しない**
- 各フィード最低 50 件は保持
- 90 日超のアイテムは削除

### favicon 取得ロジック
1. フィードのサイト HTML から `<link rel="icon">` を探索
2. 見つからなければ `/favicon.ico` にフォールバック
3. Content-Type で PNG / JPEG / GIF / ICO を検証
4. バイナリを feeds.icon に保存

---

## フィード検出・パース

### フォーマット自動検出
- `<` で始まる → XML（ルート要素で RSS / RDF / Atom を判別）
- `{` で始まる → JSON Feed

### 対応フォーマット
- RSS 2.0, 0.91, 0.92
- Atom 1.0
- RDF
- JSON Feed

### パース後処理
- 相対 URL を絶対 URL に変換
- 日時なしアイテムには現在時刻を付与
- guid なしアイテムには SHA256 で生成
- 重複メディアリンクを除去
- 文字コード検出（XML 宣言から）

---

## コンテンツ処理

### HTML サニタイザー
- ホワイトリスト方式（許可タグ・属性のみ通過）
- `<script>`, `<style>`, `<noscript>` をブロック
- `<iframe>` は YouTube / Vimeo 等の動画サービスのみ許可
- トラッキングドメイン（feedsportal.com 等）をブロック
- リンクに `rel="noopener noreferrer"` 付与
- iframe に `sandbox` 付与
- srcset 対応（レスポンシブ画像）

### Readability モード
記事本文を自動抽出するアルゴリズム：
1. `<script>`, `<style>` 削除、`<div>` を `<p>` に変換
2. 各要素をテキスト量・カンマ数・クラス/ID パターンでスコアリング
3. スコアを親要素に伝播（親 100%、祖父母 50%）
4. リンク密度でスコアにペナルティ
5. 最高スコアのノードを軸に周辺要素を統合

---

## 認証

- クッキーベース（HMAC-SHA256）
- シングルユーザー（起動時に username/password を指定）
- クッキー形式: `username:expiry_unix:hmac_signature`
- タイミング攻撃対策: `subtle.ConstantTimeCompare()` 使用
- クッキー属性: `HttpOnly`, `SameSiteLaxMode`（CSRF 対策）。`Secure` は TLS 終端方針と合わせて未対応
- 有効期限: 1 週間

---

## フロントエンド（UI）

### 3カラムレイアウト

#### 左カラム（フィードリスト）
- フォルダ/フィード階層表示（未読数バッジ付き）
- フォルダ開閉トグル
- フィルター切替（unread / starred / all）
- リフレッシュ進捗インジケーター
- 設定ドロップダウン（テーマ・自動更新・言語）

#### 中カラム（アイテムリスト）
- 検索バー（`/` ショートカット）
- 全既読ボタン（unread フィルター時のみ表示）
- フィード/フォルダ選択ドロップダウン（選択的既読マーク）
- 記事一覧（ステータスアイコン、フィード名、日時、タイトル）
- 無限スクロール / 追加読み込み

#### 右カラム（記事ビューア）
- ツールバー: スター、既読マーク、文字外観、Readabilityモードトグル、外部リンク
- 前後記事ナビゲーション
- 記事ヘッダー: タイトル、著者、日時、元リンク
- 本文（HTML 表示）
- メディア表示（音声・動画）

### キーボードショートカット

| キー | 動作 |
|---|---|
| `j` | 次の記事 |
| `k` | 前の記事 |
| `l` | 次のフィード |
| `h` | 前のフィード |
| `o` | 元リンクを新タブで開く |
| `r` | 既読トグル |
| `s` | スタートグル |
| `i` | Readability モードトグル |
| `q` | 記事を閉じる |
| `f` | 下スクロール |
| `b` | 上スクロール |
| `/` | 検索フォーカス |
| `R` | 全既読（unread ビューのみ） |
| `1` | unread フィルター |
| `2` | starred フィルター |
| `3` | all フィルター |

テキスト入力中・修飾キー（Ctrl/Alt/Cmd）押下中はショートカット無効。

### 外観設定
- テーマ: auto / light / dark
- フォント: 設定可能
- フォントサイズ: 設定可能
- レスポンシブ対応（モバイルではサイドバー折りたたみ）

### 相対時刻表示
- 5分以内 → "5m"
- 2時間以内 → "2h"
- 3日以内 → "3d"
- それ以上 → フル日時

---

## OPML

### エクスポート
- OPML 1.1 形式
- フォルダ階層を再帰的に出力
- フィードメタデータ: title, feed URL, site URL
- XML インジェクション対策（HTML エスケープ）

### インポート
- OPML XML をパース
- フォルダ・フィードを一括作成
- ネスト階層に対応
- 文字コード検出（XML 宣言から）

---

## システムトレイ（GUI モード）

- macOS, Windows でトレイアイコン表示
- Linux は CLI のみ
- メニュー: "Open"（ブラウザ起動）、"Quit"（終了）
- `fyne.io/systray` ライブラリ使用
- ビルドタグ `gui` で切り替え

---

## hayari との差分・変更点

| 項目 | yarr（オリジナル） | hayari |
|---|---|---|
| 外部互換 API | Fever API v3 | FreshRSS / Google Reader API |
| フロントエンド | Vue.js 3 | Vanilla JS |
| CSS | 独自スタイル | Pico CSS |
| RSS パーサー | 独自実装 | mmcdole/gofeed 使用 |
| フィルター機能 | なし | 汎用ルールはバックエンド API、フィード単位のタイトルキーワード非表示は Web UI 対応 |

---

## 実装状況・未実装事項（hayari）

### 実装済み

- [x] gofeed への移行（独自パーサーを置き換え）
- [x] Worker: ETag / Last-Modified、アイテム保持ルール、favicon 自動取得
- [x] API: `/api/feeds/errors`, `/api/feeds/:id/icon`, `/opml/import`, `/opml/export`, `/page`
- [x] FreshRSS / Google Reader API の主要エンドポイントとユニットテスト
- [x] UI: Readability モード、テーマ切替、OPML import/export、キーボードショートカット、相対時刻表示、記事タイトル検索、デスクトップのカラム幅リサイズ
- [x] 認証: HMAC-SHA256 署名 Cookie
- [x] ReadKit・NetNewsWire のログイン、同期、既読/スター更新を実機確認

### 未実装・要検討

- [x] Reeder は対応・実機検証の対象外
- [x] フィードエラー表示と汎用フィルター管理は、既存 API/バックエンドの現状で確定（追加 UI は対象外）
- [x] `/page`、フィード取得、favicon 取得の SSRF 対策（DNS 解決後の IP とリダイレクト先を検査）
- [x] HTTPS リバースプロキシ利用時の Secure Cookie 運用を README に記載
- [x] Forgejo Actions の CI・タグリリース・クロスビルド・リリース手順
- [x] Docker イメージなど追加の配布形態は提供しない（単一バイナリによる既存の配布・運用を維持）
