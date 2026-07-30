# 記事タイトル翻訳 要件

## 目的

利用者が明示的に開始した場合だけ、選択中フィードの未読記事タイトルを Henji で英語から日本語へ翻訳する。Hayari の Web UI は翻訳済みタイトルを表示するが、FreshRSS / Google Reader 互換 API の既存クライアントは原文タイトルを受け取る。

## 対象と非対象

- 対象は選択中の単一フィードだけとする。All / Starred / フォルダ選択時には操作を表示しない。
- 対象記事は、未読、非表示ではない、かつ翻訳状態が `pending` または `failed` の記事とする。
- 1回の開始で処理するのは新着順の最大50件とする。開始時に対象を確定する。
- 既読記事、非表示記事、すでに `translated` または `skipped` の記事は対象外とする。
- RSS 取得時の自動翻訳、既存記事の一括バックフィル、翻訳状態の自動再試行は行わない。

## UI

- 記事一覧ヘッダーで、フィード編集を開く `…` ボタンの左に AI ボタンを置く。
- AI ボタンは選択中がフィードで、Henji 実行ファイルを利用できるときだけ表示する。
- 押下後、最大50件の外部翻訳を開始することを示す確認ダイアログを出す。確認するまでジョブを開始しない。
- 確認後はバックグラウンドで処理する。進捗・完了・失敗の通知は表示しない。
- 翻訳が完了した記事は、次回の一覧または詳細ペインの再読み込み時から日本語タイトルを表示する。翻訳失敗・未翻訳・スキップ記事は原文を表示する。
- Web UI は初期化時に capability API を1回呼ぶ。Henji 実行ファイルが起動後に消えた場合、開始 API はジョブを作らず成功扱いで返し、UI は通知を出さない。

## データ

原文は常に保持し、翻訳文・状態を分離して保存する。

- `items.title`: フィードから受け取った原文。変更しない。
- `items.translated_title`: 翻訳成功時の日本語タイトル。未翻訳時は `NULL`。
- `items.title_translation_state`: `pending`、`processing`、`translated`、`skipped`、`failed` のいずれか。既存記事と新着記事の既定値は `pending`。
- `items.title_translation_claim`: `processing` にした翻訳ジョブを識別する値。処理中以外は `NULL`。

`failed` は次に利用者が AI ボタンを押したときにだけ再試行できる。`skipped` は再試行対象にしない。

`translated` のときだけ `translated_title` は非 `NULL` とし、他の状態では `NULL` とする。Web UI は状態が `translated` の場合だけ `translated_title` を表示する。

AI ボタン開始時は、対象記事の選択と `processing` / claim への更新を同一トランザクションで行う。すでに `processing` の記事は他のジョブの対象にできない。同一フィードに `processing` の記事が1件でもある場合、開始 API はジョブを追加しない。全体で待機・実行できるジョブは4件までとし、上限時も開始 API はジョブを追加しない。完了時は自身の claim が残る記事だけを `translated`、`skipped`、または `failed` へ更新する。サーバー起動時には中断された `processing` を `pending` へ戻すが、自動で翻訳を再開しない。

Henji を起動する直前にも、claim が自身のものかつ記事が未読・非表示でないことを確認する。既読または非表示へ変わった記事は claim を外して `pending` へ戻し、Henji へ送らない。削除済み記事には何も書き込まない。Henji 起動後に既読または非表示になった場合は、実行済みの結果を保存してよい。

## 言語判定と翻訳

- 機能の対象言語は英語から日本語だけとする。
- 日本語・中国語・韓国語など、明確に非ラテン文字のタイトルはローカル判定だけで `skipped` にし、Henji を呼ばない。
- 空白だけ、数字・記号だけ、またはラテン文字を一文字も含まないタイトルはローカルで `skipped` にする。漢字・ひらがな・カタカナ・ハングル・キリル・ギリシャ・アラビア・ヘブライ・タイ文字と句読点・数字だけの組み合わせもこれに含む。ラテン文字を一文字でも含む混在表記は曖昧として Henji へ渡す。
- Henji へ渡す曖昧なタイトルには構造化出力を要求する。Henji が英語以外と判定した場合は `skipped` とする。
- 英語と判定された場合だけ、翻訳済み日本語タイトルを保存して `translated` とする。
- Henji 実行、構造化出力の検証、タイムアウト、出力上限のいずれかに失敗した場合は `failed` とする。原文・既存の翻訳文は変更せず、利用者にエラーを見せない。

## 実行モデルと Henji 連携

- RSS 取得 worker とは独立したバックグラウンド job とする。翻訳待ちでフィード更新を遅らせない。
- Henji はタイトル1件ずつ同期実行する。各タイトルの成否を独立して保存できるようにする。
- 全ジョブ合計で Henji の同時実行数は最大2件とする。
- Hayari は shell を介さず `exec.CommandContext` で Henji を起動し、タイトルは標準入力で渡す。APIキー、provider URL、秘密情報は保存・表示・ログ出力しない。
- Shirushi と同じ起動設定を提供する: `--henji-path`、`--henji-api`、`--henji-model`。Henji が見つからない場合は AI ボタンを表示しない。`--henji-api` と `--henji-model` は必ず対で指定し、片方だけの場合は起動エラーとする。
- Henji は `-q -a API -m MODEL --no-cache --max-tokens 512 --json-schema SCHEMA --json-schema-retries 0 PROMPT` として起動する。既定値は Shirushi と同じ `henji`、`openrouter`、`google/gemini-2.5-flash-lite` とする。
- 1件の実行期限は30秒、タイトル入力はUTF-8で4 KiB、stdoutは4 KiBまでとする。入力が4 KiBを超えるタイトルは切り詰めず、Henji を呼ばずに `failed` とする。stderrは `io.Discard` へ流し、stdout・stderr・プロンプト・provider 応答をログや HTTP レスポンスに含めない。
- 出力は単一のUTF-8 JSON objectだけを許可する。許可フィールドは `result` と `title` だけとし、`result` は `translated` または `skipped`、`translated` の `title` は空でない500 Unicode文字以下の文字列、`skipped` の `title` は存在しないことを要求する。日本語としての内容判定は Henji の構造化出力契約に委ねる。JSON Schema と Hayari 側の厳密な JSON 検証の両方で検証する。
- サーバー停止時は新規ジョブの受付を止め、実行中 Henji の context を cancel して完了を待つ。停止期限内に完了しない claim は `pending` へ戻してから DB を閉じる。

## API と互換性

- Hayari Web UI 用の item API は、表示用に翻訳タイトルと状態を取得できるようにする。
- Web UI は `translated_title` がある場合だけそれを表示し、なければ `title` を表示する。一覧と詳細ペインで同じ規則を使う。
- FreshRSS / Google Reader 互換 API は、既存どおり原文 `title` を返す。外部クライアントから翻訳の開始・設定変更はできない。
- Web UI の記事検索は、原文 `title` と `translated_title` の両方を検索対象にする。GReader / FreshRSS 互換 API の検索互換性は変更しない。
- `GET /api/capabilities` は `{ "title_translation": boolean }` を返す。boolean は指定された Henji 実行ファイルを PATH または `--henji-path` から見つけられる場合だけ `true` とし、provider 認証や到達性は検査しない。
- `POST /api/feeds/:id/title-translations` は、ジョブを開始した場合に `202 Accepted` と `{ "accepted": N }`（`1 <= N <= 50`）を返す。Henji 利用不可、対象なし、同一フィードの実行中、または全体キュー上限では `204 No Content` を返す。存在しないフィードは `404 Not Found` とする。

## 受入条件

- 同時開始では同じ記事を二重に Henji へ送らず、全体の Henji プロセス数は2を超えない。
- timeout、非0終了、不正JSON、stdout上限超過、4 KiB超の入力は `failed` として原文を維持する。
- 翻訳状態と翻訳文の不変条件を、DB制約またはすべての更新経路のテストで保証する。
- server shutdown は実行中プロセスを cancel し、DB close 前に claim を解放する。
- Web UI は翻訳済みタイトルを一覧・詳細・検索で利用し、FreshRSS / Google Reader API は原文タイトルを維持する。

## 将来拡張

- 多言語翻訳を追加する場合は、言語判定と Henji 指示を拡張する。原文・翻訳文・状態を分離する保存形式は維持する。
- AI 要約は Henji の安全な実行基盤を再利用できるが、要約結果と状態は翻訳とは別のデータとして設計する。
