# yarr2 v1 リリースプラン

最終更新: 2026-06-21
方針: **機能追加より品質・安定性を優先**。既存機能は実装済み・レビュー済みのため、v1 は「安心してインターネットに晒せる状態」に仕上げることをゴールとする。

---

## 1. v1 スコープ定義

### v1 に含めるもの（＝既に実装済み・磨き込む対象）

- フォルダ / フィード / アイテムの CRUD・ステータス・スター管理
- 全文検索（FTS4）
- OPML インポート / エクスポート
- GReader/FreshRSS 互換 API（Reeder / NetNewsWire 実機検証込み）
- Web UI（3 ペイン、レスポンシブ、ダーク/ライト、無限スクロール、キーボードショートカット）
- Readability モード（`/page`）
- フィルタールール、定期リフレッシュ、favicon 取得
- HMAC Cookie 認証 + Basic 認証 + GReader トークン認証
- 単一バイナリ配布（asset embed、`gui` ビルドタグ）

### v1 に含めないもの（明示的に v1.x / v2 送り）

- マルチユーザー / ロールベース権限（シングルユーザー前提を維持）
- 組み込み TLS（**リバースプロキシ前提**として割り切る。`docs` で明記）
- PWA / オフライン / Service Worker
- プッシュ通知 / WebSub（PubSubHubbub）
- フィルターの高度化（正規表現以外のDSL、フォルダ単位適用など）
- テーマカスタマイズ、多言語 UI
- メトリクス / Prometheus エクスポーター（`/healthz` 程度に留める）
- アイテム本文の全文クロール自動化（手動 Readability のみ）

スコープ判断基準: 「セキュリティ・データ整合性・互換性に関わるもの」は v1、「あると嬉しい機能」は v1 以降。

---

## 2. リリース前チェックリスト（カテゴリ別・優先度順）

凡例: 🔴必須(ブロッカー) / 🟡推奨 / 🟢任意

### A. セキュリティ

- 🔴 **SSRF 対策**: `/page`（`routes.go:545`）と feed/favicon フェッチ（`worker/crawler.go`, `worker/client.go`）はユーザー指定 URL へサーバーがリクエストする。現状 `127.0.0.1`・`169.254.169.254`(メタデータ)・`10.0.0.0/8` 等の内部宛先を弾いていない。DNS 解決後の IP を検証する dialer を入れる（プライベート/ループバック/リンクローカルを拒否）。リダイレクト先も再検証すること。シングルユーザーでも、悪意あるフィードや細工された記事リンクで踏める。
- 🔴 **ログイン総当たり対策**: `/login` と `/accounts/ClientLogin` にレート制限が無い。IP 単位の簡易レートリミット（失敗 N 回で一時ロック / バックオフ）を入れる。`credentialsMatch` は定数時間比較済みなのでそこは OK。
- 🔴 **認証情報の渡し方**: 現状 `--pass` は CLI フラグ（プロセス一覧・シェル履歴に残る）。環境変数 `YARR2_PASS` 対応を追加し、README で平文パスワード保存ではなくこちらを推奨。可能なら bcrypt ハッシュ受け入れ（`--pass-hash`）も検討（🟡）。
- 🔴 **「認証未設定＝全許可」の挙動**: `middleware.go:10` と `auth.go:108` で user/pass 空なら誰でも素通り。dev では妥当だが、本番でうっかり無認証公開する事故源。起動時に「認証なしで `127.0.0.1` 以外にバインドしている」場合は警告ログ（または拒否）を出す。
- 🟡 **GReader トークンのメモリ揮発**: `greader.go:21` の `tokens` は再起動で消える。クライアントは 401 で再ログインするため致命的ではないが、頻繁な再起動運用だと体感が悪い。v1 は「仕様」として docs 化で可。DB 永続化は v1.x。
- 🟡 **正規表現フィルタの ReDoS**: `worker/worker.go:224` で `regexp.MatchString(f.Rule, field)` をユーザー入力ルールで実行。Go の RE2 は破滅的バックトラッキングは無いが、フィルタ作成時に `regexp.Compile` で検証し、不正な正規表現を弾く（現状は実行時に毎回コンパイル & エラー無視）。コンパイル結果のキャッシュも兼ねられる。
- 🟡 **Cookie の Secure 属性**: `auth.go:116` の Cookie に `Secure` が無い。TLS 終端をプロキシに任せる前提なら、設定 or `X-Forwarded-Proto` を見て `Secure` を付ける。
- 🟡 **セキュリティヘッダ**: `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy` 等を全レスポンスに付与するミドルウェア。サニタイズ済みとはいえ多層防御。
- 🟢 favicon/`/page` の取得サイズ上限は実装済み（512KB / 1MB / 2MB）。タイムアウトも 15s で設定済み。✓

### B. 運用性

- 🔴 **`/healthz`（または `/api/healthz`）**: 認証不要の軽量ヘルスチェック。リバースプロキシ / コンテナのliveness probe 用。DB ping 程度を返す。
- 🔴 **バージョン情報の貫通**: `routes.go:76` の `handleStatus` が `"version": "dev"` ハードコード。`main.Version`（ldflags 注入済み）を `Server` 経由で渡し、`/api/status` と `--version` を一致させる。
- 🟡 **構造化 / レベル付きロギング**: 現状 `log.Printf` 直書き。最低限「リクエストログ（メソッド・パス・ステータス・所要時間）」のミドルウェアと、ログレベル（`--verbose`）を入れる。`log/slog` 採用が軽量で良い。
- 🟡 **HTTP サーバーのタイムアウト**: `server.go:47` の `http.Server` に `ReadHeaderTimeout` / `ReadTimeout` / `WriteTimeout` / `IdleTimeout` が未設定。Slowloris 対策として必須級（🔴寄りの🟡）。
- 🟡 **Graceful shutdown のタイムアウト**: `server.go:57` の `Shutdown(context.Background())` は無期限。`context.WithTimeout` で上限を付ける。
- 🟡 **DB バックアップ手順**: WAL モードのため単純コピーは不整合の恐れ。`sqlite3 .backup` または `VACUUM INTO` を使う手順を docs 化。可能なら `--backup` サブコマンド（🟢）。
- 🟢 **設定の可視性**: 起動時に有効な設定（addr / db path / 認証有無 / refresh interval）を 1 行ログ出力。

### C. GReader API 互換性（実機テスト前提整備）

- 🔴 **Reeder / NetNewsWire 実機テスト**: 下記フローを各クライアントで通す。
  - ログイン → サブスクリプション同期 → 未読数 → ストリーム取得 → 既読/スター同期 → mark-all-as-read → 購読追加/削除。
- 🟡 **`stream/items/contents` の POST/GET 両対応確認**: `greader.go:469` は `r.ParseForm()` で読むが、クライアントによっては GET クエリで `i=` を送る。実機で要確認。
- 🟡 **`continuation` ページングの整合**: `greaderStreamItemIDs`（`greader.go:433`）は offset ベースの continuation。取得中に既読化で並びが変わると取りこぼし/重複の可能性。NetNewsWire の大量同期で要検証。
- 🟡 **タイムスタンプ単位の検証**: `unread-count` は usec、`edit-tag`/`mark-all` の `ts=` は nsec（`greader.go:661`）。クライアント実装差で取りこぼしが出やすい箇所。実機ログで確認。
- 🟡 **`OldestFirst`/`r=o` と `ot=`/`xt=` の組合せ**: 同期方向（最古→最新）でクライアントが使う。実機で抜けが無いか確認。
- 🟢 **`subscription/edit` の複数フォルダ非対応**: GReader はフィードに複数ラベル可だが本実装は単一フォルダ。docs に既知の制約として明記。

### D. フロントエンド UX

- 🟡 **エラー表示**: フィード追加失敗・ネットワークエラー時のユーザー向けフィードバック（トースト等）が十分か確認。`app.js` の fetch エラーハンドリングを通しで点検。
- 🟡 **空状態 / ローディング表示**: 初回起動（フィード 0 件）、検索ヒット 0 件、リフレッシュ中インジケータ。
- 🟢 **キーボードショートカット一覧**: `?` でヘルプ表示。
- 🟢 **モバイルでの操作性最終確認**: pane 切り替え、無限スクロールの実機確認。

### E. パフォーマンス / スケーラビリティ

- 🟡 **DB インデックス確認**: `migration.go` を見て `items(feed_id)`, `items(status)`, `items(starred)`, `items(date)`, `feeds(folder_id)` 相当のインデックスが揃っているか確認。`ListItems` の `JOIN feeds` + `ORDER BY date` が効くこと。数千件規模でのレスポンス計測。
- 🟡 **SQLite 接続設定**: `database/sql` は接続プールを張るが、SQLite 書き込みは直列。`db.SetMaxOpenConns(1)`（または書き込み用と読み取り用の分離）で `database is locked` を避ける検討。WAL なので読み取りは並行可。並列フィード更新（`concurrentFeeds=5`）と Web リクエストの競合を負荷テスト。
- 🟢 **大量フィード時の `unread-count`/`subscription/list` のN+1感**: 現状はまとめて取得しメモリで集計しており妥当。数百フィードで計測のみ。

### F. ドキュメント / 配布

- 🔴 **README の実運用向け追記**: リバースプロキシ（nginx / Caddy）背後での TLS 配置例、環境変数、認証必須の警告、バックアップ手順、systemd unit 例。
- 🔴 **リリースバイナリのクロスコンパイル & CI**: `go-sqlite3` は CGO 依存。Linux(amd64/arm64) / macOS / Windows のビルドマトリクスを GitHub Actions で。CGO クロスコンパイルの toolchain（zig cc 等）を整備。
- 🟡 **Dockerfile + イメージ公開**: マルチステージビルド。SQLite の CGO ビルドが通る base を選ぶ。
- 🟡 **CHANGELOG / LICENSE 確認**: LICENSE は MIT 表記済み。CHANGELOG を起こす。
- 🟢 **スクリーンショット / GIF を README に**。

### G. テスト / 品質ゲート

- 🟡 **CI で `go test ./...` + `go vet` + `golangci-lint`**。既存テスト（auth/greader/routes/storage/worker/parser/content）はあるので、SSRF・レート制限・新規ヘルスチェックのテストを追加。
- 🟡 **`-race` 付きテスト**: トークンマップ、worker の atomic/mutex 周りの競合検出。
- 🟢 **手動 E2E チェックリスト化**（C・D の項目を runbook 化）。

---

## 3. フェーズ分けロードマップ（目安）

個人開発・週末ベースを想定した現実的な工数感。専業なら 1/2〜1/3 に圧縮可。

### Phase 0: 足場固め（〜0.5 週）
- CI セットアップ（`go test` / `vet` / lint、`-race`）
- バージョン貫通（`main.Version` → `/api/status`）
- 起動時ログ整備、`/healthz`
→ 小さく終わる地味タスクをまとめて片付け、以降の変更を安全に回す土台を作る。

### Phase 1: セキュリティ硬化（1〜1.5 週）★最重要
- SSRF ガード（dialer ベース、`/page`・feed・favicon すべて）+ テスト
- ログインレートリミット + テスト
- HTTP サーバータイムアウト群、graceful shutdown タイムアウト
- 「無認証で外部バインド時」警告、Cookie Secure、セキュリティヘッダ
- 正規表現フィルタの作成時検証 + コンパイルキャッシュ
- 環境変数での認証情報受け渡し

### Phase 2: GReader 互換実機検証（1〜2 週、テスト主体）
- Reeder / NetNewsWire で全フロー実機テスト
- continuation ページング・タイムスタンプ単位・同期方向の不具合修正
- 既知の制約（複数ラベル非対応・トークン揮発）を docs 化
→ コードよりも「実機で踏んで直す」反復が中心。バッファを厚めに。

### Phase 3: 運用性 & パフォーマンス（0.5〜1 週）
- 構造化ログ + リクエストログミドルウェア
- DB インデックス監査・`SetMaxOpenConns` 調整・負荷計測
- バックアップ手順（必要なら `--backup`）

### Phase 4: フロント UX 仕上げ（0.5 週）
- エラートースト・空状態・ローディング
- ショートカットヘルプ、モバイル実機確認

### Phase 5: 配布 & ドキュメント（0.5〜1 週）
- クロスコンパイル CI（CGO 対応）、リリースバイナリ
- Dockerfile、systemd unit、README 実運用セクション、CHANGELOG
- リリースタグ `v1.0.0`

**合計目安: 約 4.5〜7 週（週末ベース）**。クリティカルパスは Phase 1（セキュリティ）と Phase 2（実機互換）。この 2 つが終われば「安全に公開できる」状態になり、Phase 3〜5 は並行・前後可。

---

## 4. リスク・懸念事項

| リスク | 影響 | 対応 |
|---|---|---|
| **SSRF を v1 に入れ損ねる** | 内部ネットワーク・クラウドメタデータ漏洩。公開リーダーの最大の穴 | Phase 1 でブロッカー扱い。リダイレクト先再検証まで含めてテスト |
| **GReader 互換の取りこぼし/重複同期** | クライアントで既読が戻る・記事が消える等、信頼を損なう | 実機テストを工数の柱に。offset continuation の挙動を重点検証 |
| **CGO クロスコンパイルの難しさ** | リリースバイナリが出せず配布が詰まる | 早めに Phase 0/5 で PoC。詰まるなら `modernc.org/sqlite`（pure-Go）への差し替えを保険として評価 |
| **無認証デフォルトの誤公開** | データ全消し・乗っ取り | 起動時警告＋README 強調。可能なら外部バインド時は認証必須化 |
| **SQLite の `database is locked`** | 並列更新中の書き込み失敗 | `SetMaxOpenConns(1)` + WAL + 負荷テストで検証 |
| **シングル開発者のレビュー限界** | セキュリティ実装のミス | SSRF・認証まわりは `deep-reviewer` サブエージェント（Opus）に PR 前レビュー依頼 |
| **スコープクリープ** | 機能追加で v1 が遠のく | 本書の「含めないもの」を都度参照し、機能要望は v1.x issue へ |

---

## 5. 配布・デプロイ戦略

### 配布形態
1. **単一バイナリ**（第一級）: `make build` で asset embed 済みバイナリ。GitHub Releases に OS/arch 別で添付。CGO のため各 OS でのネイティブ or クロス（zig cc）ビルドを CI で。
2. **Docker イメージ**: マルチステージ。`/var/lib/yarr2/yarr.db` を volume に。`ghcr.io` 公開。
3. **デスクトップ（`gui` タグ）**: systray 版。個人 PC 常駐用途。リリースに含めるかは任意（🟢）。

### 推奨デプロイ構成（README に明記）
```
[Reeder/NNW / ブラウザ] ──TLS──> [Caddy/nginx リバースプロキシ] ──> [yarr2 127.0.0.1:7070]
```
- yarr2 自身は **`127.0.0.1` バインド + 認証必須**。TLS・証明書・HSTS はプロキシに委譲（v1 では組み込み TLS 非対応を明言）。
- プロキシで `X-Forwarded-Proto` を渡し、yarr2 側は Cookie `Secure` を有効化。
- `systemd` unit 例（`Restart=on-failure`, `DynamicUser`, `StateDirectory=yarr2`）を同梱。

### リリース手順（runbook）
1. `CHANGELOG` 更新、バージョン確定
2. `make test`（`-race`）+ lint green、GReader 実機チェックリスト消化
3. `git tag v1.0.0` → CI がクロスビルド & Releases 添付 & Docker push
4. README のインストール手順を実バイナリで一度なぞって検証
5. アナウンス

### バックアップ / アップグレード
- バックアップ: `sqlite3 yarr.db ".backup backup.db"`（WAL 整合のため単純 cp 非推奨）を cron 例として提示。
- アップグレード: マイグレーションは起動時自動（`migration.go` バージョン管理済み）。バイナリ差し替え→再起動のみ。**アップグレード前バックアップ**を手順で必須化。ダウングレード非対応を明記。

---

## 付録: コードベースで確認した具体ポイント（着手時の参照用）

- `src/server/routes.go:545` `handlePage` — SSRF 対象その1（`http.Client` 直 Get）
- `src/worker/crawler.go:16` `crawlerClient` / `FindFeeds` / `FetchFavicon` — SSRF 対象その2
- `src/worker/client.go` `Fetch` — feed 取得、SSRF 対象その3
- `src/server/auth.go:99` `handleWebLogin` / `greader.go:52` `greaderLogin` — レート制限なし
- `src/server/middleware.go:10`, `auth.go:108` — 認証未設定＝全許可
- `src/server/server.go:47` — `http.Server` タイムアウト未設定 / `:57` Shutdown 無期限
- `src/server/routes.go:76` — `version: "dev"` ハードコード（`main.Version` 未使用）
- `src/worker/worker.go:224` — フィルタ正規表現を実行時コンパイル・エラー無視
- `src/server/auth.go:116` — Cookie に `Secure` なし
- `src/server/greader.go:21` — GReader トークンはメモリ揮発（再起動で消える）
- `src/storage/storage.go:22` — DSN は `WAL + foreign_keys`。`SetMaxOpenConns` 未設定
- `src/storage/item.go:279` `DeleteOldItems` — 90日 / 各feed 50件保持 / starred保護（仕様確認済み）
