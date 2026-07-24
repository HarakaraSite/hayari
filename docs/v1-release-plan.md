# Hayari v1.0.0 リリース計画

最終更新: 2026-07-24

## 公開先と前提

- 公開先は Forgejo `forge.harakara.site` とする。
- 正式リポジトリは新規作成する `littleisland/hayari` とする。公開 URL は `https://forge.harakara.site/littleisland/hayari`。
- このチェックアウトは移行元であり、リポジトリ作成、`origin` 設定、初回 push、タグ、Release の作成は、公開承認後にだけ実施する。
- Go の canonical module path、公開 CLI、配布バイナリ、利用者向け名称は `forge.harakara.site/littleisland/hayari`、`hayari`、Hayari（流行）へ統一する。
- 初回安定版は `v1.0.0` を目標とする。公開前の検証で互換性または運用上のブロッカーが残れば、タグを作らずリリース候補として継続する。

## v1 に含める要件

- シングルユーザーのセルフホスト RSS/Atom リーダー。
- フォルダ、フィード、記事、既読、スター、検索、OPML、フィルター、定期更新。
- 3 ペイン Web UI、レスポンシブ表示、テーマ、キーボード操作、Readability。
- FreshRSS / Google Reader 互換 API と、ReadKit・NetNewsWire で確認済みの同期。
- SQLite と埋め込みアセットによる単一バイナリ運用。
- SSRF 対策、ログインレート制限、HTTP タイムアウト、ヘルスチェック、バージョン表示。
- 外部公開時の TLS 終端リバースプロキシ、認証、Secure Cookie。

## 対象外

- マルチユーザー、ロールベース権限、組み込み TLS。
- PWA、オフライン利用、Service Worker、WebSub、プッシュ通知。
- 高度なフィルター DSL、フォルダ単位フィルター、任意テーマ編集、多言語 UI。
- Prometheus などのメトリクス基盤、記事本文の自動全文クロール、Fever API。
- 確認済みクライアントが必要としない FreshRSS API（`rename-tag`、`disable-tag` など）と GReader トークンの再起動後永続化。
- Docker イメージ、パッケージマネージャー配布、署名付きインストーラー、macOS notarization、GUI 版の正式配布。
- フィードエラー管理 UI、フィルター管理 UI など既存バックエンド機能向けの追加 UI。
- スマホ専用の別レイアウト・ナビゲーション・操作体系を新設するモバイル専用 UI。現行 UI のレスポンシブ調整と操作不能箇所の修正は v1 の受け入れ確認に含める。

## リリース基準

以下をすべて満たした候補コミットだけを公開する。

- `go test ./...`、`go test -race ./...`、`go vet ./...`、JavaScript 構文確認、`git diff --check` が成功する。
- Darwin、Linux、Windows の amd64/arm64 向けサーバーバイナリを `CGO_ENABLED=0` でビルドできる。
- `hayari --version` と `/api/status` が同一のタグを返す。
- 初回起動、ログイン、フィード追加、記事閲覧、既読・スター、更新、設定保存、OPML を実機確認する。
- ReadKit と NetNewsWire でログイン、同期、`edit-tag` による既読またはスター更新を確認する。
- private/loopback/link-local 宛て取得の拒否、リダイレクト検証、本文サイズ制限、無認証外部 bind 拒否を回帰確認する。
- README 英語版・日本語版、ライセンス、変更履歴、バックアップ・復元・更新手順、配布物、チェックサムを揃える。

## 実行計画

### 1. リリース境界を固定する

1. 現在の `main`、作業ツリー、リモート、タグ、CI 実行状況を記録する。
2. `docs/tasks.md`、`docs/yarr-research.md`、本書の完了済み項目を現実装へ同期し、古い未完了記述を残さない。
3. v1 の必須要件、対象外、確認済みクライアント、配布対象をチェックリスト化する。
4. 以降はセキュリティ、データ整合性、同期互換性、配布不能を除く新機能追加を凍結する。

### 2. 製品アイデンティティと安全な起動設定を仕上げる

1. ユーザーに見える製品名、CLI、バイナリ、UI、Basic realm、OPML 名、User-Agent、既定 DB を Hayari／`hayari` に統一する。
2. `make build` と `make run` が `cmd/hayari` を使い、`hayari` を出力することを確認する。
3. 認証情報をコマンドラインや履歴へ残さない推奨経路を追加する。明示フラグとの優先順位、ユーザー名とパスワードの片方だけが設定された場合の拒否をテストで固定する。
4. 無認証外部 bind 拒否、Secure Cookie、GReader GET ログイン既定拒否を維持する。

### 3. 運用・利用ドキュメントを公開内容へ同期する

1. `README.md` と新規 `README.ja.md` に、ダウンロード、初期起動、認証、データパス、対応 OS/arch、HTTPS リバースプロキシ、既知の制約を記載する。
2. Caddy または nginx の最小構成例で、loopback bind、HTTPS、Secure Cookie を必須条件として説明する。
3. SQLite WAL に適したバックアップ、別ディレクトリへの復元、バイナリ更新、ロールバックを文書化する。単純コピーを推奨しない。
4. FreshRSS 入口、Google Reader 入口、確認済みクライアント、単一フォルダ制約、トークン再起動失効、未実装 API を `docs/freshrss-api.md` に明記する。
5. `LICENSE` と `CHANGELOG.md` を追加し、ライセンス表記、著作権者、v1.0.0 の追加・安全性・既知の制約を一致させる。

### 4. Forgejo リポジトリとリリース自動化を準備する

1. Forgejo に `littleisland/hayari` を作成する。作成後、表示 URL、owner、既定ブランチ、公開可否を読み戻す。
2. 現在の履歴を保ったまま `origin` を `https://forge.harakara.site/littleisland/hayari.git` に設定し、初回 push 前に対象 SHA を確認する。
3. 通常 CI で build、race test、vet、JavaScript 構文確認を実行する。
4. Forgejo Actions のタグ起点 workflow を追加する。`v*` タグで、6 ターゲットのバイナリ、OS/arch ごとのアーカイブ、`SHA256SUMS`、CHANGELOG のリリースノートを生成し、全品質ゲート成功後にだけ Release へ添付する。
5. workflow の権限と Forgejo のリリース API 互換性を実環境で確認する。自動添付が未設定なら、同じ成果物とチェックサムを手動で添付できる手順を残す。

### 5. リリース候補を検証する

```sh
git diff --check
go mod verify
go test ./...
go test -race ./...
go vet ./...
node --check src/assets/javascripts/api.js
node --check src/assets/javascripts/app.js
node --check src/assets/javascripts/key.js
```

続けて、新しい一時 DB で以下を確認する。

- `/healthz`、認証拒否、Cookie ログイン、Basic 認証、GReader POST ログイン、GET の既定拒否、CRUD、SIGTERM 終了。
- サイト URL からのフィード検出、直接 RSS/Atom URL の追加、フォルダ作成と移動、更新、既読、スター、検索、OPML import/export、Readability。
- auto/light/dark/beige テーマ、カラム幅の保持と狭い画面でのクランプ、ネットワーク失敗後に busy 表示が残らないこと。
- ReadKit と NetNewsWire の初回同期、複数ページ取得、既読・未読復帰・スターの双方向同期、サーバー再起動後の再認証、差分同期。
- HTTPS リバースプロキシ経由の同期、HTTP から HTTPS への転送、Secure Cookie、パスワードを URL やプロキシログへ渡さない構成。

### 6. 常用データの保護を確認する

1. 実 DB と WAL/SHM の有無を確認する。
2. 文書化した方法で時刻付きバックアップを作成する。
3. バックアップを別の一時ディレクトリへ復元し、候補バイナリで起動する。
4. `/healthz`、フォルダ数、フィード数、記事数、既読・スター、検索、OPML export を確認する。
5. 常用 DB への適用前に旧バイナリを版番号・SHA 付きで退避する。

### 7. 候補を凍結して公開する

1. 実装、テスト、文書、workflow、ライセンスだけが候補差分に含まれることを read-only レビューする。
2. 認証、SSRF、データ破損、同期欠落、配布不能以外は既知の制約または後続 issue として記録し、対象を広げない。
3. 候補コミットを作成し、候補 SHA 上で品質ゲートと短縮 smoke を再実行する。
4. 明示的な公開承認後に `main`、注釈付き `v1.0.0` タグ、タグを push する。
5. Forgejo Actions 完了後に、Release のタグ、候補 SHA、成果物名、サイズ、チェックサムを読み戻す。
6. 公開アセットを新しい一時ディレクトリへ取得し、チェックサム、`hayari --version`、`/healthz`、Web ログイン、1 クライアントの同期 smoke を確認する。

## ロールバック

起動・DB マイグレーション失敗、データ喪失、継続的更新失敗、同期欠落または状態反転、認証回避、SSRF 防御後退、タグ・埋め込みバージョン・配布物・チェックサムの不一致、常用ブラウザでの中核操作不能は公開停止または修正版のブロッカーとする。

スキーマ変更がない場合は旧バイナリへ戻して再起動する。スキーマ変更がある場合は、停止後にリリース直前バックアップから DB も復元する。公開物に問題があれば同じタグを付け替えず、修正後に `v1.0.1` を発行する。

## 完了条件

- Forgejo の `littleisland/hayari` が正式なソースと Release の公開先である。
- Hayari／`hayari` が利用者向け表面、Go module path、バイナリ、文書で一貫している。
- 採用済み要件と対象外が最新文書に保存されている。
- 自動品質ゲート、手動受け入れ、実 DB のバックアップ・復元、配布物の再取得検証が候補 SHA で成功している。
- タグ、コミット、埋め込みバージョン、Release、配布物、チェックサムが一致している。
