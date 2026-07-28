# hayari タスクリスト

最終更新: 2026-07-10

実装状況の追跡用。優先度: 🔴高 / 🟡中 / 🟢低

---

## ✅ 完了

### 基盤
- [x] プロジェクト基本構造（Go モジュール・ディレクトリ構成）
- [x] SQLite ストレージ（folders / feeds / items / filters / settings）
- [x] マイグレーション機構
- [x] 単一バイナリ向け frontend asset embed
- [x] Pico CSS ベースの Vanilla JS フロントエンド
- [x] systray 対応（gui ビルドタグ）
- [x] makefile の基本ターゲット（build / build-gui / run / test / clean）

### パーサー・コンテンツ処理
- [x] gofeed への移行（独自パーサー削除）
- [x] RSS / Atom / JSON Feed の基本パース
- [x] HTML サニタイズ（bluemonday）
- [x] `javascript:` など危険なリンクの保存時除去
- [x] Readability 用 `/page` レスポンスのサーバー側サニタイズ

### ストレージ
- [x] `folders.is_expanded` カラム追加
- [x] `feeds.last_error` カラム追加
- [x] `UpdateFeedError(id, errMsg)` 追加
- [x] `UpdateFeedFolder(id, folderID)` 追加（folder_id を NULL にできる）
- [x] `UpdateFeedMeta` 追加（空フィールドのみ補完）
- [x] `UpdateFeedIcon` 追加（icon 未設定時のみ保存）
- [x] `search` 仮想テーブル追加（FTS5、既存 FTS4 DB の移行を含む）
- [x] `ListItems` / `CountItems` の全文検索を FTS に切り替え
- [x] アイテム保持ルール（90日超削除・各フィード最低50件保持・スター付き保護）
- [x] スター状態を `items.starred` に分離し、既存 `status='starred'` を移行
- [x] `status` を read/unread の2値に統一し、ItemFilter・REST API・Web UI・GReader API を追従
- [x] スター付き未読 item の回帰テストを追加
- [x] SQLite 接続を 1 本に制限し、並列更新時の `SQLITE_BUSY` を防止

### Worker
- [x] 並列フィード更新
- [x] ETag / Last-Modified 送信
- [x] gofeed による取得・解析・item 保存
- [x] フィードエラーを DB に保存
- [x] 成功時に `last_error` をクリア
- [x] favicon 自動取得・保存（初回フィード取得時）
- [x] `settings.refresh_rate` から自動更新間隔を読み込み
- [x] `refresh_rate=0` による自動更新無効化
- [x] 設定変更を最大1分で反映
- [x] 24時間ごとの古い item cleanup
- [x] フィルタールール（title/content/author 正規表現、mark_read/hide）を item 作成時に適用

### 独自 REST API
- [x] フィード CRUD
- [x] フィード検出（`GET /api/feeds/find`）
- [x] 全フィード更新（`POST /api/feeds/refresh`）
- [x] フィードエラー取得（`GET /api/feeds/errors`）
- [x] favicon 取得（`GET /api/feeds/:id/icon`、ETag キャッシュ付き）
- [x] フォルダ CRUD
- [x] `PUT /api/folders/:id` の `is_expanded` 対応
- [x] item 一覧・件数・検索・ページング
- [x] item status 更新
- [x] mark all read
- [x] settings API（theme / font_size / refresh_rate のホワイトリスト）
- [x] stats API（フィード別未読数）
- [x] OPML import / export
- [x] `/page?url=...` 記事本文クロール（Readability モード用）

### FreshRSS / Google Reader API
- [x] `POST/GET /accounts/ClientLogin`
- [x] `GET /reader/api/0/token`（CSRF トークン検証なし）
- [x] `GET /reader/api/0/user-info`
- [x] `GET /reader/api/0/subscription/list`
- [x] `POST /reader/api/0/subscription/edit`（subscribe / unsubscribe / edit）
- [x] `POST /reader/api/0/subscription/quickadd`
- [x] `GET /reader/api/0/tag/list`
- [x] `GET /reader/api/0/unread-count`（`newestItemTimestampUsec` 含む）
- [x] `GET /reader/api/0/stream/contents/{stream-id}`
- [x] stream_id パスパラメータ（reading-list / feed / label / state）
- [x] `xt=` read 除外、`it=` 絞り込み、`ot=` 日時、`r=o` oldest first
- [x] continuation ページネーション
- [x] レスポンス `categories`（read / starred / label）
- [x] `GET /reader/api/0/stream/items/ids`
- [x] `POST /reader/api/0/stream/items/contents`
- [x] `POST /reader/api/0/edit-tag`
- [x] `POST /reader/api/0/mark-all-as-read` の `ts`（ナノ秒）対応
- [x] GReader トークン TTL 30日 + ログイン時 prune

### 認証・セキュリティ
- [x] Web UI を HMAC-SHA256 署名 Cookie 方式に変更
- [x] `auth_secret` を DB に永続化
- [x] Cookie 検証に `subtle.ConstantTimeCompare` を使用
- [x] Cookie に `HttpOnly` / `SameSiteLaxMode` / 1週間期限を設定
- [x] `GET /api/settings` で `auth_secret` を返さない
- [x] `PUT /api/settings` で `auth_secret` を書き換え不可
- [x] 静的アセット（index.html 含む）を認証保護
- [x] Web / GReader ログインの IP 単位レート制限（5 回連続失敗で 15 分ロック、成功時リセット）
- [x] 無認証の non-loopback bind を既定で拒否（明示的な insecure opt-in を除く）
- [x] HTTPS プロキシ向け Secure Cookie opt-in
- [x] フィード取得の 5 MiB サイズ上限

### 運用性
- [x] 認証不要の `/healthz`（SQLite 接続確認）
- [x] ビルド時の `main.Version` を `/api/status` に反映
- [x] HTTP サーバーの Read / Write / Idle タイムアウトと graceful shutdown の上限

### フロントエンド（UI）
- [x] 3カラムレイアウト
- [x] モバイル/タブレット向け pane 遷移（sidebar / list / detail）
- [x] サイドバーのフォルダ開閉状態を永続化
- [x] フィード・フォルダ別未読数バッジ
- [x] リフレッシュ中の `aria-busy` 表示
- [x] 相対時刻表示（now / m / h / d / 日付）
- [x] 記事 detail toolbar（スター・既読・外部リンク・Readability・前後移動）
- [x] 前後記事ナビゲーション
- [x] Readability モード
- [x] 検索 UI
- [x] 無限スクロール
- [x] フィード追加（検出 → 候補選択 → subscribe）
- [x] フィード/フォルダ編集・削除 UI
- [x] テーマ切替（auto / light / dark）
- [x] フォントサイズ設定
- [x] 自動更新間隔設定
- [x] OPML import / export UI
- [x] キーボードショートカット
  - [x] `j/k` — 次/前の記事
  - [x] `l/h` — 次/前のソース
  - [x] `o` — 元リンクを新タブで開く
  - [x] `r` — 既読トグル
  - [x] `s` — スタートグル
  - [x] `i` — Readability モードトグル
  - [x] `q` — 記事を閉じる
  - [x] `f/b` — 記事スクロール前/後
  - [x] `/` — 検索フォーカス
  - [x] `R` — 全既読
  - [x] `1/2/3` — unread/starred/all フィルター切替
  - [x] 入力中・モーダル表示中は無効化

### テスト
- [x] パーサーテスト
- [x] コンテンツサニタイズテスト
- [x] ストレージ層テスト（CRUD・検索・保持ルール・エラー・マイグレーション）
- [x] Worker テスト（モック HTTP サーバー）
- [x] REST API テスト
- [x] GReader API テスト

---

## 🔴 高優先

### GReader 互換性の実機確認
- [x] ReadKit でログイン・購読一覧・未読同期・既読/スター操作を確認
- [x] NetNewsWire でログイン・購読一覧・未読同期・既読/スター操作を確認
- [x] 実機確認で見つかった不足フィールド・レスポンス形式を修正
- [x] Reeder は対応・実機検証の対象外とする

### Web UI 動作確認
- [x] フィード追加・更新・記事閲覧のブラウザ smoke test（2NN RSS、2026-07-10）
- [x] OPML import / export のブラウザ smoke test（実運用 yarr の OPML、2026-07-10）
- [x] Readability モードのブラウザ smoke test（2026-07-10）
- [x] モバイル幅・タブレット幅・デスクトップ幅で layout regression 確認（2026-07-10）

---

## 🟡 中優先

### ストレージ・検索
- [x] `feeds.icon` は TEXT data URL として保存（取得・ETag キャッシュ付き配信まで実装済み）。BLOB へは移行しない
- [x] Web UI の記事タイトル検索（FTS5 trigram による日本語の部分一致）
- [x] Web UI の既定の並び順は新着順とする。並び順設定は当面追加しない
- [ ] 将来必要になった場合、`GET /api/items` の新着順/古い順切替を UI 設定と連携

### フロントエンド
- [x] フィード追加後、初回取得の完了を待ってサイドバー・記事一覧を自動更新する
- [x] パネル幅リサイズ（左カラム・中カラムをドラッグで調整）
- [x] フィードエラー表示・継続取得不能なフィードの扱いは、既存 API/バックエンドの現状で確定（追加 UI は対象外）
- [x] 汎用フィルター管理 UI とルール種別選択は対象外。フィード単位のタイトルキーワード非表示フィルタを Web UI で提供する
- [x] item.image の表示対応は不要（記事本文内の画像表示は維持し、RSS enclosure 等の代表画像を別途 UI 表示しない）

### 認証・運用
- [x] HTTPS リバースプロキシ利用時の `--secure-cookie` 運用を README に記載
- [x] GReader トークンはインメモリのみとし、再起動後はクライアントが再ログインする現仕様を維持
- [x] `/page`・フィード取得・favicon 取得の SSRF 対策（private / loopback / link-local 等の拒否、リダイレクト先を含む）

### ドキュメント
- [x] README 英語版・日本語版（機能・使い方・API・制限事項）
- [x] `docs/freshrss-api.md` / `docs/freshrss-api.ja.md` に実装済み・確認済み・未実装 API を記載
- [x] `docs/yarr-research.md` の未実装リストを現状に合わせて更新

---

## 🟢 低優先

### ビルド・配布
- [x] ローカル用 cross-compile ターゲットは追加しない（リリース前の手動6対象検査と Forgejo CI の6対象クロスビルドで十分）
- [x] Docker イメージは提供しない（単一バイナリによる既存運用を維持）
- [x] Forgejo Actions CI / タグリリース（6対象クロスビルド、チェックサム、version injection）

### 追加機能
- [x] Fever API は対応しない（FreshRSS / Google Reader API の現行互換を維持）
- [x] Readability 抽出品質の改善は現時点では行わない（具体的な問題が出た場合に再検討）
- [ ] 未取得フィードfaviconの再試行を検討（起動時に `icon IS NULL` のフィードを再取得する案。既存faviconの手動更新は必要性が出た場合に併せて検討）
- [x] フィード別更新間隔は追加しない（全フィード共通の `refresh_rate` を維持）
- [x] 設定の import / export は追加しない（フィード・フォルダ移行は既存の OPML import / export を維持）

---

## 実装の推奨順序

```text
1. 記事の並び順・favicon 保存形式など、未決のデータ/UI 方針を決める
2. Docker・配布形態など、必要になった運用機能を選択する
3. Fever API、翻訳、フィード別更新間隔など、将来機能を個別に検討する
```
