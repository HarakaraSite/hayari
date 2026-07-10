# yarr2 タスクリスト

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
- [ ] Reeder でログイン・購読一覧・未読同期・既読/スター操作を確認
- [ ] NetNewsWire でログイン・購読一覧・未読同期・既読/スター操作を確認
- [ ] ReadKit または別クライアントで smoke test
- [ ] 実機確認で見つかった不足フィールド・レスポンス形式を修正

### Web UI 動作確認
- [ ] フィード追加・更新・記事閲覧のブラウザ smoke test
- [ ] OPML import / export のブラウザ smoke test
- [ ] Readability モードのブラウザ smoke test
- [ ] モバイル幅・タブレット幅・デスクトップ幅で layout regression 確認

---

## 🟡 中優先

### ストレージ・検索
- [ ] `feeds.icon` の保存形式を決める（現状 TEXT data URL。BLOB にするか、このまま統一するか）
- [ ] 検索クエリのエスケープ/構文エラー時の UX を改善
- [ ] `newest_first` 設定を追加するか判断
- [ ] `GET /api/items` の新着順/古い順切替を UI 設定と連携

### フロントエンド
- [ ] パネル幅リサイズ（左カラム・中カラムをドラッグで調整）
- [ ] フィードエラー表示 UI（`/api/feeds/errors` を使う）
- [ ] フィルター管理 UI（一覧・追加・編集・削除）
- [ ] フィルターのルール種別（正規表現 / 部分一致）を選択可能にする
- [ ] item.image の表示対応を検討（使う場合は URL 安全性を維持）

### 認証・運用
- [ ] HTTPS 利用時に Cookie `Secure` を付ける方針を決める
- [ ] GReader トークンを再起動後も維持するか判断（現状はインメモリ）
- [x] `/page`・フィード取得・favicon 取得の SSRF 対策（private / loopback / link-local 等の拒否、リダイレクト先を含む）

### ドキュメント
- [ ] CLAUDE.md 作成（プロジェクト構造・ビルド方法・設計方針）
- [ ] `README.md` の更新（現状の機能・使い方・API・制限事項）
- [x] `docs/yarr-research.md` の未実装リストを現状に合わせて更新
- [ ] `docs/freshrss-api.md` に実装済み/未検証の注記を追加

---

## 🟢 低優先

### ビルド・配布
- [ ] cross-compile ターゲット追加
- [ ] Docker イメージ作成
- [x] GitHub Actions CI（build / `go test -race` / `go vet`。GitHub 上での初回実行は未確認）
- [ ] リリース用 version injection 整備

### 追加機能
- [ ] Fever API 対応を検討
- [ ] Readability 抽出品質の改善
- [ ] favicon 再取得・手動更新
- [ ] フィード別更新間隔
- [ ] 設定の import / export

---

## 実装の推奨順序

```text
1. SSRF 対策、ログインレート制限、HTTP タイムアウトなどのセキュリティ硬化
2. Reeder / NetNewsWire 実機テストと、互換性差分の修正
3. Web UI smoke test とレスポンシブ確認
4. フィードエラー表示 UI と検索 UX 改善
5. README / CLAUDE.md / FreshRSS API ドキュメントの更新
6. Docker・cross compile・リリース整備
7. フィルター管理 UI と追加機能
```
