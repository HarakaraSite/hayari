# 記事タイトル翻訳 実装計画

要件は [title-translation-requirements.md](title-translation-requirements.md) を正とする。本計画は `feature/title-translations` ブランチで実施する。

## 方針

- RSS 更新 worker とは独立した、最大4ジョブ・Henji 最大2並列の翻訳 job manager を追加する。
- Henji は任意の外部実行時依存とする。通常テストは Henji、本物の provider、ネットワーク、API key を必要としない。
- 原文は常に維持し、Web UI のみ翻訳文を表示する。FreshRSS / Google Reader 互換 API は原文を維持する。

## 実装順序

1. ストレージと検索
   - `items` に翻訳文、状態、claim を追加する migration を実装する。
   - 状態と翻訳文の不変条件を DB trigger とストレージテストで保証する。
   - FTS と短語検索を原文・翻訳文の両方へ拡張する。
   - 最大50件の原子的 claim、条件付き確定／release、起動時回復を追加する。

2. Henji adapter
   - `exec.CommandContext`、30秒 timeout、stdin 入力、stderr破棄、stdout 4KiB上限、厳密JSON検証を実装する。
   - `--henji-path`、`--henji-api`、`--henji-model` と API/model 対指定を追加する。
   - ローカルの非ラテン文字判定と、Henji の translated/skipped 応答を実装する。

3. job manager と server API
   - 最大4ジョブ、最大2 Henji 実行、同一フィード排他、shutdown cancel / claim release を実装する。
   - capability API と翻訳開始 API を追加し、202 / 204 / 404 契約をテストする。
   - GReader / FreshRSS の原文互換を回帰テストする。

4. Web UI
   - capability に応じた AI ボタン、確認ダイアログ、開始 API 呼び出しを追加する。
   - 一覧・詳細の表示規則と翻訳文検索を実装する。
   - Playwright で偽 Henji を用いた画面確認を行う。

## テスト方針

- unit / storage / server / manager テストは mock adapter と一時 SQLite DB を使う。
- 外部プロセス境界は Go の helper process を偽 Henji として起動し、argv、stdin、timeout、cancel、不正出力、巨大 stdout を検証する。
- 実 Henji のライブテストは、承認済みの配布元・固定バージョン・checksum を確認して導入した後、専用の opt-in 環境変数と低権限 key を持つ場合だけ実行する。

## 完了条件

- `go test ./...`、`go test -race ./...`、`go vet ./...`、`git diff --check` が成功する。
- 通常テストは外部 Henji・ネットワーク・API key を必要としない。
- claim、並列上限、shutdown、検索、Web UI 翻訳表示、GReader / FreshRSS 原文維持を自動テストする。
