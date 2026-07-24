- 2026-07-10 15:48 [codex] docs/tasks.md と docs/yarr-research.md を実装へ同期。次は v1 Phase 0（CI・healthz・version）または SSRF 対策。
- 2026-07-10 15:56 [codex] Phase 0 完了: /healthz、/api/status の Version 貫通、GitHub Actions CI を追加。go test/vet/race は成功。次は SSRF 対策（/page・feed・favicon）。
- 2026-07-10 16:04 [codex] SSRF 対策を実装: safehttp が public HTTP(S) のみ許可し、/page・feed・favicon・リダイレクトを保護。go test/vet/race 成功。次はログインレート制限または HTTP サーバータイムアウト。
- 2026-07-10 16:09 [codex] Web/GReader ログインに IP 単位のレート制限を追加（5 回失敗で15分ロック、成功でリセット）。go test/vet/race 成功。次は HTTP サーバータイムアウト。
- 2026-07-10 16:14 [codex] HTTP server に ReadHeader 10s、Read/Write 30s、Idle 2m、shutdown 10s を設定。go test/vet/race 成功。次は無認証の外部 bind 警告または Cookie Secure/セキュリティヘッダ。
- 2026-07-10 16:24 [codex] レビュー指摘を修正: ログイン失敗 IP 記録に15分TTLと10,000件上限を追加。go test/vet/race 成功。変更一式をコミット予定。
- 2026-07-10 18:50 [codex] 全体レビューの5件を修正: keyset continuation、Server単位token、Cookie username照合、status制約、FTS削除同期。go test/vet/race 成功。コミット後にセキュリティ全体レビュー予定。
- 2026-07-10 19:01 [codex] 実用的セキュリティレビューの残件を修正: 無認証外部bind拒否、Secure Cookie opt-in、feed 5MiB上限、GReader POST既定。go test/vet/race 成功。コミット予定。
- 2026-07-10 20:01 [codex] E2E/実機検証で OPML・Readability・画面幅・既読/スターを確認。サイドバー縦並びCSSと SQLite 単一接続（SQLITE_BUSY 解消）を追加。次は HTTPS リバースプロキシ検証。

## 2026-07-23 22:03 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: UI 縮小とサイト URL からのフィード検出

### 実施したこと
- 基準フォントサイズとデスクトップ・タブレットのカラム幅を 80% に縮小。
- フィード検出に RSS 用 User-Agent、HTTP エラー処理、2 MiB の HTML 読取上限を追加。2NN と animanch.com のベース URL から RSS 候補を実機確認。
- `go test ./...` と `git diff --check` が成功。

### 次のタスク候補
- ブラウザで縮小後の密度とフィード追加フローを確認し、必要なら縮小率を調整。

### 連絡・注意事項
- ローカルテストサーバーは `http://127.0.0.1:17070`、認証は `e2e-user` / `e2e-password`。一時 DB を使用。

## 2026-07-23 22:45 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: 一覧右端揃えとショートカット一覧

### 実施したこと
- 入れ子フィード行を含めて行幅を統一し、未読数と `…` の固定右端位置を揃えた。
- 検索欄のプレースホルダーを削除し、Settings に実装済みキーボードショートカット一覧を追加。
- `node --check`、`go test ./...`、差分チェックを成功させ、ローカルサーバーを再起動。

### 次のタスク候補
- 右端揃えとショートカット一覧の表示密度をブラウザで確認。

### 連絡・注意事項
- テーマが反映されない場合は Settings の保存値を確認する。直前の確認では `light` が保存されていた。

## 2026-07-23 22:48 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: hayari ローカル UI 改善の中間記録
- アプリ名: hayari（流行）

### 実施したこと
- ベース URL からのフィード検出、UI 基準サイズ・可変カラム、フィード／フォルダ操作、フォルダ作成、テーマ・フォント・ダイアログ・未読数表示、検索欄、ショートカット一覧を変更。
- フィード検出は 2NN と animanch.com の実機確認済み。各変更後に `node --check`、`go test ./...`、差分チェックを実施。
- ローカル確認サーバーを `http://127.0.0.1:17070` で起動中。

### 次のタスク候補
- ユーザーから続く UI 改善要望を受け、最新スクリーンショットを基に表示・操作を調整。
- Beige は Settings で選択して Save する。保存値が `light` のままなら Firefox 起因ではない。

### 連絡・注意事項
- 作業ツリーには未コミット変更あり（`.handoff/handoff.md`、assets、crawler と crawler_test）。コミット・push は未実施。
- ローカル確認用 DB は一時領域 `/tmp/hayari-local-test.sTPhEq/`。認証は `e2e-user` / `e2e-password`。

## 2026-07-23 22:41 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: 未読数の右端揃えとテーマ反映確認

### 実施したこと
- 未読数と `…` を右端から固定配置し、フォルダ／インデントされたフィードを含めて数値の右端を揃えた。
- 保存済み設定を確認し、テーマが反映されない原因は Firefox ではなく `theme: light` が保存されていたためと特定。
- `node --check`、`go test ./...`、差分チェックを成功させ、ローカルサーバーを再起動。

### 次のタスク候補
- Settings で Beige を選択して Save し、色味が期待どおりか確認。

### 連絡・注意事項
- ローカルテストサーバーは `http://127.0.0.1:17070`、認証は `e2e-user` / `e2e-password`。一時 DB を使用。

## 2026-07-23 22:35 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: 表示密度とベージュテーマ

### 実施したこと
- ダイアログを本文・最大幅ともに 90% 相当に縮小し、未読数をフィード名の後方で右寄せ。
- Font size 設定を記事本文だけでなくアプリ全体の基準フォントサイズへ適用し、Settings に Beige テーマを追加。
- `node --check`、`go test ./...`、差分チェックを成功させ、ローカルサーバーを再起動。

### 次のタスク候補
- Beige の色味、ダイアログ縮小率、Small/Large の体感をブラウザで確認し、必要なら色値・倍率を微調整。

### 連絡・注意事項
- ローカルテストサーバーは `http://127.0.0.1:17070`、認証は `e2e-user` / `e2e-password`。一時 DB を使用。

## 2026-07-23 22:15 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: リロード操作と可変カラム

### 実施したこと
- リロード操作を `try/finally` 化し、失敗時も busy 表示と無効化を必ず解除。
- デスクトップでサイドバー／記事一覧の境界をドラッグ可能にし、幅を localStorage に保持。基準 UI サイズは 90% に変更。
- `node --check`、`go test ./...`、差分チェック、ローカルの refresh API（202）を確認。

### 次のタスク候補
- ブラウザで 90% の密度とドラッグ幅を確認。必要に応じて縮小率・最小幅を再調整。

### 連絡・注意事項
- ローカルテストサーバーは引き続き `http://127.0.0.1:17070` で稼働中。一時 DB を使用。

## 2026-07-23 22:24 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: フィード一覧の個別操作とフォルダ作成

### 実施したこと
- UI 基準サイズを 100% に復帰し、フィード／フォルダ行の `…` 操作を常時表示。従来の編集ダイアログをそのまま開く。
- サイドバー下部に `+ Folder` を追加し、名前入力ダイアログからフォルダを作成可能にした。
- `node --check`、`go test ./...`、差分チェックを成功させ、ローカルサーバーを再起動。

### 次のタスク候補
- 実データで 100% の表示密度と、フィード行操作／フォルダ作成フローを確認。

### 連絡・注意事項
- ローカルテストサーバーは `http://127.0.0.1:17070`、認証は `e2e-user` / `e2e-password`。一時 DB を使用。

## 2026-07-24 16:00 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: UI reviewer P1/P2 対応
- アプリ名: hayari（流行）

### 実施したこと
- 保存済みカラム幅を現在のビューポートへクランプし、フォルダ選択時は子フィードに active を付けないよう状態更新を分離。
- 未読数を固定幅・右寄せのプレーンテキストへ変更。Refresh の誤った `r` ヒントとキーボード操作不能な区切りのフォーカスを削除。
- Light/Beige のツールバーを高コントラスト化。`node --check`、`go test ./...`、差分チェックを成功させ、ローカルサーバーを再起動。

### 次のタスク候補
- 最新 UI をブラウザ確認。Beige は Settings で選択・Save する（直前の保存値は light）。

### 連絡・注意事項
- Settings 内ショートカット一覧の再追加はユーザー意図に反するため見送り。検索欄はプレースホルダーなしで `title="Search (/)"` のみ保持。

## 2026-07-24 19:26 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: FreshRSS互換同期とUI改善
- アプリ名: hayari（流行）

### 実施したこと
- `/api/greader.php` 別名、10進数記事ID／継続トークン、両方の本文ID形式、必須 `updated` を実装し、ReadKit と NetNewsWire のログイン・同期・`edit-tag` 更新を実機確認した。
- `docs/freshrss-api.md` に入口URL、同期形式、確認済みクライアント、未実装APIを記録した。
- UIでは中央ヘッダーのフィード管理、検索アイコン削除、設定アイコン、テーマ整理、キーボード／更新処理を調整した。

### 次のタスク候補
- 常用環境での同期継続と、未読数表示など保留したUI調整を必要になった時点で再検討する。

### 連絡・注意事項
- ローカルテストサーバーは `http://127.0.0.1:17070` で稼働中。ReadKit診断プロキシは停止済み。

## 2026-07-24 19:58 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: Hayari リリース計画と名称移行

### 実施したこと
- `docs/v1-release-plan.md` を更新し、Forgejo の新規リポジトリ `forge.harakara.site/littleisland/hayari`、採用済み v1 要件、対象外、公開・検証・ロールバック手順を保存した。
- Go module path、`cmd/hayari`、ビルド、CI、UI、テスト、文書、既定 DB、User-Agent を Hayari／`hayari` へ統一し、旧表記の残存を除去した。
- `go test ./...`、`go vet ./...`、JavaScript 構文確認、`make build VERSION=v1.0.0-rc.0`、`./hayari --version`、差分チェックを成功させた。

### 次のタスク候補
- 認証情報を安全に渡す起動設定、README 日本語版、LICENSE/CHANGELOG、Forgejo Actions リリース workflow を計画順に実装する。

### 連絡・注意事項
- Forgejo リポジトリ作成、remote 設定、commit、push、タグ、Release は未実施。公開承認後にだけ行う。

## 2026-07-24 20:00 JST

- 実行エージェント: Codex
- モデル: 不明
- 作業トピック: Hayari Forgejo リポジトリ作成

### 実施したこと
- `fja repo create hayari --force-remote` で private の `littleisland/hayari` を Forgejo に作成した。
- `origin` を `https://forge.harakara.site/littleisland/hayari.git` に設定し、空のリモートであることを確認した。

### 次のタスク候補
- 常用環境で確認できる候補コミットを作成して private リポジトリへ push する。公開設定の変更は確認後に別途行う。

### 連絡・注意事項
- 現在の名称移行・リリース準備の変更は未コミットかつ未 push。リポジトリの public 化、タグ、Release は未実施。
